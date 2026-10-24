"""统一建议收件箱（v2 治理地基）：规则层产出的建议在此排队，人工/自动审核后执行。

设计要点（评审与重排.md §四 idea 6、§八）：
- **表先建、UI 押后**：LLM 接入时同表复用，只多一种 source='ai'，零改表；
- **置信度三级**（治理文档 §十六 的落点）：
    ≥ AUTO_ACCEPT(0.99) → 可自动执行（但必须落审计）——v2.0 规则层默认**关闭**该开关，
                           因为 TF-IDF 分数并非概率，误判即不可逆变更，全部走人工；
    ≥ REVIEW(0.60)      → 进收件箱默认视图
    < REVIEW            → 只入库不骚扰（admin 显式带 min_confidence 才看得到）
- approve 才执行，且**只通过对应 service 执行**（写操作全走 service）+ 同事务落审计。
"""

import json
from datetime import datetime, timezone

from fastapi import HTTPException
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..models import (
    Demo,
    DemoModel,
    DemoTag,
    EntitySuggestion,
    Model,
    SUGGESTION_KINDS,
    SUGGESTION_SOURCES,
    Tag,
    TagKey,
    Task,
)
from . import audit_service, matching_service, model_service, task_service

AUTO_ACCEPT = 0.99  # 自动执行阈值（v2.0 仅 LLM 接入后启用）
REVIEW = 0.60       # 收件箱默认视图阈值


def _now() -> datetime:
    return datetime.now(timezone.utc).replace(tzinfo=None)


def create(
    db: Session,
    *,
    kind: str,
    payload: dict,
    confidence: float | None = None,
    source: str = "inferred",
    demo_id: int | None = None,
    ref_id: int | None = None,
    created_by: int | None = None,
    auto_accept: bool = False,
    dedupe: bool = True,
) -> EntitySuggestion | None:
    """落一条建议（幂等：同 kind + ref_id + demo_id 的 pending 不重复堆）。

    返回 None = 被去重丢弃。高置信 + auto_accept=True 才当场执行（LLM 时代的行为）。
    dedupe=False 跳过该检查：三条定位键全为 None 的调用（如独立出题）会被判成同一条，
    全站只能堆进一条 pending —— 去重改由调用方按自己的业务键做（端点内同用户同题名幂等）。
    """
    if kind not in SUGGESTION_KINDS:
        raise HTTPException(status_code=422, detail=f"未知建议类型: {kind}")
    if source not in SUGGESTION_SOURCES:
        raise HTTPException(status_code=422, detail=f"未知来源: {source}")

    dup = None
    if dedupe:
        dup = db.query(EntitySuggestion).filter(
            EntitySuggestion.kind == kind,
            EntitySuggestion.ref_id == ref_id,
            EntitySuggestion.demo_id == demo_id,
            EntitySuggestion.status == "pending",
        ).first()
    if dup is not None:
        # 已排队：只在置信度更高时刷新证据，不重复建行
        if confidence is not None and (dup.confidence or 0) < confidence:
            dup.confidence = confidence
            dup.payload = json.dumps(payload, ensure_ascii=False)
            db.flush()  # KB-16：不自行 commit，事务边界归调用方
        return None

    s = EntitySuggestion(
        kind=kind,
        payload=json.dumps(payload, ensure_ascii=False),
        confidence=confidence,
        source=source,
        demo_id=demo_id,
        ref_id=ref_id,
        created_by=created_by,
    )
    db.add(s)
    db.flush()

    if auto_accept and confidence is not None and confidence >= AUTO_ACCEPT:
        return review(db, s, "approve", actor_id=None, actor_type="system")

    db.flush()  # KB-16：由调用方 commit（上传 / 巡检 / 批量入队各自的事务里收尾）
    return s


def _payload_of(s: EntitySuggestion) -> dict:
    try:
        data = json.loads(s.payload or "{}")
        return data if isinstance(data, dict) else {}
    except json.JSONDecodeError:
        return {}


