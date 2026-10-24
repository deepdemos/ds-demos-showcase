"""模型实体服务（v2）：CRUD / 别名 / 合并 / 状态机 / 双写同步 / 聚合统计。

聚合不做冗余计数列（落地计划 §6.1）：demo 数与社区分全部 GROUP BY 现算，
规模阈值 ~5 万条再议物化。

写路径约定（评审与重排.md §八「写操作全走 service」）：
- 一切改变实体的操作都经本 service，admin 路由只做鉴权 + 转调；
- 每个写操作**同事务**落一条 audit_log（审计失败即连带回滚业务）；
- merge 强制防呆：不成环、目标不得是已退役实体（验收「canonical 关系不成环」）。
"""

import hashlib

from fastapi import HTTPException
from sqlalchemy import case, func
from sqlalchemy.orm import Session

from ..models import AuditLog, Demo, DemoModel, DemoTask, DemoTag, Model, ModelAlias, Tag, Task
from . import audit_service, cluster_service, matching_service, tag_service

_UNVERIFIED_NAMES = {"ds-unknown", "unknown"}  # 灰测模型：建实体即 unverified
# 必须同样规范化后再比对：normalize 会吃掉分隔符，直接拿原始写法比会让 ds-unknown
# 被判成普通模型（仿真线上数据时实测到：396 个灰测作品全部漏掉 unverified 归属）。
_UNVERIFIED_NORM = {matching_service.normalize(n) for n in _UNVERIFIED_NAMES}

# 「未标注模型」实体：上传时不指定 model 的作品归这里（评审与重排.md §四 idea 7 异议裁决）。
# 刻意与 ds-unknown（网传灰测）分开——后者是「灰测揭晓」机制的资产池，
# 混入「懒得填」会让将来揭晓时无法区分两类来源。
UNSPECIFIED_SLUG = "unspecified"
UNSPECIFIED_NAME = "unspecified"

MODEL_STATUSES = ("candidate", "active", "unverified", "deprecated")

# 断言强度（Q2 决议）：exact 精确型号 / family 知厂商不知型号 / unknown 完全不知 / guess 猜测未证实
RESOLUTIONS = ("exact", "family", "unknown", "guess")
FALLBACK_RESOLUTIONS = ("family", "unknown", "guess")

# run 语义标签 → demo 列（v2 B5′）：这三个键本质是「一次生成过程」的属性，不是描述性标签
RUN_META_KEYS = {"rounds": "gen_rounds", "time": "gen_minutes", "platform": "gen_platform"}


def family_slug(vendor: str) -> str:
    """厂商族节点的标签值：`DeepSeek` → `deepseek-unknown`。

    厂商名是纯中文（slugify 后为空）时用确定性哈希后缀，**不能**统一退化成
    `vendor-unknown` —— 那会让第二个中文厂商把第一个的族节点改指向自己。
    """
    base = matching_service.slugify(vendor)
    if not base:
        base = "vendor-" + hashlib.sha1(vendor.strip().encode("utf-8")).hexdigest()[:6]
    return f"{base}-unknown"


def is_fallback(model: Model | None) -> bool:
    return model is not None and model.resolution in FALLBACK_RESOLUTIONS


def infer_resolution(name: str, vendor: str | None, slug: str) -> str:
    """按名称/厂商推断断言强度（决定新数据落在 A/B/C/D 哪一档）。"""
    norm = matching_service.normalize(name)
    if slug == UNSPECIFIED_SLUG or norm == matching_service.normalize(UNSPECIFIED_NAME):
        return "unknown"
    if norm in _UNVERIFIED_NORM:
        return "guess"  # ds-unknown：有猜测未证实，留给「揭晓」机制改映射
    if slug.endswith("-unknown") and vendor:
        return "family"  # <vendor>-unknown：知厂商不知型号
    return "exact"


def ensure_tag_value(db: Session, value: str, group: str | None = None, description: str = "") -> Tag:
    """确保 `model:<value>` 这个 fixed 标签值存在（不存在则建）。

    model 是 fixed 键，`_resolve_tag` 只放行词表里已有的值 —— 兜底值必须先进词表，
    否则「我不确定型号」这条合法路径会在上传时被 422 挡死。

    KB-4：Tag 写入统一收口到 tag_service（本函数只是 model 域的便捷入口，保持既有调用面）。
    """
    return tag_service.ensure_value(db, value, group=group, description=description)


def model_tag(db: Session, model: Model) -> Tag | None:
    """实体对应的 `model:<name>` 词表值（可能不存在 —— 词表是选择器口径）。"""
    return db.query(Tag).filter(Tag.key == "model", Tag.value == model.name).first()


def sync_model_tag(db: Session, model: Model) -> Tag | None:
    """实体 → 词表单向补写（KB-2）：保证值存在，且退役状态与实体一致。

    为什么需要它：`model` 是 fixed 键，作者只能选词表里已有的值。管理端新建实体若不同步
    词表，就会造出「实体存在但作者永远选不到」的死值（实测上传 422）。反向（标签 → 实体）
    由 `sync_demo_models` 负责，两侧合起来才是双写。

    审计不在这里单独落行：调用方（create / status_set / merge / unmerge）的实体级审计
    已覆盖本次变化，再补一条只会把 audit_log 灌成噪音。
    """
    tag = model_tag(db, model)
    if tag is None:
        if model.status == "deprecated":
            return None
        return ensure_tag_value(
            db, model.name, group=model.vendor, description=model.description
        )
    # 状态同步也走 tag_service（Tag 写入只在本模块收口，见 KB-4）
    return tag_service.sync_status(db, tag, model.status)


def get_or_create_unspecified(db: Session) -> Model:
    """取/建「完全不知道」兜底实体（幂等；status=active，它是元数据而非待确认的新模型）。"""
    ensure_tag_value(db, UNSPECIFIED_SLUG, description="完全不知道是什么模型做的（合法兜底值）")
    model = db.query(Model).filter(Model.slug == UNSPECIFIED_SLUG).first()
    if model is not None:
        return model
    model = Model(
        slug=UNSPECIFIED_SLUG,
        name=UNSPECIFIED_NAME,
        vendor=None,
        status="active",
        resolution="unknown",
        description="未标注模型：上传时说不清是什么模型做的作品归此，可与「网传灰测」区分",
    )
    db.add(model)
    db.flush()
    add_alias(db, model, UNSPECIFIED_NAME)
    matching_service.invalidate_alias_cache()
    return model


def ensure_family_for_vendor(db: Session, vendor: str) -> Model | None:
    """为厂商建/取「知厂商不知型号」族节点（`<vendor>-unknown`），Tag 与 Model 同步。

    vendor 为空或就是族节点自身时返回 None（避免自指）。
    """
    v = (vendor or "").strip()
    if not v:
        return None
    slug = family_slug(v)
    if matching_service.normalize(v) == "unknown":
        return None
    ensure_tag_value(db, slug, group=v, description=f"{v}（知厂商、不确定具体型号）")
    existing = db.query(Model).filter(Model.slug == slug).first()
    if existing is not None:
        if existing.vendor != v:
            existing.vendor = v
        if existing.resolution != "family":
            existing.resolution = "family"
        return existing
    fam = Model(
        slug=slug,
        name=slug,
        vendor=v,
        status="active",
        resolution="family",
        description=f"{v} 系列 —— 知道是这家，但不确定具体型号",
    )
    db.add(fam)
    db.flush()
    add_alias(db, fam, slug)
    matching_service.invalidate_alias_cache()
    return fam


