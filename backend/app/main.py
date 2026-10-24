from pathlib import Path

import asyncio
import logging
import mimetypes
import re
import time
from datetime import datetime

from fastapi import Depends, FastAPI, HTTPException, Request, Response
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse, RedirectResponse
from sqlalchemy import text
from sqlalchemy.orm import Session
from starlette.exceptions import HTTPException as StarletteHTTPException

from .client_ip import get_client_ip
from .config import settings
from .database import Base, SessionLocal, engine, get_db
from .errors import AppError
from .models import ForumTopic, Setting, Tag, TagKey, User
from .routers import admin, admin_entities, announcements, auth, collections, comments, demos, explore, forum, history, meta, models, notifications, peek, prompts, ratings, sessions, stats, tags, tasks, users
from .security import hash_password
from .services import oss
from .services import scope as scope_service
from .services.settings_service import KEY_AUTO_APPROVE

logger = logging.getLogger("app")
logging.basicConfig(level=logging.INFO)

# slim 镜像的 mime 库不认识 .webp：不注册的话 /media 封面会以 application/octet-stream 下发
mimetypes.add_type("image/webp", ".webp")

# 发布版本：与 frontend/package.json 的 version 同步维护（前后端各一份声明，改版本必须一起改）。
APP_VERSION = "0.2.0-alpha"

# naive UTC 统一补 Z 的口径由各响应模型/服务自行保证（pydantic v2 原生输出 Z）；
# 旧版 FastAPI 的 json_encoders 在 pydantic v2 下从未实际生效，0.161 起参数被移除。
app = FastAPI(title="DS 民间科研成果展示 API", version=APP_VERSION)

settings.media_path.mkdir(parents=True, exist_ok=True)


@app.exception_handler(AppError)
async def app_error_handler(request: Request, exc: AppError):
    return JSONResponse(
        status_code=exc.status_code,
        content={"detail": exc.detail, "code": exc.code},
    )


@app.exception_handler(StarletteHTTPException)
async def http_exception_handler(request: Request, exc: StarletteHTTPException):
    return JSONResponse(
        status_code=exc.status_code,
        content={"detail": exc.detail, "code": f"http_{exc.status_code}"},
    )


@app.exception_handler(RequestValidationError)
async def validation_exception_handler(request: Request, exc: RequestValidationError):
    """参数校验失败也遵守统一错误契约（KB-22）。

    默认 handler 只回 `{"detail": [...]}`，缺契约里的 `code`；前端拿 detail 当字符串
    渲染时会显示 [object Object]。这里保留 detail 原样（前端已有消费），补上 code。
    """
    return JSONResponse(
        status_code=422,
        content={"detail": exc.errors(), "code": "http_422"},
    )

