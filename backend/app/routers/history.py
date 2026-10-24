"""浏览历史（服务端侧）。

隐私口径（与 services/visits.py 的「不收集访客 IP」一致）：
**只存 user_id / demo_id / viewed_at** —— 不存 IP、不存 UA、不存来源页、不存停留时长。

规则：
  · 匿名不写服务端（前端只记本机 localStorage）；
  · `history_enabled=False` 时不写新记录（已有记录由用户显式「清空」删除）；
  · 同一作品只保留最近一次（unique + 刷新 viewed_at）；
  · 每用户上限 200 条，超出删最旧。
"""

from __future__ import annotations

from fastapi import APIRouter, Depends, Query
from fastapi import HTTPException
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..database import get_db
from ..domains.identity.deps import current_user
from ..models import Demo, DemoView, User, utcnow
from ..schemas import HistoryPage
from ..serializers import preload_demo_relations, serialize_demo

router = APIRouter(tags=["history"])

MAX_ROWS = 200


@router.post("/me/history/{slug}", status_code=204)
def record_view(slug: str, db: Session = Depends(get_db), user: User = Depends(current_user)):
    if not user.history_enabled:
        # 开关关闭：静默成功（前端不需要区分"关了"与"记下了"，避免打扰）
        return None
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="作品不存在")
    row = (
        db.query(DemoView)
        .filter(DemoView.user_id == user.id, DemoView.demo_id == demo.id)
        .first()
    )
    if row is None:
        db.add(DemoView(user_id=user.id, demo_id=demo.id, viewed_at=utcnow()))
    else:
        row.viewed_at = utcnow()
    db.commit()
    _trim(db, user.id)
    return None


@router.get("/me/history", response_model=HistoryPage)
def list_history(
    page: int = Query(1, ge=1),
    page_size: int = Query(20, ge=1, le=100),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    total = db.query(func.count(DemoView.id)).filter(DemoView.user_id == user.id).scalar() or 0
    rows = (
        db.query(DemoView, Demo)
        .join(Demo, Demo.id == DemoView.demo_id)
        .filter(DemoView.user_id == user.id)
        .order_by(DemoView.viewed_at.desc())
        .offset((page - 1) * page_size)
        .limit(page_size)
        .all()
    )
    demos = [d for _v, d in rows]
    preload_demo_relations(db, demos)
    items = [
        {"demo": serialize_demo(db, d, current_user_id=user.id), "viewed_at": v.viewed_at}
        for v, d in rows
    ]
    return HistoryPage(items=items, total=int(total), page=page, page_size=page_size)


@router.delete("/me/history", status_code=204)
def clear_history(db: Session = Depends(get_db), user: User = Depends(current_user)):
    db.query(DemoView).filter(DemoView.user_id == user.id).delete()
    db.commit()
    return None


@router.delete("/me/history/{slug}", status_code=204)
def delete_history_item(slug: str, db: Session = Depends(get_db), user: User = Depends(current_user)):
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is not None:
        db.query(DemoView).filter(DemoView.user_id == user.id, DemoView.demo_id == demo.id).delete()
        db.commit()
    return None


def _trim(db: Session, user_id: int) -> None:
    """每用户最多 MAX_ROWS 条：超出删最旧（历史是"最近看过什么"，不是永久档案）。"""
    extra = (
        db.query(DemoView.id)
        .filter(DemoView.user_id == user_id)
        .order_by(DemoView.viewed_at.desc())
        .offset(MAX_ROWS)
        .all()
    )
    if not extra:
        return
    ids = [r[0] for r in extra]
    db.query(DemoView).filter(DemoView.id.in_(ids)).delete(synchronize_session=False)
    db.commit()