def ensure_fallback_models(db: Session) -> dict:
    """启动/迁移时兜底齐备：unspecified + 每个已知厂商一个族节点（幂等，约 15 个）。"""
    get_or_create_unspecified(db)  # 确保 unspecified 的 Tag + Model 都在
    vendors = {
        v for (v,) in db.query(Model.vendor).filter(Model.vendor.isnot(None)).all() if v
    } | {
        g for (g,) in db.query(Tag.group).filter(Tag.key == "model", Tag.group.isnot(None)).all() if g
    }
    made = 0
    for v in sorted(vendors):
        exists = db.query(Model.id).filter(Model.slug == family_slug(v)).first()
        ensure_family_for_vendor(db, v)
        if exists is None:
            made += 1
    db.commit()
    return {"families_created": made, "vendors": len(vendors)}


def _slugify_unique(db: Session, name: str) -> str:
    """实体 slug：**保留连字符的可读写法**（`dsv4-flash` → `dsv4-flash`）。

    这里刻意不用 normalize()：normalize 会吃掉分隔符（给别名匹配用），
    若拿它当 slug，`ds-unknown` 会变成 `dsunknown` —— 人与 agent 按模型名拼 URL
    必然 404（仿真 637 条线上数据时实测到）。归一化只用于匹配，不用于对外标识。
    """
    base = matching_service.slugify(name)[:100] or "model"
    slug = base
    i = 1
    while db.query(Model.id).filter(Model.slug == slug).first() is not None:
        i += 1
        slug = f"{base}-{i}"
    return slug


def add_alias(db: Session, model: Model, alias: str) -> bool:
    """加别名（幂等）。返回是否新插入。

    审计由调用方（admin 路由 / merge）统一记录，避免自动双写路径刷屏审计。
    """
    alias = (alias or "").strip()
    if not alias or alias == model.name:
        return False
    exists = db.get(ModelAlias, alias)
    if exists is not None:
        return False
    db.add(ModelAlias(alias=alias, model_id=model.id))
    matching_service.invalidate_alias_cache()
    return True


def get_or_create_model(
    db: Session,
    name: str,
    vendor: str | None = None,
    status: str = "candidate",
    description: str = "",
) -> tuple[Model, bool]:
    """按名称/别名取模型；缺失则新建（默认 candidate，灰测名直接 unverified）。

    幂等性靠 match_model（精确名 → 规范化别名表）保证：同一字符串重复调用
    必然复用同一实体，绝不重复建（评审与重排.md §七.1 点名的必测项）。
    """
    hit = matching_service.match_model(db, name)
    if hit is not None:
        return hit, False
    clean = (name or "").strip()
    slug = _slugify_unique(db, clean)
    resolution = infer_resolution(clean, vendor, slug)
    effective_status = "unverified" if resolution == "guess" else status
    model = Model(
        slug=slug,
        name=clean,
        vendor=(vendor or None),
        status=effective_status,
        resolution=resolution,
        description=description,
    )
    db.add(model)
    db.flush()
    add_alias(db, model, clean)
    # 登记了精确型号 → 顺手保证该厂商的「未定型号」族节点存在（B 档永远可一键选）
    if vendor and resolution == "exact":
        ensure_family_for_vendor(db, vendor)
    # 新建实体后必须显式失效别名缓存：add_alias 在 alias == name 时直接返回不失效，
    # 而别名映射本身含 Model.name —— 不失效会让等价写法（大小写/分隔符差异）在 TTL 内重复建实体。
    matching_service.invalidate_alias_cache()
    return model, True


def sync_run_meta(demo: Demo) -> None:
    """从 run 语义标签派生 demo 列（v2 B5′：`rounds/time/platform` 收编为可排序聚合的列）。

    **标签照旧保留**——`?tag=rounds:3-10` 是已发布的 agent 契约（AI_AGENT_GUIDE §6），
    列只是「可计算的那一份」，两者同源不冲突。同键多值时轮数/耗时取最大（更完整的
    那次生成），平台取第一个非空值。非数字值静默忽略（time 键历史上存在语义不明的脏值）。
    """
    buckets: dict[str, list[str]] = {}
    for dt in demo.tag_associations:
        if dt.tag.key in RUN_META_KEYS:
            buckets.setdefault(dt.tag.key, []).append(dt.tag.value or "")

    for key, col in RUN_META_KEYS.items():
        values = [v.strip() for v in buckets.get(key, []) if v and v.strip()]
        if not values:
            setattr(demo, col, None)
            continue
        if col == "gen_platform":
            setattr(demo, col, values[0][:32])
            continue
        nums = []
        for v in values:
            try:
                nums.append(int(float(v)))
            except ValueError:
                continue
        setattr(demo, col, max(nums) if nums else None)


def sync_demo_models(db: Session, demo: Demo, fallback_unspecified: bool = False) -> None:
    """双写：按 demo 的 model 标签同步 demo_models（_set_demo_tags 之后调用）。

    model 标签是双写期的可读源；实体缺失时自动建 candidate（「模型创建尽量自动完成」）。
    自动双写路径**不记审计**（每次上传都跑，会把 audit_log 灌成噪音）；
    只有「新建了 candidate 实体」这一不可逆变化值得记，交给调用方按需补。

    fallback_unspecified：无 model 标签时挂「未标注」实体（D6 替代方案）。
    默认 False = 保持 B1 行为不变，待该决策确认后在 demos.py 显式开启。
    """
    # 必须先 flush：SessionLocal 是 autoflush=False，_set_demo_tags 里刚 db.add 的
    # DemoTag 行仍停留在 session 内；不 flush 就读 demo.tag_associations 会走懒加载
    # 拿「已落库」的旧快照 —— 标签首次创建时靠 _resolve_tag 里的 flush 侥幸躲过，
    # 标签已存在（第二次起的常规上传）时集合为空，模型实体会被静默漏挂。
    db.flush()
    pairs = [
        (dt.tag.value, dt.tag.group)
        for dt in demo.tag_associations
        if dt.tag.key == "model"
    ]
    db.query(DemoModel).filter(DemoModel.demo_id == demo.id).delete()
    if not pairs and fallback_unspecified:
        model = get_or_create_unspecified(db)
        db.add(DemoModel(demo_id=demo.id, model_id=model.id))
        return
    # 防重必须**在本次运行内自记**：SessionLocal 是 autoflush=False，"先查再插"
    # 看不到本事务里刚 db.add 的行 —— 同一 demo 挂两种写法（dsv4-flash / DSV4-Flash）
    # 经 normalize 落到同一 Model 实体时，同一 (demo, model) 会被插两遍，
    # 等到 flush 才炸 UNIQUE（与迁移脚本踩到的是同一个坑，真实语料已验证）。
    # 上方已 delete 掉该 demo 的全部旧链接，这里只需对本次运行去重。
    seen_models: set[int] = set()
    for v, group in pairs:
        # group 必须当 vendor 传下去：族节点 <vendor>-unknown 的 resolution 判定依赖它
        model, _created = get_or_create_model(db, v, vendor=group)
        if model.id in seen_models:
            continue
        seen_models.add(model.id)
        db.add(DemoModel(demo_id=demo.id, model_id=model.id))


