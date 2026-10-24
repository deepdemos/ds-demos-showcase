import json
import time
import urllib.request
from collections import defaultdict
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Query, Request
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..client_ip import get_client_ip
from ..database import get_db
from ..domains.identity.deps import optional_user, require_admin
from ..models import Demo, DemoTag, Tag, TagKey, TagValueSuggestion, User
from ..schemas import (
    AiSuggestIn,
    DeriveIn,
    DeriveItemOut,
    DeriveOut,
    TagCreate,
    TagDetail,
    TagGroupRename,
    TagKeyOut,
    TagKeyUpdate,
    TagKeyUpsert,
    TagMergeIn,
    TagMergeResult,
    TagOut,
    TagSuggestionReview,
    TagValueGroupSet,
    TagValueSuggestionIn,
    TagValueSuggestionOut,
)
from ..serializers import tag_dict
from ..services import audit_service, model_service, ratelimit, tag_service

router = APIRouter(prefix="/tags", tags=["tags"])

# 用户申请固定值限流：每 IP 每小时 10 次（实现见 services.ratelimit）
_SUGGEST_RATE = 10


@router.get("/tag-keys", response_model=list[TagKeyOut])
def list_tag_keys(db: Session = Depends(get_db)):
    """标签键定义（供发布/编辑页做选择器 + 标签主页展示）。

    公开读口（T3·M5-B2）：词表剔除 deprecated（已退役不该出现在新页面——Model 先例）；
    管理端全量读口见 GET /tags/admin/tag-keys（复活入口的数据源）。
    """
    keys = db.query(TagKey).order_by(TagKey.sort, TagKey.key).all()
    return [_tag_key_out(db, k) for k in keys]