def review(
    db: Session,
    suggestion: EntitySuggestion,
    action: str,
    actor_id: int | None = None,
    actor_type: str = "user",
) -> EntitySuggestion:
    """审核一条建议：approve 即调对应 service 真正落库（同事务 + 审计）。"""
    if suggestion.kind == "new_prompt":
        from . import prompt_library_service

        prompt_library_service.lock_review(db, suggestion)
    if suggestion.status != "pending":
        raise HTTPException(status_code=409, detail="该建议已处理")
    if action not in ("approve", "reject"):
        raise HTTPException(status_code=422, detail="action 需为 approve / reject")

    payload = _payload_of(suggestion)
    result_note = ""
    if action == "approve":
        result_note = _execute(db, suggestion, payload, actor_id)
    suggestion.status = "approved" if action == "approve" else "rejected"
    suggestion.reviewed_by = actor_id
    suggestion.reviewed_at = _now()
    suggestion._result_note = result_note  # 供 suggestion_out 回给调用方（非持久字段）

    audit_service.record(
        db,
        action="review",
        entity_type="suggestion",
        entity_id=suggestion.id,
        actor_id=actor_id,
        actor_type=actor_type,
        before={"kind": suggestion.kind, "status": "pending", "confidence": suggestion.confidence},
        after={"kind": suggestion.kind, "status": suggestion.status, "result": result_note},
        reason=result_note or f"建议 {action}",
    )
    db.commit()
    return suggestion


def _execute(db: Session, s: EntitySuggestion, payload: dict, actor_id: int | None) -> str:
    """按 kind 分派到对应 service —— 本函数绝不自己 UPDATE 实体表。

    KB-16：所有内部 service 调用都传 `commit=False`，实体变更与「建议状态 + 审核审计」
    在 review() 的同一个事务里提交。旧实现内部各自 commit，中断就会留下
    「实体已建、建议仍 pending」的假象，再点一次批准会重复建实体。
    """
    if s.kind == "new_model":
        name = (payload.get("name") or "").strip()
        if not name:
            raise HTTPException(status_code=422, detail="建议缺少 name，无法建实体")
        model, created = model_service.get_or_create_model(db, name, vendor=payload.get("vendor"))
        if created and model.status == "candidate":
            model_service.model_status_set(
                db, model, "active", actor_id=actor_id, reason="收件箱批准：新模型", commit=False
            )
        elif not created:
            # 批准时同名实体已存在 → 视作别名归一，避免静默丢建议
            model_service.alias_add(db, model, name, actor_id=actor_id, commit=False)
        return f"模型 {name} → {model.slug}"

    if s.kind == "alias":
        target = model_service.get_model_or_404(db, payload.get("model_id") or s.ref_id or 0)
        name = (payload.get("alias") or "").strip()
        added = model_service.alias_add(db, target, name, actor_id=actor_id, commit=False)
        return f"别名 {name} {'归入' if added else '已存在于'} {target.slug}"

    if s.kind == "merge_model":
        src = model_service.get_model_or_404(db, payload.get("source_id") or s.ref_id or 0)
        tgt = model_service.get_model_or_404(db, payload.get("target_id") or 0)
        out = model_service.merge_model(
            db, src, tgt, dry_run=False, actor_id=actor_id, reason="收件箱批准：模型合并", commit=False
        )
        return f"合并 {out['source']['slug']} → {out['target']['slug']}，影响 {out['affected_demos']} 个作品"

    if s.kind == "new_task":
        title = (payload.get("title") or "").strip()
        if not title:
            raise HTTPException(status_code=422, detail="建议缺少 title，无法建题目")
        task = task_service.create_task(
            db,
            title=title,
            description=payload.get("description") or "",
            category=payload.get("category"),
            status="active",
            created_by=actor_id,
            reason="收件箱批准：新建题目",
            commit=False,
        )
        demo_ids = payload.get("demo_ids") or ([s.demo_id] if s.demo_id else [])
        demo_ids = [d for d in demo_ids if d]
        if demo_ids:
            task_service.attach_demos(db, task, demo_ids, actor_id=actor_id, commit=False)
        return f"题目 {task.slug} 挂 {len(demo_ids)} 个作品"

    if s.kind == "new_prompt":
        from . import prompt_library_service

        entry = prompt_library_service.publish_prompt(db, payload, created_by=s.created_by)
        # 持久化目标 id，让已处理收件箱可直接打开条目，不从结果文案解析地址。
        s.payload = json.dumps({**payload, "prompt_id": entry.prompt_id}, ensure_ascii=False)
        return f"提示词 /prompts/{entry.prompt_id} 已收录"

    if s.kind == "task_match":
        task = task_service.get_task_or_404(db, payload.get("task_id") or s.ref_id or 0)
        demo_id = payload.get("demo_id") or s.demo_id
        if not demo_id:
            raise HTTPException(status_code=422, detail="建议缺少 demo_id")
        task_service.attach_demos(db, task, [demo_id], actor_id=actor_id, commit=False)
        return f"作品 {demo_id} 挂入 {task.slug}"

    if s.kind == "merge_task":
        src = task_service.get_task_or_404(db, payload.get("source_id") or s.ref_id or 0)
        tgt = task_service.get_task_or_404(db, payload.get("target_id") or 0)
        out = task_service.merge_task(
            db, src, tgt, dry_run=False, actor_id=actor_id, reason="收件箱批准：题目合并", commit=False
        )
        return f"合并《{out['source']['title']}》→《{out['target']['title']}》，迁移 {out['affected_demos']} 个作品"

    if s.kind == "retag_demo":
        from . import refine_service

        demo = db.get(Demo, payload.get("demo_id") or s.demo_id or 0)
        if demo is None:
            raise HTTPException(status_code=404, detail="目标作品不存在")
        add_value = str(payload.get("add") or "").strip()
        if not add_value:
            raise HTTPException(status_code=422, detail="建议缺少目标 type 值")
        # remove 允许是字符串或列表（巡检的「多值收敛」会一次给多个待删值）
        remove = payload.get("remove", "demo")
        remove = list(remove) if isinstance(remove, (list, tuple)) else str(remove)
        out = refine_service.apply_retag(db, demo, add_value, remove_value=remove)
        if not out["changed"]:
            return f"《{demo.title}》已是 type:{add_value}，无需改动"
        return f"《{demo.title}》type → {add_value}（现 type：{'/'.join(out['type_values'])}）"

    raise HTTPException(status_code=422, detail=f"暂不支持执行的建议类型: {s.kind}")


