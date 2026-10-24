"""站点公开信息聚合：一次请求返回内容 / 社区 / 流量概况。

- 只聚合公开安全数字（不含待审队列、存储路径等管理面信息，后者见 /admin/stats）
- 60s 内存缓存；响应带 Cache-Control: public, max-age=60，Cloudflare 可直接挡流量
- admin ?refresh=1 可强制刷新（发布新内容后立即生效）
"""

import threading
import time
from datetime import datetime, timedelta, timezone

from sqlalchemy import func

from ..database import SessionLocal
from ..models import Demo, DemoTag, ForumReply, ForumTopic, Tag, TagKey, User
from ..services import visits
from .scope import ASTRA, DEEP
from .settings_service import get_fun_mode

_TTL = 60  # 秒
_lock = threading.Lock()
# 按视区分键缓存（deep/astra 数据面不同，共用会把主站规模数字漏进橱窗）
_cache: dict[str, dict] = {}
# 正在回源重建的视区：TTL 过期时只放一个线程去查库，其余线程吃旧值
_refreshing: set[str] = set()


def _utc_iso(dt: datetime) -> str:
    """naive UTC 补 Z，避免前端按本地时间解析提前 8 小时。"""
    if dt.tzinfo is None:
        return dt.isoformat() + "Z"
    return dt.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def _top_tag_values(db, key: str, limit: int = 5, scope: str = DEEP) -> list[dict]:
    """某键下按已上架 demo 数排序的热门值（限定当前视区）。"""
    rows = (
        db.query(Tag.value, func.count(DemoTag.demo_id))
        .join(DemoTag, DemoTag.tag_id == Tag.id)
        .join(Demo, Demo.id == DemoTag.demo_id)
        .filter(Tag.key == key, Demo.status == "approved", Demo.sites.contains(scope))
        .group_by(Tag.value)
        .order_by(func.count(DemoTag.demo_id).desc())
        .limit(limit)
        .all()
    )
    return [{"value": value, "demos": count} for value, count in rows]