@router.get("/admin/tag-keys", response_model=list[TagKeyOut])
def list_tag_keys_admin(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """管理端词表全量：含 deprecated（状态徽章随附），知识中心总表/详情导航的数据源。"""
    keys = db.query(TagKey).order_by(TagKey.sort, TagKey.key).all()
    return [tag_service.tag_key_out(db, k, include_deprecated=True) for k in keys]


def find_tag_by_key_value(db: Session, key_value: str) -> Tag:
    if ":" not in key_value:
        raise HTTPException(status_code=404, detail="标签不存在", )
    key, value = key_value.split(":", 1)
    tag = db.query(Tag).filter(Tag.key == key, Tag.value == value).first()
    if tag is None:
        raise HTTPException(status_code=404, detail="标签不存在", )
    return tag


@router.get("/{key_value}", response_model=TagDetail)
def get_tag(key_value: str, db: Session = Depends(get_db)):
    tag = find_tag_by_key_value(db, key_value)
    # 公开详情读口（T3·M5-B2）：deprecated = 已退役，公开页不再出现（404，同 Model 先例）
    if (tag.status or "active") == "deprecated":
        raise HTTPException(status_code=404, detail="标签不存在", )
    data = tag_dict(db, tag)
    parent = tag.parent
    data["parent"] = tag_dict(db, parent) if parent and (parent.status or "active") != "deprecated" else None
    data["children"] = [
        tag_dict(db, c) for c in tag.children if (c.status or "active") != "deprecated"
    ]
    return data


@router.post("", status_code=201, response_model=TagOut)
def create_tag(body: TagCreate, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """新增固定值标签（仅 admin）。规则与审计在 tag_service（KB-4）。"""
    tag = tag_service.create_tag_value(
        db,
        key=body.key,
        value=body.value,
        description=body.description,
        group=body.group,
        parent_id=body.parent_id,
        actor_id=admin.id,
    )
    return tag_dict(db, tag)


# ---------- 标签键管理（admin） ----------
@router.post("/admin/tag-keys", status_code=201, response_model=TagKeyOut)
def create_tag_key(body: TagKeyUpsert, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    k = tag_service.create_tag_key(
        db,
        key=body.key,
        mode=body.mode,
        label=body.label,
        description=body.description,
        sort=body.sort,
        actor_id=admin.id,
    )
    return _tag_key_out(db, k)


RESERVED_TAG_KEYS = tag_service.RESERVED_TAG_KEYS


@router.put("/admin/tag-keys/{key}", response_model=TagKeyOut)
def update_tag_key(
    key: str,
    body: TagKeyUpdate,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    k = tag_service.update_tag_key(
        db,
        key=key,
        mode=body.mode,
        label=body.label,
        description=body.description,
        sort=body.sort,
        actor_id=admin.id,
    )
    return _tag_key_out(db, k)


@router.delete("/admin/tag-keys/{key}", status_code=204)
def delete_tag_key(key: str, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """删除标签键（同时删除该键下未被引用的标签值）；子标签/引用一律 409，不再 500。"""
    tag_service.delete_tag_key(db, key, actor_id=admin.id)


@router.delete("/admin/tag-keys/{key}/values/{value}", status_code=204)
def delete_tag_value(
    key: str,
    value: str,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """删除某个标签值（被 demo 引用或作为父标签时禁止删除）。"""
    tag = db.query(Tag).filter(Tag.key == key, Tag.value == value).first()
    if tag is None:
        raise HTTPException(status_code=404, detail="标签值不存在")
    tag_service.delete_tag_value(db, tag, actor_id=admin.id)


def _tag_key_out(db: Session, k: TagKey) -> TagKeyOut:
    return tag_service.tag_key_out(db, k)


# ---------- 固定值申请（用户） ----------
def _suggest_rate_limit(request: Request) -> None:
    # KB-21：统一走 services.ratelimit
    ip = get_client_ip(request) or "unknown"
    ratelimit.hit(f"tagsuggest:{ip}", _SUGGEST_RATE, 3600)


@router.post("/suggestions", status_code=201, response_model=TagValueSuggestionOut)
def create_suggestion(
    body: TagValueSuggestionIn,
    request: Request,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    """用户申请新增 fixed 标签值：只写 pending 建议，不直接创建 Tag。"""
    _suggest_rate_limit(request)
    key_def = db.get(TagKey, body.key)
    if key_def is None:
        raise HTTPException(status_code=422, detail="未知标签 key", )
    if key_def.mode != "fixed":
        raise HTTPException(status_code=422, detail=f"{body.key} 为 {key_def.mode} 模式，无需申请固定值", )
    # KB-6：申请可以绑定作品（上传后回挂），但必须校验存在性与归属 ——
    # 否则匿名用户能把申请挂到别人的作品上，管理员批准即等于替他人改标签。
    if body.demo_id is not None:
        demo = db.get(Demo, body.demo_id)
        if demo is None:
            raise HTTPException(status_code=404, detail="作品不存在", )
        owned = demo.author_id is None or (
            user is not None and (user.id == demo.author_id or user.role == "admin")
        )
        if not owned:
            raise HTTPException(status_code=403, detail="无权为该作品申请标签", )
    if db.query(Tag).filter(Tag.key == body.key, Tag.value == body.value).first():
        raise HTTPException(status_code=409, detail="该固定值已存在", )
    if db.query(TagValueSuggestion).filter(
        TagValueSuggestion.key == body.key,
        TagValueSuggestion.value == body.value,
        TagValueSuggestion.status == "pending",
    ).first():
        raise HTTPException(status_code=409, detail="已有待审核的相同申请", )
    s = TagValueSuggestion(
        key=body.key,
        value=body.value,
        description=body.description,
        group=body.group,
        demo_id=body.demo_id,
        created_by=user.id if user else None,
    )
    db.add(s)
    db.commit()
    db.refresh(s)
    return s


# ---------- 固定值建议审核（admin） ----------
@router.get("/admin/suggestions", response_model=list[TagValueSuggestionOut])
def list_suggestions(
    status: str | None = Query(default=None, pattern="^(pending|approved|rejected)$"),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    q = db.query(TagValueSuggestion)
    if status:
        q = q.filter(TagValueSuggestion.status == status)
    return q.order_by(TagValueSuggestion.created_at.desc()).all()


@router.post("/admin/suggestions/{sid}/review", response_model=TagValueSuggestionOut)
def review_suggestion(
    sid: int,
    body: TagSuggestionReview,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    s = db.get(TagValueSuggestion, sid)
    if s is None:
        raise HTTPException(status_code=404, detail="建议不存在", )
    if s.status != "pending":
        raise HTTPException(status_code=409, detail="该建议已处理", )
    if body.action == "approve":
        tag = db.query(Tag).filter(Tag.key == s.key, Tag.value == s.value).first()
        if tag is None:
            # Tag 写入收口在 tag_service（KB-4）
            tag = tag_service.ensure_value(
                db, s.value, group=s.group or body.group, description=s.description, key=s.key
            )
            # 词表变更与建议审核同事务留痕（谁批的、批出了哪个值）
            audit_service.record(
                db,
                action="create",
                entity_type="tag",
                entity_id=tag.id,
                actor_id=admin.id,
                after=tag_service.tag_snapshot(tag),
                reason=f"用户申请批准：{s.key}:{s.value}",
            )
        # 可选：同时补挂到提交者 demo；model 键必须双写 demo_models（身份绑定闭环 A）
        if s.demo_id:
            demo = db.get(Demo, s.demo_id)
            if demo:
                if not db.query(DemoTag).filter(DemoTag.demo_id == demo.id, DemoTag.tag_id == tag.id).first():
                    db.add(DemoTag(demo_id=demo.id, tag_id=tag.id))
                if s.key == "model":
                    db.flush()
                    db.expire(demo, ["tag_associations"])
                    model_service.sync_demo_models(db, demo)
        s.status = "approved"
    else:
        s.status = "rejected"
    s.reviewed_by = admin.id
    s.reviewed_at = datetime.utcnow()
    db.commit()
    db.refresh(s)
    return s


# ---------- AI 辅助整理（admin，只返回建议不落库） ----------
@router.post("/admin/fetch-models")
def fetch_models(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """抓取/整理主流 AI 模型：写入 model 键的 pending 建议（人工审核后生效）。"""
    curated = [
        ("DeepSeek", ["dsv4-flash", "dsv4-pro", "dsv4", "ds-unknown", "dsv4flash", "deepseek-chat", "deepseek-reasoner"]),
        ("OpenAI", ["gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "o1", "o1-mini", "o3", "o3-mini", "o4-mini"]),
        ("Anthropic", ["claude-3-5-sonnet", "claude-3-5-haiku", "claude-3-7-sonnet", "claude-4-sonnet", "claude-4-opus", "claude-4-haiku"]),
        ("Google", ["gemini-1.5-pro", "gemini-1.5-flash", "gemini-2.0-flash", "gemini-2.0-flash-lite", "gemini-2.5-pro", "gemini-2.5-flash", "gemini-3.7-flash", "gemini-3.7-pro"]),
        ("Meta", ["llama-3.1-8b", "llama-3.1-70b", "llama-3.1-405b", "llama-3.3-70b", "llama-4-maverick", "llama-4-scout"]),
        ("Mistral", ["mistral-large", "mistral-medium", "mistral-small", "mistral-nemo", "codestral"]),
        ("Qwen", ["qwen2.5-7b", "qwen2.5-72b", "qwen2.5-coder", "qwen3-30b", "qwen3-235b", "qwen3-coder"]),
        ("Kimi", ["moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k", "kimi-k2", "kimi-k2-thinking"]),
        ("GLM", ["glm-4", "glm-4-flash", "glm-4-plus", "glm-4.5", "glm-4.6", "glm-4.6v"]),
        ("MiniMax", ["abab6.5", "abab6.5s", "minimax-text-01"]),
        ("智谱", ["chatglm-4", "chatglm-4-air", "chatglm-4-long"]),
        ("百川", ["baichuan2-7b", "baichuan2-13b", "baichuan3"]),
        ("月之暗面", ["moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"]),
        ("阿里", ["qwen-turbo", "qwen-plus", "qwen-max", "qwen-long"]),
    ]
    created = 0
    for group, models in curated:
        for m in models:
            if db.query(Tag).filter(Tag.key == "model", Tag.value == m).first():
                continue
            if db.query(TagValueSuggestion).filter(
                TagValueSuggestion.key == "model",
                TagValueSuggestion.value == m,
                TagValueSuggestion.status == "pending",
            ).first():
                continue
            db.add(TagValueSuggestion(key="model", value=m, description="", group=group, status="pending"))
            created += 1
    db.commit()
    return {"created": created, "note": "已写入 pending 建议，需人工审核后生效"}


@router.post("/derive", response_model=DeriveOut)
def derive_tags(
    body: DeriveIn,
    db: Session = Depends(get_db),
):
    """标签建议包（上传页第 2 步用）：**不要求登录、不写库**，作者收下或跳过都行。

    规则来自现有词表：`type` 用拆分流水线的关键词引擎、`model` 用型号名/别名命中、
    其余键用「值本身或值的中文介绍出现在文本里」自匹配 —— 新登记固定值自动可被推荐。
    """
    from ..services import derive_service

    items = derive_service.suggest_pack(
        db,
        title=body.title,
        description=body.description,
        prompt=body.prompt,
        limit=body.limit,
    )
    return DeriveOut(
        items=[DeriveItemOut(**i) for i in items],
        note="规则推导，仅供参考；不收也不影响提交。" if items else "",
    )


@router.post("/admin/ai-suggest")
def ai_suggest(
    body: AiSuggestIn,
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """AI 辅助整理（管理端）：与 `/tags/derive` **同一规则引擎**，只是响应形态不同。

    原来这里另写了一份硬编码关键词表（5 个值、命中即 break），两处规则会各自漂移；
    现在统一委托 `derive_service`，将来接 LLM 也只改一个地方。
    """
    from ..services import derive_service

    demo = db.get(Demo, body.demo_id) if body.demo_id else None
    title = demo.title if demo else ""
    description = (demo.description if demo else body.text) or ""
    prompt = (demo.prompt or "") if demo else ""
    items = derive_service.suggest_pack(db, title=title, description=description, prompt=prompt)
    return {
        "suggestions": [{"key": i["key"], "value": i["value"], "reason": i["reason"]} for i in items],
        "note": "规则版（与上传建议包同一引擎），仅供参考；确认后请手动写入标签。",
    }


# ---------- 标签 group 管理（admin） ----------
@router.get("/admin/groups")
def list_groups(key: str = Query(...), db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """列出某 key 下的 group 分布。"""
    rows = (
        db.query(Tag.group, func.count(Tag.id))
        .filter(Tag.key == key, Tag.group.isnot(None))
        .group_by(Tag.group)
        .all()
    )
    ungrouped = db.query(func.count(Tag.id)).filter(Tag.key == key, Tag.group.is_(None)).scalar() or 0
    return {
        "key": key,
        "groups": [{"group": g, "count": c} for g, c in rows],
        "ungrouped": ungrouped,
    }


@router.put("/admin/groups/{key}/{group}")
def rename_group(
    key: str,
    group: str,
    body: TagGroupRename,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """重命名 group：批量更新该 group 下所有 Tag.group（同事务落审计）。"""
    updated = tag_service.rename_group(
        db, key=key, group=group, new_group=body.new_group, actor_id=admin.id
    )
    return {"updated": updated, "new_group": body.new_group}


@router.delete("/admin/groups/{key}/{group}")
def clear_group(
    key: str,
    group: str,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """清除 group：该 group 下所有值变为无分组（同事务落审计）。"""
    cleared = tag_service.clear_group(db, key=key, group=group, actor_id=admin.id)
    return {"cleared": cleared}


@router.put("/admin/values/{tag_id}/group", response_model=TagOut)
def set_value_group(
    tag_id: int,
    body: TagValueGroupSet,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """给单个固定值设置/清除 group。"""
    tag = db.get(Tag, tag_id)
    if tag is None:
        raise HTTPException(status_code=404, detail="标签不存在", )
    tag = tag_service.set_value_group(db, tag, body.group, actor_id=admin.id)
    return tag_dict(db, tag)


# ---------- 标签合并（admin） ----------
@router.post("/admin/merge", response_model=TagMergeResult)
def merge_tags(body: TagMergeIn, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """合并标签：把 from 值的引用迁到 to 值，删除源值（单事务 + 审计 + 派生数据回填）。

    规则与完整性闸在 `tag_service.merge_tags`（KB-1/KB-4）。
    """
    return TagMergeResult(
        **tag_service.merge_tags(
            db,
            from_key=body.from_key,
            from_value=body.from_value,
            to_key=body.to_key,
            to_value=body.to_value,
            dry_run=body.dry_run,
            actor_id=admin.id,
        )
    )


# ---------- 从 models.dev 同步模型标签（admin） ----------
MODELS_DEV_URL = "https://models.dev/api.json"
MODELS_DEV_LIMIT = 10 * 1024 * 1024  # 10MB


@router.post("/admin/sync-models")
def sync_models(db: Session = Depends(get_db), _: User = Depends(require_admin)):
    """从 models.dev 拉取模型字典，同步 model 固定值（新模型写 pending 建议，已有模型更新 group）。"""
    try:
        req = urllib.request.Request(MODELS_DEV_URL, headers={"User-Agent": "ds-demos-showcase/1.0"})
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = resp.read(MODELS_DEV_LIMIT + 1)
    except Exception as e:  # noqa: BLE001
        raise HTTPException(status_code=502, detail=f"拉取 models.dev 失败: {e}", )
    if len(data) > MODELS_DEV_LIMIT:
        raise HTTPException(status_code=502, detail="models.dev 数据超过大小限制", )
    try:
        payload = json.loads(data)
    except json.JSONDecodeError:
        raise HTTPException(status_code=502, detail="models.dev 返回非 JSON", )

    new_pending = 0
    updated_group = 0
    total_models = 0
    providers = 0

    # KB-21：预载既有词表值与 pending 建议（一次两条查询），循环里不再逐型号查库
    existing_by_value = {
        value: tag for value, tag in db.query(Tag.value, Tag).filter(Tag.key == "model").all()
    }
    pending_by_value = {
        value: row
        for value, row in db.query(TagValueSuggestion.value, TagValueSuggestion)
        .filter(TagValueSuggestion.key == "model", TagValueSuggestion.status == "pending")
        .all()
    }

    for provider_id, provider in payload.items():
        if not isinstance(provider, dict):
            continue
        provider_name = str(provider.get("name") or provider_id)
        models = provider.get("models") or {}
        if not isinstance(models, dict):
            continue
        providers += 1
        for model_id, meta in models.items():
            if not isinstance(meta, dict):
                continue
            total_models += 1
            value = str(meta.get("id") or model_id)
            name = str(meta.get("name") or "")
            existing = existing_by_value.get(value)
            if existing is not None:
                if existing.group != provider_name:
                    existing.group = provider_name
                    updated_group += 1
                continue
            pending = pending_by_value.get(value)
            if pending is not None:
                if pending.group != provider_name:
                    pending.group = provider_name
                    updated_group += 1
                continue
            row = TagValueSuggestion(
                key="model",
                value=value,
                description=name,
                group=provider_name,
                status="pending",
            )
            db.add(row)
            pending_by_value[value] = row
            new_pending += 1

    db.commit()
    return {
        "providers": providers,
        "total_models": total_models,
        "new_pending": new_pending,
        "updated_group": updated_group,
        "note": "新模型已写入 pending 建议，需人工审核后生效",
    }