# ---------------- 查询 ----------------


def _list_out(s: EntitySuggestion) -> dict:
    """收件箱列表序列化：new_prompt 正文超长时截断（480 字 + 标记），全量走详情端点。
    审核执行读的是库里存的完整 payload，这里只裁剪展示，不影响批准结果。"""
    out = suggestion_out(s)
    if s.kind == "new_prompt":
        from .prompt_library_service import content_excerpt

        content = out["payload"].get("content")
        if isinstance(content, str):
            excerpt, truncated = content_excerpt(content)
            if truncated:
                out["payload"] = {**out["payload"], "content": excerpt, "content_truncated": True}
    return out


def suggestion_out(s: EntitySuggestion) -> dict:
    out = {
        "id": s.id,
        "kind": s.kind,
        "payload": _payload_of(s),
        "confidence": s.confidence,
        "source": s.source,
        "status": s.status,
        "demo_id": s.demo_id,
        "ref_id": s.ref_id,
        "created_at": s.created_at,
        "reviewed_at": s.reviewed_at,
    }
    # 刚执行完才有的瞬时字段：把"到底改了什么"回给调用方，前端提示就不用自己编文案
    note = getattr(s, "_result_note", None)
    if note:
        out["result"] = note
    return out


def list_suggestions(
    db: Session,
    status: str = "pending",
    kind: str | None = None,
    min_confidence: float | None = None,
) -> list[dict]:
    """收件箱列表。默认只看 pending，且隐去低置信度（< REVIEW）的噪音。"""
    q = db.query(EntitySuggestion)
    if status and status != "all":
        q = q.filter(EntitySuggestion.status == status)
    if kind:
        q = q.filter(EntitySuggestion.kind == kind)
    floor = min_confidence if min_confidence is not None else REVIEW
    if status == "pending" and min_confidence is None:
        q = q.filter(EntitySuggestion.confidence >= floor)
    rows = q.order_by(EntitySuggestion.confidence.desc().nullslast(), EntitySuggestion.id.desc()).limit(200).all()
    return [_list_out(s) for s in rows]