app.add_middleware(
    CORSMiddleware,
    allow_origins=[o.strip() for o in settings.cors_origins.split(",") if o.strip()],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.middleware("http")
async def log_requests(request: Request, call_next):
    """请求日志：方法/路径/状态/耗时/解析出的访客 IP（验证 IP 解析与排查刷量用）。"""
    start = time.time()
    response = await call_next(request)
    duration_ms = (time.time() - start) * 1000
    logger.info(
        "%s %s -> %s %.0fms ip=%s",
        request.method,
        request.url.path,
        response.status_code,
        duration_ms,
        get_client_ip(request) or "-",
    )
    return response


@app.middleware("http")
async def site_scope(request: Request, call_next):
    """astra 橱窗可见门禁（services/scope.py）：Host → 视区；astra 下非白名单路径直接 404。

    deep 视区零改动：middleware 只挂 state + 设 contextvar（serializer/预览门禁读取），
    不拦截任何请求。astra 视区白名单制：论坛/评论/登录/上传/docs/admin 等整体不存在。
    """
    scope = scope_service.resolve_scope(request)
    request.state.scope = scope
    if scope == scope_service.ASTRA and not scope_service.astra_path_allowed(request.method, request.url.path):
        return JSONResponse({"detail": "Not Found", "code": "http_404"}, status_code=404)
    token = scope_service.current_scope.set(scope)
    try:
        return await call_next(request)
    finally:
        scope_service.current_scope.reset(token)

API_PREFIX = "/api/v1"
app.include_router(auth.router, prefix=API_PREFIX)
app.include_router(collections.router, prefix=API_PREFIX)
app.include_router(history.router, prefix=API_PREFIX)
app.include_router(users.router, prefix=API_PREFIX)
app.include_router(tags.router, prefix=API_PREFIX)
app.include_router(demos.router, prefix=API_PREFIX)
app.include_router(comments.router, prefix=API_PREFIX)
app.include_router(sessions.router, prefix=API_PREFIX)
app.include_router(admin.router, prefix=API_PREFIX)
app.include_router(announcements.router, prefix=API_PREFIX)
app.include_router(meta.router, prefix=API_PREFIX)
app.include_router(stats.router, prefix=API_PREFIX)
app.include_router(ratings.router, prefix=API_PREFIX)
app.include_router(forum.router, prefix=API_PREFIX)
app.include_router(notifications.router, prefix=API_PREFIX)
# v2 实体（B1）：Model / Task / Explore
app.include_router(models.router, prefix=API_PREFIX)
app.include_router(tasks.router, prefix=API_PREFIX)
app.include_router(prompts.router, prefix=API_PREFIX)
app.include_router(explore.router, prefix=API_PREFIX)
# 侧滑"瞄一眼"的紧凑摘要（Demo 页第 3 期）
app.include_router(peek.router, prefix=API_PREFIX)
# v2 治理写接口（B1.5）：实体 CRUD / 合并 / 收件箱 / 体检 / 审计（全部 admin）
app.include_router(admin_entities.router, prefix=API_PREFIX)


@app.get(API_PREFIX)
def api_root():
    """API 根信息：agent 探测 /api/v1 即可发现上传指南。"""
    return {
        "name": "DS 民间科研成果展示 API",
        "version": "1",
        "docs": "/docs",
        "agent_guide": f"{API_PREFIX}/meta/agent-guide",
        "tag_keys": f"{API_PREFIX}/tags/tag-keys",
        "site_info": f"{API_PREFIX}/meta/site-info",
        "health": f"{API_PREFIX}/health",
    }


@app.get(API_PREFIX + "/health")
def health(db: Session = Depends(get_db)):
    """存活探针：DB 可查即健康。no-store，避免监控读到缓存假活。

    KB-23：默认密钥/口令的告警随响应返回（`warnings`），让部署方能从探针发现配置风险。
    """
    warnings = settings.secret_warnings()
    try:
        db.execute(text("SELECT 1"))
    except Exception:  # noqa: BLE001 —— 探针不抛栈，按 503 语义返回
        return JSONResponse(
            {"status": "error", "db": "error", "warnings": warnings},
            status_code=503,
            headers={"Cache-Control": "no-store"},
        )
    return JSONResponse(
        {"status": "ok", "db": "ok", "warnings": warnings},
        headers={"Cache-Control": "no-store"},
    )


# 预览版本段的合法形态（与 serializers.version 的两条生成路径一致）：
#   · content_hash[:12] → 12 位十六进制；· 无 hash 时 -> str(int(created_at.timestamp())) 纯数字。
# 必须白名单校验：该段会原样拼进 HTML 的 <base href>，含引号/尖括号即可逃出属性注入标签。
_VERSION_RE = re.compile(r"^[A-Za-z0-9_.-]{1,64}$")


def _serve_preview(slug: str, path: str, version: str | None = None):
    if "/" in slug or "\\" in slug or not slug:
        raise HTTPException(status_code=400, detail="非法的 demo 标识", )
    if version is not None and not _VERSION_RE.match(version):
        raise HTTPException(status_code=400, detail="非法的版本标识", )
    # 预览可见域门禁（60s 缓存护子资源逐文件请求）：
    # astra 域只出策展池且已上架；deep 域存量行默认 sites 含 'deep' → 判定恒过，行为不变。
    if not scope_service.demo_public_in_scope(slug, scope_service.current_scope.get()):
        raise HTTPException(status_code=404, detail="文件不存在", )
    safe = _safe_join(slug, path)
    is_html = safe.lower().endswith((".html", ".htm"))
    versioned = version is not None
    # 带版本号的 URL 可安全长缓存（更新后版本号变化 → 新 URL）；旧的无版本 URL 保持 no-cache 兼容。
    cache_control = "public, max-age=86400, immutable" if versioned else "no-cache"

    # HTML 文档：同源返回 + 注入 <base>，保证 localStorage 属主站源，
    # 而页内 js/css/图片相对地址会经 <base> 走版本化路径，CDN 可长缓存。
    if is_html:
        data = _read_preview_byte(slug, safe)
        if data is None:
            raise HTTPException(status_code=404, detail="文件不存在", )
        import re as _re
        html = data.decode("utf-8", errors="replace")
        # 版本化路径：base 指回 /preview/{slug}/v{version}/，子资源也带版本号；
        # 无版本路径：保持原逻辑（本地或 OSS 直连）。
        if versioned:
            base_url = f"/preview/{slug}/v{version}/"
        else:
            base_url = (
                oss.public_url(f"demos/{slug}/files/")
                if (oss.enabled() and not settings.oss_serve_local)
                else f"/preview/{slug}/"
            )
        if _re.search(r"<base\s", html, _re.IGNORECASE):
            # 若页面自带 base，替换成我们的
            html = _re.sub(r"(?i)<base[^>]*>", f'<base href="{base_url}">', html, count=1)
        elif _re.search(r"<head[^>]*>", html, _re.IGNORECASE):
            html = _re.sub(r"(?i)(<head[^>]*>)", r'\1<base href="' + base_url + '">', html, count=1)
        else:
            html = f'<base href="{base_url}">' + html
        return Response(
            content=html.encode("utf-8"),
            media_type="text/html; charset=utf-8",
            headers={"Cache-Control": cache_control},
        )

    # 非 HTML（js/css/图片等）：OSS 已启用且非「本地服务」模式才 302 直连 OSS
    if oss.enabled() and not settings.oss_serve_local:
        # HEAD 检查存在性（不要 get_bytes 全量下载，避免服务器重复拉取）
        if oss.object_exists(f"demos/{slug}/files/{safe}"):
            url = oss.public_url(f"demos/{slug}/files/{safe}")
            if versioned:
                url += f"?v={version}"
            return RedirectResponse(url, headers={"Cache-Control": cache_control})

    file_path = (settings.demos_path / slug / "files" / safe).resolve()
    if file_path.is_file():
        return FileResponse(file_path, headers={"Cache-Control": cache_control})
    raise HTTPException(status_code=404, detail="文件不存在", )


@app.get("/preview/{slug}/v{version}/{path:path}")
def preview_file_versioned(slug: str, version: str, path: str):
    return _serve_preview(slug, path, version)


@app.get("/preview/{slug}/{path:path}")
def preview_file(slug: str, path: str):
    return _serve_preview(slug, path)


def _read_preview_byte(slug: str, safe: str) -> bytes | None:
    file_path = (settings.demos_path / slug / "files" / safe).resolve()
    if file_path.is_file():
        return file_path.read_bytes()
    if oss.enabled():
        return oss.get_bytes(f"demos/{slug}/files/{safe}")
    return None


@app.get("/media/{path:path}")
def media_file(path: str):
    from pathlib import Path as _P
    safe = _P(path).as_posix().replace("\\", "/")
    if oss.enabled() and not settings.oss_serve_local:
        # HEAD 检查存在性，避免全量下载对象（重定向本身 no-store，最终对象自带 immutable 缓存）
        if oss.object_exists(f"media/{safe}"):
            return RedirectResponse(
                oss.public_url(f"media/{safe}"),
                headers={"Cache-Control": "no-store"},
            )
    file_path = (settings.media_path / safe).resolve()
    # KB-13：必须用「目录归属」判定。旧实现是字符串前缀比较，`../media_backup/x`
    # 会解析到同前缀的兄弟目录而通过检查（实测能读到 media/ 之外的文件）。
    if not file_path.is_relative_to(settings.media_path.resolve()):
        raise HTTPException(status_code=400, detail="非法的路径", )
    if not file_path.is_file():
        raise HTTPException(status_code=404, detail="文件不存在", )
    return FileResponse(file_path, headers={"Cache-Control": "public, max-age=86400, immutable"})


def _safe_join(slug: str, path: str) -> str:
    """预览子资源路径解析：越界一律 400（KB-13）。

    旧写法 `str(resolved).startswith(str(root))` 对同前缀兄弟目录失效，且越界后
    还调 `relative_to` 抛未捕获的 ValueError → 500。现在先按目录归属判定，
    再用 resolved 根做 relative_to（不会抛）。
    """
    root = (settings.demos_path / slug / "files").resolve()
    file_path = (root / path).resolve()
    if not file_path.is_relative_to(root):
        raise HTTPException(status_code=400, detail="非法的路径", )
    return file_path.relative_to(root).as_posix()


def _sqlite_db_backup(tag: str) -> None:
    """SQLite ADD COLUMN 前置备份（07 §2.2 红线：策展列迁移不备份不动库）。

    用 sqlite3 在线备份 API（WAL 安全），产物落在同目录 `app.db.bak-<tag>-<ts>`；
    失败只告警不阻断（SQLite ADD COLUMN 是元数据操作，回滚=整库恢复该备份）。
    """
    try:
        import sqlite3 as _sq3

        from sqlalchemy.engine import make_url

        url = make_url(settings.database_url)
        if url.drivername != "sqlite" or not url.database or url.database == ":memory:":
            return
        src = Path(url.database).expanduser().resolve()
        if not src.exists():
            return
        ts = datetime.now().strftime("%Y%m%d-%H%M%S")
        dst = src.with_name(f"{src.stem}.bak-{tag}-{ts}{src.suffix}")
        with _sq3.connect(src) as con, _sq3.connect(dst) as bak:
            con.backup(bak)
        print(f"[migrate] DB 已备份（{tag} 列迁移前置）: {dst}", flush=True)
    except Exception as e:  # noqa: BLE001 —— 备份失败只告警，不阻断启动
        print(f"[warn] DB 迁移备份失败（继续；SQLite ADD COLUMN 回滚=整库恢复备份）: {e}", flush=True)


def _ensure_demo_columns() -> None:
    """SQLite 增量迁移：给已存在的 demos 表补充新增列（create_all 不会改旧表）。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "demos" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("demos")}
    additions = [
        ("demo_type", "TEXT NOT NULL DEFAULT 'web'"),
        ("external_url", "TEXT"),
        ("prompt", "TEXT NOT NULL DEFAULT ''"),
        ("video_url", "TEXT"),
        ("guest_name", "TEXT"),
        ("idempotency_key", "TEXT"),
        ("content_hash", "TEXT"),
        ("single_file", "TEXT"),
        ("rating_sum", "INTEGER NOT NULL DEFAULT 0"),
        ("rating_count", "INTEGER NOT NULL DEFAULT 0"),
        ("rating_avg", "REAL NOT NULL DEFAULT 0"),
        ("rating_god", "INTEGER NOT NULL DEFAULT 0"),
        ("rating_ghost", "INTEGER NOT NULL DEFAULT 0"),
        ("updated_at", "DATETIME"),
        ("prompt_id", "INTEGER"),
        ("sites", "TEXT NOT NULL DEFAULT 'deep'"),  # astra 橱窗可见域（逗号枚举）
        ("lang", "TEXT NOT NULL DEFAULT 'zh'"),     # 作品内容语言 zh|en
        # v2 B5′：Run 元数据收编（标签仍是对外写入面，列用于排序/聚合）
        ("gen_rounds", "INTEGER"),
        ("gen_minutes", "INTEGER"),
        ("gen_platform", "VARCHAR(32)"),
        ("model_hint", "TEXT NOT NULL DEFAULT ''"),
        # 计数列兜底：create_all 新库自带，但若生产库是远古备份/手工建表缺这两列，
        # 列表 ORDER BY view_count 会直接 500 —— 与其余列同一套自愈机制。
        ("view_count", "INTEGER NOT NULL DEFAULT 0"),
        ("download_count", "INTEGER NOT NULL DEFAULT 0"),
        # 首页策展（07 §2.2）：featured=1 进首页精选/hero 策展池；featured_order=排序位（1 起连续）
        ("featured", "BOOLEAN NOT NULL DEFAULT 0"),
        ("featured_order", "INTEGER"),
        # 列表缩略图（探索页题目行封面）：空 = 无缩略图，前端不渲染图片。
        # 存量回填走 scripts/backfill_cover_thumbs.py（先上线后端→再跑脚本，此前列表照旧无图）
        ("cover_thumb_url", "VARCHAR(500) NOT NULL DEFAULT ''"),
    ]
    if "featured" not in cols:
        # 迁移前置备份（07 §2.2 红线；仅首次加列时做一次）
        _sqlite_db_backup("featured")
    if "cover_thumb_url" not in cols:
        # 同上：新列首次添加也先备份（回滚 = 恢复该备份）
        _sqlite_db_backup("cover_thumb_url")
    with engine.begin() as conn:
        for name, ddl in additions:
            if name not in cols:
                conn.exec_driver_sql(f"ALTER TABLE demos ADD COLUMN {name} {ddl}")
        # 计数列 NULL 回填（幂等，无 NULL 时零行受影响）：NULL+1=NULL 会让
        # counters.py 的原子自增 UPDATE 永远停在 NULL（2026-09-04 计数事故排查假设 A）。
        conn.exec_driver_sql("UPDATE demos SET view_count = 0 WHERE view_count IS NULL")
        conn.exec_driver_sql("UPDATE demos SET download_count = 0 WHERE download_count IS NULL")
        # 幂等键唯一索引（SQLite 中 NULL 可重复，不影响无 key 的历史行）
        conn.exec_driver_sql("CREATE UNIQUE INDEX IF NOT EXISTS ix_demos_idempotency_key ON demos (idempotency_key)")
        # 内容哈希普通索引（按作者去重查询）
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_demos_content_hash ON demos (content_hash)")
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_demos_prompt_id ON demos (prompt_id)")
        # astra 橱窗：按可见域过滤列表的主索引
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_demos_sites ON demos (sites)")
        # 首页策展：featured=1 查询（列表量小，但避免无索引全表扫）
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_demos_featured ON demos (featured, featured_order)")


def _ensure_model_columns() -> None:
    """SQLite 增量迁移：models 表补 resolution（Q2 断言强度轴）。"""
    from sqlalchemy import inspect as sa_inspect

    from .models import Model

    insp = sa_inspect(engine)
    if "models" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("models")}
    if "resolution" not in cols:
        with engine.begin() as conn:
            conn.exec_driver_sql("ALTER TABLE models ADD COLUMN resolution TEXT NOT NULL DEFAULT 'exact'")
        # 存量回填：ds-unknown/unknown → guess；<vendor>-unknown 且有 vendor → family；unspecified → unknown
        db = SessionLocal()
        try:
            from .services import model_service

            for m in db.query(Model).all():
                m.resolution = model_service.infer_resolution(m.name, m.vendor, m.slug)
                if m.resolution == "guess":
                    m.status = "unverified"  # 存量灰测一并纠正到「猜测未证实」档
            db.commit()
        finally:
            db.close()


def _ensure_tag_key_columns() -> None:
    """SQLite 增量迁移：tag_keys 表补 tier（v2 重要性分层：1核心/2常用/3扩展）。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "tag_keys" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("tag_keys")}
    if "tier" not in cols:
        with engine.begin() as conn:
            conn.exec_driver_sql("ALTER TABLE tag_keys ADD COLUMN tier INTEGER NOT NULL DEFAULT 2")