def _build(scope: str = DEEP) -> dict:
    """聚合当前视区的内容数字。astra 橱窗额外做叙事收敛：
    社区/论坛/流量归零（橱窗无这些面），fun_mode 恒真、站点名换成 astra 口径。"""
    db = SessionLocal()
    try:
        week_ago = datetime.utcnow() - timedelta(days=7)
        scope_f = Demo.sites.contains(scope)

        demos_total = db.query(func.count(Demo.id)).filter(Demo.status == "approved", scope_f).scalar() or 0
        demos_by_type = {
            t: c
            for t, c in db.query(Demo.demo_type, func.count(Demo.id))
            .filter(Demo.status == "approved", scope_f)
            .group_by(Demo.demo_type)
            .all()
        }
        uploads_7d = (
            db.query(func.count(Demo.id))
            .filter(Demo.status == "approved", Demo.created_at >= week_ago, scope_f)
            .scalar()
            or 0
        )
        if scope == ASTRA:
            # 橱窗叙事：策展作品全部署名为实验室，社区面归零
            registered_authors = 0
            has_anon = False
            forum_topics = 0
            users_total = 0
            active_users = 0
            pv = {"today": 0, "yesterday": 0, "total": 0}
            online_now = 0
            fun_mode = True
        else:
            # 创作者 = 有已上架作品的注册用户数；匿名（public）算 1 个
            registered_authors = (
                db.query(func.count(func.distinct(Demo.author_id)))
                .filter(Demo.status == "approved", Demo.author_id.isnot(None), scope_f)
                .scalar()
                or 0
            )
            has_anon = (
                db.query(Demo.id)
                .filter(Demo.status == "approved", Demo.author_id.is_(None), scope_f)
                .first()
                is not None
            )

            forum_topics = (
                db.query(func.count(ForumTopic.id)).filter(ForumTopic.status == "normal").scalar() or 0
            )
            users_total = db.query(func.count(User.id)).filter(User.status != "deleted").scalar() or 0

            # 周活：7 天内发过 demo / 发过主题 / 回复过的去重用户
            active: set[int] = set()
            active.update(
                a
                for (a,) in db.query(Demo.author_id)
                .filter(Demo.author_id.isnot(None), Demo.created_at >= week_ago)
                .all()
            )
            active.update(
                a
                for (a,) in db.query(ForumTopic.author_id)
                .filter(ForumTopic.author_id.isnot(None), ForumTopic.created_at >= week_ago)
                .all()
            )
            active.update(
                a
                for (a,) in db.query(ForumReply.author_id)
                .filter(ForumReply.author_id.isnot(None), ForumReply.created_at >= week_ago)
                .all()
            )
            active_users = len(active)

            fun_mode = get_fun_mode(db)

            # 传 db 复用本函数已持有的会话（否则一个 site-info 请求会嵌套借 3 个连接）
            pv = visits.get_stats(db)
            online_now = visits.get_live_stats(db)["online"]

        # tags 只统计键定义内的（排除 author:/version-of 等内部保留标签）
        tag_keys_total = db.query(func.count(TagKey.key)).scalar() or 0
        tag_values_total = db.query(func.count(Tag.id)).join(TagKey, Tag.key == TagKey.key).scalar() or 0

        latest = (
            db.query(Demo)
            .filter(Demo.status == "approved", scope_f)
            .order_by(Demo.created_at.desc(), Demo.id.desc())
            .first()
        )
        latest_demo = (
            {"slug": latest.slug, "title": latest.title, "created_at": _utc_iso(latest.created_at)}
            if latest
            else None
        )

        top_models = _top_tag_values(db, "model", scope=scope)
        top_games = _top_tag_values(db, "game", scope=scope)
    finally:
        db.close()

    return {
        "site": (
            {"name": "astra canary collection", "description": "Selected works from a private grey test", "info_version": 1}
            if scope == ASTRA
            else {"name": "AI 全民制作人", "description": "AI 网页 Demo 作品集", "info_version": 1}
        ),
        # 显示层开关（整活模式等）；只影响前端展示文案，不改任何数据（astra 视区恒真）
        "display": {"fun_mode": fun_mode},
        "content": {
            "demos_total": demos_total,
            "demos_by_type": demos_by_type,
            "authors_total": registered_authors + (1 if has_anon else 0),
            "uploads_last_7d": uploads_7d,
            "tags": {"keys": tag_keys_total, "values": tag_values_total},
            "forum_topics": forum_topics,
        },
        "community": {"users_total": users_total, "users_active_week": active_users},
        "traffic": {
            "pv_today": pv["today"],
            "pv_yesterday": pv["yesterday"],
            "pv_total": pv["total"],
            "online_now": online_now,
        },
        "hot": {
            "top_models": top_models,
            "top_games": top_games,
            "latest_demo": latest_demo,
        },
        "capabilities": {
            "upload": (
                # 橱窗只读：不广播上传通道（agent 指南也不在 astra 白名单内）
                {"anonymous": False, "guide": "", "tag_keys": "", "idempotency": False}
                if scope == ASTRA
                else {
                    "anonymous": True,
                    "guide": "/api/v1/meta/agent-guide",
                    "tag_keys": "/api/v1/tags/tag-keys",
                    "idempotency": True,
                }
            ),
            "features": {
                "forum": scope != ASTRA,
                "ratings": scope != ASTRA,
                "session_logs": scope != ASTRA,
                "preview": "versioned-url",
            },
        },
        "generated_at": _utc_iso(datetime.utcnow()),
    }


def get_site_info(force: bool = False, scope: str = DEEP) -> dict:
    """带 60s TTL 的站点概况，按视区分键（force=True 跳过缓存重建，仅 admin）。

    TTL 过期时只有一个线程回源，其余线程直接吃旧值（stale-while-revalidate）：
    首屏必打的聚合接口一次要跑十几条查询，若 TTL 一到就 N 个并发一起回源，
    会瞬时把 QueuePool 吃满（2026-09 全站挂死事故的放大点之一）。
    """
    now = time.monotonic()
    owns_refresh = False
    stale: dict | None = None
    with _lock:
        hit = _cache.get(scope)
        if hit is not None and not force:
            if now - hit["ts"] < _TTL:
                return hit["data"]                      # 新鲜
            if scope in _refreshing:
                return hit["data"]                      # 过期但已有人在刷 → 吃旧值
            _refreshing.add(scope)                      # 本线程负责刷
            owns_refresh = True
            stale = hit["data"]
    try:
        data = _build(scope)
    except Exception:
        if stale is not None:
            return stale                                # 回源失败继续吃旧值，下一批再试
        raise
    finally:
        if owns_refresh:
            with _lock:
                _refreshing.discard(scope)
    with _lock:
        _cache[scope] = {"ts": time.monotonic(), "data": data}
    return data
