"""用户评分系统：1~5 分（5=神作，1=鬼作），支持登录/匿名，多口径排行榜。"""

import hashlib
import time
from collections import defaultdict
from datetime import datetime, timedelta

from fastapi import APIRouter, Depends, HTTPException, Query, Request
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from ..client_ip import get_client_ip
from ..config import settings
from ..database import get_db
from ..domains.identity.deps import optional_user
from ..models import Demo, DemoRating, User
from ..schemas import Paginated, RatingIn, RatingOut
from ..serializers import serialize_demo
from ..services import rating_service, ratelimit

router = APIRouter(tags=["ratings"])

# 匿名评分限流：每 IP 每 demo 10 次/小时 + 每 IP 全局 60 次/小时（实现见 services.ratelimit）
DEMO_RATE = 10
GLOBAL_RATE = 60

SCORE_LABELS = {1: "鬼作", 2: "差", 3: "一般", 4: "佳作", 5: "神作"}


def _client_ip(request: Request) -> str:
    return get_client_ip(request) or "unknown"


def _rating_salt() -> str:
    return settings.rating_salt or hashlib.sha256(settings.jwt_secret.encode()).hexdigest()


def _rater_key(user: User | None, device_id: str, ip: str) -> str:
    if user is not None:
        return f"user:{user.id}"
    if not device_id or len(device_id) < 8:
        raise HTTPException(status_code=422, detail="匿名评分需要有效的 device_id（≥8 位）", )
    raw = f"{device_id}|{ip}|{_rating_salt()}"
    return "anon:" + hashlib.sha256(raw.encode()).hexdigest()


def _anon_rate_limit(request: Request, slug: str) -> None:
    # KB-21：统一走 services.ratelimit（全局 + 单作品两个维度）
    ip = _client_ip(request)
    ratelimit.hit(f"rate:{ip}", GLOBAL_RATE, 3600)
    ratelimit.hit(f"rate:{ip}:{slug}", DEMO_RATE, 3600)


def _find_approved_demo(db: Session, slug: str) -> Demo:
    demo = db.query(Demo).filter(Demo.slug == slug, Demo.status == "approved").first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在或未上线", )
    return demo


@router.post("/demos/{slug}/rating", response_model=RatingOut)
def rate_demo(
    slug: str,
    body: RatingIn,
    request: Request,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    demo = _find_approved_demo(db, slug)
    ip = _client_ip(request)
    if user is None:
        _anon_rate_limit(request, slug)
    rater_key = _rater_key(user, body.device_id, ip)

    row = db.query(DemoRating).filter(DemoRating.demo_id == demo.id, DemoRating.rater_key == rater_key).first()
    old_score = row.score if row else None
    if row is None:
        row = DemoRating(demo_id=demo.id, user_id=user.id if user else None, rater_key=rater_key, score=body.score)
        db.add(row)
        try:
            db.flush()   # 让新行进入事务，聚合才能包含它
        except IntegrityError:
            # KB-18：并发首评撞 uq_demo_rater → 回滚后按「已有行更新」重走，不再 500
            db.rollback()
            demo = _find_approved_demo(db, slug)
            row = (
                db.query(DemoRating)
                .filter(DemoRating.demo_id == demo.id, DemoRating.rater_key == rater_key)
                .first()
            )
            if row is None:
                raise HTTPException(status_code=409, detail="评分冲突，请重试", )
            old_score = row.score
            row.score = body.score
            row.updated_at = datetime.utcnow()
            db.flush()
    else:
        row.score = body.score
        row.updated_at = datetime.utcnow()
        db.flush()   # 更新也 flush，确保聚合读到新分数
    rating_service.apply_rating_delta(db, demo, old_score, body.score)
    db.commit()
    return rating_service.rating_out(db, demo, rater_key)


@router.delete("/demos/{slug}/rating", response_model=RatingOut)
def unrate_demo(
    slug: str,
    request: Request,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    demo = _find_approved_demo(db, slug)
    ip = _client_ip(request)
    device_id = request.query_params.get("device_id", "")
    rater_key = _rater_key(user, device_id, ip)
    row = db.query(DemoRating).filter(DemoRating.demo_id == demo.id, DemoRating.rater_key == rater_key).first()
    if row is not None:
        old_score = row.score
        db.delete(row)
        db.flush()   # 让删除立即生效，聚合才能剔除
        rating_service.apply_rating_delta(db, demo, old_score, None)
        db.commit()
    return rating_service.rating_out(db, demo, rater_key)


@router.get("/demos/{slug}/rating", response_model=RatingOut)
def get_rating(
    slug: str,
    request: Request,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    demo = _find_approved_demo(db, slug)
    ip = _client_ip(request)
    device_id = request.query_params.get("device_id", "")
    rater_key = _rater_key(user, device_id, ip) if (user is not None or device_id) else None
    return rating_service.rating_out(db, demo, rater_key)


@router.get("/leaderboard", response_model=Paginated)
def leaderboard(
    sort: str = Query(default="avg", pattern="^(avg|god|ghost|net|count|heat)$"),
    range: str = Query(default="all", pattern="^(all|week|month)$"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
):
    """排行榜：只展示 approved demo。质量榜（avg/god/ghost/net）排除 0 评。range=all|week|month。"""
    query = db.query(Demo).filter(Demo.status == "approved")
    if range == "week":
        query = query.filter(Demo.created_at >= datetime.utcnow() - timedelta(days=7))
    elif range == "month":
        query = query.filter(Demo.created_at >= datetime.utcnow() - timedelta(days=30))
    if sort in ("avg", "god", "ghost", "net"):
        query = query.filter(Demo.rating_count > 0)

    if sort == "avg":
        query = query.order_by(Demo.rating_avg.desc(), Demo.rating_count.desc(), Demo.rating_god.desc(), Demo.id.desc())
    elif sort == "god":
        query = query.order_by(Demo.rating_god.desc(), Demo.rating_avg.desc(), Demo.id.desc())
    elif sort == "ghost":
        query = query.order_by(Demo.rating_ghost.desc(), Demo.rating_avg.asc(), Demo.id.desc())
    elif sort == "net":
        query = query.order_by((Demo.rating_god - Demo.rating_ghost).desc(), Demo.rating_count.desc(), Demo.id.desc())
    elif sort == "count":
        query = query.order_by(Demo.rating_count.desc(), Demo.rating_avg.desc(), Demo.id.desc())
    else:  # heat
        query = query.order_by((Demo.view_count + 2 * Demo.download_count + Demo.rating_count).desc(), Demo.id.desc())

    total = query.count()
    items = query.offset((page - 1) * page_size).limit(page_size).all()
    return Paginated(
        items=[serialize_demo(db, d) for d in items],
        total=total,
        page=page,
        page_size=page_size,
    )