def _ensure_tag_columns() -> None:
    """SQLite 增量迁移：给已存在的 tags 表补充 group 列（固定值分组/厂商）与 status 列。

    T3·M5-B2：status 列（Tag 状态机三态 candidate|active|deprecated，06 附录 B 实施）。
    存量全量视为 active（NOT NULL DEFAULT 'active' = 零行为变化，无需逐行回填；NULL
    行防御性归位）。部署前需对 data/*.db 手工备份（任务书前置动作；SQLite ADD COLUMN
    是元数据操作不重写表，回滚=恢复备份）。
    """
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "tags" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("tags")}
    with engine.begin() as conn:
        if "group" not in cols:
            # group 是 SQLite 保留字，必须加双引号
            conn.exec_driver_sql('ALTER TABLE tags ADD COLUMN "group" TEXT')
        if "status" not in cols:
            conn.exec_driver_sql("ALTER TABLE tags ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active'")
        # 防御性归位：历史上手工建表/异常写入产生的 NULL 不影响 DEFAULT 语义
        conn.exec_driver_sql("UPDATE tags SET status = 'active' WHERE status IS NULL")
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_tags_status ON tags (status)")


def _ensure_announcement_columns() -> None:
    """SQLite 增量迁移：给已存在的 announcements 表补充扩展列。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "announcements" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("announcements")}
    additions = [
        ("pinned", "BOOLEAN NOT NULL DEFAULT 0"),
        ("status", "TEXT NOT NULL DEFAULT 'published'"),
        ("category", "TEXT NOT NULL DEFAULT 'general'"),
        ("published_at", "DATETIME"),
        ("expires_at", "DATETIME"),
        ("topic_id", "INTEGER"),
    ]
    with engine.begin() as conn:
        for name, ddl in additions:
            if name not in cols:
                conn.exec_driver_sql(f"ALTER TABLE announcements ADD COLUMN {name} {ddl}")


def _add_column_if_missing(conn, table: str, name: str, ddl: str) -> None:  # type: ignore[no-untyped-def]
    """幂等加列：先读 PRAGMA 再决定是否 ALTER（重复启动不会因"列已存在"炸库）。"""
    cols = {row[1] for row in conn.exec_driver_sql(f"PRAGMA table_info({table})")}
    if name not in cols:
        conn.exec_driver_sql(f"ALTER TABLE {table} ADD COLUMN {name} {ddl}")


def _ensure_account_columns() -> None:
    """本轮（账号/收藏/历史）给 users 补三列：展示名、头像、历史开关。

    用 _add_column_if_missing 而不是照抄既有函数的写法 —— 既有几个函数各自手写存在性判断，
    新代码没必要再复制一份易错的样板。
    """
    with engine.begin() as conn:  # engine 由 .database 提供，main.py 顶部已导入
        _add_column_if_missing(conn, "users", "display_name", "VARCHAR(64) NOT NULL DEFAULT ''")
        _add_column_if_missing(conn, "users", "avatar_url", "VARCHAR(300) NOT NULL DEFAULT ''")
        _add_column_if_missing(conn, "users", "history_enabled", "BOOLEAN NOT NULL DEFAULT 1")


def _ensure_user_columns() -> None:
    """SQLite 增量迁移：给已存在的 users 表补充论坛信任字段。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "users" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("users")}
    additions = [
        ("trust_level", "INTEGER NOT NULL DEFAULT 0"),
        ("need_review", "BOOLEAN NOT NULL DEFAULT 1"),
        ("github_bound", "BOOLEAN NOT NULL DEFAULT 0"),
        ("reputation", "INTEGER NOT NULL DEFAULT 0"),
        ("received_likes", "INTEGER NOT NULL DEFAULT 0"),
        ("received_thanks", "INTEGER NOT NULL DEFAULT 0"),
    ]
    with engine.begin() as conn:
        for name, ddl in additions:
            if name not in cols:
                conn.exec_driver_sql(f"ALTER TABLE users ADD COLUMN {name} {ddl}")