def set_demo_prompt(db: Session, demo: Demo) -> None:
    """把 demo.prompt 规范化去重写入 prompts 并回填 prompt_id（demos.prompt 原列保留双写）。

    必须复用 get_or_create_prompt（而非直插）：并发唯一约束竞争由保存点兜底，
    且 FTS 搜索索引在写入路径内同步——直插会让上传/编辑的作品正文在 ≥3 字符搜索下静默漏结果。
    """
    content = (demo.prompt or "").replace("\r\n", "\n").strip()
    if not content:
        demo.prompt_id = None
        return
    from .prompt_library_service import get_or_create_prompt

    row = get_or_create_prompt(db, content)
    demo.prompt_id = row.id
    # 语料变了（新增/改提示词）→ 聚类缓存作废，管理面板不会看到过期簇
    cluster_service.invalidate()


def _find_model(db: Session, ident: str | int) -> Model | None:
    """唯一的实体解析路径：id → slug → 别名 → 名称。

    公开详情、admin 详情、合并/撤销/别名都必须走这里 —— 之前路由与 service 各写了一遍
    `Model.slug == slug` 查询，改 slug 后旧链接能不能救回来就变成了看运气。

    name 兜底（P-06）：词表固定值存的是实体 **name**（`sync_model_tag` 以 `model.name` 写
    `model:<name>`），而 name 与 slug 常不同（`GLM-4.5` → slug `glm-4-5`）。上传页
    「站内战绩」拿词表值直接调 `GET /models/{词表值}`，缺这条兜底会 404 被静默吞掉。
    name 列是 unique，且 `match_model` 早已按 name 精确匹配，这里与之一致。
    """
    model = None
    if isinstance(ident, int) or str(ident).isdigit():
        model = db.get(Model, int(ident))
    if model is None:
        model = db.query(Model).filter(Model.slug == str(ident)).first()
    if model is None:
        row = db.get(ModelAlias, str(ident))
        if row is not None:
            model = db.get(Model, row.model_id)
    if model is None:
        model = db.query(Model).filter(Model.name == str(ident)).first()
    return model


def get_model_or_404(db: Session, ident: str | int) -> Model:
    """按 id / slug / 别名 取实体（找不到即 404）。

    别名兜底是刻意的：改 slug 或合并后，旧写法仍要能解析到当前实体 ——
    这正是 `model_aliases` 存在的意义（历史标签、外部贴出去的旧链接都不该突然 404）。
    """
    model = _find_model(db, ident)
    if model is None:
        raise HTTPException(status_code=404, detail="模型不存在")
    return model


# ---------------- 聚合查询 ----------------


def _agg_subqueries(db: Session):
    """模型级聚合：作品数、**等权均分（raw，保留旧语义）**、总票数、票数加权和。

    votes/wsum 是社区分（收缩均值）的原料 —— 只在这里算一次，别处不再各写一套。
    """
    demo_count = (
        db.query(DemoModel.model_id, func.count(Demo.id).label("c"))
        .join(Demo, Demo.id == DemoModel.demo_id)
        .filter(Demo.status == "approved")
        .group_by(DemoModel.model_id)
        .subquery()
    )
    rating_avg = (
        db.query(
            DemoModel.model_id,
            func.avg(Demo.rating_avg).label("a"),
            func.coalesce(func.sum(Demo.rating_count), 0).label("votes"),
            func.coalesce(func.sum(Demo.rating_avg * Demo.rating_count), 0.0).label("wsum"),
        )
        .join(Demo, Demo.id == DemoModel.demo_id)
        .filter(Demo.status == "approved", Demo.rating_count > 0)
        .group_by(DemoModel.model_id)
        .subquery()
    )
    return demo_count, rating_avg


def score_prior(db: Session) -> tuple[float, float]:
    """收缩基准 `(C, m)`：全站先验分与收缩强度。

    C = 全站按票数加权的整体均分（先验：没证据时假定它是这个水平）
    m = 各模型票数的**中位数**（自适应门槛：全站样本普遍变多，门槛自动抬高）

    中位数而不是平均数：票数分布长尾（一件爆款就能拉高均值），中位数才代表"典型模型的
    证据量"。空库退化为 m=1，等价于不收缩。
    """
    import statistics

    rows = (
        db.query(func.coalesce(func.sum(Demo.rating_count), 0))
        .join(DemoModel, DemoModel.demo_id == Demo.id)
        .filter(Demo.status == "approved", Demo.rating_count > 0)
        .group_by(DemoModel.model_id)
        .all()
    )
    votes_list = [int(r[0]) for r in rows if r and r[0]]
    total_votes, total_wsum = db.query(
        func.coalesce(func.sum(Demo.rating_count), 0),
        func.coalesce(func.sum(Demo.rating_avg * Demo.rating_count), 0.0),
    ).join(DemoModel, DemoModel.demo_id == Demo.id).filter(
        Demo.status == "approved", Demo.rating_count > 0
    ).one()
    C = (float(total_wsum) / float(total_votes)) if total_votes else 0.0
    m = max(1.0, float(statistics.median(votes_list))) if votes_list else 1.0
    return round(C, 4), m


def shrink_score(votes: int, wsum: float, prior: tuple[float, float]) -> float | None:
    """经验贝叶斯收缩分（IMDB weighted rating 同族），闭式：

        score = (wsum + m·C) / (votes + m)

    推导：v/(v+m)·(wsum/v) + m/(v+m)·C —— 代数化简后不必除两次，SQL 也能直接排序。
    性质：票数少 → 拉回全站先验 C；票数多 → 逼近自身真实加权均分。
    零票返回 None（"没有证据"不等于"分数是 0"）。
    """
    C, m = prior
    if not votes:
        return None
    return round((float(wsum) + m * C) / (float(votes) + m), 2)


def sample_level(votes: int) -> str:
    """样本可信度：让读者自己判断这个分能不能信，而不是替它保密。"""
    if not votes:
        return "none"
    if votes < 10:
        return "low"
    if votes < 50:
        return "mid"
    return "high"


def _model_out(
    model: Model,
    demo_count: int,
    rating_avg: float | None,
    votes: int = 0,
    wsum: float = 0.0,
    prior: tuple[float, float] | None = None,
    score_sql: float | None = None,
) -> dict:
    if prior is None:
        prior = (0.0, 1.0)
    # KB-8：列表路径把 SQL 排序键原样当展示值（排序与展示同源）；详情等路径没传
    # score_sql 时退回 Python 同公式计算。
    score = (
        round(float(score_sql), 2)
        if (votes and score_sql is not None)
        else shrink_score(int(votes or 0), float(wsum or 0.0), prior)
    )
    return {
        "id": model.id,
        "slug": model.slug,
        "name": model.name,
        "vendor": model.vendor,
        "status": model.status,
        "resolution": model.resolution,
        "description": model.description,
        "demo_count": demo_count,
        "rating_avg": round(float(rating_avg), 2) if rating_avg is not None else None,
        # score=收缩后的社区分（对外排序与展示用它）；rating_avg 保留等权旧语义
        "score": score,
        "votes": int(votes or 0),
        "sample_level": sample_level(int(votes or 0)),
        "created_at": model.created_at,
    }


def is_fallback_value(value: str | None) -> bool:
    """标签值层面判定兜底位（无需查库）：unspecified / <vendor>-unknown / ds-unknown / unknown。

    用于「热门模型」榜单折叠 —— 兜底位不是型号，让它上榜等于给一个空概念排名。
    """
    v = matching_service.normalize(value or "")
    return v in {"unspecified", "unknown", "dsunknown"} or v.endswith("unknown")


