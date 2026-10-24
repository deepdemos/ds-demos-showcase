"""提示词收录与公开聚合。读操作不改库；投稿批准才创建独立收录信息。"""

import hashlib
from urllib.parse import urlsplit

from fastapi import HTTPException
from sqlalchemy import and_, case, func, or_, select, text, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, aliased

from ..models import Demo, DemoModel, DemoTag, EntitySuggestion, Model, Prompt, PromptLibraryEntry, Setting, Tag, User
from ..serializers import preload_demo_relations, serialize_demo
from .scope import VALID_SCOPES, scope_contains_filter

# 列表/收件箱摘要统一截断长度：卡片预览 480 字即够，全文按 id 按需取。
PROMPT_EXCERPT_LEN = 480
# 回填版本标记：完整跑过一次即打标，稳态启动不再扫描；手动重跑仍走 backfill_missing_prompt_links。
BACKFILL_MARK_KEY = "prompt_backfill_links_done"
# FTS 就绪标记：trigram 表的 CREATE 对其他连接立即可见，而首灌随启动事务提交——
# 并发首启窗口内其他进程可能看到空表；标记未打前查询一律回退 LIKE，宁可慢不可漏。
FTS_READY_KEY = "prompt_fts_ready"


def normalize_content(content: str) -> str:
    """与作品双写一致：保留大小写、内文空白，仅规范换行和首尾空白。"""
    return content.replace("\r\n", "\n").strip()


def content_hash(content: str) -> str:
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def content_excerpt(content: str) -> tuple[str, bool]:
    """列表/收件箱摘要：480 字截断；全文一律按 id 单取。"""
    if len(content) <= PROMPT_EXCERPT_LEN:
        return content, False
    return content[:PROMPT_EXCERPT_LEN], True


# ---------------- FTS5 trigram 搜索预筛（仅 SQLite；语义由 icontains 精确校验兜底） ----------------
#
# 正文 icontains 在 SQLite 上是 lower()||LIKE 全表扫描。trigram 虚拟表把 ≥3 字符查询的
# 候选集先筛出来，再在候选上跑原 icontains：trigram 的大小写折叠可能比 SQL lower()（只折
# ASCII）更宽，候选只会偏大不会漏，精确校验保证最终口径与全表 LIKE 逐字节一致。
# <3 字符（常见中文二字词）、非 SQLite、无 FTS5 编译项、虚拟表缺失——全部回退原 LIKE 路径。
# Prompt 正文不可变且无删除接口，索引只需 INSERT 同步，无需更新/删除路径。


def ensure_prompt_search_index(db: Session) -> bool:
    """启动时创建 trigram 虚拟表；返回是否**本次新建**（新建才需要首灌存量正文）。
    不可用时静默降级（查询路径按表是否存在自行回退）。"""
    connection = db.connection()
    if connection.dialect.name != "sqlite":
        return False
    if not connection.exec_driver_sql("SELECT sqlite_compileoption_used('ENABLE_FTS5')").scalar():
        return False
    if _search_index_ready(db):
        return False
    connection.exec_driver_sql(
        "CREATE VIRTUAL TABLE IF NOT EXISTS prompt_fts USING fts5(content, tokenize='trigram')")
    return True


def _search_index_ready(db: Session) -> bool:
    connection = db.connection()
    if connection.dialect.name != "sqlite":
        return False
    return connection.exec_driver_sql(
        "SELECT 1 FROM sqlite_master WHERE type='table' AND name='prompt_fts'").first() is not None


def _fts_candidate_ids(db: Session, q: str) -> list[int] | None:
    """正文候选 id 预筛；None 表示本查询不走 FTS（调用方回退全表 icontains）。"""
    if len(q) < 3 or not _search_index_ready(db):
        return None
    if db.get(Setting, FTS_READY_KEY) is None:
        # 就绪标记未打（并发首启窗口/首灌未提交）：表可能为空，回退 LIKE
        return None
    phrase = '"' + q.replace('"', '""') + '"'
    rows = db.execute(text("SELECT rowid FROM prompt_fts WHERE prompt_fts MATCH :m"), {"m": phrase}).all()
    ids = [r[0] for r in rows]
    if not ids or len(ids) > 10_000:
        # 空候选也可能是索引被清空/损坏——回退 LIKE 保证不静默漏正文；
        # 命中面过大（>1 万）时预筛无收益，同时避开 in_ 绑定参数数上限。
        return None
    return ids


