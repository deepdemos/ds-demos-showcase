import os
import time
from pathlib import Path

from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..config import settings
from ..database import get_db
from ..domains.identity.deps import require_admin
from ..models import Announcement, Demo, User
from ..schemas import (
    AdminDemoOut,
    AdminStatsOut,
    AdminUserOut,
    DemoCounts,
    DemoCurationIn,
    ReviewAction,
    SettingsIn,
    SettingsOut,
    StorageStatusOut,
    UserOut,
)
from ..serializers import serialize_demo
from ..services import audit_service
from ..services import notification_service
from ..services import oss, settings_service
from ..services.scope import invalidate_visibility

router = APIRouter(prefix="/admin", tags=["admin"])


@router.get("/review", response_model=list[AdminDemoOut])
def review_list(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    demos = db.query(Demo).filter(Demo.status == "pending").order_by(Demo.created_at.desc()).all()
    return [serialize_demo(db, d, detail=True) for d in demos]


@router.post("/review/{slug}")
def review_demo(slug: str, body: ReviewAction, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """审核作品：approve/reject。

    KB-14：审核是治理动作，同事务落审计；KB-10：通过时才补发公开公告（待审期不发，
    否则 slug 会随公告泄露）；改状态后立刻失效可见域缓存（astra 域已驳回作品不再能预览）。
    """
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    new_status = "approved" if body.action == "approve" else "rejected"
    if demo.status == new_status:
        raise HTTPException(status_code=409, detail=f"该作品已是 {new_status} 状态", )
    before = {"slug": demo.slug, "status": demo.status, "title": demo.title}
    demo.status = new_status
    if new_status == "approved":
        # 待审期没发公告（KB-10），通过时补一条，与直接上传的公告口径一致
        db.add(Announcement(
            type="auto",
            title="新 Demo 发布",
            content=demo.title,
            demo_slug=demo.slug,
            status="published",
            category="demo",
            created_by=admin.id,
        ))
    audit_service.record(
        db,
        action="review",
        entity_type="demo",
        entity_id=demo.id,
        actor_id=admin.id,
        before=before,
        after={"slug": demo.slug, "status": new_status},
        reason=f"审核{'通过' if body.action == 'approve' else '驳回'}：{demo.title}",
    )
    db.commit()
    # 预览/下载的可见域缓存立即失效（TTL 300s，不等它过期）
    invalidate_visibility(demo.slug)
    # 通知作者审核结果
    if demo.author_id:
        notification_service.create(
            user_id=demo.author_id,
            type="review_result",
            actor_id=admin.id,
            demo_slug=slug,
        )
    return {"status": demo.status}


@router.get("/demos", response_model=list[AdminDemoOut])
def admin_demos(
    sites: str | None = Query(default=None, description="按可见域过滤（deep/astra）"),
    page: int = Query(default=1, ge=1),
    page_size: int | None = Query(
        default=None, ge=1, le=500, description="缺省=全量（前端本地分页）；给了就服务端分页"
    ),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """作品全量/分页列表。

    KB-20：改为**轻量序列化**（detail=False）——旧实现对每条作品都查一次时间线、
    再做两次磁盘遍历（`files_dir.stat()` + `demo_storage_size()` 的 rglob），
    637 件规模就是 600+ 次查询 + 1200 次磁盘走查。storage_size/inconsistency/timeline
    没有任何前端消费方，去掉不影响界面。
    """
    query = db.query(Demo)
    if sites:
        query = query.filter(Demo.sites.contains(sites))
    query = query.order_by(Demo.created_at.desc(), Demo.id.desc())
    if page_size:
        query = query.offset((page - 1) * page_size).limit(page_size)
    demos = query.all()
    return [serialize_demo(db, d) for d in demos]


@router.put("/demos/{slug}/curation")
def set_demo_curation(
    slug: str, body: DemoCurationIn, db: Session = Depends(get_db), admin: User = Depends(require_admin)
):
    """astra 橱窗策展：发放站点通行证（sites）+ 语言标记（lang）。None 字段保持不变。"""
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    before = {"slug": demo.slug, "sites": demo.sites, "lang": demo.lang}
    if body.sites is not None:
        picked = set(body.sites)
        if not picked or not picked.issubset({"deep", "astra"}):
            raise HTTPException(status_code=422, detail="sites 需为 deep/astra 的非空子集", )
        # 规范化存储顺序：deep 在前（与存量默认值一致，前缀匹配 LIKE '%deep%' 不受顺序影响）
        demo.sites = ",".join(s for s in ("deep", "astra") if s in picked)
    if body.lang is not None:
        demo.lang = body.lang
    # KB-14：策展（可见域/语言）是治理动作，同事务留痕
    if before != {"slug": demo.slug, "sites": demo.sites, "lang": demo.lang}:
        audit_service.record(
            db,
            action="update",
            entity_type="demo",
            entity_id=demo.id,
            actor_id=admin.id,
            before=before,
            after={"slug": demo.slug, "sites": demo.sites, "lang": demo.lang},
            reason=f"策展：sites={demo.sites} lang={demo.lang}",
        )
    db.commit()
    # 预览门禁缓存立即失效（不必等 60s TTL）
    from ..services.scope import invalidate_visibility

    invalidate_visibility(demo.slug)
    return {"slug": demo.slug, "sites": demo.sites, "lang": demo.lang}


@router.get("/users", response_model=list[AdminUserOut])
def admin_users(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """用户列表（含作品数）。KB-19：作品数走一次 GROUP BY 聚合，不再逐用户懒加载 demos。"""
    users = db.query(User).order_by(User.id).all()
    counts = dict(
        db.query(Demo.author_id, func.count(Demo.id))
        .filter(Demo.author_id.isnot(None))
        .group_by(Demo.author_id)
        .all()
    )
    return [
        AdminUserOut(
            id=u.id,
            username=u.username,
            role=u.role,
            status=u.status,
            bio=u.bio,
            created_at=u.created_at,
            demo_count=int(counts.get(u.id, 0)),
        )
        for u in users
    ]


@router.get("/settings", response_model=SettingsOut)
def get_settings(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    return SettingsOut(
        auto_approve=settings_service.get_auto_approve(db),
        auto_approve_public=settings_service.get_auto_approve_public(db),
        fun_mode=settings_service.get_fun_mode(db),
    )


@router.put("/settings", response_model=SettingsOut)
def update_settings(body: SettingsIn, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """更新站点设置：缺省字段不改；三项同事务写 + 一条审计（KB-22/KB-17）。"""
    before = settings_service.snapshot(db)
    changes = {
        key: value
        for key, value in (
            (settings_service.KEY_AUTO_APPROVE, body.auto_approve),
            (settings_service.KEY_AUTO_APPROVE_PUBLIC, body.auto_approve_public),
            (settings_service.KEY_FUN_MODE, body.fun_mode),
        )
        if value is not None
    }
    if not changes:
        raise HTTPException(status_code=422, detail="至少提供一个要修改的设置项")
    after = settings_service.apply(db, changes)
    audit_service.record(
        db,
        action="update",
        entity_type="setting",
        entity_id=0,
        actor_id=admin.id,
        before=before,
        after=after,
        reason="更新站点设置：" + ", ".join(f"{k}={v}" for k, v in sorted(changes.items())),
    )
    db.commit()
    return SettingsOut(
        auto_approve=after[settings_service.KEY_AUTO_APPROVE],
        auto_approve_public=after[settings_service.KEY_AUTO_APPROVE_PUBLIC],
        fun_mode=after[settings_service.KEY_FUN_MODE],
    )


@router.post("/oss-sync")
def oss_sync(force: bool = Query(False), _: User = Depends(require_admin)):
    """启动后台 OSS 同步（不阻塞请求）；已有任务在跑则返回 started=false。"""
    from ..services.oss_sync import get_sync_status, start_sync

    started = start_sync(force=force)
    return {"started": started, "job": get_sync_status()}


@router.get("/oss-sync-status")
def oss_sync_status(_: User = Depends(require_admin)):
    """查询后台 OSS 同步进度/结果。"""
    from ..services.oss_sync import get_sync_status

    return get_sync_status()


_STORAGE_STATUS_TTL = 60.0
_storage_status_cache: dict = {"ts": 0.0, "value": None}


def _storage_status() -> dict:
    """本地存储规模 + 当前生效模式（oss / local），供 storage-status 与 admin/stats 复用。

    KB-20：结果带 60s 缓存 —— 旧实现每次调用都 `os.walk` 整棵 demos 树并逐个 stat，
    管理台刷新一次就把磁盘扫一遍。
    """
    now = time.time()
    cached = _storage_status_cache["value"]
    if cached is not None and now - _storage_status_cache["ts"] < _STORAGE_STATUS_TTL:
        return dict(cached)
    demos_root = settings.demos_path
    demo_dirs = 0
    files = 0
    size = 0
    if demos_root.exists():
        demo_dirs = sum(1 for d in demos_root.iterdir() if d.is_dir())
        for root, _, fs in os.walk(demos_root):
            files += len(fs)
            for name in fs:
                try:
                    size += (Path(root) / name).stat().st_size
                except OSError:
                    pass
    value = {
        "oss_enabled": oss.enabled(),
        # 模式：oss=OSS 直连（serve_local=false）；oss_backup=本地存储+OSS 备份（serve_local=true）；local=纯本地
        "mode": "oss" if (oss.enabled() and not settings.oss_serve_local) else ("oss_backup" if oss.enabled() else "local"),
        "local_demos": demo_dirs,
        "local_files": files,
        "local_size_bytes": size,
    }
    _storage_status_cache["ts"] = now
    _storage_status_cache["value"] = value
    return dict(value)


@router.get("/storage-status")
def storage_status(_: User = Depends(require_admin)):
    """本地存储规模 + 当前生效模式（oss / local）。"""
    return _storage_status()


@router.get("/stats", response_model=AdminStatsOut)
def admin_stats(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """管理后台顶部概览统计：作品状态计数 / 用户数 / 存储模式（轻量，不序列化完整列表）。"""
    rows = db.query(Demo.status, func.count(Demo.id)).group_by(Demo.status).all()
    counts = {status: count for status, count in rows}
    total = sum(counts.values())
    demos = DemoCounts(
        total=total,
        approved=counts.get("approved", 0),
        pending=counts.get("pending", 0),
        rejected=counts.get("rejected", 0),
    )
    users = db.query(func.count(User.id)).scalar() or 0
    return AdminStatsOut(
        demos=demos,
        users=users,
        storage=StorageStatusOut(**_storage_status()),
    )


# ==================== 首页策展（07 §2.2 / T5·M5-F1）====================
# 策展池 = demos.featured=1 的行，featured_order 1 起连续（每写操作后重排归一）。
# hero 大卡语义 = 池内 order 最小（=1）的那件（GET /demos?featured=1 的首件）。
# 纪律：全部写操作 require_admin + 同事务落 audit（featured_add/remove/order/hero）。


def _featured_ordered(db: Session) -> list[Demo]:
    return (
        db.query(Demo)
        .filter(Demo.featured.is_(True))
        .order_by(Demo.featured_order.asc(), Demo.id.asc())
        .all()
    )


def _featured_reindex(db: Session, rows: list[Demo]) -> None:
    """把池内排序位归一为 1..n（featured_order 与列表位置永远一致，防陈旧碎片值）。"""
    for i, d in enumerate(rows, start=1):
        d.featured_order = i


def _featured_row_out(db: Session, d: Demo) -> dict:
    out = serialize_demo(db, d, detail=True)
    out["id"] = d.id  # 后台写操作（order/hero/remove）以 demo_id 为键；公开列表口径不含 id
    out["featured_order"] = d.featured_order
    return out


@router.get("/featured")
def admin_list_featured(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """精选池列表（含 featured_order；面板排序/hero/移除的数据源）。"""
    rows = _featured_ordered(db)
    return {"items": [_featured_row_out(db, d) for d in rows], "total": len(rows)}


@router.post("/featured")
def admin_add_featured(body: dict, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """把已上架 demo 加入首页策展池（尾部追加 order）。只接受 approved（上架）作品——
    策展池只从已上线作品里精选，pending/rejected 不该出现在首页（含未来意外上线面）。"""
    slug = (body.get("slug") or "").strip() or None
    demo_id = body.get("demo_id")
    if not slug and not demo_id:
        raise HTTPException(status_code=422, detail="需要 slug 或 demo_id", )
    demo = db.query(Demo).filter(Demo.id == demo_id).first() if demo_id else db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    if demo.status != "approved":
        raise HTTPException(status_code=422, detail="仅已上架（approved）作品可进首页策展池", )
    if demo.featured:
        raise HTTPException(status_code=409, detail=f"已在精选池：{demo.slug}", )
    rows = _featured_ordered(db)
    demo.featured = True
    rows.append(demo)
    _featured_reindex(db, rows)
    audit_service.record(
        db,
        action="featured_add",
        entity_type="demo",
        entity_id=demo.id,
        actor_id=admin.id,
        after={"slug": demo.slug, "featured": True, "featured_order": demo.featured_order},
        reason="",
    )
    db.commit()
    return {"ok": True, "slug": demo.slug, "featured_order": demo.featured_order, "total": len(rows)}


@router.delete("/featured/{demo_id}")
def admin_remove_featured(demo_id: int, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """把 demo 移出策展池并重排剩余行。"""
    demo = db.get(Demo, demo_id)
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    if not demo.featured:
        raise HTTPException(status_code=404, detail="该作品不在精选池", )
    before = demo.featured_order
    demo.featured = False
    demo.featured_order = None
    _featured_reindex(db, _featured_ordered(db))
    audit_service.record(
        db,
        action="featured_remove",
        entity_type="demo",
        entity_id=demo.id,
        actor_id=admin.id,
        before={"slug": demo.slug, "featured": True, "featured_order": before},
        after={"slug": demo.slug, "featured": False},
        reason="",
    )
    db.commit()
    return {"ok": True, "slug": demo.slug}


@router.put("/featured/{demo_id}/order")
def admin_move_featured(demo_id: int, body: dict, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """池内上移/下移（body: {"direction": "up"|"down"}）；相邻交换后重排归一。"""
    direction = body.get("direction")
    if direction not in ("up", "down"):
        raise HTTPException(status_code=422, detail='direction 需为 "up" 或 "down"', )
    demo = db.get(Demo, demo_id)
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    if not demo.featured:
        raise HTTPException(status_code=404, detail="该作品不在精选池", )
    rows = _featured_ordered(db)
    idx = next((i for i, d in enumerate(rows) if d.id == demo_id), None)
    if idx is None:
        raise HTTPException(status_code=404, detail="该作品不在精选池", )
    swap = idx - 1 if direction == "up" else idx + 1
    if swap < 0 or swap >= len(rows):
        raise HTTPException(status_code=409, detail="已在边界，不能再移", )
    rows[idx], rows[swap] = rows[swap], rows[idx]
    _featured_reindex(db, rows)
    audit_service.record(
        db,
        action="featured_order",
        entity_type="demo",
        entity_id=demo.id,
        actor_id=admin.id,
        after={"slug": demo.slug, "featured_order": demo.featured_order, "direction": direction},
        reason="",
    )
    db.commit()
    return {"ok": True, "slug": demo.slug, "featured_order": demo.featured_order}


@router.put("/featured/{demo_id}/hero")
def admin_set_featured_hero(demo_id: int, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """把该件置为 hero（=移到池首，order 归一为 1）——首页 hero 大卡取池内首件。"""
    demo = db.get(Demo, demo_id)
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    if not demo.featured:
        raise HTTPException(status_code=404, detail="该作品不在精选池", )
    rows = _featured_ordered(db)
    if rows and rows[0].id == demo_id:
        raise HTTPException(status_code=409, detail="它已是 hero（池内第一件）", )
    rows = [d for d in rows if d.id != demo_id]
    rows.insert(0, demo)
    _featured_reindex(db, rows)
    audit_service.record(
        db,
        action="featured_hero",
        entity_type="demo",
        entity_id=demo.id,
        actor_id=admin.id,
        after={"slug": demo.slug, "featured_order": 1, "hero": True},
        reason="",
    )
    db.commit()
    return {"ok": True, "slug": demo.slug, "featured_order": 1}