def list_models(
    db: Session,
    status: str | None = None,
    vendor: str | None = None,
    q: str | None = None,
    sort: str = "demos",
    page: int = 1,
    page_size: int = 20,
    exclude_fallback: bool = False,
) -> tuple[list[dict], int]:
    """公开模型列表。status 缺省展示 active+unverified（candidate 仅 admin 渠道可见）。

    exclude_fallback=True 用于「热门模型」类榜单：兜底位（family/unknown/guess）不参与排名，
    由调用方另行为其显示一个「其他 / 未定 N 个」的折叠行。
    """
    dc, ra = _agg_subqueries(db)
    prior = score_prior(db)
    C, m = prior
    demo_count_col = func.coalesce(dc.c.c, 0)
    rating_col = func.coalesce(ra.c.a, 0.0)
    votes_col = func.coalesce(ra.c.votes, 0)
    wsum_col = func.coalesce(ra.c.wsum, 0.0)
    # 收缩分的 SQL 形态：(wsum + m·C) / (votes + m) —— 与 shrink_score() 同一公式
    score_col = (wsum_col + m * C) / (votes_col + m)
    # KB-8：排序键取到 2 位小数（与响应里的 score 同精度）。用未取整值排序会让
    # 「都显示 4.41」的两个模型按看不见的尾数排，出现 1 票压过 2 票的观感矛盾。
    # 零票没有分数（shrink_score 返回 None）→ 排序也置 NULL 排最后，
    # 否则它会按先验 C 混在中间，出现「显示没有分，位置却在中游」。
    score_sort_col = case((votes_col > 0, func.round(score_col, 2)), else_=None)

    query = (
        db.query(
            Model,
            demo_count_col.label("demo_count"),
            rating_col.label("rating_avg"),
            votes_col.label("votes"),
            wsum_col.label("wsum"),
            score_sort_col.label("score_sql"),
        )
        .outerjoin(dc, dc.c.model_id == Model.id)
        .outerjoin(ra, ra.c.model_id == Model.id)
    )
    if status:
        query = query.filter(Model.status == status)
    else:
        query = query.filter(Model.status.in_(("active", "unverified")))
    if exclude_fallback:
        query = query.filter(Model.resolution == "exact")
    if vendor:
        query = query.filter(Model.vendor == vendor)
    if q:
        like = f"%{q}%"
        alias_ids = db.query(ModelAlias.model_id).filter(ModelAlias.alias.ilike(like))
        query = query.filter(Model.name.ilike(like) | Model.slug.ilike(like) | Model.id.in_(alias_ids))

    total = query.count()
    if sort in ("score", "rating"):  # rating 是旧参数名，现在按收缩分排（同分比票数）
        query = query.order_by(score_sort_col.desc().nullslast(), votes_col.desc(), Model.id.asc())
    elif sort == "votes":
        query = query.order_by(votes_col.desc(), Model.id.asc())
    elif sort == "new":
        query = query.order_by(Model.created_at.desc(), Model.id.desc())
    elif sort == "name":
        query = query.order_by(Model.name.asc())
    else:  # demos
        query = query.order_by(demo_count_col.desc(), Model.id.asc())
    rows = query.offset((page - 1) * page_size).limit(page_size).all()

    # score_sql 是 SQL 里算出的排序键（2 位小数）：直接用它当展示值，保证
    # 「看到的顺序」与「看到的数字」出自同一个值（KB-8）
    items = [_model_out(m_, c, a, v, w, prior, score_sql=s) for m_, c, a, v, w, s in rows]
    return items, total


def fallback_demo_count(db: Session) -> int:
    """兜底位（family/unknown/guess）挂的已上架作品数 —— 「其他/未定 N」折叠行。"""
    return (
        db.query(func.count(func.distinct(DemoModel.demo_id)))
        .join(Demo, Demo.id == DemoModel.demo_id)
        .join(Model, Model.id == DemoModel.model_id)
        .filter(Demo.status == "approved", Model.resolution.in_(FALLBACK_RESOLUTIONS))
        .scalar()
        or 0
    )


def vendors(db: Session) -> list[str]:
    """已确认实体里出现过的厂商清单（Explore / 列表页筛选条数据源）。"""
    rows = (
        db.query(Model.vendor)
        .filter(Model.status.in_(("active", "unverified")), Model.vendor.isnot(None))
        .distinct()
        .all()
    )
    return sorted({r[0] for r in rows if r[0]})


def tag_distribution(db: Session, model_id: int, key: str, limit: int = 6) -> list[dict]:
    """该模型已上架 demo 中某标签键的值分布（§11 行为档案：常见类型/常见玩法）。"""
    rows = (
        db.query(Tag.value, func.count(func.distinct(Demo.id)))
        .join(DemoTag, DemoTag.tag_id == Tag.id)
        .join(Demo, Demo.id == DemoTag.demo_id)
        .join(DemoModel, DemoModel.demo_id == Demo.id)
        .filter(DemoModel.model_id == model_id, Demo.status == "approved", Tag.key == key)
        .group_by(Tag.value)
        .order_by(func.count(func.distinct(Demo.id)).desc())
        .limit(limit)
        .all()
    )
    return [{"value": v, "demos": c} for v, c in rows]


def model_detail(db: Session, ident: str | int) -> dict | None:
    """模型详情：统计 + 最近作品 + 参与题目 + 别名（id/slug/别名 均可解析）。"""
    model = _find_model(db, ident)
    if model is None:
        return None
    dc, ra = _agg_subqueries(db)
    prior = score_prior(db)
    row = (
        db.query(
            func.coalesce(dc.c.c, 0),
            func.coalesce(ra.c.a, 0.0),
            func.coalesce(ra.c.votes, 0),
            func.coalesce(ra.c.wsum, 0.0),
        )
        .select_from(Model)
        .outerjoin(dc, dc.c.model_id == Model.id)
        .outerjoin(ra, ra.c.model_id == Model.id)
        .filter(Model.id == model.id)
        .first()
    )
    if row:
        demo_count, rating_avg, votes, wsum = int(row[0]), row[1], int(row[2]), float(row[3])
    else:
        demo_count, rating_avg, votes, wsum = 0, None, 0, 0.0

    # 参与的题目（该模型已上架 demo 关联的去重任务 + 各自作品数）
    task_rows = (
        db.query(Task, func.count(func.distinct(Demo.id)))
        .join(DemoTask, DemoTask.task_id == Task.id)
        .join(Demo, Demo.id == DemoTask.demo_id)
        .join(DemoModel, DemoModel.demo_id == Demo.id)
        .filter(DemoModel.model_id == model.id, Demo.status == "approved", Task.status == "active")
        .group_by(Task.id)
        .order_by(func.count(func.distinct(Demo.id)).desc())
        .limit(12)
        .all()
    )

    # 别名列表
    aliases = [a.alias for a in db.query(ModelAlias).filter(ModelAlias.model_id == model.id).all()]

    out = _model_out(model, demo_count, rating_avg, votes, wsum, prior)
    # 把先验一起给出：读者能自己验算「为什么 6 票的 4.9 排在 412 票的 4.6 后面」
    out["prior"] = {"C": prior[0], "m": prior[1]}
    out.update(
        {
            "aliases": aliases,
            "tasks": [
                {"id": t.id, "slug": t.slug, "title": t.title, "demo_count": c} for t, c in task_rows
            ],
            "merged_into": model.merged_into_id,
            # §11 行为档案：常见类型 / 常见玩法（技术行待技术标签键建立后接入）
            "type_dist": tag_distribution(db, model.id, "type"),
            "game_dist": tag_distribution(db, model.id, "game"),
        }
    )
    return out


