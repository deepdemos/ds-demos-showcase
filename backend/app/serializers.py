from sqlalchemy import func
from sqlalchemy.orm import Session

from .config import settings
from .models import Demo, DemoModel, DemoTask, DemoTag, DemoTimeline, ForumTopic, Model, SessionLog, Tag, TagKey, Task, User
from .services import oss
from .services.scope import ASTRA, current_scope


def preload_demo_relations(db: Session, demos: list[Demo]) -> None:
    """列表页批量预加载：tags/models/tasks 关联 + 讨论/会话日志计数。

    消除 serialize_demo 的 N+1（每页 20 条原本 ~60 条查询 → 5 条）。
    预载结果挂 demo 实例的 _pre_* 属性，serialize_demo 优先消费。
    """
    if not demos:
        return
    ids = [d.id for d in demos]

    tags_by: dict[int, list] = {}
    for link, tag in (
        db.query(DemoTag, Tag)
        .join(Tag, Tag.id == DemoTag.tag_id)
        # T3·M5-B2 与模型同口径：deprecated 词表值已退役，不进公开作品卡（标签 chips 不留死链）
        .filter(DemoTag.demo_id.in_(ids), Tag.status != "deprecated")
        .all()
    ):
        tags_by.setdefault(link.demo_id, []).append({"key": tag.key, "value": tag.value})

    models_by: dict[int, list] = {}
    for did, m in (
        db.query(DemoModel.demo_id, Model)
        .join(Model, Model.id == DemoModel.model_id)
        # 评审与重排.md §三.1 裁决：deprecated（已并入别处的空壳）不进公开页面；
        # candidate 保留（作者上传时自己选的模型，前端 pending 徽章兜底）。
        .filter(Model.status != "deprecated")
        .filter(DemoModel.demo_id.in_(ids))
        .all()
    ):
        models_by.setdefault(did, []).append(m)

    tasks_by: dict[int, list] = {}
    for did, t in (
        db.query(DemoTask.demo_id, Task)
        .join(Task, Task.id == DemoTask.task_id)
        # 与模型同口径：merged（已并入）/hidden（下架）的题目是空壳，不进公开页
        .filter(Task.status.notin_(("merged", "hidden")))
        .filter(DemoTask.demo_id.in_(ids))
        .all()
    ):
        tasks_by.setdefault(did, []).append(t)

    # T3·M5-B1（用户裁决 b）：讨论数口径从已退役 Comment 表切到关联论坛主题的回复数。
    # 评论系统已 410、发言导流论坛；历史评论经 migrate_comments_to_forum.py 迁为
    # demo_slug 关联主题的回复——reply_count 同样覆盖历史数。字段名 comment_count
    # 保持不变（前端 DemoCard/DemoView 绑定不动，值语义=讨论数）。
    # 只算 normal 主题（hidden/reviewing 主题的回复不进公开口径）。
    # 注意键是 slug（forum_topics.demo_slug 关联 demos.slug，非 id）——下文按 d.slug 取。
    slugs = [d.slug for d in demos]
    comment_counts: dict[str, int] = {}
    if slugs:
        comment_counts = {
            slug: int(total or 0)
            for slug, total in (
                db.query(ForumTopic.demo_slug, func.sum(ForumTopic.reply_count))
                .filter(ForumTopic.demo_slug.in_(slugs), ForumTopic.status == "normal")
                .group_by(ForumTopic.demo_slug)
                .all()
            )
        }
    # 作者：批量取用户名（serialize 里一旦碰 demo.author 关系就会逐行懒加载，
    # 实测 637 规模 page_size=20 因此多出发 11 条查询，page_size=100 涨到 49）
    author_ids = {d.author_id for d in demos if d.author_id}
    authors = (
        dict(db.query(User.id, User.username).filter(User.id.in_(author_ids)).all())
        if author_ids
        else {}
    )
    log_counts = dict(
        db.query(SessionLog.demo_id, func.count(SessionLog.id))
        .filter(SessionLog.demo_id.in_(ids))
        .group_by(SessionLog.demo_id)
        .all()
    )

    for d in demos:
        d._pre_tags = tags_by.get(d.id, [])
        d._pre_models = models_by.get(d.id, [])
        d._pre_tasks = tasks_by.get(d.id, [])
        # comment_counts 以 demo_slug 为键（forum 关联口径）；d.id 是恒 0 的错键（前手半成品遗留）
        d._pre_comment_count = comment_counts.get(d.slug, 0)
        d._pre_session_log_count = log_counts.get(d.id, 0)
        d._pre_author = authors.get(d.author_id) if d.author_id else None


