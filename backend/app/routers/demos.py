import asyncio
import hashlib
import ipaddress
import json
import os
import random
import re
import shutil
import socket
import threading
import time
import urllib.error
import urllib.request
import uuid
import zipfile
from collections import defaultdict
from datetime import datetime
from pathlib import Path
from urllib.parse import urlparse

from fastapi import APIRouter, Depends, File, Form, HTTPException, Query, Request, Response, UploadFile
from fastapi.responses import FileResponse, RedirectResponse
from sqlalchemy import func
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from ..client_ip import get_client_ip
from ..config import settings
from ..database import get_db
from ..domains.identity.deps import current_user, optional_user
from ..models import CollectionItem, DemoView, Announcement, Demo, DemoModel, DemoTask, DemoTimeline, DemoTag, Model, SessionLog, Tag, TagKey, Task, User
from ..schemas import DemoCreateResult, DemoDetailOut, DemoFromUrlIn, DemoMetaOut, DemoSummaryOut, Paginated, SamePromptOut
from ..serializers import preload_demo_relations, serialize_demo
from ..services import counters, model_service, oss, storage
from ..services import notification_service, ratelimit, suggestion_service, tag_service
from ..services.scope import demo_in_scope, get_scope, scope_contains_filter
from ..services.settings_service import get_auto_approve, get_auto_approve_public

router = APIRouter(prefix="/demos", tags=["demos"])

# 匿名上传限流：每 IP 每小时 20 次（实现见 services.ratelimit）
ANON_RATE_LIMIT = 20  # 次/小时/IP

# URL 抓取只允许这些端口（KB-12）：内网服务常挂在 8080/6379/9200 等非常规端口
ALLOWED_FETCH_PORTS = frozenset({80, 443})


def _anon_rate_limit(request: Request) -> None:
    """匿名上传限流：每 IP 每小时最多 ANON_RATE_LIMIT 次（KB-21：统一走 services.ratelimit）。"""
    ip = get_client_ip(request) or "unknown"
    ratelimit.hit(f"upload:anon:{ip}", ANON_RATE_LIMIT, 3600)


# ---- 高并发缓存：相关推荐 / 首页随机（锁只护缓存字典，不护 DB 查询）----
# 缓存一律按「视区:slug」分键（astra 橱窗/deep 主站数据面不同，跨视区复用会漏内容）
_CACHE_LOCK = threading.Lock()
_RELATED_CACHE: dict[str, tuple[float, list]] = {}
_RANDOM_CACHE: dict[str, tuple[float, list[int]]] = {}  # scope -> (过期时间, 随机 id 序)
_RANDOM_TTL = 60  # 秒
_RANDOM_TTL_REL = 60  # related 缓存秒数


def _related_cache_get(key: str) -> list | None:
    with _CACHE_LOCK:
        hit = _RELATED_CACHE.get(key)
        if hit and hit[0] > time.time():
            return hit[1]
    return None


def _related_cache_set(key: str, result: list) -> None:
    with _CACHE_LOCK:
        _RELATED_CACHE[key] = (time.time() + _RANDOM_TTL_REL, result)


def _random_ids(db, page: int, page_size: int, scope: str, rs: str | None = None) -> tuple[list[int], str]:
    """返回一次随机序下的 [offset, offset+page_size) 的已上架 demo id 及其种子。

    - rs（会话固定种子）：确定性洗牌、无视时间桶 —— 同一种子跨请求/跨 60s 桶顺序恒同，
      翻页不重复不漏件（这是漏项的根修；前端去重只能遮重复）。
    - 无 rs：保持旧行为（60s 缓存整份随机序，按视区分键），但洗牌改用「生成并随缓存保存
      的种子」，响应把该种子回传，客户端可选择 pin 住 —— 缓存过期后回传种子仍能续上原序。
    按视区分键：astra 橱窗只随机策展池。锁只护缓存；DB 查询在锁外执行（复用请求会话）。
    """
    if rs is not None:
        live_ids = [
            d
            for (d,) in db.query(Demo.id).filter(Demo.status == "approved", scope_contains_filter(scope)).all()
        ]
        random.Random(f"{scope}|rs|{rs}").shuffle(live_ids)
        start = (page - 1) * page_size
        return live_ids[start : start + page_size], rs
    with _CACHE_LOCK:
        hit = _RANDOM_CACHE.get(scope)
        ids = list(hit[1]) if hit and hit[0] > time.time() else None
        seed = hit[2] if ids is not None else None
    if ids is None:
        # 锁外：用请求会话查库 + 洗牌（种子随缓存保存，seedless 响应也能回传可续的种子）
        seed = uuid.uuid4().hex
        live_ids = [
            d
            for (d,) in db.query(Demo.id).filter(Demo.status == "approved", scope_contains_filter(scope)).all()
        ]
        random.Random(f"{scope}|rs|{seed}").shuffle(live_ids)
        with _CACHE_LOCK:
            hit = _RANDOM_CACHE.get(scope)
            if not hit or hit[0] <= time.time():
                _RANDOM_CACHE[scope] = (time.time() + _RANDOM_TTL, live_ids, seed)
            else:
                seed = hit[2]
            ids = live_ids
    start = (page - 1) * page_size
    return ids[start : start + page_size], seed


def _find_demo(db: Session, slug: str) -> Demo:
    demo = db.query(Demo).filter(Demo.slug == slug).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    return demo


def _ensure_tag(db: Session, key: str, value: str) -> Tag:
    """内部标签（author / version-of 等保留 key）直接创建或复用。

    KB-4：Tag 写入统一走 tag_service（本函数只保留语义清晰的入口名）。
    """
    return tag_service.ensure_value(db, value, key=key)


def _resolve_tag(db: Session, item: str | dict, allow_tag_ids: set[int] | None = None) -> Tag:
    """按标签键定义校验并解析用户提交的标签：
    - 支持字符串 "k:v" 或对象 {"key","value","description?"}
    - fixed: value 必须是已存在且**在用**的固定值
    - open:  任意自定义 value（自动建标签；首次创建可写入 description）
    - int:   value 必须是整数（自动建标签，规范化存储）

    allow_tag_ids（KB-3）：作品**已经挂着**的标签 id 集合。已退役值不得再被新选，但存量
    作品编辑时要放行自己原有的值 —— 否则一次改描述就会因为历史值被 422 卡死，
    把退役成本转嫁给作者。传 None 表示纯新增场景（一律拦）。
    """
    if isinstance(item, str):
        key, _, value = item.partition(":")
        description = None
    else:
        key = item.get("key", "")
        value = item.get("value", "")
        description = item.get("description") or None

    key = key.strip()
    value = value.strip()
    if not key or not value:
        raise HTTPException(status_code=422, detail=f"非法标签格式: {item}", )
    if len(key) > 64 or len(value) > 64:
        raise HTTPException(status_code=422, detail=f"标签 key/value 过长: {key}:{value}", )

    key_def = db.get(TagKey, key)
    if key_def is None:
        raise HTTPException(status_code=422, detail=f"未知标签 key: {key}（请管理员先在标签键管理中定义）", )

    if key_def.mode == "fixed":
        tag = db.query(Tag).filter(Tag.key == key, Tag.value == value).first()
        if tag is None:
            raise HTTPException(status_code=422, detail=f"{key}:{value} 不是该键的固定值，请从候选中选择", )
        status = tag.status or "active"
        if status != "active" and (allow_tag_ids is None or tag.id not in allow_tag_ids):
            raise HTTPException(
                status_code=422,
                detail=f"{key}:{value} 已退役（{status}），请从在用候选中另选",
            )
        return tag

    if key_def.mode == "int":
        try:
            int(value)
        except ValueError:
            raise HTTPException(status_code=422, detail=f"{key} 的值必须是整数（如 rounds:3）", )
        value = str(int(value))

    tag = db.query(Tag).filter(Tag.key == key, Tag.value == value).first()
    if tag is None:
        # 首次创建 open/int 值时，可写入用户提供的介绍（写入收口在 tag_service）
        tag = tag_service.ensure_value(db, value, description=description or "", key=key)
    return tag


def _require_model_tags(items: list) -> None:
    """Q2 决议（D6）：必须至少声明 1 个 model 标签（入参为已解析标签列表）。

    强制的前提是「不确定也有正门」，三条路都写进错误文案，避免逼人来个瞎填：
      - 知厂商不知型号 → `model:<vendor>-unknown`（family 档）
      - 完全不知道 → `model:unspecified`（unknown 档）
      - 网传灰测未证实 → `model:ds-unknown`（guess 档，可被后续「揭晓」改映射）
    """
    for item in items or []:
        key = item.split(":", 1)[0] if isinstance(item, str) else item.get("key", "")
        if key == "model":
            return
    raise HTTPException(
        status_code=422,
        detail="必须选择至少 1 个模型标签；不确定就选 model:unspecified（完全不知道）"
               "或 model:<厂商>-unknown（知厂商不知型号），并在 model_hint 里留一句依据",
    )


def _require_model_tag(tags_raw: str | None) -> None:
    _require_model_tags(_parse_tags(tags_raw))