def recent_demos(db: Session, model: Model, limit: int = 12) -> list[Demo]:
    """该模型的最近已上架作品（序列化由调用方做，便于复用预加载）。"""
    return (
        db.query(Demo)
        .join(DemoModel, DemoModel.demo_id == Demo.id)
        .filter(DemoModel.model_id == model.id, Demo.status == "approved")
        .order_by(Demo.created_at.desc(), Demo.id.desc())
        .limit(limit)
        .all()
    )


def model_demos_page(
    db: Session,
    model: Model,
    *,
    sort: str = "newest",
    type_: str | None = None,
    game: str | None = None,
    page: int = 1,
    page_size: int = 24,
) -> tuple[list[Demo], int]:
    """模型页的作品清单（分页 + 排序 + facet 筛选）。

    存在的原因：详情接口原本硬编码 `limit=12`，`ds-unknown` 有 396 件却只能看到 3% ——
    数据层没问题，是展示层把一等实体截断了。排序默认 newest 与旧行为一致，用户可选。
    """
    query = (
        db.query(Demo)
        .join(DemoModel, DemoModel.demo_id == Demo.id)
        .filter(DemoModel.model_id == model.id, Demo.status == "approved")
    )
    if type_:
        query = query.join(DemoTag, DemoTag.demo_id == Demo.id).join(Tag, Tag.id == DemoTag.tag_id).filter(
            Tag.key == "type", Tag.value == type_
        )
    if game:
        query = query.join(DemoTag, DemoTag.demo_id == Demo.id).join(Tag, Tag.id == DemoTag.tag_id).filter(
            Tag.key == "game", Tag.value == game
        )
    total = query.count()
    if sort == "score":
        query = query.order_by(Demo.rating_avg.desc(), Demo.rating_count.desc(), Demo.id.desc())
    elif sort == "popular":
        query = query.order_by((Demo.view_count + Demo.download_count).desc(), Demo.id.desc())
    else:  # newest
        query = query.order_by(Demo.created_at.desc(), Demo.id.desc())
    rows = query.offset((page - 1) * page_size).limit(page_size).all()
    return rows, total


# ---------------- 管理写操作（B1.5：全部同事务落审计） ----------------


def model_status_set(db: Session, model: Model, status: str, actor_id: int | None = None, reason: str = "", commit: bool = True) -> Model:
    """状态机迁移：candidate→active（确认）/ →deprecated（退役）/ 灰测→unverified。"""
    if status not in MODEL_STATUSES:
        raise HTTPException(status_code=422, detail=f"非法状态，可选：{', '.join(MODEL_STATUSES)}")
    before = audit_service.snapshot_model(model)
    model.status = status
    sync_model_tag(db, model)  # KB-2：实体退役/复活同步词表，避免词表留着可选死值
    audit_service.record(
        db,
        action="status_set",
        entity_type="model",
        entity_id=model.id,
        actor_id=actor_id,
        before=before,
        after=audit_service.snapshot_model(model),
        reason=reason or f"状态置为 {status}",
    )
    if commit:
        db.commit()
    return model


def model_update(db: Session, model: Model, actor_id: int | None = None, reason: str = "", **fields) -> Model:
    """改实体基本字段（name/vendor/description/slug）。改名或改 slug 都会把旧值转成别名。
    M3-B1 契约 §31.1：PATCH 补丁体的 reason 是审计元数据——空缺省回既有默认语。"""
    before = audit_service.snapshot_model(model)
    old_name = model.name
    for f in ("vendor", "description"):
        if f in fields and fields[f] is not None:
            setattr(model, f, fields[f])
    if fields.get("name") and fields["name"].strip() != model.name:
        model.name = fields["name"].strip()
    if "vendor" in fields and fields["vendor"] is None:
        model.vendor = None

    slug_changed = False
    new_slug = (fields.get("slug") or "").strip()
    if new_slug and new_slug != model.slug:
        clean = matching_service.slugify(new_slug)
        if not clean:
            raise HTTPException(status_code=422, detail="slug 必须是 ASCII（字母数字与连字符）")
        if clean != new_slug:
            raise HTTPException(status_code=422, detail=f"slug 含非法字符，建议用「{clean}」")
        clash = db.query(Model).filter(Model.slug == clean, Model.id != model.id).first()
        if clash is not None:
            raise HTTPException(status_code=409, detail=f"slug「{clean}」已被实体 {clash.name} 占用")
        model.slug = clean
        slug_changed = True

    if model.name != old_name:
        add_alias(db, model, old_name)  # 旧名继续可匹配，历史上传不受影响
    if slug_changed:
        # 旧 slug 转别名：外部贴出去的旧链接与历史标签仍可解析（详情查找已支持别名兜底）
        if before.get("slug"):
            add_alias(db, model, str(before["slug"]))
    # KB-2：改名后保证新名在词表里可选（旧值刻意不退役 —— 别名已兜住解析，
    # 退役旧值只会让存量作品的标签从公开页消失）
    if model.name != old_name:
        sync_model_tag(db, model)
    # 补登厂商 → 顺手保证该厂商的「未定型号」族节点存在（B 档永远可一键选）
    if model.vendor:
        ensure_family_for_vendor(db, model.vendor)
    audit_service.record(
        db,
        action="slug_set" if slug_changed else "update",
        entity_type="model",
        entity_id=model.id,
        actor_id=actor_id,
        before=before,
        after=audit_service.snapshot_model(model),
        reason=(
            reason
            or (
                f"slug：{before.get('slug')} → {model.slug}（旧值已转别名，对外链接会变）"
                if slug_changed
                else "编辑实体信息"
            )
        ),
    )
    db.commit()
    matching_service.invalidate_alias_cache()
    return model


def alias_add(db: Session, model: Model, alias: str, actor_id: int | None = None, commit: bool = True) -> bool:
    """admin 显式加别名（走审计；自动双写路径的 add_alias 不记）。"""
    added = add_alias(db, model, alias)
    if added:
        audit_service.record(
            db,
            action="alias_add",
            entity_type="model",
            entity_id=model.id,
            actor_id=actor_id,
            after=audit_service.snapshot_model(model),
            reason=f"新增别名 {alias}",
        )
        if commit:
            db.commit()
    return added


def alias_remove(db: Session, model: Model, alias: str, actor_id: int | None = None) -> bool:
    """删别名（禁止删掉实体的规范名本身）。"""
    row = db.get(ModelAlias, (alias or "").strip())
    if row is None or row.model_id != model.id:
        return False
    if model.name == row.alias:
        raise HTTPException(status_code=400, detail="规范名不可作为别名删除")
    db.delete(row)
    matching_service.invalidate_alias_cache()
    audit_service.record(
        db,
        action="alias_remove",
        entity_type="model",
        entity_id=model.id,
        actor_id=actor_id,
        reason=f"删除别名 {row.alias}",
    )
    db.commit()
    return True


