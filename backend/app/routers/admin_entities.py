"""v2 治理写接口（B1.5）：Model / Task 实体管理 + 建议收件箱 + 体检 KPI + 审计回溯。

本文件**只做鉴权、取实体、转调 service**：不写业务规则、不直接 UPDATE 实体表
（验收「写操作全走 service」）。全部 require_admin。
"""

import json

from fastapi import APIRouter, Body, Depends, HTTPException, Query
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..database import get_db
from ..deps import require_admin
from ..models import AUDIT_ACTIONS, AUDIT_ENTITY_TYPES, SUGGESTION_KINDS, AuditLog, Demo, DemoModel, DemoTask, EntitySuggestion, Tag, Task, User
from ..schemas import (
    AliasIn,
    AttachDemosIn,
    AttributeIn,
    BatchReviewIn,
    MergeIn,
    ModelCreate,
    ModelStatusIn,
    ModelUpdate,
    SuggestionReviewIn,
    TagStatusIn,
    TaskCreateIn,
    TaskUpdateIn,
    UnmergeIn,
)
from ..services import cluster_service, entity_admin_service, model_service, suggestion_service, task_service

router = APIRouter(prefix="/admin", tags=["admin-entities"])


def _model_brief(db: Session, model) -> dict:
    """实体回显（含当前引用数），避免前端提交后再拉一次列表。"""
    linked = db.query(func.count(DemoModel.demo_id)).filter(DemoModel.model_id == model.id).scalar() or 0
    return {
        "id": model.id,
        "slug": model.slug,
        "name": model.name,
        "vendor": model.vendor,
        "status": model.status,
        "description": model.description,
        "demo_count": linked,
        "aliases": [a.alias for a in model.aliases],
    }


# ---------------- Model 实体 ----------------