def counts_by_kind(db: Session) -> dict[str, int]:
    """收件箱首屏计数（治理文档 §三：管理员第一眼是「待处理 37」而不是一张表）。"""
    rows = (
        db.query(EntitySuggestion.kind, func.count(EntitySuggestion.id))
        .filter(EntitySuggestion.status == "pending", EntitySuggestion.confidence >= REVIEW)
        .group_by(EntitySuggestion.kind)
        .all()
    )
    return {k: c for k, c in rows}


def knowledge_stats(db: Session) -> dict:
    """治理体检面板（KPI 用覆盖率/积压/重复率，明确不用「标签数量」当指标）。"""
    demos_total = db.query(func.count(Demo.id)).filter(Demo.status == "approved").scalar() or 0

    def key_coverage(key: str) -> int:
        return (
            db.query(func.count(func.distinct(DemoTag.demo_id)))
            .join(Tag, Tag.id == DemoTag.tag_id)
            .join(Demo, Demo.id == DemoTag.demo_id)
            .filter(Tag.key == key, Demo.status == "approved")
            .scalar()
            or 0
        )

    entity_coverage = (
        db.query(func.count(func.distinct(DemoModel.demo_id)))
        .join(Demo, Demo.id == DemoModel.demo_id)
        .filter(Demo.status == "approved")
        .scalar()
        or 0
    )
    tier_keys = db.query(TagKey.key, TagKey.tier, TagKey.label).order_by(TagKey.tier, TagKey.key).all()

    # 重名口径（KB-7）：按**规范化名称**分组统计。原实现按 Model.slug 分组，而 slug 有
    # 唯一约束 → 这个 KPI 结构性恒为 0（实测插入两个 normalize 同名实体后仍是 0）。
    name_groups: dict[str, list[str]] = {}
    for (name,) in db.query(Model.name).all():
        name_groups.setdefault(matching_service.normalize(name), []).append(name)
    dup_names = {norm: names for norm, names in name_groups.items() if len(names) > 1}

    return {
        "demos_approved": demos_total,
        "coverage": {
            key: {"label": label, "tier": tier, "demos": key_coverage(key),
                  "rate": round(key_coverage(key) / demos_total, 3) if demos_total else 0.0}
            for key, tier, label in tier_keys
        },
        "model_entity": {
            "demos": entity_coverage,
            "rate": round(entity_coverage / demos_total, 3) if demos_total else 0.0,
            "total_models": db.query(func.count(Model.id)).scalar() or 0,
            "active": db.query(func.count(Model.id)).filter(Model.status == "active").scalar() or 0,
            "candidate": db.query(func.count(Model.id)).filter(Model.status == "candidate").scalar() or 0,
            "unverified": db.query(func.count(Model.id)).filter(Model.status == "unverified").scalar() or 0,
            "deprecated": db.query(func.count(Model.id)).filter(Model.status == "deprecated").scalar() or 0,
        },
        "task": {
            "total": db.query(func.count(Task.id)).scalar() or 0,
            "active": db.query(func.count(Task.id)).filter(Task.status == "active").scalar() or 0,
            "candidate": db.query(func.count(Task.id)).filter(Task.status == "candidate").scalar() or 0,
        },
        "inbox": {
            "pending": db.query(func.count(EntitySuggestion.id)).filter(EntitySuggestion.status == "pending").scalar() or 0,
            "pending_actionable": counts_by_kind(db),
        },
        "duplicate_slugs": len(dup_names),  # 键名保持兼容（前端已消费）；语义=规范化同名实体组数
        "duplicate_names": [
            {"normalized": norm, "names": names} for norm, names in sorted(dup_names.items())
        ],
    }