def _assert_no_cycle(db: Session, source: Model, target: Model) -> None:
    """合并防呆：source 的归宿链若最终回到 source 即成环（SQLite 无递归 CTE，链极短，逐跳查）。"""
    seen: set[int] = {source.id, target.id}
    if target.id == source.id:
        raise HTTPException(status_code=422, detail="合并会形成环，已拒绝")
    cur_id = target.merged_into_id
    guard = 0
    while cur_id is not None and guard < 20:
        if cur_id == source.id or cur_id in seen:
            raise HTTPException(status_code=422, detail="合并会形成环，已拒绝")
        seen.add(cur_id)
        nxt = db.get(Model, cur_id)
        cur_id = nxt.merged_into_id if nxt is not None else None
        guard += 1


def merge_model(
    db: Session,
    source: Model,
    target: Model,
    dry_run: bool = False,
    actor_id: int | None = None,
    reason: str = "",
    commit: bool = True,
) -> dict:
    """把 source 合并进 target：迁引用 → 别名指向 → 废弃源（单事务 + 审计，可回溯）。

    dry_run=True 只返回影响面预览，不写库、不落审计（预览不是变更）。
    防呆：不成环 / 目标不得是 deprecated（已退役实体不能再当归宿）。
    """
    if source.id == target.id:
        raise HTTPException(status_code=422, detail="不能合并到自身")
    if target.status == "deprecated":
        raise HTTPException(status_code=422, detail="合并目标已退役，请选 active/unverified 实体")
    if source.status == "deprecated" and source.merged_into_id:
        raise HTTPException(status_code=422, detail="源实体已被合并过，请直接用其归宿")
    _assert_no_cycle(db, source, target)

    affected = db.query(func.count(DemoModel.demo_id)).filter(DemoModel.model_id == source.id).scalar() or 0
    preview = {
        "source": {"id": source.id, "slug": source.slug, "name": source.name},
        "target": {"id": target.id, "slug": target.slug, "name": target.name},
        "affected_demos": affected,
        "aliases_moved": db.query(func.count(ModelAlias.alias)).filter(ModelAlias.model_id == source.id).scalar() or 0,
        "dry_run": dry_run,
    }
    if dry_run:
        return preview

    before = audit_service.snapshot_model(source)
    demo_ids = [d for (d,) in db.query(DemoModel.demo_id).filter(DemoModel.model_id == source.id).all()]
    db.query(DemoModel).filter(DemoModel.model_id == source.id).delete()
    for did in demo_ids:
        exists = (
            db.query(DemoModel)
            .filter(DemoModel.demo_id == did, DemoModel.model_id == target.id)
            .first()
        )
        if exists is None:
            db.add(DemoModel(demo_id=did, model_id=target.id))

    for alias_row in db.query(ModelAlias).filter(ModelAlias.model_id == source.id).all():
        alias_row.model_id = target.id
    add_alias(db, target, source.name)

    source.status = "deprecated"
    source.merged_into_id = target.id
    sync_model_tag(db, source)  # KB-2：源值退役，词表不再提供「已并入别处」的旧型号
    audit_service.record(
        db,
        action="merge",
        entity_type="model",
        entity_id=source.id,
        actor_id=actor_id,
        before=before,
        # moved_demo_ids 是为「撤销合并」留的钩子：没有它，事后无从知道当初迁走了哪几个，
        # 撤销就只能恢复实体状态、不能把引用迁回去（那会把治理做成半截活）。
        after={**audit_service.snapshot_model(source), "moved_demo_ids": demo_ids},
        reason=reason or f"合并入 {target.name}（id={target.id}），迁移 {affected} 个作品引用",
    )
    if commit:
        db.commit()
    matching_service.invalidate_alias_cache()
    preview["merged"] = True
    return preview


def _latest_merge_record(db: Session, source_id: int) -> dict | None:
    """取该实体最近一次合并的审计载荷（含 moved_demo_ids 与合并前状态）。"""
    import json

    row = (
        db.query(AuditLog)
        .filter(AuditLog.action == "merge", AuditLog.entity_type == "model", AuditLog.entity_id == source_id)
        .order_by(AuditLog.id.desc())
        .first()
    )
    if row is None:
        return None
    try:
        after = json.loads(row.after) if row.after else {}
        before = json.loads(row.before) if row.before else {}
    except (json.JSONDecodeError, TypeError):
        return None
    return {"audit_id": row.id, "reason": row.reason, "after": after or {}, "before": before or {}}


def merge_history(db: Session, limit: int = 30) -> list[dict]:
    """当前处于「已被合并」状态的实体 —— 合并向导的撤销列表。"""
    out = []
    rows = (
        db.query(Model)
        .filter(Model.status == "deprecated", Model.merged_into_id.isnot(None))
        .order_by(Model.id.desc())
        .limit(limit)
        .all()
    )
    for src in rows:
        rec = _latest_merge_record(db, src.id)
        tgt = db.get(Model, src.merged_into_id) if src.merged_into_id else None
        moved = (rec or {}).get("after", {}).get("moved_demo_ids") or []
        still = (
            db.query(func.count(DemoModel.demo_id)).filter(DemoModel.demo_id.in_(moved), DemoModel.model_id == src.merged_into_id).scalar()
            if moved
            else 0
        )
        out.append(
            {
                "source": {"id": src.id, "slug": src.slug, "name": src.name},
                "target": {"id": tgt.id, "slug": tgt.slug, "name": tgt.name} if tgt else None,
                "moved_total": len(moved),
                "movable_back": int(still or 0),
                # 老数据没有 moved_demo_ids（钩子是后加的）：只能恢复实体、无法精确迁回引用
                "reliable": bool(moved),
                "reason": (rec or {}).get("reason") or "",
                "restored_status": (rec or {}).get("before", {}).get("status") or "active",
            }
        )
    return out