def _search_index_add(db: Session, prompt_id: int, content: str) -> None:
    """与正文同事务写入索引：失败即整体失败，不会出现正文在而索引缺的漂移。
    OR REPLACE：生产无删除场景等于幂等；测试直删 Prompt 后 id 复用时吸收旧行。"""
    if not _search_index_ready(db):
        return
    db.execute(text("INSERT OR REPLACE INTO prompt_fts(rowid, content) VALUES (:i, :c)"),
               {"i": prompt_id, "c": content})


def sync_prompt_search_index(db: Session) -> int:
    """把缺位正文补进索引（启动建表后首灌、回填后兜底）；幂等，非 sqlite/无表 no-op。"""
    if not _search_index_ready(db):
        return 0
    result = db.execute(text(
        "INSERT INTO prompt_fts(rowid, content) "
        "SELECT id, content FROM prompts WHERE id NOT IN (SELECT rowid FROM prompt_fts)"))
    return result.rowcount


def backfill_missing_prompt_links_once(db: Session) -> int:
    """带版本标记的启动回填：标记存在即跳过（稳态零扫描）；标记只在此函数读，写入归调用方。"""
    if db.get(Setting, BACKFILL_MARK_KEY) is not None:
        return 0
    return backfill_missing_prompt_links(db)


def mark_backfill_done() -> None:
    """回填成功提交后打标。独立短事务 + 冲突容忍：并发首启时另一进程已打标（主键冲突）
    或 SQLite 写锁未抢到（busy）都够用——标记只是优化，回填本身幂等，下次启动兜底。"""
    from sqlalchemy.exc import IntegrityError, OperationalError

    from ..database import SessionLocal

    try:
        with SessionLocal() as db:
            db.add(Setting(key=BACKFILL_MARK_KEY, value="1"))
            db.commit()
    except (IntegrityError, OperationalError):
        pass


def search_index_marked(db: Session) -> bool:
    """就绪标记是否已打；init_db 决定是否需要重灌时使用。"""
    return db.get(Setting, FTS_READY_KEY) is not None


def mark_search_index_ready() -> None:
    """FTS 首灌随启动事务提交成功后打标；模式与 mark_backfill_done 一致（独立短事务+冲突容忍）。"""
    from sqlalchemy.exc import IntegrityError, OperationalError

    from ..database import SessionLocal

    try:
        with SessionLocal() as db:
            db.add(Setting(key=FTS_READY_KEY, value="1"))
            db.commit()
    except (IntegrityError, OperationalError):
        pass


def _savepoint(db: Session):
    # Python sqlite3 的旧事务模式不会为 SELECT 开启实际事务。
    # 此时直接 SAVEPOINT / RELEASE 会提前提交，外层审核失败也回滚不了。
    connection = db.connection()
    if connection.dialect.name == "sqlite" and not connection.connection.driver_connection.in_transaction:
        connection.exec_driver_sql("BEGIN")
    return db.begin_nested()


def _lock_write(db: Session, statement) -> None:
    """SQLite 短写事务串行；其他数据库对业务定位行加锁，不写无关字段。"""
    connection = db.connection()
    if connection.dialect.name == "sqlite":
        # 本服务调用前只有鉴权/读操作；若已有实际事务，调用方已在写入流程中。
        if not connection.connection.driver_connection.in_transaction:
            connection.exec_driver_sql("BEGIN IMMEDIATE")
    else:
        db.execute(statement.with_for_update()).first()


def lock_submission(db: Session, user_id: int) -> None:
    """重复查询与创建处于同一串行区，确保同一人的并发投稿只入箱一次。"""
    _lock_write(db, select(User.id).where(User.id == user_id))


def lock_review(db: Session, suggestion: EntitySuggestion) -> None:
    """等待其他审核结束后重读状态，防止已载入的 pending 被第二个决策覆盖。"""
    _lock_write(db, select(EntitySuggestion.id).where(EntitySuggestion.id == suggestion.id))
    db.refresh(suggestion)


def get_or_create_prompt(db: Session, content: str) -> Prompt:
    """正文按精确哈希复用；保存点保护并发写入的唯一约束竞争。"""
    digest = content_hash(content)
    existing = db.query(Prompt).filter(Prompt.content_hash == digest).first()
    if existing is not None:
        return existing
    row = Prompt(content_hash=digest, content=content)
    try:
        with _savepoint(db):
            db.add(row)
            db.flush()
            _search_index_add(db, row.id, content)
    except IntegrityError:
        existing = db.query(Prompt).filter(Prompt.content_hash == digest).first()
        if existing is None:
            raise
        return existing
    return row