@router.get("/models")
def admin_list_models(
    status: str | None = Query(default=None, pattern="^(candidate|active|unverified|deprecated)$"),
    q: str | None = None,
    sort: str = Query(default="demos", pattern="^(demos|name|new)$"),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """管理端列表：任何状态都可见（公开列表只出 active+unverified），附状态计数。"""
    items, total, status_counts = model_service.list_models_admin(db, status=status, q=q, sort=sort)
    return {"items": items, "total": total, "status_counts": status_counts}


@router.post("/models", status_code=201)
def admin_create_model(
    body: ModelCreate,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    model = model_service.create_model(
        db,
        name=body.name,
        vendor=body.vendor,
        description=body.description,
        status=body.status,
        actor_id=admin.id,
    )
    db.refresh(model)
    return _model_brief(db, model)


@router.put("/models/{ident}")
def admin_update_model(
    ident: str,
    body: ModelUpdate,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    model = model_service.get_model_or_404(db, ident)
    fields = body.model_dump(exclude_unset=True)
    model_service.model_update(db, model, actor_id=admin.id, **fields)
    db.refresh(model)
    return _model_brief(db, model)


@router.put("/models/{ident}/status")
def admin_model_status(
    ident: str,
    body: ModelStatusIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    model = model_service.get_model_or_404(db, ident)
    model_service.model_status_set(db, model, body.status, actor_id=admin.id, reason=body.reason)
    return {"id": model.id, "slug": model.slug, "status": model.status}


@router.delete("/models/{ident}", status_code=204)
def admin_delete_model(ident: str, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    model = model_service.get_model_or_404(db, ident)
    model_service.delete_model(db, model, actor_id=admin.id)
    return None


@router.post("/models/{ident}/aliases", status_code=201)
def admin_add_alias(
    ident: str,
    body: AliasIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    model = model_service.get_model_or_404(db, ident)
    if not model_service.alias_add(db, model, body.alias, actor_id=admin.id):
        raise HTTPException(status_code=409, detail="别名已存在或等于规范名")
    db.refresh(model)
    return {"model_id": model.id, "aliases": [a.alias for a in model.aliases]}


@router.delete("/models/{ident}/aliases/{alias}", status_code=204)
def admin_remove_alias(
    ident: str,
    alias: str,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    model = model_service.get_model_or_404(db, ident)
    if not model_service.alias_remove(db, model, alias, actor_id=admin.id):
        raise HTTPException(status_code=404, detail="别名不存在或不属于该实体")
    return None


@router.post("/models/{ident}/merge")
def admin_merge_model(
    ident: str,
    body: MergeIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """合并实体：治理铁律要求**先 dry_run 预览影响面**，确认后才真合。"""
    source = model_service.get_model_or_404(db, ident)
    target = model_service.get_model_or_404(db, body.target_id)
    return model_service.merge_model(
        db, source, target, dry_run=body.dry_run, actor_id=admin.id, reason=body.reason
    )


@router.get("/models/merge-history")
def admin_merge_history(
    limit: int = Query(30, ge=1, le=100),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """可撤销的合并清单（处于「已被合并」状态的实体 + 撤销可行性判断）。"""
    return {"items": model_service.merge_history(db, limit=limit)}


@router.post("/models/{ident}/unmerge")
def admin_unmerge_model(
    ident: str,
    body: UnmergeIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """撤销合并：迁回当初被搬走的引用 + 源实体恢复合并前状态。

    与合并同规：**先 dry_run 看影响面**。早期合并没记 moved_demo_ids 时只恢复实体、
    不猜引用（`reliable=false` 会在预览里明说）。
    """
    source = model_service.get_model_or_404(db, ident)
    return model_service.unmerge_model(
        db, source, dry_run=body.dry_run, actor_id=admin.id, reason=body.reason
    )


# ---------------- Task 实体 ----------------


@router.get("/tasks")
def admin_list_tasks(
    status: str | None = Query(default=None, pattern="^(candidate|active|merged|hidden)$"),
    q: str | None = None,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=50, ge=1, le=200),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """管理端题目列表：缺省看全部状态（公开列表只出 active）；传 status 可筛待审 candidate。"""
    items, total = task_service.list_tasks_admin(db, status=status, q=q, page=page, page_size=page_size)
    by_status = dict(db.query(Task.status, func.count(Task.id)).group_by(Task.status).all())
    return {
        "items": items,
        "total": total,
        "page": page,
        "page_size": page_size,
        "status_counts": {s: by_status.get(s, 0) for s in task_service.TASK_STATUSES},
    }


@router.get("/tasks/{ident}")
def admin_task_detail(
    ident: str,
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """M3-B2 管理端题目详情（任何状态含 merged/hidden）+ 归属作品全量列表（含 pending/rejected）。

    挂载摘除 UI 的数据源：公开 GET /tasks/{slug} 只出 approved 且 merged/hidden 直接 404，
    管理员看不到待挂/已下架作品就没法管理归属（用户点名痛点「题目没法管理归属它的 demo」）。
    """
    task = task_service.get_task_or_404(db, ident)
    rows = (
        db.query(Demo.id, Demo.slug, Demo.title, Demo.status)
        .join(DemoTask, DemoTask.demo_id == Demo.id)
        .filter(DemoTask.task_id == task.id)
        .order_by(Demo.id.desc())
        .all()
    )
    return {
        "id": task.id,
        "slug": task.slug,
        "title": task.title,
        "description": task.description,
        "category": task.category,
        "status": task.status,
        "merged_into_id": task.merged_into_id,
        "created_at": task.created_at,
        "demos": [{"id": r[0], "slug": r[1], "title": r[2], "status": r[3]} for r in rows],
    }


@router.post("/tasks", status_code=201)
def admin_create_task(
    body: TaskCreateIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """管理员直接建题（治理边界内合法，不经候选队列，落审计）；带 demo_ids/demo_slugs 时一并挂题。

    M3-B2：slug 先解析后建题（fail-fast）——未知 slug 整批 404，不留下已建好的空题。
    """
    ids = list(body.demo_ids or [])
    if body.demo_slugs:
        ids += entity_admin_service.resolve_demo_slugs(db, body.demo_slugs)
    task = task_service.create_task(
        db,
        title=body.title,
        description=body.description,
        category=body.category,
        status=body.status,
        created_by=admin.id,
    )
    attached = 0
    if ids:
        attached = task_service.attach_demos(db, task, ids, actor_id=admin.id)
    return {"id": task.id, "slug": task.slug, "title": task.title, "status": task.status, "attached": attached}


@router.put("/tasks/{ident}")
def admin_update_task(
    ident: str,
    body: TaskUpdateIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    task = task_service.get_task_or_404(db, ident)
    task_service.update_task(db, task, actor_id=admin.id, **body.model_dump(exclude_unset=True))
    db.refresh(task)
    return {"id": task.id, "slug": task.slug, "title": task.title, "status": task.status}


@router.delete("/tasks/{ident}", status_code=204)
def admin_delete_task(ident: str, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    task = task_service.get_task_or_404(db, ident)
    task_service.delete_task(db, task, actor_id=admin.id)
    return None


@router.post("/tasks/{ident}/merge")
def admin_merge_task(
    ident: str,
    body: MergeIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    source = task_service.get_task_or_404(db, ident)
    target = task_service.get_task_or_404(db, body.target_id)
    return task_service.merge_task(
        db, source, target, dry_run=body.dry_run, actor_id=admin.id, reason=body.reason
    )


@router.post("/tasks/{ident}/demos")
def admin_attach_demos(
    ident: str,
    body: AttachDemosIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """批量挂题：冷启动与 prompt 簇「成题」的执行入口。M3-B2：支持 demo_ids 或 demo_slugs（实体详情按 slug 挂）。"""
    task = task_service.get_task_or_404(db, ident)
    ids = list(body.demo_ids or [])
    if body.demo_slugs:
        ids += entity_admin_service.resolve_demo_slugs(db, body.demo_slugs)
    if not ids:
        raise HTTPException(status_code=422, detail="demo_ids / demo_slugs 至少给一种")
    added = task_service.attach_demos(db, task, ids, actor_id=admin.id)
    return {"task_id": task.id, "attached": added}


@router.delete("/tasks/{ident}/demos/{demo_id}", status_code=204)
def admin_detach_demo(
    ident: str,
    demo_id: int,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    task = task_service.get_task_or_404(db, ident)
    if not task_service.detach_demo(db, task, demo_id, actor_id=admin.id):
        raise HTTPException(status_code=404, detail="该作品不在此题目下")
    return None


@router.delete("/tasks/{ident}/demos/slug/{demo_slug}", status_code=204)
def admin_detach_demo_by_slug(
    ident: str,
    demo_slug: str,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """M3-B2 摘题（按 slug）：实体详情关联作品列表是 DemoSummary（只有 slug），按 slug 摘除。"""
    task = task_service.get_task_or_404(db, ident)
    row = db.query(Demo.id).filter(Demo.slug == demo_slug).first()
    if row is None:
        raise HTTPException(status_code=404, detail="demo slug 不存在")
    if not task_service.detach_demo(db, task, row[0], actor_id=admin.id):
        raise HTTPException(status_code=404, detail="该作品不在此题目下")
    return None


# ---------------- Prompt 聚类建议（B3′：Task 从簇里长出来） ----------------


@router.get("/prompt-clusters")
def admin_prompt_clusters(
    min_score: float = Query(default=cluster_service.MIN_SCORE, ge=0.1, le=1, description="相似档阈值"),
    exact_min_demos: int = Query(default=cluster_service.EXACT_MIN_DEMOS, ge=2, le=50),
    similar_min_demos: int = Query(default=cluster_service.SIMILAR_MIN_DEMOS, ge=2, le=50),
    similar_min_models: int = Query(default=cluster_service.SIMILAR_MIN_MODELS, ge=1, le=10),
    refresh: bool = Query(default=False, description="true = 绕过 60s 缓存重算"),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """提示词簇 → 待确认题目（两档：exact 同句 / similar 相似；60s 缓存，写路径主动失效）。

    阈值默认值来自线上真实语料标定（评审与重排.md §六）：0.35 是唯一有质量档位，
    降到 0.25 即混入「相近但不同题」的误簇。产物一律是建议，管理员命名 + 点成题
    （`POST /admin/tasks {title, demo_ids}`）才落库。
    """
    return cluster_service.prompt_clusters(
        db,
        min_score=min_score,
        exact_min_demos=exact_min_demos,
        similar_min_demos=similar_min_demos,
        similar_min_models=similar_min_models,
        use_cache=not refresh,
    )


# ---------------- 收件箱 / 体检 / 审计 ----------------


@router.get("/suggestions")
def admin_list_suggestions(
    status: str = Query(default="pending", pattern="^(pending|approved|rejected|all)$"),
    # KB-22：白名单由 SUGGESTION_KINDS 常量生成（旧写法手抄一份，漏了最大宗的 retag_demo → 422）
    kind: str | None = Query(default=None, pattern="^(" + "|".join(SUGGESTION_KINDS) + ")$"),
    min_confidence: float | None = Query(default=None, ge=0, le=1),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    return {
        "items": suggestion_service.list_suggestions(db, status=status, kind=kind, min_confidence=min_confidence),
        "pending_by_kind": suggestion_service.counts_by_kind(db),
        "thresholds": {"auto_accept": suggestion_service.AUTO_ACCEPT, "review": suggestion_service.REVIEW},
    }


@router.get("/suggestions/{sid}")
def admin_get_suggestion(
    sid: int,
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """收件箱详情：列表对 new_prompt 正文做了截断，展开全文按 id 取完整 payload。"""
    s = db.get(EntitySuggestion, sid)
    if s is None:
        raise HTTPException(status_code=404, detail="建议不存在")
    return suggestion_service.suggestion_out(s)


@router.post("/suggestions/{sid}/review")
def admin_review_suggestion(
    sid: int,
    body: SuggestionReviewIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    s = db.get(EntitySuggestion, sid)
    if s is None:
        raise HTTPException(status_code=404, detail="建议不存在")
    s = suggestion_service.review(db, s, body.action, actor_id=admin.id)
    return suggestion_service.suggestion_out(s)


@router.post("/suggestions/batch-review")
def admin_batch_review_suggestions(
    body: BatchReviewIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """M3-B3 收件箱批量审核（t4 前端限速循环→真批量端点）：逐条独立提交+审计，单条失败不拖垮整批。"""
    return entity_admin_service.batch_review(db, body.action, body.ids, actor_id=admin.id)


@router.patch("/entities/{entity_type}/{ident}")
def admin_patch_entity(
    entity_type: str,
    ident: str,
    patch: dict = Body(default_factory=dict, description='字段白名单直改：model={name,vendor,description} task={title,description,category,status,reason} tag={description,group}；"reason" 键为审计元数据'),
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """M3-B1 统一实体字段直改（06 §A2「自由」格落地）：白名单逐实体定义，全部走 service 层+审计。"""
    return entity_admin_service.patch_entity(db, entity_type, ident, patch, actor_id=admin.id)


@router.put("/entities/tag/{tag_id}/status")
def admin_tag_status(
    tag_id: int,
    body: TagStatusIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """T3·M5-B2 Tag 状态跃迁（06 §A3.3 + 附录 B 实施）：candidate|active|deprecated。

    独立端点（不并入 PATCH——状态是「受限」格，理由随审计落行）；写操作走 service
    entity_admin_service.tag_status_set + 同事务审计。tag 实体 ident 恒为数值 id。
    """
    tag = db.get(Tag, tag_id)
    if tag is None:
        raise HTTPException(status_code=404, detail="标签值不存在")
    entity_admin_service.tag_status_set(
        db, tag, body.status, actor_id=admin.id, reason=body.reason
    )
    return {"id": tag.id, "key": tag.key, "value": tag.value, "status": tag.status}


@router.get("/knowledge/stats")
def admin_knowledge_stats(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """治理体检：覆盖率 / 实体积压 / 收件箱待处理 / 重复率（明确不用「标签数量」当 KPI）。"""
    return suggestion_service.knowledge_stats(db)


@router.get("/audit")
def admin_audit(
    # KB-17：实体类型白名单由常量生成（论坛/用户/设置动作也要筛得出来）
    entity_type: str | None = Query(default=None, pattern="^(" + "|".join(AUDIT_ENTITY_TYPES) + ")$"),
    entity_id: int | None = None,
    action: str | None = Query(default=None, pattern="^(" + "|".join(AUDIT_ACTIONS) + ")$"),
    q: str | None = Query(default=None, description="按 reason 关键词搜（定位是谁的哪次操作）"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=50, ge=1, le=200),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """审计回溯：谁在什么时候改了什么、改前改后是什么。

    `actions` / `entity_types` 随响应返回 —— 前端下拉直接用它，常量只在一处定义
    （避免白名单与写入脱节）。
    """
    query = db.query(AuditLog)
    if entity_type:
        query = query.filter(AuditLog.entity_type == entity_type)
    if entity_id is not None:
        query = query.filter(AuditLog.entity_id == entity_id)
    if action:
        query = query.filter(AuditLog.action == action)
    if q:
        like = f"%{q.strip()}%"
        query = query.filter(AuditLog.reason.like(like))
    total = query.count()
    rows = query.order_by(AuditLog.id.desc()).offset((page - 1) * page_size).limit(page_size).all()
    # 「谁」要能看懂：批量解析 actor 用户名（不逐行查，避免审计页变成 N+1）
    actor_ids = {r.actor_id for r in rows if r.actor_id}
    actors = dict(db.query(User.id, User.username).filter(User.id.in_(actor_ids)).all()) if actor_ids else {}
    return {
        "items": [_audit_out(r, actors.get(r.actor_id)) for r in rows],
        "total": total,
        "page": page,
        "page_size": page_size,
        "actions": list(AUDIT_ACTIONS),
        "entity_types": list(AUDIT_ENTITY_TYPES),
    }


def _audit_out(row: AuditLog, actor_name: str | None = None) -> dict:
    return {
        "id": row.id,
        "actor_type": row.actor_type,
        "actor_id": row.actor_id,
        # 给人看的署名：系统/匿名动作没有用户名时保留 actor_type 语义
        "actor": actor_name or (row.actor_type if row.actor_type != "user" else "unknown"),
        "action": row.action,
        "entity_type": row.entity_type,
        "entity_id": row.entity_id,
        "before": _load_json(row.before),
        "after": _load_json(row.after),
        "reason": row.reason,
        "created_at": row.created_at,
    }


def _load_json(raw: str | None):
    if not raw:
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw


# ---------- type:demo 拆分流水线（B4 规则版） ----------


@router.get("/type-demo/preview")
def admin_type_demo_preview(
    limit: int = Query(500, ge=1, le=2000),
    min_confidence: float = Query(0.6, ge=0.0, le=1.0),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """规则预览（**不写库**）：type:demo 会被拆成哪些值、各自命中多少、置信度分布。

    先看质量再决定入队 —— 规则在真实语料上不一定准，直接写候选会污染收件箱。
    """
    from ..services import refine_service

    props = refine_service.scan(db, limit=limit, min_confidence=min_confidence)
    by_target: dict[str, int] = {}
    for p in props:
        by_target[p.add] = by_target.get(p.add, 0) + 1
    return {
        "stats": refine_service.stats(db),
        "scanned": limit,
        "proposed": len(props),
        "by_target": dict(sorted(by_target.items(), key=lambda kv: -kv[1])),
        "samples": [
            {
                "demo_slug": p.slug,
                "demo_title": p.title,
                "add": p.add,
                "alt": p.alt,
                "confidence": p.confidence,
                "matched": p.matched[:6],
                "label_zh": refine_service.LABELS_ZH.get(p.add, p.add),
            }
            for p in props[:40]
        ],
    }


@router.post("/type-demo/queue")
def admin_type_demo_queue(
    limit: int = Query(500, ge=1, le=2000),
    min_confidence: float = Query(0.6, ge=0.0, le=1.0),
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """把规则建议落进收件箱（`retag_demo`，人工 approve 才真改标签）。

    幂等：同 demo 的 pending 建议由 suggestion_service 去重，重复扫描不堆噪音。
    """
    from ..services import refine_service

    props = refine_service.scan(db, limit=limit, min_confidence=min_confidence)
    queued = 0
    for p in props:
        made = suggestion_service.create(
            db,
            kind="retag_demo",
            payload=p.to_payload(),
            confidence=p.confidence,
            source="inferred",
            demo_id=p.demo_id,
            created_by=admin.id,
        )
        if made is not None:
            queued += 1
    db.commit()
    return {"proposed": len(props), "queued": queued}


# ---------- 治理巡检（B4）：结构性缺口 → 可处理清单 ----------


@router.get("/inspection")
def admin_inspection(
    sample_limit: int = Query(8, ge=1, le=30),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """巡检全部检查项（只读，不写库）。"""
    from ..services import inspect_service

    return inspect_service.run(db, sample_limit=sample_limit)


@router.post("/inspection/{check_id}/queue")
def admin_inspection_queue(
    check_id: str,
    min_confidence: float = Query(0.8, ge=0.0, le=1.0),
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """把可执行巡检项落成候选（不可执行的项返回 422，不造假动作）。"""
    from ..services import inspect_service

    return inspect_service.queue(db, check_id, actor_id=admin.id, min_confidence=min_confidence)


@router.get("/entity-conflicts")
def admin_entity_conflicts(
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """规范化同名的实体冲突组（合并向导的发现层）：只报不动手。"""
    from ..services import inspect_service

    return inspect_service.entity_conflicts(db)


# ---------- 归属工作台（Q2 第三步）：兜底位作品迁回真实型号 ----------


@router.get("/attribution/pending")
def admin_attribution_pending(
    limit_models: int = Query(20, ge=1, le=50),
    limit_demos: int = Query(60, ge=1, le=200),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """待归属清单：兜底实体（未标注 / 未定型号 / 灰测）+ 其下作品 + 规则预填目标。"""
    return model_service.pending_attribution(db, limit_models=limit_models, limit_demos=limit_demos)


@router.post("/attribution")
def admin_attribution(
    body: AttributeIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """把选中的作品从兜底位归属到真实型号。

    注意 service 内部走的是**标签回写 + 实体双写同步**：只改 `demo_models` 的归属
    会在作者下次编辑 tags 时被静默退回兜底位。
    """
    return model_service.attribute_demos(
        db,
        demo_ids=body.demo_ids,
        target_id=body.target_id,
        actor_id=admin.id,
        reason=body.reason,
    )