def _parse_tags(raw: str | None) -> list:
    """解析 tags JSON：支持字符串数组 ["k:v"] 或对象数组 [{"key","value","description?"}]。"""
    if not raw:
        return []
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        raise HTTPException(status_code=422, detail="tags 字段需为 JSON 数组", )
    if not isinstance(data, list):
        raise HTTPException(status_code=422, detail="tags 字段需为 JSON 数组", )
    for item in data:
        if isinstance(item, str):
            continue
        if isinstance(item, dict) and isinstance(item.get("key"), str) and isinstance(item.get("value"), str):
            continue
        raise HTTPException(status_code=422, detail="tags 元素需为 \"k:v\" 字符串或 {key,value,description?} 对象", )
    return data


async def _read_limited(file: UploadFile, limit: int, msg: str) -> bytes:
    """分块读取，超限立即 413（KB-11）。

    旧实现 `await file.read()` 先把整个上传读进内存再比大小：200MB 上限下几十个并发
    就能把进程 RSS 顶爆，而匿名限流还排在这之后。
    """
    chunks: list[bytes] = []
    total = 0
    while True:
        chunk = await file.read(1 << 20)  # 1MB
        if not chunk:
            break
        total += len(chunk)
        if total > limit:
            raise HTTPException(status_code=413, detail=msg)
        chunks.append(chunk)
    return b"".join(chunks)


def _save_image_cover(data: bytes, ext: str) -> tuple[None, tuple[str, str, str]]:
    """替换封面用（update image）：返回 (None, save_cover 三元组)，便于一次性解包。

    与 _create_demo_record 的封面块共用同一个 storage.save_cover —— 压缩/2 档缩略图/越界
    防护规则只有一份，不在路由里再造。"""
    return None, storage.save_cover(data, ext)


def _set_demo_tags(db: Session, demo: Demo, key_values: list[str]) -> None:
    # 先记下「本作品已挂的标签 id」：编辑时允许保留自己原有的已退役值（KB-3），
    # 但新选退役值一律 422。必须在删除旧关联之前取。
    existing_tag_ids = {
        row[0] for row in db.query(DemoTag.tag_id).filter(DemoTag.demo_id == demo.id).all()
    }
    db.query(DemoTag).filter(DemoTag.demo_id == demo.id).delete()
    for kv in key_values:
        tag = _resolve_tag(db, kv, allow_tag_ids=existing_tag_ids)
        db.add(DemoTag(demo_id=demo.id, tag_id=tag.id))
    # 自动附加作者标签（保留 key，跳过键定义校验）；匿名统一为 public
    author_name = None
    if demo.author_id is not None:
        author = db.get(User, demo.author_id)
        if author:
            author_name = author.username
    else:
        author_name = "public"
    if author_name:
        author_tag = _ensure_tag(db, "author", author_name)
        db.add(DemoTag(demo_id=demo.id, tag_id=author_tag.id))
    # v2 双写：model 标签 → demo_models 实体关联（旧 tag 筛选保持兼容）
    model_service.sync_demo_models(db, demo)
    # v2 B5′：run 语义标签 → demo 列（依赖刚写入的标签关联，故在其后；sync 内部已 flush）
    model_service.sync_run_meta(demo)


def _add_publish_announcement(db: Session, demo: Demo, user: User | None) -> None:
    """新作品公告（仅已上架作品调用，见 KB-10）。审核通过时由 admin.review_demo 补发。"""
    db.add(Announcement(
        type="auto",
        title="新 Demo 发布",
        content=demo.title,
        demo_slug=demo.slug,
        status="published",
        category="demo",
        created_by=user.id if user else None,
    ))


def _add_timeline(
    db: Session,
    demo_id: int,
    version_label: str,
    message: str,
    old_slug: str | None = None,
) -> None:
    db.add(DemoTimeline(
        demo_id=demo_id,
        version_label=version_label,
        message=message,
        old_slug=old_slug,
    ))


def _unique_slug(db: Session, title: str) -> str:
    for _ in range(10):
        slug = storage.make_slug(title)
        if db.query(Demo).filter(Demo.slug == slug).first() is None:
            return slug
    raise HTTPException(status_code=500, detail="slug 生成失败", )


def _parse_int_range(raw: str) -> tuple[int | None, int | None]:
    """`3` / `3-10` / `-10` / `3-` → (lo, hi)；非法返回 (None, None)（不因此报错，与 int 标签口径一致）。

    两端都缺时按闭区间处理单值（`3` = 恰好 3），单边写法开区间。
    """
    s = (raw or "").strip()
    if not s:
        return None, None
    if "-" in s:
        lo_s, _, hi_s = s.partition("-")
        lo = int(lo_s) if lo_s.strip().lstrip("+").isdigit() else None
        hi = int(hi_s) if hi_s.strip().lstrip("+").isdigit() else None
        return lo, hi
    return (int(s), int(s)) if s.lstrip("+").isdigit() else (None, None)