def validate_proposal(payload: dict) -> dict:
    """请求和审核执行共用校验；来源只保存链接，不发起外部请求。"""
    fields = {"title": 200, "content": 20000, "category": 64, "source_url": 2048}
    values = {}
    for field, maximum in fields.items():
        value = payload.get(field)
        if value is None and field in ("category", "source_url"):
            value = ""
        if not isinstance(value, str) or len(value) > maximum:
            raise HTTPException(status_code=422, detail=f"{field} 需为不超过 {maximum} 字符的文本")
        values[field] = normalize_content(value) if field == "content" else value.strip()
    if not values["title"] or not values["content"]:
        raise HTTPException(status_code=422, detail="标题和提示词正文不能为空")
    source = values["source_url"]
    if source:
        try:
            parsed = urlsplit(source)
            valid = (parsed.scheme.lower() in ("http", "https") and bool(parsed.hostname)
                     and parsed.username is None and parsed.password is None
                     and not any(c.isspace() or ord(c) < 32 for c in source))
            parsed.port  # 同时拒绝不合法的端口号
        except ValueError:
            valid = False
        if not valid:
            raise HTTPException(status_code=422, detail="来源链接需为有效的 HTTP(S) 网址，且不能包含账号密码")
    values["category"] = values["category"] or None
    values["source_url"] = source or None
    return values


def backfill_missing_prompt_links(db: Session, batch_size: int = 200) -> int:
    """按主键分批修复旧作品缺失的 prompt_id；不改原文、时间或已有链接。

    缓存实体可以包含待审作品的正文，公开查询另行检查作品状态和视区。
    函数只 flush，事务边界归启动方或维护脚本；重复启动不会改写已修复行。
    """
    if batch_size < 1:
        raise ValueError("batch_size must be positive")
    last_id = 0
    linked = 0
    while True:
        rows = (db.query(Demo.id, Demo.prompt)
                .filter(Demo.id > last_id, Demo.prompt_id.is_(None), Demo.prompt != "")
                .order_by(Demo.id).limit(batch_size).all())
        if not rows:
            break
        last_id = rows[-1].id
        by_content: dict[str, list[int]] = {}
        for row in rows:
            content = normalize_content(row.prompt or "")
            if content:
                by_content.setdefault(content, []).append(row.id)
        for content, ids in by_content.items():
            prompt = get_or_create_prompt(db, content)
            result = db.execute(update(Demo).where(Demo.id.in_(ids), Demo.prompt_id.is_(None))
                                .values(prompt_id=prompt.id).execution_options(synchronize_session=False))
            linked += result.rowcount
        db.flush()
    sync_prompt_search_index(db)
    if linked:
        # 批量修复只失效一次，避免每件作品都重置聚类状态。
        from . import cluster_service

        cluster_service.invalidate()
    return linked


def publish_prompt(db: Session, payload: dict, *, created_by: int | None) -> PromptLibraryEntry:
    """审核执行用：复用正文，已有收录的标题和来源不被后来投稿覆盖。"""
    values = validate_proposal(payload)
    scope = payload.get("scope", "deep")
    if scope not in VALID_SCOPES:
        raise HTTPException(status_code=422, detail="提示词视区无效")
    prompt = get_or_create_prompt(db, values.pop("content"))
    existing = (db.query(PromptLibraryEntry)
                .filter(PromptLibraryEntry.prompt_id == prompt.id, PromptLibraryEntry.scope == scope).first())
    if existing is not None:
        return existing
    entry = PromptLibraryEntry(prompt_id=prompt.id, scope=scope, status="approved",
                               created_by=created_by, **values)
    try:
        with _savepoint(db):
            db.add(entry)
            db.flush()
    except IntegrityError:
        existing = (db.query(PromptLibraryEntry)
                    .filter(PromptLibraryEntry.prompt_id == prompt.id, PromptLibraryEntry.scope == scope).first())
        if existing is None:
            raise
        return existing
    return entry