def tag_dict(db: Session, tag: Tag) -> dict:
    demo_count = (
        db.query(func.count(DemoTag.demo_id))
        .filter(DemoTag.tag_id == tag.id)
        .scalar()
        or 0
    )
    child_count = db.query(func.count(Tag.id)).filter(Tag.parent_id == tag.id).scalar() or 0
    key_def = db.get(TagKey, tag.key)
    return {
        "id": tag.id,
        "key": tag.key,
        "value": tag.value,
        "description": tag.description,
        "parent_id": tag.parent_id,
        "demo_count": demo_count,
        "child_count": child_count,
        "mode": key_def.mode if key_def else "open",
        # T3·M5-B2：状态机字段随实体输出（徽章读同字段）；tag_dict 是 admin/公开共用输出
        "status": tag.status or "active",
    }


_NO_AUTHOR = object()  # 区分「未预加载」与「预加载结果为 public」


def serialize_demo(
    db: Session,
    demo: Demo,
    current_user_id: int | None = None,
    detail: bool = False,
) -> dict:
    pre_author = getattr(demo, "_pre_author", _NO_AUTHOR)
    if pre_author is _NO_AUTHOR:
        author: User | None = demo.author
        author_name = author.username if author else "public"
    else:
        author_name = pre_author or "public"
    pre_tags = getattr(demo, "_pre_tags", None)
    tags = pre_tags if pre_tags is not None else [
        # T3·M5-B2：fallback 同口径剔除 deprecated（与 preload 的 SQL 过滤一致）
        {"key": dt.tag.key, "value": dt.tag.value}
        for dt in demo.tag_associations
        if (dt.tag.status or "active") != "deprecated"
    ]
    comment_count = getattr(demo, "_pre_comment_count", None)
    if comment_count is None:
        # 单条（未预加载）兜底：与 preload 同一 forum 口径——normal 主题的回复数合计
        comment_count = (
            db.query(func.coalesce(func.sum(ForumTopic.reply_count), 0))
            .filter(ForumTopic.demo_slug == demo.slug, ForumTopic.status == "normal")
            .scalar()
            or 0
        )
    session_log_count = getattr(demo, "_pre_session_log_count", None)
    if session_log_count is None:
        session_log_count = (
            db.query(func.count(SessionLog.id)).filter(SessionLog.demo_id == demo.id).scalar() or 0
        )

    pre_models = getattr(demo, "_pre_models", None)
    model_rows = pre_models if pre_models is not None else [m for m in demo.models if m.status != "deprecated"]
    models_out = [
        {
            "id": m.id,
            "slug": m.slug,
            "name": m.name,
            "vendor": m.vendor,
            "status": m.status,
            "resolution": m.resolution,
        }
        for m in model_rows
    ]
    pre_tasks = getattr(demo, "_pre_tasks", None)
    task_rows = pre_tasks if pre_tasks is not None else [t for t in demo.tasks if t.status not in ("merged", "hidden")]
    tasks_out = [{"id": t.id, "slug": t.slug, "title": t.title} for t in task_rows]

    # 版本 key 必须只随「内容变化」变：用内容指纹（文件变了 hash 才变）。
    # 不能用 updated_at —— 它曾被 onupdate 在每次浏览/下载/评分时刷新（2026-09-03 事故），
    # 导致每次浏览都换新 URL、CDN 缓存命中率归零、全站预览流量回源。
    # 历史数据无 content_hash 时退到 created_at（内容不变则版本稳定）。
    version = (demo.content_hash or "")[:12] or str(int(demo.created_at.timestamp()))
    preview_ext = "svg" if demo.single_file == "svg" else "html"
    scope = current_scope.get()
    preview_path = f"/preview/{demo.slug}/v{version}/index.{preview_ext}"
    if demo.demo_type != "web":
        preview_url = ""
    elif scope == ASTRA:
        # astra 橱窗预览域（可选增值）；留空 = 同源相对路径（橱窗 nginx 块自行服务 /preview）
        preview_url = settings.astra_preview_base_url.rstrip("/") + preview_path if settings.astra_preview_base_url else preview_path
    elif settings.preview_base_url:
        preview_url = f"{settings.preview_base_url.rstrip('/')}{preview_path}"
    else:
        preview_url = preview_path

    # astra 橱窗输出层（只改响应形态，不碰存储数据）：作者统一实验室署名、剔除内部标签
    author_out = author_name
    if scope == ASTRA:
        author_out = "astra lab"
        tags = [t for t in tags if t.get("key") not in ("author", "version-of")]

    data = {
        "slug": demo.slug,
        "title": demo.title,
        "description": demo.description,
        "cover_url": demo.cover_url,
        "demo_type": demo.demo_type,
        "external_url": demo.external_url,
        "preview_url": preview_url,
        "author": author_out,
        "author_id": demo.author_id,
        "tags": tags,
        "models": models_out,
        "tasks": tasks_out,
        "view_count": demo.view_count,
        "download_count": demo.download_count,
        "comment_count": comment_count,
        "created_at": demo.created_at,
        "updated_at": demo.updated_at,
        "status": demo.status,
        "rating_avg": round(demo.rating_avg or 0.0, 2),
        "rating_count": demo.rating_count,
        "rating_god": demo.rating_god,
        "rating_ghost": demo.rating_ghost,
        "prompt": demo.prompt,
        "gen_rounds": demo.gen_rounds,
        "gen_minutes": demo.gen_minutes,
        # 策展字段：仅 AdminDemoOut 声明消费，公开 schema 未声明 → pydantic 自动剥离
        "sites": demo.sites,
        "lang": demo.lang,
    }

    if detail:
        from .services.storage import demo_files_dir, demo_storage_size

        timeline = (
            db.query(DemoTimeline)
            .filter(DemoTimeline.demo_id == demo.id)
            .order_by(DemoTimeline.created_at.desc(), DemoTimeline.id.desc())
            .all()
        )
        files_dir = demo_files_dir(demo.slug)
        data.update(
            {
                "session_log_count": session_log_count,
                "is_author": bool(current_user_id is not None and demo.author_id == current_user_id),
                "prompt": demo.prompt,
                "model_hint": demo.model_hint or "",
                # KB-29：单个文件作品（html/svg）此前**没有**序列化出来 ——
                # 前端据此决定下载按钮写「下载文件」还是「下载 ZIP」，
                # 缺字段导致单文件作品永远显示成 ZIP（详情页文案与实际下载内容不符）。
                "single_file": demo.single_file,
                "video_url": demo.video_url,
                "file_size": files_dir.stat().st_size if files_dir.exists() else None,
                "storage_size": demo_storage_size(demo.slug),
                "inconsistency": demo.demo_type != "link" and not files_dir.exists(),
                "timeline": [
                    {
                        "id": t.id,
                        "version_label": t.version_label,
                        "message": t.message,
                        "old_slug": t.old_slug,
                        "created_at": t.created_at,
                    }
                    for t in timeline
                ],
            }
        )
    return data