def unmerge_model(
    db: Session,
    source: Model,
    dry_run: bool = False,
    actor_id: int | None = None,
    reason: str = "",
) -> dict:
    """撤销一次合并：把当初迁走的引用迁回、源实体恢复合并前状态。

    诚实面对边界：
    - 只有**记了 moved_demo_ids 的合并**才能精确迁回（钩子是后加的，早期合并没有），
      没有该记录时 `reliable=False`，执行只恢复实体本身，绝不猜哪些作品是它的；
    - 撤销前必须 dry_run 预览：合并之后可能又做过归属，直接撤销会踩掉后来的决定，
      所以预览会把"当初迁走 / 现在仍在归宿上 / 已被改走"三个数都摆出来。
    """
    if source.status != "deprecated" or not source.merged_into_id:
        raise HTTPException(status_code=422, detail="该实体不处于「已被合并」状态，无需撤销")
    target = db.get(Model, source.merged_into_id)
    if target is None:
        raise HTTPException(status_code=422, detail="归宿实体已不存在，无法安全撤销")

    rec = _latest_merge_record(db, source.id) or {}
    moved = [int(i) for i in (rec.get("after", {}).get("moved_demo_ids") or [])]
    currently_on_target = set(
        did for (did,) in db.query(DemoModel.demo_id).filter(DemoModel.demo_id.in_(moved), DemoModel.model_id == target.id).all()
    ) if moved else set()
    restored_status = rec.get("before", {}).get("status") or "active"

    preview = {
        "source": {"id": source.id, "slug": source.slug, "name": source.name},
        "target": {"id": target.id, "slug": target.slug, "name": target.name},
        "moved_total": len(moved),
        "will_restore": len(currently_on_target),
        "already_moved_away": len(moved) - len(currently_on_target),
        "restored_status": restored_status,
        "reliable": bool(moved),
        "dry_run": dry_run,
    }
    if dry_run:
        return preview
    if not moved:
        # 没有依据就不假装能恢复引用：只把实体本身放回去，并明确告知
        source.status = restored_status
        source.merged_into_id = None
        sync_model_tag(db, source)  # KB-2：撤销合并后词表值重新可选
        audit_service.record(
            db,
            action="unmerge",
            entity_type="model",
            entity_id=source.id,
            actor_id=actor_id,
            before=audit_service.snapshot_model(source),
            after={**audit_service.snapshot_model(source), "moved_back": 0, "note": "无 moved_demo_ids 记录，仅恢复实体"},
            reason=reason or f"撤销合并入 {target.name}（无迁移依据，作品仍留在归宿）",
        )
        db.commit()
        matching_service.invalidate_alias_cache()
        return {**preview, "unmerged": True, "will_restore": 0}

    for did in sorted(currently_on_target):
        db.query(DemoModel).filter(DemoModel.demo_id == did, DemoModel.model_id == target.id).delete()
        exists = db.query(DemoModel).filter(DemoModel.demo_id == did, DemoModel.model_id == source.id).first()
        if exists is None:
            db.add(DemoModel(demo_id=did, model_id=source.id))

    source.status = restored_status
    source.merged_into_id = None
    sync_model_tag(db, source)  # KB-2：撤销合并后词表值重新可选
    audit_service.record(
        db,
        action="unmerge",
        entity_type="model",
        entity_id=source.id,
        actor_id=actor_id,
        before=rec.get("before") or {},
        after={**audit_service.snapshot_model(source), "moved_back": len(currently_on_target)},
        reason=reason or f"撤销合并：{len(currently_on_target)} 个作品引用迁回 {source.name}",
    )
    db.commit()
    matching_service.invalidate_alias_cache()
    cluster_service.invalidate()
    return {**preview, "unmerged": True, "will_restore": len(currently_on_target)}


def delete_model(db: Session, model: Model, actor_id: int | None = None) -> None:
    """仅允许删除无作品引用的实体（有引用走 merge/deprecated）。"""
    linked = db.query(func.count(DemoModel.demo_id)).filter(DemoModel.model_id == model.id).scalar() or 0
    if linked:
        raise HTTPException(status_code=409, detail=f"该模型仍有 {linked} 个作品引用，请使用合并而不是删除")
    audit_service.record(
        db,
        action="delete",
        entity_type="model",
        entity_id=model.id,
        actor_id=actor_id,
        before=audit_service.snapshot_model(model),
        reason="删除零引用实体",
    )
    db.delete(model)
    db.commit()
    matching_service.invalidate_alias_cache()


def create_model(
    db: Session,
    name: str,
    vendor: str | None = None,
    description: str = "",
    status: str = "active",
    actor_id: int | None = None,
    reason: str = "",
) -> Model:
    """手工新建实体（管理端）：名称/别名已存在则 409，避免重复建（验收「匹配不重复建」）。"""
    clean = (name or "").strip()
    if not clean:
        raise HTTPException(status_code=422, detail="名称不能为空")
    if status not in MODEL_STATUSES:
        raise HTTPException(status_code=422, detail=f"非法状态，可选：{', '.join(MODEL_STATUSES)}")
    if matching_service.match_model(db, clean) is not None:
        raise HTTPException(status_code=409, detail=f"模型「{clean}」已存在（或其别名已指向某实体），请改用合并或加别名")
    slug = _slugify_unique(db, clean)
    model = Model(
        slug=slug,
        name=clean,
        vendor=(vendor or None),
        status=status,
        resolution=infer_resolution(clean, vendor, slug),
        description=description or "",
    )
    db.add(model)
    db.flush()
    add_alias(db, model, model.name)
    if vendor and model.resolution == "exact":
        ensure_family_for_vendor(db, vendor)
    # KB-2：实体建出来必须同时进词表，否则作者在选择器里看不到这个型号（fixed 键只认词表）
    sync_model_tag(db, model)
    audit_service.record(
        db,
        action="create",
        entity_type="model",
        entity_id=model.id,
        actor_id=actor_id,
        after=audit_service.snapshot_model(model),
        reason=reason or "管理端手工新建实体",
    )
    db.commit()
    matching_service.invalidate_alias_cache()
    return model


def list_models_admin(
    db: Session,
    status: str | None = None,
    q: str | None = None,
    sort: str = "demos",
    limit: int = 500,
) -> tuple[list[dict], int, dict[str, int]]:
    """管理端列表：不隐藏任何状态，并附各状态计数（缺省 status 时看全量）。"""
    dc, ra = _agg_subqueries(db)
    prior = score_prior(db)
    C, m = prior
    demo_count_col = func.coalesce(dc.c.c, 0)
    rating_col = func.coalesce(ra.c.a, 0.0)
    votes_col = func.coalesce(ra.c.votes, 0)
    wsum_col = func.coalesce(ra.c.wsum, 0.0)
    score_col = (wsum_col + m * C) / (votes_col + m)
    # KB-8：与展示精度一致；零票置 NULL 排最后（同 list_models）
    score_sort_col = case((votes_col > 0, func.round(score_col, 2)), else_=None)
    query = (
        db.query(
            Model,
            demo_count_col.label("demo_count"),
            rating_col.label("rating_avg"),
            votes_col.label("votes"),
            wsum_col.label("wsum"),
            score_sort_col.label("score_sql"),
        )
        .outerjoin(dc, dc.c.model_id == Model.id)
        .outerjoin(ra, ra.c.model_id == Model.id)
    )
    if status:
        query = query.filter(Model.status == status)
    if q:
        like = f"%{q}%"
        alias_ids = db.query(ModelAlias.model_id).filter(ModelAlias.alias.ilike(like))
        query = query.filter(Model.name.ilike(like) | Model.slug.ilike(like) | Model.id.in_(alias_ids))
    total = query.count()
    if sort == "name":
        query = query.order_by(Model.name.asc())
    elif sort == "new":
        query = query.order_by(Model.created_at.desc(), Model.id.desc())
    elif sort in ("score", "rating"):
        query = query.order_by(score_sort_col.desc().nullslast(), votes_col.desc(), Model.id.asc())
    else:
        query = query.order_by(demo_count_col.desc(), Model.id.asc())
    rows = query.limit(limit).all()
    counts = dict(db.query(Model.status, func.count(Model.id)).group_by(Model.status).all())
    items = [_model_out(m_, c, a, v, w, prior, score_sql=s) for m_, c, a, v, w, s in rows]
    status_counts = {s: counts.get(s, 0) for s in MODEL_STATUSES}
    return items, total, status_counts