@router.get("", response_model=Paginated)
def list_demos(
    status: str | None = Query(default="approved", description="缺省 approved；其它状态仅 admin"),
    tag: list[str] = Query(default=[]),
    q: str | None = None,
    author: str | None = None,
    model: str | None = Query(default=None, description="按模型实体 slug 过滤（v2）"),
    task: str | None = Query(default=None, description="按题目实体 slug 过滤（v2）"),
    rounds: str | None = Query(default=None, description="生成轮数：3 / 3-10 / -10 / 3-（v2 B5′）"),
    minutes: str | None = Query(default=None, description="生成耗时（分钟）：同上（v2 B5′）"),
    platform: str | None = Query(default=None, description="运行平台精确匹配（v2 B5′）"),
    sort: str = Query(default="newest", pattern="^(newest|popular|random|prompt)$"),
    # 会话固定随机种子（sort=random 专用）：首响回传 random_seed，客户端翻页回传同一个值，
    # 整段翻页都建立在同一次洗牌上 —— 跨 60s 时间桶也不重复不漏件（前端去重只能遮重复，
    # 漏件必须靠顺序本身稳定）。不带 rs 的调用保持旧行为（响应仍回传当前有效种子，可选采纳）。
    rs: str | None = Query(default=None, max_length=64, pattern=r"^[A-Za-z0-9_-]+$", description="sort=random 会话固定种子"),
    featured: int | None = Query(default=None, ge=0, le=1, description="首页策展池（07 §2.2）：featured=1 只出精选并按 featured_order 排序（hero=首件）"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
    user: User | None = Depends(optional_user),
):
    # KB-10：公开列表只出已上架。`?status=`（空串）历史上等价于「不过滤」→ 现在按 approved 处理；
    # 其余状态只有 admin 能查（审核队列有专门的 /admin/review，不靠公开列表枚举）。
    status = (status or "").strip() or "approved"
    if status not in ("pending", "approved", "rejected"):
        raise HTTPException(status_code=422, detail="status 需为 pending/approved/rejected", )
    if status != "approved" and not (user is not None and user.role == "admin"):
        raise HTTPException(status_code=403, detail="仅管理员可按状态查询作品", )

    query = db.query(Demo)
    # 可见域过滤（astra 橱窗只出策展池；deep 存量行 sites='deep' 恒真，主站行为不变）
    query = query.filter(scope_contains_filter(scope))
    query = query.filter(Demo.status == status)

    if author:
        if author == "public":
            # public 虚拟身份：所有未注册上传（author_id 为空）
            query = query.filter(Demo.author_id.is_(None))
        else:
            user = db.query(User).filter(User.username == author).first()
            if user is None:
                return Paginated(items=[], total=0, page=page, page_size=page_size)
            query = query.filter(Demo.author_id == user.id)

    # 标签过滤（v2 修正）：键间 AND、键内 OR ——「model:HY4 或 model:ds-unknown」这类组合才成立
    from sqlalchemy import cast, or_, select
    from sqlalchemy import Integer as SAInteger

    key_values: dict[str, list[str]] = {}
    int_ranges: list[tuple[str, int, int]] = []
    for kv in tag:
        parts = kv.split(":", 1)
        if len(parts) != 2:
            raise HTTPException(status_code=422, detail=f"非法标签过滤: {kv}", )
        key, val = parts[0], parts[1]
        key_def = db.get(TagKey, key)
        if key_def is not None and key_def.mode == "int" and "-" in val:
            # int 键范围：tag=rounds:3-10（每个范围是独立约束，键间仍 AND）
            lo_s, _, hi_s = val.partition("-")
            try:
                lo, hi = int(lo_s), int(hi_s)
            except ValueError:
                raise HTTPException(status_code=422, detail=f"int 标签范围格式需为 key:lo-hi，如 rounds:3-10", )
            int_ranges.append((key, lo, hi))
        else:
            key_values.setdefault(key, []).append(val)

    for key, vals in key_values.items():
        subs = [
            select(DemoTag.demo_id)
            .join(Tag, DemoTag.tag_id == Tag.id)
            .where(Tag.key == key, Tag.value == v)
            for v in vals
        ]
        if len(subs) == 1:
            query = query.filter(Demo.id.in_(subs[0]))
        else:
            query = query.filter(or_(*[Demo.id.in_(s) for s in subs]))
    for key, lo, hi in int_ranges:
        sub = (
            select(DemoTag.demo_id)
            .join(Tag, DemoTag.tag_id == Tag.id)
            .where(
                Tag.key == key,
                cast(Tag.value, SAInteger) >= lo,
                cast(Tag.value, SAInteger) <= hi,
            )
        )
        query = query.filter(Demo.id.in_(sub))

    # v2 实体过滤：?model=slug / ?task=slug
    if model:
        m = db.query(Model).filter(Model.slug == model).first()
        if m is None:
            return Paginated(items=[], total=0, page=page, page_size=page_size)
        query = query.filter(Demo.id.in_(select(DemoModel.demo_id).where(DemoModel.model_id == m.id)))
    if task:
        t = db.query(Task).filter(Task.slug == task).first()
        if t is None:
            return Paginated(items=[], total=0, page=page, page_size=page_size)
        query = query.filter(Demo.id.in_(select(DemoTask.demo_id).where(DemoTask.task_id == t.id)))

    # v2 B5′：run 元数据按列过滤（比 tag=rounds: 的字符串 CAST 更准更快）
    # 语法与 int 标签一致：`3` / `3-10` / `-10` / `3-`；老参数 tag=rounds:3-10 保持可用
    for raw, col in ((rounds, Demo.gen_rounds), (minutes, Demo.gen_minutes)):
        if not raw:
            continue
        lo, hi = _parse_int_range(raw)
        if lo is not None:
            query = query.filter(col >= lo)
        if hi is not None:
            query = query.filter(col <= hi)
    if platform:
        query = query.filter(Demo.gen_platform.ilike(platform))

    # sort=random 的分流依据（见下方 random 分支）：任一实质过滤存在时，随机序必须建立在
    # **过滤后的集合**上，否则过滤被静默忽略。featured 走自己的分支，不在此列。
    has_filter = bool(
        tag or q or author or model or task or rounds or minutes or platform or status != "approved"
    )

    if q:
        like = f"%{q}%"
        from sqlalchemy import func as sa_func, select

        tag_ids = (
            db.query(DemoTag.demo_id)
            .join(Tag, DemoTag.tag_id == Tag.id)
            .filter(sa_func.concat(Tag.key, ":", Tag.value).ilike(like))
        )
        ids = select(Demo.id).where(
            (Demo.title.ilike(like))
            | (Demo.description.ilike(like))
            | (Demo.id.in_(tag_ids))
        )
        query = query.filter(Demo.id.in_(ids))

    if featured:
        # 首页策展池（07 §2.2）：只出 featured 行、按 featured_order 排（hero=首件=order 最小）。
        # 其余参数（status 默认 approved / q / scope）照常生效——策展只在已上架+可见域内成立。
        total = query.filter(Demo.featured.is_(True)).count()
        items = (
            query.filter(Demo.featured.is_(True))
            .order_by(Demo.featured_order.asc(), Demo.id.asc())
            .offset((page - 1) * page_size)
            .limit(page_size)
            .all()
        )
        preload_demo_relations(db, items)
        return Paginated(
            items=[serialize_demo(db, d) for d in items],
            total=total,
            page=page,
            page_size=page_size,
        )

    if sort == "popular":
        query = query.order_by(Demo.view_count.desc(), Demo.created_at.desc(), Demo.id.desc())
        total = query.count()
        items = query.offset((page - 1) * page_size).limit(page_size).all()
    elif sort == "random":
        total = query.count()
        random_seed = ""
        if has_filter:
            # 有实质过滤：必须对**过滤后**的 id 集合洗牌 —— 否则过滤条件被静默忽略，
            # 且 total（过滤后计数）与 items（全局池）不同源自相矛盾。
            # 顺序种子：rs（会话固定）优先 —— 同一种子跨请求/跨 60s 桶顺序恒同，翻页不重复
            # 不漏件；无 rs 用「scope + 过滤签名 + 60s 时间桶」（桶内稳定），并把桶号作为
            # random_seed 回传 —— 客户端回传它即可钉住当前顺序（桶过期后仍能续上）。
            # 不引入无界缓存（过滤组合空间大，用签名字典缓存会长大）。
            sig = repr((status, sorted(tag), q or "", author or "", model or "", task or "",
                        rounds or "", minutes or "", platform or ""))
            random_seed = rs if rs else str(int(time.time() // _RANDOM_TTL))
            ids = [d for (d,) in query.with_entities(Demo.id).all()]
            random.Random(f"{scope}|{sig}|{random_seed}").shuffle(ids)
            start = (page - 1) * page_size
            ids = ids[start : start + page_size]
        else:
            # 无过滤：保持原有整批随机语义（60s 缓存整份随机 id 序，按视区分键，
            # 避免每次 ORDER BY RANDOM() 全表扫）——首页「随便看看」行为不变；
            # rs 提供时绕过缓存直接确定性洗牌（翻页会话内稳定）。
            ids, random_seed = _random_ids(db, page, page_size, scope, rs)
        if ids:
            items = db.query(Demo).filter(Demo.id.in_(ids)).all()
            order = {did: i for i, did in enumerate(ids)}
            items.sort(key=lambda d: order.get(d.id, 10**9))
        else:
            items = []
    elif sort == "prompt":
        # 提示词优先：填了 prompt 的排前面（SQL 层排序，跨页稳定），同组按最新
        query = query.order_by((Demo.prompt == "").asc(), Demo.created_at.desc(), Demo.id.desc())
        total = query.count()
        items = query.offset((page - 1) * page_size).limit(page_size).all()
    else:
        # 次级键 id 兜底：同一秒发布的 demo 也有确定顺序，避免刷新/翻页抖动
        query = query.order_by(Demo.created_at.desc(), Demo.id.desc())
        total = query.count()
        items = query.offset((page - 1) * page_size).limit(page_size).all()

    preload_demo_relations(db, items)
    return Paginated(
        items=[serialize_demo(db, d) for d in items],
        total=total,
        page=page,
        page_size=page_size,
        # 只在 random 分支有意义；其他 sort 的 random_seed 恒 None（schema 默认）
        random_seed=random_seed if sort == "random" else None,
    )


@router.get("/by-tasks", response_model=list[DemoSummaryOut])
def demos_by_tasks(
    task_ids: list[int] = Query(default=[], description="题目 id；作品库 demo 组按题取整组"),
    per_task_limit: int = Query(default=100, ge=1, le=300, description="单题最多返回件数"),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    """按题目批量取**整组**作品（作品库「组一出现就必须整组可见」用）。

    为什么需要这个口：主列表按页切片返回，一个同题组会被页边界切开；前端只有拿到整组，
    才能把堆叠卡画完整、并算出正确的组级排序值（组内最近更新 / 浏览最多 / 最高分）。

    可见性口径与主列表一致：只出 approved + 当前视区的 scope 过滤。
    **刻意不过滤搜索/标签条件**——组的语义是「这道题的全部作品」：组因某个成员命中筛选而
    出现时，整组一起出现（这是与「按筛选条件再筛一次」不同的产品口径）。
    """
    ids = sorted({int(i) for i in task_ids if int(i) > 0})[:20]
    if not ids:
        return []
    rows = (
        db.query(Demo, DemoTask.task_id)
        .join(DemoTask, DemoTask.demo_id == Demo.id)
        .filter(DemoTask.task_id.in_(ids), Demo.status == "approved")
        .filter(scope_contains_filter(scope))
        .order_by(Demo.created_at.desc(), Demo.id.desc())
        .all()
    )
    per: dict[int, list[Demo]] = {tid: [] for tid in ids}
    seen: set[int] = set()
    for demo, tid in rows:
        if tid not in per:
            continue
        # 一个作品可能挂多个题：只出一次（前端按同一套单归属规则重新分组）
        if demo.id in seen or len(per[tid]) >= per_task_limit:
            continue
        seen.add(demo.id)
        per[tid].append(demo)
    # 合并去重（保持查出来的顺序），并给个总量上限防滥用
    merged: dict[int, Demo] = {}
    for tid in ids:
        for d in per[tid]:
            merged.setdefault(d.id, d)
    items = list(merged.values())[:2000]
    preload_demo_relations(db, items)
    return [serialize_demo(db, d) for d in items]


@router.get("/{slug}", response_model=DemoDetailOut)
def get_demo(
    slug: str,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
    scope: str = Depends(get_scope),
):
    demo = _find_demo(db, slug)
    # 可见域外 = 不存在（错误文案一致，不泄露策展池外作品的存在性）
    if not demo_in_scope(demo, scope):
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    # 计数走内存批次 + 30s 落库（services/counters.py）：读路径零写事务，
    # 不再有 "database is locked" 500，也不再触发 updated_at 刷新打穿预览缓存。
    # 这里对 ORM 对象 +1 只为了让本次响应数字新鲜；不 commit，会话关闭即丢弃。
    counters.bump("demo_view", demo.id)
    demo.view_count += 1
    return serialize_demo(db, demo, user.id if user else None, detail=True)


@router.get("/{slug}/meta", response_model=DemoMetaOut)
def demo_meta(slug: str, db: Session = Depends(get_db), scope: str = Depends(get_scope)):
    """轻量作品 meta（富卡片用）：不增加 view_count。"""
    demo = db.query(Demo).filter(Demo.slug == slug, Demo.status == "approved", scope_contains_filter(scope)).first()
    if demo is None:
        raise HTTPException(status_code=404, detail="Demo 不存在或未上线", )
    author = demo.author.username if demo.author else (demo.guest_name or "public")
    return DemoMetaOut(slug=demo.slug, title=demo.title, cover_url=demo.cover_url, author=author)


@router.get("/{slug}/same-prompt", response_model=SamePromptOut)
def same_prompt_demos(
    slug: str,
    limit: int = Query(default=12, ge=1, le=50),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    """同提示词的其他作品（v2 B2′）：prompt_id 精确共享 = 严格复现对比，零 Task 依赖。

    与 Task 构成粗细两档：同 prompt = 同一句话交给不同模型；同 task = 同一题材。
    """
    current = _find_demo(db, slug)
    if not demo_in_scope(current, scope):
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    prompt = (current.prompt or "").strip()
    if not prompt:
        return SamePromptOut(prompt="", prompt_id=None, items=[])

    query = db.query(Demo).filter(
        Demo.status == "approved",
        Demo.id != current.id,
        scope_contains_filter(scope),
    )
    if current.prompt_id is not None:
        query = query.filter(Demo.prompt_id == current.prompt_id)
    else:
        # 兜底：历史行尚未回填 prompt_id（迁移脚本未跑）时按文本比对。
        # 只做 lower/trim 的近似归一，完整规范化靠 migrate_models_v2 回填后走索引。
        query = query.filter(
            func.lower(func.trim(Demo.prompt)) == prompt.lower(),
            Demo.prompt_id.is_(None),
        )
    rows = query.order_by(Demo.rating_avg.desc(), Demo.created_at.desc(), Demo.id.desc()).limit(limit).all()
    preload_demo_relations(db, rows)
    return SamePromptOut(
        prompt=prompt,
        prompt_id=current.prompt_id,
        items=[serialize_demo(db, d) for d in rows],
    )


@router.get("/{slug}/related", response_model=list[DemoSummaryOut])
def related_demos(
    slug: str,
    limit: int = Query(default=30, ge=1, le=50),
    db: Session = Depends(get_db),
    scope: str = Depends(get_scope),
):
    """相关推荐候选池：标签重合度 + 同类型 + 热度 + 随机（60s 缓存按视区分键）。"""
    current = _find_demo(db, slug)
    if not demo_in_scope(current, scope):
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    cache_key = f"{scope}:{slug}"
    cached = _related_cache_get(cache_key)
    if cached is None:
        cached = _compute_related(db, slug, scope)
        _related_cache_set(cache_key, cached)
    return cached[:limit]


def _compute_related(db: Session, slug: str, scope: str) -> list:
    """计算相关推荐（结果已序列化为字典；锁外执行 DB 查询）。

    KB-19：候选集一次取回后用 `preload_demo_relations` 批量预载标签/模型，
    不再对每行懒加载 `tag_associations`（637 件规模冷启动会放大成 600+ 次查询）。
    """
    current = _find_demo(db, slug)
    cur_tags = {f"{dt.tag.key}:{dt.tag.value}" for dt in current.tag_associations}
    cur_type = current.demo_type
    cur_id = current.id

    core_weight = {"type": 3, "game": 3, "model": 2, "category": 2,
                   "plugin": 1, "skills": 1, "preset": 1, "rounds": 1}

    rows = db.query(Demo).filter(
        Demo.status == "approved", Demo.id != cur_id, scope_contains_filter(scope)
    ).all()
    preload_demo_relations(db, rows)
    scored: list[tuple[float, Demo]] = []
    for d in rows:
        d_tags = {f"{t['key']}:{t['value']}" for t in getattr(d, "_pre_tags", [])}
        shared = cur_tags & d_tags
        if not shared and d.demo_type != cur_type:
            # 完全无关的弱推荐：给很低的保底分，保证池子不至于空
            score = random.random() * 0.3
        else:
            score = sum(core_weight.get(k.split(":", 1)[0], 1) for k in shared)
            if d.demo_type == cur_type:
                score += 0.5
            score += (d.view_count + 2 * d.download_count) * 0.001
            score += random.random() * 0.5
        scored.append((score, d))

    scored.sort(key=lambda x: -x[0])
    top = [d for _, d in scored[:50]]
    preload_demo_relations(db, top)
    return [serialize_demo(db, d) for d in top]


def _validate_demo_type(t: str) -> str:
    if t not in ("web", "zip", "link", "image"):
        raise HTTPException(status_code=422, detail="demo_type 需为 web（网页应用）/ zip（文件包）/ link（外部链接）/ image（图像作品）", )
    return t


def _image_file_ext(filename: str | None) -> str | None:
    """图像作品白名单后缀 → (扩展名, ...)；非图像返回 None。内容真伪交给
    compress_cover 的 Pillow 校验（损坏/伪扩展名 → 400；像素超限 → 413），与封面同口径。"""
    if not filename:
        return None
    name = filename.lower()
    for ext in ("png", "jpg", "jpeg", "webp"):
        if name.endswith("." + ext):
            return ext
    return None


def _single_file_ext(filename: str | None) -> str | None:
    """按后缀识别单文件模式：.html/.htm → html，.svg → svg，否则 None。"""
    if not filename:
        return None
    name = filename.lower()
    if name.endswith(".html") or name.endswith(".htm"):
        return "html"
    if name.endswith(".svg"):
        return "svg"
    return None


def _validate_single_file(data: bytes, ext: str) -> None:
    head = data[:512].lstrip().lower()
    if ext == "html":
        if not (head.startswith(b"<!doctype html") or head.startswith(b"<html")):
            raise HTTPException(status_code=400, detail="单 HTML 文件内容不是有效 HTML（需以 <!doctype html> 或 <html 开头）", )
    else:
        if not head.startswith(b"<svg"):
            raise HTTPException(status_code=400, detail="单 SVG 文件内容不是有效 SVG（需以 <svg 开头）", )


def _clean_url(url: str | None) -> str | None:
    """可选链接字段：空则 None，非空必须是 http(s)。"""
    if not url or not url.strip():
        return None
    url = url.strip()
    if not (url.startswith("http://") or url.startswith("https://")):
        raise HTTPException(status_code=422, detail="链接需为 http(s) 地址", )
    if len(url) > 2000:
        raise HTTPException(status_code=422, detail="链接过长", )
    return url


def _validate_url(url: str | None, field: str) -> str:
    """必填链接字段（link 类型的 external_url）。"""
    cleaned = _clean_url(url)
    if cleaned is None:
        raise HTTPException(status_code=422, detail=f"{field} 为链接类型必填", )
    return cleaned


def _assert_public_url(url: str) -> None:
    """SSRF 防护（KB-12）：只放行 http/https + 白名单端口，且解析结果不得是内网/回环/保留地址。

    注意：本函数是**逐跳**调用的（见 `_SafeRedirectHandler`），重定向目标同样要过这一关。
    """
    parsed = urlparse(url)
    if parsed.scheme not in ("http", "https"):
        raise HTTPException(status_code=422, detail="只允许 http/https 地址", )
    try:
        port = parsed.port
    except ValueError:
        raise HTTPException(status_code=422, detail="无效的下载地址端口", )
    if port is not None and port not in ALLOWED_FETCH_PORTS:
        raise HTTPException(status_code=422, detail=f"不允许的端口：{port}", )
    host = parsed.hostname
    if not host:
        raise HTTPException(status_code=422, detail="无效的下载地址", )
    try:
        infos = socket.getaddrinfo(host, None)
    except socket.gaierror:
        raise HTTPException(status_code=400, detail="无法解析下载地址", )
    for info in infos:
        try:
            ip = ipaddress.ip_address(info[4][0])
        except ValueError:
            continue
        if (
            ip.is_private
            or ip.is_loopback
            or ip.is_link_local
            or ip.is_reserved
            or ip.is_multicast
            or ip.is_unspecified
        ):
            raise HTTPException(status_code=422, detail="不允许下载内网/保留地址", )


class _SafeRedirectHandler(urllib.request.HTTPRedirectHandler):
    """重定向逐跳复查（KB-12）。

    默认 opener 会静默跟随 302/307，`_assert_public_url` 只校验了首跳 ——
    攻击者用一个公网域名 302 到 169.254.169.254/内网管理接口即可绕过。
    """

    max_redirections = 5

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        _assert_public_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


_SAFE_OPENER = urllib.request.build_opener(_SafeRedirectHandler)


def _download_url_bytes(url: str, limit: int, what: str) -> bytes:
    """从 URL 下载字节（逐跳 SSRF 校验 + 大小上限 + 超时）。"""
    url = _clean_url(url)
    if url is None:
        raise HTTPException(status_code=422, detail=f"{what} 地址无效", )
    _assert_public_url(url)
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "ds-demos-showcase/1.0"})
        with _SAFE_OPENER.open(req, timeout=60) as resp:
            data = resp.read(limit + 1)
    except urllib.error.HTTPError as e:
        raise HTTPException(status_code=400, detail=f"下载{what}失败: HTTP {e.code}", )
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"下载{what}失败: {e}", )
    if len(data) > limit:
        raise HTTPException(status_code=400, detail=f"{what}超过大小限制", )
    return data


def _oss_upload_safe(slug: str, zip_bytes: bytes | None = None) -> None:
    """上传到 OSS（失败不阻塞：降级本地存储，仅警告）。"""
    try:
        storage.upload_demo_to_oss(slug)
        if zip_bytes is not None:
            oss.put_bytes(
                f"demos/{slug}/{slug}.zip",
                zip_bytes,
                "application/zip",
                extra_headers={"Cache-Control": "no-cache"},
            )
    except Exception as e:  # noqa: BLE001
        print(f"[warn] OSS 上传失败（降级本地存储）: {slug} {e}", flush=True)


def _zip_content_hash(data: bytes) -> str:
    """zip 原始字节 sha256（按作者去重）。"""
    return hashlib.sha256(data).hexdigest()


def _author_scope_id(user: User | None) -> int | None:
    """作者去重作用域：登录用 user.id；匿名（public）共用 None。"""
    return user.id if user else None


def _find_duplicate_demo(
    db: Session,
    content_hash: str,
    author_scope_id: int | None,
    exclude_id: int | None = None,
) -> Demo | None:
    """查找同一作者下内容相同（同 content_hash）的已有 demo。
    匿名（author_scope_id=None）表示所有 public 上传共享同一去重池。"""
    q = db.query(Demo).filter(Demo.content_hash == content_hash)
    if author_scope_id is None:
        q = q.filter(Demo.author_id.is_(None))
    else:
        q = q.filter(Demo.author_id == author_scope_id)
    if exclude_id is not None:
        q = q.filter(Demo.id != exclude_id)
    return q.first()


def _dup_conflict(demo: Demo) -> HTTPException:
    return HTTPException(
        status_code=409,
        detail=f"已存在相同内容的 Demo（同一作者）：/demo/{demo.slug}",
    )


async def _create_demo_record(
    db: Session,
    user: User | None,
    *,
    slug: str,
    title: str,
    description: str,
    tags_raw: str | None,
    demo_type: str,
    external_url: str | None,
    prompt: str,
    video_url: str | None,
    cover_bytes: bytes | None = None,
    cover_ext: str = "png",
    zip_bytes: bytes | None = None,
    trusted: bool = False,
    idempotency_key: str | None = None,
    content_hash: str | None = None,
    single_file: str | None = None,
    challenge_task: Task | None = None,
    model_hint: str | None = None,
    propose_task: dict | None = None,
) -> tuple[Demo, str, bool]:
    """创建 Demo 公共流程：落库 → 解压/OSS → 标签 → 公告 → 时间线。
    - user 为空 = 匿名（public 虚拟身份）：author_id=NULL，作者恒为 public
    - trusted（UPLOAD_CODE 匹配）或已登录且 auto_approve → approved
    - 匿名：auto_approve_all 或 auto_approve_public 任一开 → approved，否则 pending
    - idempotency_key：唯一幂等键；并发/重试撞键时返回已有结果（created=False）
    - content_hash：zip/单文件内容哈希（按作者去重，由调用方校验后传入）
    - single_file：'html' | 'svg' 时按单文件保存（zip_bytes 存的是该文件内容）
    - challenge_task：调用方已校验的 active 题目；只落一条 task_match 候选，不直接挂题
    - propose_task：调用方已校验的新题草稿 {title,description,category?}；只落 new_task 候选，不建 Task
    - 解压/OSS/封面上传等阻塞操作放线程池，避免卡死事件循环（批量上传时其他接口还能响应）
    """
    demo = Demo(
        slug=slug,
        title=title,
        description=description,
        demo_type=demo_type,
        external_url=external_url,
        prompt=(prompt or "").strip(),
        model_hint=(model_hint or "").strip()[:500],
        video_url=_clean_url(video_url),
        idempotency_key=idempotency_key,
        content_hash=content_hash,
        single_file=single_file,
    )
    demo.author_id = user.id if user else None

    if trusted:
        status = "approved"
    elif user is not None:
        status = "approved" if get_auto_approve(db) else "pending"
    else:
        status = "approved" if (get_auto_approve(db) or get_auto_approve_public(db)) else "pending"
    demo.status = status

    if cover_bytes:
        # save_cover 返回 (封面, 200px 缩略图, 640px 卡片图)：封面与两个档位的小图一起落库

        # （列表/卡片只加载小图，原图留给详情页与首页 hero；档位定义见 storage.COVER_TIERS）
        demo.cover_url, demo.cover_thumb_url, _cover_card = await asyncio.to_thread(

            storage.save_cover, cover_bytes, cover_ext

        )
    else:
        demo.cover_url = "/media/covers/default.svg"
        demo.cover_thumb_url = ""  # SVG 无法栅格化 → 列表页不渲染图片（不留空洞）

    db.add(demo)
    db.flush()
    try:
        db.commit()
    except IntegrityError:
        # 幂等键冲突：返回已存在的 demo，不重复创建
        db.rollback()
        existing = (
            db.query(Demo).filter(Demo.idempotency_key == idempotency_key).first() if idempotency_key else None
        )
        if existing is None:
            raise
        return existing, existing.status, False

    if single_file:
        # 单文件 demo：直接保存 index.html / index.svg，不解压
        await asyncio.to_thread(storage.save_single_file, slug, single_file, zip_bytes or b"")
        await asyncio.to_thread(_oss_upload_safe, slug)
    elif zip_bytes is not None:
        try:
            await asyncio.to_thread(storage.extract_zip, zip_bytes, slug, require_index=(demo_type == "web"))
        except HTTPException:
            _purge_demo_references(db, demo.id)  # 外键清理：收藏/浏览记录没有 ondelete 级联

            db.delete(demo)
            db.commit()
            shutil.rmtree(storage.demo_dir(slug), ignore_errors=True)
            raise
        await asyncio.to_thread(_oss_upload_safe, slug, zip_bytes)

    _set_demo_tags(db, demo, _parse_tags(tags_raw))
    # v2 双写：提示词实体（规范化去重，demos.prompt 原列保留）
    model_service.set_demo_prompt(db, demo)
    db.commit()

    # KB-10：公告只给已上架作品。待审作品发公告会把 slug 暴露到公开列表（capability URL 失效）。
    # 待审作品在审核通过时由 admin.review_demo 补发。
    if status == "approved":
        _add_publish_announcement(db, demo, user)
    _add_timeline(db, demo.id, "v1", "创建", None)
    # v2 B4′：挑战声明 → 候选（不直接挂题；重活已完成，此处只写一行建议）
    if challenge_task is not None:
        _queue_task_match(db, demo, challenge_task, user)
    # 身份绑定闭环 B：用户提议新题 → 候选（与 task= 挂已有题并存，互不替代）
    if propose_task is not None:
        _queue_new_task(db, demo, propose_task, user)
    db.commit()
    # 待审：通知所有管理员
    if status == "pending":
        admins = db.query(User).filter(User.role == "admin").all()
        for a in admins:
            notification_service.create(
                user_id=a.id,
                type="demo_review",
                actor_id=user.id if user else None,
                demo_slug=slug,
            )
    return demo, status, True


def _uploader_context(
    request: Request,
    user: User | None,
    upload_code: str | None,
) -> bool:
    """匿名上传身份解析：返回 trusted。
    - 已登录：trusted=False（身份用账号）
    - 未登录 + upload_code 匹配：trusted=True（跳过限流、直接放行）
    - 未登录：限流（作者恒为 public）
    """
    if user is not None:
        return False
    trusted = bool(settings.upload_code and upload_code and upload_code.strip() == settings.upload_code)
    if not trusted:
        _anon_rate_limit(request)
    return trusted


# 用户自报「挑战此题」的置信度：低于 AUTO_ACCEPT(0.99) 确保只进收件箱不自动执行，
# 又高于 REVIEW(0.60) 保证在收件箱默认视图可见（NULL 会被阈值过滤，绝不能传 None）。
USER_MATCH_CONFIDENCE = 0.98


def _resolve_challenge_task(db: Session, task_slug: str | None) -> Task | None:
    """挑战题面校验（B4′）：只允许挂到已确认（active）的题目上。

    校验必须发生在**重活之前**（解压/OSS），否则一个拼错的 task 参数就会留下孤儿 demo。
    用户声明不等于挂题权限——任意作品都能塞进 Benchmark 会污染对比，
    因此只生成一条 task_match 候选，管理员 approve 才真正生效。
    """
    slug = (task_slug or "").strip()
    if not slug:
        return None
    task = db.query(Task).filter(Task.slug == slug).first()
    if task is None:
        raise HTTPException(status_code=422, detail=f"挑战的题目不存在：{slug}", )
    if task.status != "active":
        raise HTTPException(status_code=422, detail="该题目尚未确认，暂不能接受挑战", )
    return task


def _queue_task_match(db: Session, demo: Demo, task: Task, user: User | None) -> None:
    """把「我要挑战此题」落成待审候选（同 demo+task 的 pending 自动去重）。"""
    suggestion_service.create(
        db,
        kind="task_match",
        payload={"task_id": task.id, "task_title": task.title, "demo_id": demo.id, "demo_title": demo.title},
        confidence=USER_MATCH_CONFIDENCE,
        source="user",
        demo_id=demo.id,
        ref_id=task.id,
        created_by=user.id if user else None,
    )


def _parse_propose_task(raw: str | None) -> dict | None:
    """解析上传可选字段 propose_task。有字段则 title 必填；非法 422（须在解压/OSS 之前调用）。"""
    if raw is None:
        return None
    text = raw.strip() if isinstance(raw, str) else ""
    if not text:
        raise HTTPException(status_code=422, detail="propose_task 需为 JSON 对象，且 title 必填")
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        raise HTTPException(status_code=422, detail="propose_task 需为 JSON 对象")
    if not isinstance(data, dict):
        raise HTTPException(status_code=422, detail="propose_task 需为 JSON 对象")
    title_raw = data.get("title")
    title = title_raw.strip() if isinstance(title_raw, str) else ""
    if not title:
        raise HTTPException(status_code=422, detail="propose_task.title 必填")
    desc_raw = data.get("description")
    description = desc_raw if isinstance(desc_raw, str) else ("" if desc_raw is None else str(desc_raw))
    cat_raw = data.get("category")
    category = None
    if isinstance(cat_raw, str):
        category = cat_raw.strip() or None
    elif cat_raw is not None:
        category = str(cat_raw).strip() or None
    out: dict = {"title": title, "description": description}
    if category:
        out["category"] = category
    return out


def _queue_new_task(db: Session, demo: Demo, propose: dict, user: User | None) -> None:
    """把「上传时提议新题」落成 pending EntitySuggestion kind=new_task，禁止直建 Task。"""
    payload: dict = {
        "title": propose["title"],
        "description": propose.get("description") or "",
        "demo_ids": [demo.id],
    }
    if propose.get("category"):
        payload["category"] = propose["category"]
    suggestion_service.create(
        db,
        kind="new_task",
        payload=payload,
        confidence=USER_MATCH_CONFIDENCE,
        source="user",
        demo_id=demo.id,
        created_by=user.id if user else None,
    )


def _validate_idempotency_key(key: str | None) -> str | None:
    """校验幂等键：8~128 位字母数字 _ . -；空返回 None。"""
    if not key:
        return None
    key = key.strip()
    if not (8 <= len(key) <= 128):
        raise HTTPException(status_code=422, detail="idempotency_key 长度需为 8~128", )
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", key):
        raise HTTPException(status_code=422, detail="idempotency_key 仅允许字母数字 _ . -", )
    return key


def _existing_demo_by_key(db: Session, key: str | None) -> Demo | None:
    if not key:
        return None
    return db.query(Demo).filter(Demo.idempotency_key == key).first()


@router.post("", status_code=201, response_model=DemoCreateResult)
async def create_demo(
    request: Request,
    title: str = Form(...),
    description: str = Form(""),
    tags: str | None = Form(None),
    demo_type: str = Form("web"),
    external_url: str | None = Form(None),
    prompt: str | None = Form(None),
    video_url: str | None = Form(None),
    upload_code: str | None = Form(None),
    idempotency_key: str | None = Form(None),
    task: str | None = Form(None, description="挑战的题目 slug（v2 B4′，进候选待审）"),
    propose_task: str | None = Form(None, description="提议新题 JSON：title/description/category?（身份绑定闭环 B，进候选待审）"),
    model_hint: str | None = Form(None, description="选了兜底型号时的依据（Q2，可选但鼓励）"),
    force: bool = Form(False),
    cover: UploadFile | None = File(None),
    file: UploadFile | None = File(None),
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    demo_type = _validate_demo_type(demo_type)
    # Q2（D6）：模型必填 —— 不确定就走兜底值，绝不逼人瞎填
    _require_model_tag(tags)
    hint = (model_hint or "").strip()
    idem_key = _validate_idempotency_key(idempotency_key)
    # KB-11：限流必须在读文件之前（旧顺序是先读满 200MB 再限流，内存/CPU 已付出）
    trusted = _uploader_context(request, user, upload_code)
    # 题面先校验：非法 task / propose_task 必须在解压/传 OSS 之前失败，免得留下孤儿 demo
    challenge = _resolve_challenge_task(db, task)
    proposed = _parse_propose_task(propose_task)
    # 幂等：同 key 已成功创建过 → 直接返回已有结果（agent 重试去重，省去重新上传）
    existing = _existing_demo_by_key(db, idem_key)
    if existing is not None:
        return DemoCreateResult(id=existing.id, slug=existing.slug, status=existing.status, created=False)

    # force 仅管理员生效
    allow_force = bool(force) and user is not None and user.role == "admin"
    content_hash = None

    if demo_type == "link":
        ext_url = _validate_url(external_url, "external_url")
        if file is not None and file.filename:
            raise HTTPException(status_code=400, detail="链接类型不需要上传文件", )
        zip_bytes = None
        single_file = None
    elif demo_type == "image":
        # 图像作品：file 即作品本体，同时充当封面（D3：图片本身即封面，忽略额外 cover 参数）
        ext_url = None
        if file is None or not file.filename:
            raise HTTPException(status_code=400, detail="必须上传图像文件", )
        if not _image_file_ext(file.filename):
            raise HTTPException(status_code=400, detail="图像作品仅支持 PNG / JPG / JPEG / WEBP", )
        zip_bytes = None
        single_file = None
        cover_ext = _image_file_ext(file.filename) or "png"
        cover_bytes = await _read_limited(file, settings.max_file_size, "上传超过大小限制")
        # 内容真伪（损坏 / 伪扩展名 / 像素超限）在 _create_demo_record → save_cover → compress_cover 里
        # 统一 400/413，与封面上传同口径，不在这里另写一套 Pillow 校验（避免错误文案分叉）。
        # 内容一致性：同一作者不允许上传相同内容（管理员 force 可跳过）
        content_hash = _zip_content_hash(cover_bytes)
        dup = _find_duplicate_demo(db, content_hash, _author_scope_id(user))
        if dup is not None and not allow_force:
            raise _dup_conflict(dup)
    else:
        ext_url = None
        if file is None or not file.filename:
            raise HTTPException(status_code=400, detail="必须上传文件", )
        single_file = _single_file_ext(file.filename)
        if single_file:
            if demo_type == "zip":
                raise HTTPException(status_code=400, detail="zip 类型需要上传 zip 文件", )
            zip_bytes = await _read_limited(file, settings.max_file_size, "上传超过大小限制")
            _validate_single_file(zip_bytes, single_file)
        else:
            if not file.filename.lower().endswith(".zip"):
                raise HTTPException(status_code=400, detail="必须上传 zip 文件或单个 .html/.svg 文件", )
            zip_bytes = await _read_limited(file, settings.max_upload_size, "上传超过大小限制")
        # 内容一致性：同一作者不允许上传相同内容（管理员 force 可跳过）
        content_hash = _zip_content_hash(zip_bytes)
        dup = _find_duplicate_demo(db, content_hash, _author_scope_id(user))
        if dup is not None and not allow_force:
            raise _dup_conflict(dup)

    if demo_type == "image":
        pass  # 作品图片已在上面读入 cover_bytes，这里不再重置（顺序陷阱，见 K3 交接）
    else:
        cover_bytes = None
        cover_ext = "png"
        if cover is not None and cover.filename:
            cover_ext = Path(cover.filename or "").suffix.lstrip(".") or "png"
            cover_bytes = await _read_limited(cover, settings.max_file_size, "封面文件过大")

    demo, status, created = await _create_demo_record(
        db, user,
        slug=_unique_slug(db, title),
        title=title.strip(),
        description=description,
        tags_raw=tags,
        demo_type=demo_type,
        external_url=ext_url,
        prompt=prompt or "",
        video_url=video_url,
        cover_bytes=cover_bytes,
        cover_ext=cover_ext,
        zip_bytes=zip_bytes,
        trusted=trusted,
        idempotency_key=idem_key,
        content_hash=content_hash,
        single_file=single_file,
        challenge_task=challenge,
        model_hint=hint,
        propose_task=proposed,
    )
    return DemoCreateResult(id=demo.id, slug=demo.slug, status=status, created=created)


@router.post("/from-url", status_code=201, response_model=DemoCreateResult)
async def create_demo_from_url(
    request: Request,
    body: DemoFromUrlIn,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    """AI agent 友好：JSON 提交，zip/封面走 URL（后端下载），免 multipart；可匿名上传。
    强制要求简介与至少 1 个标签，保证 AI 上传的信息质量。"""
    demo_type = _validate_demo_type(body.demo_type)

    if not body.description.strip():
        raise HTTPException(status_code=422, detail="description 必填（AI 自动上传需要填写简介）", )
    if not body.tags or len(body.tags) == 0:
        raise HTTPException(status_code=422, detail="tags 至少需要 1 个标签（AI 自动上传需要打适宜标签）", )
    # Q2（D6）：模型必填。agent 不确定时同样有正门（unspecified / <vendor>-unknown / ds-unknown），
    # 否则强制只会让它们编一个型号出来 —— 那比空着更糟。
    _require_model_tags(body.tags)
    hint = (body.model_hint or "").strip()

    idem_key = _validate_idempotency_key(body.idempotency_key)
    # KB-11：限流必须在下载之前（旧顺序是先抓完 zip/封面再限流，出站带宽与内存已付出）
    trusted = _uploader_context(request, user, body.upload_code)
    # 幂等：同 key 已创建 → 直接返回已有结果（agent 超时重试不再重复上传）
    existing = _existing_demo_by_key(db, idem_key)
    if existing is not None:
        return DemoCreateResult(id=existing.id, slug=existing.slug, status=existing.status, created=False)
    # 题面校验放在下载之前：拼错的 task 不该留下已建好的孤儿 demo
    challenge = _resolve_challenge_task(db, body.task)

    if demo_type == "link":
        ext_url = _validate_url(body.external_url, "external_url")
        if body.zip_url or body.file_url:
            raise HTTPException(status_code=400, detail="链接类型不需要提供 zip_url/file_url", )
        zip_bytes = None
        content_hash = None
        single_file = None
    elif demo_type == "image":
        # 图像作品：file_url 即作品本体 URL（后端下载），同时充当封面（D3，忽略 cover_url）
        ext_url = None
        single_file = None
        zip_bytes = None
        if not body.file_url:
            raise HTTPException(status_code=422, detail="image 类型需要提供 file_url（png/jpg/jpeg/webp）", )
        if body.zip_url:
            raise HTTPException(status_code=422, detail="image 类型请用 file_url，不是 zip_url", )
        if not _image_file_ext(urlparse(body.file_url).path):
            raise HTTPException(status_code=422, detail="image 类型的 file_url 需为 png/jpg/jpeg/webp", )
        cover_ext = _image_file_ext(urlparse(body.file_url).path) or "png"
        cover_bytes = await asyncio.to_thread(_download_url_bytes, body.file_url, settings.max_file_size, "图像")
        # 内容一致性：同一作者不允许上传相同内容（管理员 force 可跳过）
        allow_force = bool(body.force) and user is not None and user.role == "admin"
        content_hash = _zip_content_hash(cover_bytes)
        dup = _find_duplicate_demo(db, content_hash, _author_scope_id(user))
        if dup is not None and not allow_force:
            raise _dup_conflict(dup)
    else:
        ext_url = None
        single_file = None
        if body.file_url:
            if demo_type == "zip":
                raise HTTPException(status_code=422, detail="zip 类型需要提供 zip_url", )
            single_file = _single_file_ext(urlparse(body.file_url).path)
            if not single_file:
                raise HTTPException(status_code=422, detail="file_url 需为 .html/.svg 单文件", )
            zip_bytes = await asyncio.to_thread(_download_url_bytes, body.file_url, settings.max_upload_size, "单文件")
            _validate_single_file(zip_bytes, single_file)
        elif body.zip_url:
            zip_bytes = await asyncio.to_thread(_download_url_bytes, body.zip_url, settings.max_upload_size, "zip")
            if zip_bytes[:2] != b"PK":
                raise HTTPException(status_code=400, detail="zip_url 下载的内容不是 zip 文件", )
        else:
            raise HTTPException(status_code=422, detail="web/zip 类型需要提供 zip_url 或 file_url", )
        # 内容一致性：同一作者不允许上传相同内容（管理员 force 可跳过）
        allow_force = bool(body.force) and user is not None and user.role == "admin"
        content_hash = _zip_content_hash(zip_bytes)
        dup = _find_duplicate_demo(db, content_hash, _author_scope_id(user))
        if dup is not None and not allow_force:
            raise _dup_conflict(dup)

    if demo_type == "image":
        pass  # 图像已在上面下载进 cover_bytes，这里不再重置（顺序陷阱）
    else:
        cover_bytes = None
        cover_ext = "png"
        if body.cover_url:
            cover_bytes = await asyncio.to_thread(_download_url_bytes, body.cover_url, settings.max_upload_size, "封面")
            cover_ext = Path(urlparse(body.cover_url).path).suffix.lstrip(".") or "png"

    tags_raw = json.dumps(body.tags, ensure_ascii=False) if body.tags is not None else None

    demo, status, created = await _create_demo_record(
        db, user,
        slug=_unique_slug(db, body.title),
        title=body.title.strip(),
        description=body.description,
        tags_raw=tags_raw,
        demo_type=demo_type,
        external_url=ext_url,
        prompt=body.prompt,
        video_url=body.video_url,
        cover_bytes=cover_bytes,
        cover_ext=cover_ext,
        zip_bytes=zip_bytes,
        trusted=trusted,
        idempotency_key=idem_key,
        content_hash=content_hash,
        single_file=single_file,
        challenge_task=challenge,
        model_hint=hint,
    )
    return DemoCreateResult(id=demo.id, slug=demo.slug, status=status, created=created)


@router.put("/{slug}", status_code=204)
async def update_demo(
    slug: str,
    title: str | None = Form(None),
    description: str | None = Form(None),
    tags: str | None = Form(None),
    demo_type: str | None = Form(None),
    external_url: str | None = Form(None),
    prompt: str | None = Form(None),
    video_url: str | None = Form(None),
    cover: UploadFile | None = File(None),
    file: UploadFile | None = File(None),
    commit_message: str | None = Form(None),
    keep_old_version: bool = Form(False),
    force: bool = Form(False),
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    demo = _find_demo(db, slug)
    if demo.author_id != user.id and user.role != "admin":
        raise HTTPException(status_code=403, detail="无权修改该 Demo", )

    changed = False
    snapshot: Demo | None = None
    if title is not None and title.strip() != demo.title:
        demo.title = title.strip()
        changed = True
    if description is not None and description != demo.description:
        demo.description = description
        changed = True
    if demo_type is not None:
        new_type = _validate_demo_type(demo_type)
        if new_type != demo.demo_type:
            demo.demo_type = new_type
            changed = True
    if external_url is not None:
        if demo.demo_type == "link":
            demo.external_url = _validate_url(external_url, "external_url")
        else:
            demo.external_url = _clean_url(external_url)
        changed = True
    if prompt is not None:
        demo.prompt = prompt.strip()
        changed = True
    if video_url is not None:
        demo.video_url = _clean_url(video_url)
        changed = True
    if tags is not None:
        _require_model_tag(tags)  # Q2：编辑也不得把模型声明清空
        _set_demo_tags(db, demo, _parse_tags(tags))
        changed = True
    if cover is not None and cover.filename:
        ext = Path(cover.filename).suffix.lstrip(".") or "png"
        cover_bytes = await _read_limited(cover, settings.max_file_size, "封面文件过大")
        demo.cover_url, demo.cover_thumb_url, _cover_card = await asyncio.to_thread(

            storage.save_cover, cover_bytes, ext

        )
        changed = True
    if file is not None and file.filename:
        if demo.demo_type == "link":
            raise HTTPException(status_code=400, detail="链接类型不需要上传文件", )
        if demo.demo_type == "image":
            # 换图：新图即新封面（cover_bytes 交给下一处封面处理块统一 save_cover）
            if not _image_file_ext(file.filename):
                raise HTTPException(status_code=400, detail="图像作品仅支持 PNG / JPG / JPEG / WEBP", )
            ext = _image_file_ext(file.filename) or "png"
            new_img = await _read_limited(file, settings.max_file_size, "上传超过大小限制")
            allow_force = bool(force) and user.role == "admin"
            h = _zip_content_hash(new_img)
            dup = _find_duplicate_demo(db, h, demo.author_id, exclude_id=demo.id)
            if dup is not None and not allow_force:
                raise _dup_conflict(dup)
            # cover_bytes 变量在封面处理块里被读取；此处直接走「换封面」路径换图
            _, save_cover = await asyncio.to_thread(_save_image_cover, new_img, ext)
            demo.cover_url, demo.cover_thumb_url, _ = save_cover
            demo.content_hash = h
            changed = True
        else:
            single_file = _single_file_ext(file.filename)
            if single_file:
                if demo.demo_type == "zip":
                    raise HTTPException(status_code=400, detail="zip 类型需要上传 zip 文件", )
                zip_bytes = await _read_limited(file, settings.max_file_size, "上传超过大小限制")
                _validate_single_file(zip_bytes, single_file)
            else:
                if not file.filename.lower().endswith(".zip"):
                    raise HTTPException(status_code=400, detail="必须上传 zip 文件或单个 .html/.svg 文件", )
                zip_bytes = await _read_limited(file, settings.max_upload_size, "上传超过大小限制")
            # 内容一致性：不更新成与同作者其他 demo 相同的内容（排除自身；管理员 force 可跳过）
            allow_force = bool(force) and user.role == "admin"
            h = _zip_content_hash(zip_bytes)
            dup = _find_duplicate_demo(db, h, demo.author_id, exclude_id=demo.id)
            if dup is not None and not allow_force:
                raise _dup_conflict(dup)
            demo.content_hash = h
            demo.single_file = single_file
            # 勾选「保留旧版本」：先把当前文件快照成独立 demo 页面，再覆盖
            if keep_old_version:
                snapshot = _snapshot_demo(db, demo, user)
            if single_file:
                await asyncio.to_thread(storage.save_single_file, slug, single_file, zip_bytes)
                await asyncio.to_thread(_oss_upload_safe, slug)
            else:
                await asyncio.to_thread(storage.extract_zip, zip_bytes, slug, require_index=(demo.demo_type == "web"))
                await asyncio.to_thread(_oss_upload_safe, slug, zip_bytes)
            changed = True

    demo.updated_at = datetime.utcnow()
    if changed:
        # v2 双写：提示词实体回填（prompt 为空也会清掉 prompt_id）
        model_service.set_demo_prompt(db, demo)
    db.commit()

    if changed:
        message = (commit_message or "更新 demo").strip() or "更新 demo"
        # 作品更新公告：内容即更新说明（不再依赖 git）
        db.add(Announcement(
            type="demo_update",
            title=f"Demo 更新：{demo.title}",
            content=message,
            demo_slug=slug,
            status="published",
            category="demo",
            created_by=user.id,
        ))
        # 轻量时间线：记录本次更新；若保留了旧版本，可点击跳转到旧版页面
        version_count = db.query(func.count(DemoTimeline.id)).filter(DemoTimeline.demo_id == demo.id).scalar() or 0
        _add_timeline(
            db,
            demo.id,
            f"v{version_count + 1}",
            message,
            old_slug=snapshot.slug if snapshot else None,
        )
        db.commit()

    return Response(status_code=204)


def _snapshot_demo(db: Session, demo: Demo, user: User) -> Demo:
    """把当前 demo 的快照复制成独立的新 demo（保留旧版本为单独页面）。"""
    from ..models import Demo as DemoRow

    old_slug = demo.slug
    new_slug = _unique_slug(db, demo.title)

    files_src = storage.demo_files_dir(old_slug)
    sessions_src = storage.demo_sessions_dir(old_slug)
    if demo.demo_type != "link":
        if files_src.exists():
            dst = storage.demo_files_dir(new_slug)
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(files_src, dst)
        if sessions_src.exists():
            dst = storage.demo_sessions_dir(new_slug)
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(sessions_src, dst)

    snapshot = DemoRow(
        slug=new_slug,
        title=demo.title,
        description=demo.description,
        cover_url=demo.cover_url,
        # 版本快照复用同一张封面文件 → 缩略图也必须一起复制，否则快照版在列表页永远没图
        cover_thumb_url=demo.cover_thumb_url,
        demo_type=demo.demo_type,
        external_url=demo.external_url,
        prompt=demo.prompt,
        prompt_id=demo.prompt_id,
        video_url=demo.video_url,
        status=demo.status,
        author_id=demo.author_id,
        content_hash=demo.content_hash,
        single_file=demo.single_file,
    )
    db.add(snapshot)
    db.flush()

    # 复制标签 + 标记旧版本归属
    for dt in db.query(DemoTag).filter(DemoTag.demo_id == demo.id).all():
        db.add(DemoTag(demo_id=snapshot.id, tag_id=dt.tag_id))
    version_tag = _ensure_tag(db, "version-of", old_slug)
    db.add(DemoTag(demo_id=snapshot.id, tag_id=version_tag.id))

    # 复制会话日志记录（文件已随目录复制）
    for log in db.query(SessionLog).filter(SessionLog.demo_id == demo.id).all():
        db.add(SessionLog(
            demo_id=snapshot.id,
            filename=log.filename,
            file_size=log.file_size,
            created_at=log.created_at,
        ))

    # 旧版本页面自己的时间线：可跳回最新版
    _add_timeline(db, snapshot.id, "旧版", "旧版本快照", old_slug=old_slug)

    db.commit()
    _oss_upload_safe(new_slug)
    return snapshot


def _purge_demo_references(db: Session, demo_id: int) -> None:
    """删除 Demo 前清掉**没有 ondelete 级联**的引用行。

    为什么必须显式清：`CollectionItem.demo_id` / `DemoView.demo_id` 两个外键建表时没写 ondelete
    （见 models.py），而 SQLite 开着 `PRAGMA foreign_keys=ON` ⇒ 作品只要被收藏过或被浏览过，
    删除就会 `sqlite3.IntegrityError: FOREIGN KEY constraint failed`（生产 500，2026-09 事故）。
    其余引用表（tags/models/tasks/timeline/session_logs）都是 CASCADE，`TagValueSuggestion` /
    `EntitySuggestion` 是 SET NULL —— **不在这里手动删**（手删会绕过 SET NULL 语义）。

    不改表结构：SQLite 给已有表加外键要重建表，而 `_ensure_*` 那套只会加列；
    显式清理对存量生产库立刻生效。
    """
    db.query(CollectionItem).filter(CollectionItem.demo_id == demo_id).delete(synchronize_session=False)
    db.query(DemoView).filter(DemoView.demo_id == demo_id).delete(synchronize_session=False)


@router.delete("/{slug}", status_code=204)
def delete_demo(slug: str, db: Session = Depends(get_db), user: User = Depends(current_user)):
    demo = _find_demo(db, slug)
    if demo.author_id != user.id and user.role != "admin":
        raise HTTPException(status_code=403, detail="无权删除该 Demo", )
    _purge_demo_references(db, demo.id)  # 外键清理：收藏/浏览记录没有 ondelete 级联

    db.delete(demo)
    db.commit()
    shutil.rmtree(storage.demo_dir(slug), ignore_errors=True)
    storage.delete_demo_from_oss(slug)
    return Response(status_code=204)


@router.get("/{slug}/download")
def download_demo(slug: str, db: Session = Depends(get_db), scope: str = Depends(get_scope)):
    demo = _find_demo(db, slug)
    if not demo_in_scope(demo, scope):
        raise HTTPException(status_code=404, detail="Demo 不存在", )
    if demo.demo_type == "link":
        raise HTTPException(status_code=400, detail="链接类型无下载，请直接访问外部链接", )
    # 计数走内存批次 + 30s 落库：下载路径零写事务（同 get_demo 的 view_count 处理）
    counters.bump("demo_download", demo.id)
    demo.download_count += 1

    # 图像作品：下载 = 拿到的就是压缩后的展示大图（不保留上传原图，避免误导）
    if demo.demo_type == "image":
        if not demo.cover_url.startswith("/media/covers/"):
            raise HTTPException(status_code=404, detail="图像作品无本地封面文件", )
        path = settings.media_path / "covers" / demo.cover_url.rsplit("/", 1)[-1]
        if not path.is_file():
            raise HTTPException(status_code=404, detail="图像作品文件不存在", )
        return FileResponse(
            path,
            media_type="image/webp",
            filename=f"{slug}.webp",
            headers={"Cache-Control": "public, max-age=86400, immutable"},
        )

    # 单文件 demo：直接返回原文件（不打包 zip）
    if demo.single_file:
        name = "index.html" if demo.single_file == "html" else "index.svg"
        path = storage.demo_files_dir(slug) / name
        if not path.is_file():
            raise HTTPException(status_code=404, detail="Demo 文件不存在", )
        media = "text/html" if demo.single_file == "html" else "image/svg+xml"
        return FileResponse(path, media_type=media, filename=f"{slug}.{demo.single_file}")

    # OSS 已启用且非「本地服务」模式：302 到 OSS 公有读地址，不占服务器带宽
    if oss.enabled() and not settings.oss_serve_local:
        return RedirectResponse(
            oss.public_url(f"demos/{slug}/{slug}.zip"),
            headers={"Cache-Control": "public, max-age=60"},
        )

    files_dir = storage.demo_files_dir(slug)
    if not files_dir.exists():
        raise HTTPException(status_code=404, detail="Demo 文件不存在", )

    tmp_dir = settings.demos_path / ".tmp_download"
    tmp_dir.mkdir(parents=True, exist_ok=True)
    zip_path = tmp_dir / f"{slug}-{uuid.uuid4().hex}.zip"
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for p in files_dir.rglob("*"):
            if p.is_file():
                zf.write(p, p.relative_to(files_dir))

    from starlette.background import BackgroundTask

    return FileResponse(
        zip_path,
        media_type="application/zip",
        filename=f"{slug}.zip",
        background=BackgroundTask(os.remove, zip_path),
    )