def _ensure_forum_columns() -> None:
    """SQLite 增量迁移：给已存在的 forum_topics 表补充互动列（locked/solved）。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "forum_topics" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("forum_topics")}
    with engine.begin() as conn:
        for name, ddl in [("locked", "BOOLEAN NOT NULL DEFAULT 0"), ("solved", "BOOLEAN NOT NULL DEFAULT 0")]:
            if name not in cols:
                conn.exec_driver_sql(f"ALTER TABLE forum_topics ADD COLUMN {name} {ddl}")


def _ensure_forum_reply_columns() -> None:
    """SQLite 增量迁移：给已存在的 forum_replies 表补充 status / source_comment_id。"""
    from sqlalchemy import inspect as sa_inspect

    insp = sa_inspect(engine)
    if "forum_replies" not in insp.get_table_names():
        return
    cols = {c["name"] for c in insp.get_columns("forum_replies")}
    additions = [
        ("status", "TEXT NOT NULL DEFAULT 'normal'"),
        ("source_comment_id", "INTEGER"),
        ("parent_id", "INTEGER"),
    ]
    with engine.begin() as conn:
        for name, ddl in additions:
            if name not in cols:
                conn.exec_driver_sql(f"ALTER TABLE forum_replies ADD COLUMN {name} {ddl}")
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_forum_replies_status ON forum_replies (status)")
        conn.exec_driver_sql("CREATE INDEX IF NOT EXISTS ix_forum_replies_source_comment_id ON forum_replies (source_comment_id)")
        # 统一 reply_count 口径：只统计 normal 回复，顺带修复历史漂移/双重扣减
        conn.exec_driver_sql(
            """
            UPDATE forum_topics
            SET reply_count = (
                SELECT COUNT(*) FROM forum_replies
                WHERE forum_replies.topic_id = forum_topics.id
                  AND forum_replies.status = 'normal'
            )
            """
        )


def _seed_forum_notice() -> None:
    """论坛首帖初始化：幂等创建置顶的「用户须知 & 安全说明」。"""
    db = SessionLocal()
    try:
        if db.query(ForumTopic).filter(ForumTopic.category == "notice", ForumTopic.pinned == True).first():  # noqa: E712
            return
        admin = db.query(User).filter(User.role == "admin").first()
        path = settings.site_repo_path / "docs/论坛首帖-用户须知与安全说明.md"
        if path.exists():
            content = path.read_text(encoding="utf-8")
        else:
            content = "论坛发帖须知：请遵守法律法规，文明发言，不得发布违规内容。"
        db.add(ForumTopic(
            title="论坛发帖须知 & 安全说明",
            content=content,
            author_id=admin.id if admin else None,
            category="notice",
            tags="须知,安全",
            pinned=True,
            sticky=True,
            status="normal",
        ))
        db.commit()
    finally:
        db.close()


def init_db() -> None:
    settings.demos_path.mkdir(parents=True, exist_ok=True)
    settings.media_path.mkdir(parents=True, exist_ok=True)

    # 默认封面
    default_cover = settings.media_path / "covers" / "default.svg"
    default_cover.parent.mkdir(parents=True, exist_ok=True)
    if not default_cover.exists():
        default_cover.write_text(
            '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="480" viewBox="0 0 640 480">'
            '<rect width="640" height="480" fill="#4ecdc4"/>'
            '<rect x="14" y="14" width="612" height="452" fill="none" stroke="#000" stroke-width="8"/>'
            '<text x="320" y="250" font-family="Arial, sans-serif" font-size="64" font-weight="900" text-anchor="middle" fill="#000">DS DEMO</text>'
            '</svg>',
            encoding="utf-8",
        )
    if oss.enabled():
        try:
            oss.put_bytes(
                "media/covers/default.svg",
                default_cover.read_text(encoding="utf-8").encode(),
                "image/svg+xml",
                extra_headers={"Cache-Control": "public, max-age=86400, immutable"},
            )
        except Exception as e:  # noqa: BLE001 —— OSS 不可用不阻塞启动，降级本地存储
            print(f"[warn] OSS 默认封面上传失败（降级本地存储）: {e}", flush=True)

    Base.metadata.create_all(bind=engine)
    _ensure_demo_columns()
    # 旧库可能尚未执行 v2 全模型迁移；这里只补缺失的提示词关联，不动其他实体。
    # 版本标记（settings 主键）让稳态启动零扫描：完整回填过一次即不再跑；
    # 无标记的旧库照常执行，多进程并发首启两边都跑也幂等。FTS5 可用时同步建搜索索引。
    from .services.prompt_library_service import (
        backfill_missing_prompt_links_once,
        ensure_prompt_search_index,
        mark_backfill_done,
        mark_search_index_ready,
        search_index_marked,
        sync_prompt_search_index,
    )

    with SessionLocal() as prompt_db:
        index_created = ensure_prompt_search_index(prompt_db)
        # 标记缺失（升级首启/并发首启窗口）时也重灌一次：提交成功才打标，
        # 未打标期间所有查询回退 LIKE；稳态（标记在、表在）保持零扫描。
        fill_needed = index_created or not search_index_marked(prompt_db)
        linked = backfill_missing_prompt_links_once(prompt_db)
        if fill_needed:
            sync_prompt_search_index(prompt_db)
        prompt_db.commit()
    mark_backfill_done()
    if fill_needed:
        mark_search_index_ready()
    if linked:
        logger.info("启动回填：本次补关联 %s 件旧作品的 prompt_id", linked)
    _ensure_tag_columns()
    _ensure_tag_key_columns()
    _ensure_announcement_columns()
    _ensure_user_columns()
    _ensure_account_columns()
    _ensure_forum_columns()
    _ensure_forum_reply_columns()
    _ensure_model_columns()
    _seed_forum_notice()

    db = SessionLocal()
    try:
        # 标签键定义（固定值 / 开放值 / 数字值）——幂等：缺失才插入，已有数据不动
        # tier（v2 重要性分层）：1 核心 / 2 常用 / 3 扩展
        _DEFAULT_TAG_KEYS = [
            ("model", "fixed", "模型", "AI 模型版本（固定值）", 1, 1),
            ("plugin", "fixed", "插件", "使用的插件（固定值）", 2, 3),
            ("type", "fixed", "类型", "Demo 类型（固定值）", 3, 2),
            ("skills", "fixed", "技能", "技能工作区（固定值）", 4, 3),
            ("preset", "fixed", "预设", "预设配置（固定值）", 5, 3),
            ("category", "fixed", "分类", "作品分类（固定值）", 6, 2),
            ("game", "open", "游戏", "游戏名称（自定义值，如 mc / pvz）", 7, 2),
            ("rounds", "int", "轮数", "生成轮数（必须为整数）", 8, 3),
        ]
        for key, mode, label, description, sort, tier in _DEFAULT_TAG_KEYS:
            existing = db.get(TagKey, key)
            if existing is None:
                db.add(TagKey(key=key, mode=mode, label=label, description=description, sort=sort, tier=tier))
            else:
                # 默认键的 tier 以 seed 为准（ALTER 加列带 DEFAULT 2，不能靠 "空才写" 判断）
                existing.tier = tier

        # 默认标签（含历史自由值）——幂等：缺失才插入
        _DEFAULT_TAGS = [
            ("model", "dsv4", "模型版本总类", None),
            ("model", "dsv4-flash", "DeepSeek V4 Flash —— 快速推理", "dsv4"),
            ("model", "dsv4-pro", "DeepSeek V4 Pro —— 强推理", "dsv4"),
            ("model", "dsv4flash", "历史自由值：dsv4-flash 的旧写法", None),
            ("model", "ds-unknown", "历史自由值：未识别的模型", None),
            ("plugin", "routing-suite", "路由套件插件", None),
            ("plugin", "suite", "历史自由值：路由套件的旧写法", None),
            ("skills", "J-space", "J-space 技能工作区", None),
            ("skills", "j-space", "历史自由值：J-space 的旧写法", None),
            ("preset", "router-standard", "标准路由预设", None),
            ("preset", "spec", "历史自由值：规格预设", None),
            ("type", "effect", "视觉特效类", None),
            ("type", "widget", "小组件类", None),
            ("type", "game", "小游戏类", None),
            ("type", "demo", "综合演示类", None),
            ("category", "3D建模", "3D 建模类", None),
            ("category", "仿真", "仿真类", None),
            ("category", "动画", "动画类", None),
            ("category", "图形学", "图形学类", None),
        ]
        for key, value, description, parent_value in _DEFAULT_TAGS:
            if db.query(Tag).filter(Tag.key == key, Tag.value == value).first() is None:
                parent = None
                if parent_value:
                    parent = db.query(Tag).filter(Tag.key == key, Tag.value == parent_value).first()
                db.add(Tag(
                    key=key,
                    value=value,
                    description=description,
                    parent_id=parent.id if parent else None,
                ))

        if db.query(User).filter(User.username == "admin").first() is None:
            db.add(User(
                username="admin",
                password_hash=hash_password(settings.admin_initial_password),
                role="admin",
                bio="站点管理员",
            ))

        if db.get(Setting, KEY_AUTO_APPROVE) is None:
            db.add(Setting(key=KEY_AUTO_APPROVE, value="true" if settings.auto_approve else "false"))

        db.commit()
    finally:
        db.close()

    _ensure_fallback_models()


def _ensure_fallback_models() -> None:
    """兜底型号齐备（Q2）：全局 `unspecified` + 每个已知厂商一个 `<vendor>-unknown` 族节点。

    「不确定」必须有正门，否则强制 model 只会把人和 agent 逼成瞎填。
    启动失败不阻塞站点（与论坛首帖、OSS 同步同款容错）。
    """
    from .services import model_service

    db = SessionLocal()
    try:
        model_service.ensure_fallback_models(db)
    except Exception as e:  # noqa: BLE001
        logger.warning("兜底型号初始化失败（不阻塞启动）: %s", e)
        db.rollback()
    finally:
        db.close()


def _check_secrets() -> None:
    """默认密钥/口令告警（KB-23）：默认只告警不阻断；STRICT_SECRETS=true 时拒绝启动。"""
    warnings = settings.secret_warnings()
    for w in warnings:
        logger.warning("[security] %s", w)
    if warnings and settings.strict_secrets:
        raise RuntimeError("拒绝启动：默认密钥/口令未替换（STRICT_SECRETS=true）—— " + "；".join(warnings))


@app.on_event("startup")
def on_startup() -> None:
    _check_secrets()
    init_db()
    _auto_sync_oss()


def _auto_sync_oss() -> None:
    """OSS 可用时，后台任务把本地已有文件补传到 OSS（幂等：只补缺失，不阻塞启动）。"""
    from .services import oss as _oss
    from .services.oss_sync import start_sync

    if not _oss.enabled():
        return
    try:
        started = start_sync(force=False)
        print(f"[oss-sync] 启动自动同步: started={started}", flush=True)
    except Exception as e:  # noqa: BLE001
        print(f"[oss-sync] 自动同步启动失败: {e}", flush=True)