def attribute_demos(
    db: Session,
    demo_ids: list[int],
    target_id: int,
    actor_id: int | None = None,
    reason: str = "",
) -> dict:
    """把作品从兜底位（未标注 / 未定型号 / 灰测）**归属**到真实型号。

    关键约束：归属必须写 `model:` 标签，不能只改 `demo_models`。
    因为 `update_demo` 会用 `_set_demo_tags → sync_demo_models` 从标签重新派生实体 ——
    只改实体表的归属会在作者下一次编辑时**静默退回兜底位**（比不归属更糟：还留了个已处理过的假象）。
    迁移动作 = 删掉指向兜底实体的 model 标签 + 保证目标 model 的 Tag 在位并挂上，再走双写同步。
    """
    target = get_model_or_404(db, target_id)
    if target.resolution != "exact":
        raise HTTPException(
            status_code=422,
            detail="归属目标必须是已确认的真实型号；兜底位之间请走实体合并",
        )
    if target.status == "deprecated":
        raise HTTPException(status_code=422, detail="目标型号已退役，请选择在用的型号")

    ensure_tag_value(db, target.name, group=target.vendor)
    tgt_tag = db.query(Tag).filter(Tag.key == "model", Tag.value == target.name).first()
    tag_rows = {t.id: t for t in db.query(Tag).filter(Tag.key == "model").all()}
    # 标签值 → 实体（走匹配层，和双写同一套规则，不另造判定）
    resolve: dict[str, Model | None] = {}
    for v in {t.value for t in tag_rows.values()}:
        resolve[v] = matching_service.match_model(db, v)

    moved: list[int] = []
    from_slugs: set[str] = set()
    for demo in db.query(Demo).filter(Demo.id.in_(demo_ids)).all():
        current = [tag_rows[link.tag_id] for link in list(demo.tag_associations) if link.tag_id in tag_rows]
        values = {t.value for t in current}
        # 先算「归属后的值集合」：摘掉所有兜底位的值，补上目标型号
        kept = {v for v in values if resolve.get(v) is None or resolve[v].resolution == "exact"}
        fallback_hits = {resolve[v].slug for v in values if resolve.get(v) is not None and resolve[v].resolution in FALLBACK_RESOLUTIONS}
        if not fallback_hits:
            continue  # 这个作品没挂在兜底位上，不动
        new_values = kept | {target.name}
        if new_values == values:
            continue  # 已归属过（幂等）
        for link in list(demo.tag_associations):
            if link.tag_id in tag_rows and tag_rows[link.tag_id].value in values - new_values:
                db.delete(link)
        if tgt_tag is not None and not any(t.id == tgt_tag.id for t in current):
            db.add(DemoTag(demo_id=demo.id, tag_id=tgt_tag.id))
        from_slugs |= fallback_hits
        db.flush()
        db.expire(demo, ["tag_associations"])
        sync_demo_models(db, demo)
        moved.append(demo.id)

    if not moved:
        # 无实际变更不写审计（变更日志不是「操作尝试」记录）；此处 commit 而非 rollback，
        # 以免撤销上面 ensure_tag_value 刚补建的标签。
        db.commit()
        return {"moved": 0, "demo_ids": [], "target": {"id": target.id, "slug": target.slug, "name": target.name}}

    audit_service.record(
        db,
        action="attribute",
        entity_type="model",
        entity_id=target.id,
        actor_id=actor_id,
        after={"target": target.slug, "moved": len(moved), "demo_ids": moved, "from": sorted(from_slugs)},
        reason=(reason or f"归属 {len(moved)} 个作品到 {target.name}")[:500],
    )
    db.commit()
    matching_service.invalidate_alias_cache()
    cluster_service.invalidate()
    return {"moved": len(moved), "demo_ids": moved, "target": {"id": target.id, "slug": target.slug, "name": target.name}}


def guess_model(db: Session, text: str, *, exact_only: bool = False) -> Model | None:
    """纯规则：在任意文本里找已知型号名/别名，取最长命中者。

    归属工作台（`model_hint` + 提示词 + 标题）与上传建议包共用这一份判定，
    避免同一个"猜型号"逻辑在两处漂移。LLM 后置时替换本函数即可（四层原则）。

    `exact_only=True`：只认已确认的真实型号 —— 归属目标必须是真型号（兜底位当目标
    会被 `attribute_demos` 422 拒掉，推荐出来只会误导管理员白点一次）。
    """
    hay = matching_service.normalize(text or "")
    if len(hay) < 3:
        return None
    # KB-19：键集走匹配层的 60s 缓存（一次查询），不再每次全表扫 Model + 逐行懒加载 aliases
    best: tuple[int, int] | None = None
    for key, model_id, resolution in matching_service.alias_pairs(db):
        if exact_only and resolution != "exact":
            continue
        if len(key) >= 3 and key in hay and (best is None or len(key) > best[0]):
            best = (len(key), model_id)
    return db.get(Model, best[1]) if best else None


def guess_target(db: Session, demo: Demo) -> Model | None:
    """归属预填：证据字段 + 提示词 + 标题拼成文本后走统一判定（只认真实型号）。"""
    return guess_model(
        db,
        " ".join([(demo.model_hint or ""), (demo.prompt or "")[:400], demo.title or ""]),
        exact_only=True,
    )


def pending_attribution(db: Session, limit_models: int = 20, limit_demos: int = 60) -> dict:
    """归属工作台数据：兜底实体 + 其下作品（带证据与规则预填目标）。"""
    rows = (
        db.query(Model, func.count(func.distinct(DemoModel.demo_id)))
        .join(DemoModel, DemoModel.model_id == Model.id)
        .join(Demo, Demo.id == DemoModel.demo_id)
        .filter(Demo.status == "approved", Model.resolution.in_(FALLBACK_RESOLUTIONS))
        .group_by(Model.id)
        .order_by(func.count(func.distinct(DemoModel.demo_id)).desc())
        .limit(limit_models)
        .all()
    )
    items = []
    for m, n in rows:
        demos = (
            db.query(Demo)
            .join(DemoModel, DemoModel.demo_id == Demo.id)
            .filter(DemoModel.model_id == m.id, Demo.status == "approved")
            .order_by(Demo.created_at.desc())
            .limit(limit_demos)
            .all()
        )
        items.append(
            {
                "model": _model_out(m, n, None),
                "demos": [
                    {
                        "id": d.id,
                        "slug": d.slug,
                        "title": d.title,
                        "model_hint": d.model_hint or "",
                        "rating_avg": d.rating_avg,
                        "rating_count": d.rating_count,
                        "guess": (lambda g: {"id": g.id, "slug": g.slug, "name": g.name} if g else None)(
                            guess_target(db, d)
                        ),
                    }
                    for d in demos
                ],
            }
        )
    exact = db.query(Model).filter(Model.status.in_(("active", "unverified")), Model.resolution == "exact").all()
    return {
        "groups": items,
        "targets": [
            {"id": m.id, "slug": m.slug, "name": m.name, "vendor": m.vendor}
            for m in sorted(exact, key=lambda x: x.name.lower())
        ],
    }


def model_candidates(db: Session) -> list[dict]:
    """待确认清单（candidate）+ 每个的引用作品数：收件箱「新模型 N」的数据源。"""
    rows = (
        db.query(Model, func.count(func.distinct(DemoModel.demo_id)))
        .outerjoin(DemoModel, DemoModel.model_id == Model.id)
        .filter(Model.status == "candidate")
        .group_by(Model.id)
        .order_by(func.count(func.distinct(DemoModel.demo_id)).desc(), Model.id.asc())
        .all()
    )
    return [
        {
            "id": m.id,
            "slug": m.slug,
            "name": m.name,
            "vendor": m.vendor,
            "status": m.status,
            "demo_count": c,
            "created_at": m.created_at,
        }
        for m, c in rows
    ]