def _summary_query(db: Session, scope: str):
    """单一公开口径；计数、派生标题、详情与分页均使用相同的可见作品集合。"""
    visible = and_(Demo.status == "approved", scope_contains_filter(scope), Demo.prompt_id.is_not(None))
    counts = (db.query(Demo.prompt_id.label("prompt_id"), func.count(Demo.id).label("demos_total"),
                       func.min(Demo.id).label("first_demo_id"))
              .filter(visible).group_by(Demo.prompt_id).subquery())
    model_counts = (db.query(Demo.prompt_id.label("prompt_id"),
                            func.count(func.distinct(DemoModel.model_id)).label("models_total"))
                    .join(DemoModel, DemoModel.demo_id == Demo.id)
                    .join(Model, Model.id == DemoModel.model_id)
                    .filter(visible, Model.status != "deprecated")
                    .group_by(Demo.prompt_id).subquery())
    first = aliased(Demo)
    entry = aliased(PromptLibraryEntry)
    demo_category = (select(Tag.value).join(DemoTag, DemoTag.tag_id == Tag.id)
                     .where(DemoTag.demo_id == first.id, Tag.key == "category", Tag.status != "deprecated")
                     .order_by(Tag.value, Tag.id).limit(1).correlate(first).scalar_subquery())
    query = (db.query(
        Prompt.id.label("id"), func.coalesce(entry.title, first.title).label("title"),
        Prompt.content.label("content"), func.coalesce(entry.category, demo_category).label("category"),
        entry.source_url.label("source_url"),
        case((entry.id.is_not(None), "contributed"), else_="demo").label("source"),
        func.coalesce(entry.created_at, first.created_at, Prompt.created_at).label("created_at"),
        func.coalesce(counts.c.demos_total, 0).label("demos_total"),
        func.coalesce(model_counts.c.models_total, 0).label("models_total"),
    ).outerjoin(entry, and_(entry.prompt_id == Prompt.id, entry.scope == scope, entry.status == "approved"))
      .outerjoin(counts, counts.c.prompt_id == Prompt.id)
      .outerjoin(first, first.id == counts.c.first_demo_id)
      .outerjoin(model_counts, model_counts.c.prompt_id == Prompt.id)
      .filter(or_(entry.id.is_not(None), counts.c.demos_total > 0)))
    return query


def list_prompts(db: Session, *, scope: str, q: str | None = None, category: str | None = None,
                 sort: str = "newest", has_results: bool | None = None, page: int = 1,
                 page_size: int = 20) -> tuple[list[dict], int]:
    summary = _summary_query(db, scope).subquery()
    query = db.query(summary)
    if q and q.strip():
        value = q.strip()
        conditions = [summary.c.title.icontains(value, autoescape=True)]
        fts_ids = _fts_candidate_ids(db, value)
        if fts_ids is None:
            # 回退路径：短查询/非 SQLite/无索引表，维持原全表 icontains 口径
            conditions.append(summary.c.content.icontains(value, autoescape=True))
        elif fts_ids:
            # 候选集内再过一次 icontains：口径与全表 LIKE 一致，只是少扫了绝大多数行
            conditions.append(and_(summary.c.id.in_(fts_ids),
                                   summary.c.content.icontains(value, autoescape=True)))
        query = query.filter(or_(*conditions))
    if category and category.strip():
        query = query.filter(summary.c.category == category.strip())
    if has_results is not None:
        query = query.filter(summary.c.demos_total > 0 if has_results else summary.c.demos_total == 0)
    total = query.count()
    if sort == "most_tested":
        query = query.order_by(summary.c.demos_total.desc())
    query = query.order_by(summary.c.created_at.desc(), summary.c.id.desc())
    items = []
    for r in query.offset((page - 1) * page_size).limit(page_size).all():
        row = dict(r._mapping)
        content = row.pop("content")
        row["content_excerpt"], row["content_truncated"] = content_excerpt(content)
        items.append(row)
    return items, total


def get_prompt(db: Session, prompt_id: int, *, scope: str) -> dict | None:
    row = _summary_query(db, scope).filter(Prompt.id == prompt_id).first()
    if row is None:
        return None
    summary = dict(row._mapping)
    summary["content_excerpt"], summary["content_truncated"] = content_excerpt(summary["content"])
    return summary


def list_prompt_demos(db: Session, prompt_id: int, *, scope: str, page: int = 1,
                      page_size: int = 20) -> tuple[list[dict], int]:
    query = db.query(Demo).filter(Demo.prompt_id == prompt_id, Demo.status == "approved", scope_contains_filter(scope))
    total = query.count()
    demos = query.order_by(Demo.created_at.desc(), Demo.id.desc()).offset((page - 1) * page_size).limit(page_size).all()
    preload_demo_relations(db, demos)
    return [serialize_demo(db, demo) for demo in demos], total
