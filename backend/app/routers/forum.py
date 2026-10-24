"""论坛：主题 + 回复。发帖/回复需登录；匿名可读；新用户进审核；管理端可管 hidden/reviewing/封禁/举报。"""

import time
from collections import defaultdict
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Query, Request
from sqlalchemy import func
from sqlalchemy.orm import Session

from ..client_ip import get_client_ip
from ..database import get_db
from ..domains.identity.deps import current_user, optional_user, require_admin
from ..models import Announcement, Demo, ForumReply, ForumReport, ForumTopic, User, UserFollow
from ..schemas import (
    ForumReplyIn,
    ForumReplyOut,
    ForumReplyPage,
    ForumReportHandleIn,
    ForumReportIn,
    ForumReportOut,
    ForumReviewIn,
    ForumTopicAdminUpdate,
    ForumTopicIn,
    ForumTopicOut,
    ForumTopicPage,
    ReactionSummary,
    ReactionToggleIn,
    ReactionToggleOut,
)
from ..services import audit_service, community_service, counters, forum_service, notification_service, ratelimit

router = APIRouter(prefix="/forum", tags=["forum"])

# 发帖/回复限流：用户 + IP 双维度（实现见 services.ratelimit）
_TOPIC_RATE = 10
_REPLY_RATE = 30


def _client_ip(request: Request) -> str:
    return get_client_ip(request) or "unknown"


def _topic_snapshot(t: ForumTopic) -> dict:
    """主题治理字段快照（KB-17 审计 before/after 用）。"""
    return {
        "id": t.id,
        "title": t.title,
        "status": t.status,
        "pinned": t.pinned,
        "sticky": t.sticky,
        "locked": t.locked,
        "solved": t.solved,
        "category": t.category,
        "tags": t.tags,
    }


def _bump_reply_count(db: Session, topic_id: int, delta: int) -> None:
    """主题回复数原子增减（KB-18）。

    旧写法是 ORM 读改写（`topic.reply_count += 1`），并发两条回复各自读到同值再写回 →
    丢更新；删除走级联时还只扣 1。这里用 `SET reply_count = reply_count + delta`
    并在扣减时夹住 0（历史漂移不至于把计数写成负数）。
    """
    if not delta:
        return
    q = db.query(ForumTopic).filter(ForumTopic.id == topic_id)
    if delta < 0:
        q = q.filter(ForumTopic.reply_count >= -delta)
    q.update({ForumTopic.reply_count: ForumTopic.reply_count + delta}, synchronize_session=False)


def _rate_limit(request: Request, key: str, limit: int, user: User) -> None:
    """发帖/回复限流：用户 + IP 双维度（KB-21：统一走 services.ratelimit）。"""
    ip = _client_ip(request)
    ratelimit.hit(f"forum:{key}:user:{user.id}", limit, 3600)
    ratelimit.hit(f"forum:{key}:ip:{ip}", limit, 3600)


# ---------- 公开 ----------
@router.get("/topics", response_model=ForumTopicPage)
def list_topics(
    q: str | None = None,
    category: str | None = None,
    tag: str | None = None,
    demo: str | None = None,
    sort: str = Query(default="newest", pattern="^(newest|popular|replies|hot)$"),
    sticky: bool = Query(False),
    participated: bool = Query(False),
    kind: str | None = Query(default=None, pattern="^(general|demo)$"),
    followed: bool = Query(False),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    query = db.query(ForumTopic).filter(ForumTopic.status == "normal")
    if q:
        like = f"%{q}%"
        query = query.filter((ForumTopic.title.ilike(like)) | (ForumTopic.content.ilike(like)))
    if category:
        query = query.filter(ForumTopic.category == category)
    if tag:
        # KB-25：tags 是逗号分隔列表，必须精确匹配成员（ilike 子串会让 "demo" 命中 "demoscene"）
        query = query.filter(func.concat(",", ForumTopic.tags, ",").like(f"%,{tag},%"))
    if demo:
        query = query.filter(ForumTopic.demo_slug == demo)
    if kind == "demo":
        query = query.filter(ForumTopic.demo_slug.isnot(None))
    elif kind == "general":
        query = query.filter(ForumTopic.demo_slug.is_(None))
    if sticky:
        query = query.filter(ForumTopic.sticky == True)  # noqa: E712
    if participated:
        if user is None:
            return ForumTopicPage(items=[], total=0, page=page, page_size=page_size)
        my_topic_ids = db.query(ForumReply.topic_id).filter(ForumReply.author_id == user.id).distinct()
        query = query.filter(ForumTopic.id.in_(my_topic_ids))
    if followed:
        if user is None:
            return ForumTopicPage(items=[], total=0, page=page, page_size=page_size)
        followed_ids = db.query(UserFollow.following_id).filter(UserFollow.follower_id == user.id)
        query = query.filter(ForumTopic.author_id.in_(followed_ids))

    if sort == "popular":
        query = query.order_by(ForumTopic.view_count.desc(), ForumTopic.created_at.desc(), ForumTopic.id.desc())
    elif sort == "replies":
        query = query.order_by(ForumTopic.reply_count.desc(), ForumTopic.created_at.desc(), ForumTopic.id.desc())
    elif sort == "hot":
        # 热度 = 回复数 + 浏览/50 + 时间衰减（时间衰减用 created_at 兜底）
        query = query.order_by(
            (ForumTopic.reply_count + ForumTopic.view_count / 50.0).desc(),
            ForumTopic.created_at.desc(),
            ForumTopic.id.desc(),
        )
    else:
        query = query.order_by(
            ForumTopic.pinned.desc(), ForumTopic.sticky.desc(), ForumTopic.created_at.desc(), ForumTopic.id.desc()
        )

    total = query.count()
    items = query.offset((page - 1) * page_size).limit(page_size).all()
    viewer_id = user.id if user else None
    # KB-19：互动汇总一次批量取，不再逐主题查（page_size=100 旧实现约 200 条 SQL）
    summaries = community_service.reaction_summaries(db, "topic", [t.id for t in items], viewer_id)
    return ForumTopicPage(
        items=[forum_service.topic_out(t, db, viewer_id, summary=summaries.get(t.id)) for t in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.get("/topics/{tid}", response_model=ForumTopicOut)
def get_topic(
    tid: int,
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    t = forum_service.find_visible_topic(db, tid)
    # 计数走内存批次 + 30s 落库：读路径零写事务（同 demos 的 view_count 处理）
    counters.bump("topic_view", t.id)
    t.view_count += 1
    return forum_service.topic_out(t, db, user.id if user else None)


@router.get("/topics/{tid}/replies", response_model=ForumReplyPage)
def list_replies(
    tid: int,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=50, ge=1, le=200),
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    forum_service.find_visible_topic(db, tid)
    q = db.query(ForumReply).filter(ForumReply.topic_id == tid, ForumReply.status == "normal")
    total = q.count()
    rows = (
        q.order_by(ForumReply.created_at, ForumReply.id)
        .offset((page - 1) * page_size)
        .limit(page_size)
        .all()
    )
    viewer_id = user.id if user else None
    # KB-19：回复列表同样批量取互动汇总
    summaries = community_service.reaction_summaries(db, "reply", [r.id for r in rows], viewer_id)
    return ForumReplyPage(
        items=[forum_service.reply_out(r, db, viewer_id, summary=summaries.get(r.id)) for r in rows],
        total=total,
        page=page,
        page_size=page_size,
    )


# ---------- 互动（赞/感谢） ----------
@router.get("/reactions/summary", response_model=ReactionSummary)
def get_reaction_summary(
    target_type: str = Query(pattern="^(topic|reply)$"),
    target_id: int = Query(ge=1),
    db: Session = Depends(get_db),
    user: User | None = Depends(optional_user),
):
    return community_service.visible_reaction_summary(db, target_type, target_id, user.id if user else None)


@router.post("/reactions", response_model=ReactionToggleOut)
def toggle_reaction(
    body: ReactionToggleIn,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    return community_service.toggle_reaction(db, user, body.target_type, body.target_id, body.reaction_type)


# ---------- 登录 ----------
@router.post("/topics", status_code=201, response_model=ForumTopicOut)
def create_topic(
    body: ForumTopicIn,
    request: Request,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    _rate_limit(request, "topic", _TOPIC_RATE, user)
    forum_service.validate_links(body.content)
    if body.demo_slug:
        demo = db.query(Demo).filter(Demo.slug == body.demo_slug, Demo.status == "approved").first()
        if demo is None:
            raise HTTPException(status_code=422, detail="关联 demo 不存在或未上线", )
    status = "reviewing" if forum_service.needs_review(user) else "normal"
    category = "demo" if body.demo_slug else (body.category or "general")
    t = ForumTopic(
        title=body.title,
        content=body.content,
        author_id=user.id,
        demo_slug=body.demo_slug,
        category=category,
        tags=body.tags,
        status=status,
    )
    db.add(t)
    db.commit()
    db.refresh(t)
    return forum_service.topic_out(t, db, user.id)


@router.post("/topics/{tid}/replies", status_code=201, response_model=ForumReplyOut)
def create_reply(
    tid: int,
    body: ForumReplyIn,
    request: Request,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    t = forum_service.find_visible_topic(db, tid)
    if t.locked:
        raise HTTPException(status_code=403, detail="该主题已关闭讨论", )
    _rate_limit(request, "reply", _REPLY_RATE, user)
    forum_service.validate_links(body.content)
    parent_id = None
    if body.parent_id is not None:
        parent = db.get(ForumReply, body.parent_id)
        if parent is None or parent.topic_id != t.id or parent.status != "normal":
            raise HTTPException(status_code=422, detail="父回复不存在或不属于该主题", )
        parent_id = parent.id
    status = "reviewing" if forum_service.needs_review(user) else "normal"
    r = ForumReply(topic_id=t.id, author_id=user.id, content=body.content, status=status, parent_id=parent_id)
    db.add(r)
    db.flush()
    if status == "normal":
        _bump_reply_count(db, t.id, +1)  # KB-18：原子自增，不再读改写
    t.updated_at = datetime.utcnow()
    db.commit()
    db.refresh(r)
    # 通知：主题作者（非本人）+ @提及用户
    if t.author_id and t.author_id != user.id:
        notification_service.create(
            user_id=t.author_id,
            type="forum_reply",
            actor_id=user.id,
            topic_id=t.id,
            reply_id=r.id,
        )
    exclude = {user.id, t.author_id or -1}
    notification_service.notify_mentions(body.content, user.id, t.id, r.id, exclude)
    return forum_service.reply_out(r, db, user.id)


# ---------- 举报 ----------
@router.post("/reports", status_code=201, response_model=ForumReportOut)
def create_report(
    body: ForumReportIn,
    request: Request,
    db: Session = Depends(get_db),
    user: User = Depends(current_user),
):
    _rate_limit(request, "report", 20, user)
    # KB-25：目标必须存在且可见；同一人对同一目标已有 open 举报 → 409（不堆重复）
    forum_service.validate_report_target(db, body.target_type, body.target_id)
    dup = (
        db.query(ForumReport)
        .filter(
            ForumReport.target_type == body.target_type,
            ForumReport.target_id == body.target_id,
            ForumReport.reporter_id == user.id,
            ForumReport.status == "open",
        )
        .first()
    )
    if dup is not None:
        raise HTTPException(status_code=409, detail="你已举报过该内容，管理员正在处理", )
    r = ForumReport(
        target_type=body.target_type,
        target_id=body.target_id,
        reporter_id=user.id,
        reason=body.reason,
    )
    db.add(r)
    db.commit()
    db.refresh(r)
    return r


# ---------- 管理 ----------
@router.get("/admin/topics", response_model=ForumTopicPage)
def admin_list_topics(
    q: str | None = None,
    status: str | None = Query(default=None, pattern="^(normal|hidden|reviewing)$"),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    query = db.query(ForumTopic)
    if q:
        like = f"%{q}%"
        query = query.filter((ForumTopic.title.ilike(like)) | (ForumTopic.content.ilike(like)))
    if status:
        query = query.filter(ForumTopic.status == status)
    total = query.count()
    items = query.order_by(ForumTopic.created_at.desc(), ForumTopic.id.desc()).offset((page - 1) * page_size).limit(page_size).all()
    return ForumTopicPage(
        items=[forum_service.topic_out(t, db) for t in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.get("/admin/replies", response_model=list[ForumReplyOut])
def admin_list_replies(
    topic_id: int | None = None,
    status: str | None = Query(default=None, pattern="^(normal|hidden|reviewing)$"),
    q: str | None = Query(default=None, description="搜回复内容或所属主题标题"),
    limit: int = Query(default=50, ge=1, le=200),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    """管理端回复列表：**默认全局**（不必先选主题），可按主题/状态/关键词过滤。

    带 topic_title 是因为：跨主题的回复列表如果不知道每条属于哪个帖子，
    管理员就只能回到"先选主题"的老路 —— 那正是这次要修的交互。
    不逐条算表情反应（管理列表用不到，省掉 N 次查询）。
    """
    query = db.query(ForumReply, ForumTopic.title).join(ForumTopic, ForumTopic.id == ForumReply.topic_id)
    if topic_id is not None:
        query = query.filter(ForumReply.topic_id == topic_id)
    if status:
        query = query.filter(ForumReply.status == status)
    if q:
        like = f"%{q.strip()}%"
        query = query.filter(ForumReply.content.ilike(like) | ForumTopic.title.ilike(like))
    rows = query.order_by(ForumReply.created_at.desc(), ForumReply.id.desc()).limit(limit).all()
    out = []
    for reply, topic_title in rows:
        item = forum_service.reply_out(reply)
        item.topic_title = topic_title
        out.append(item)
    return out


@router.put("/admin/topics/{tid}", response_model=ForumTopicOut)
def admin_update_topic(
    tid: int,
    body: ForumTopicAdminUpdate,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    """改主题属性（置顶/精华/锁定/已解决/分类/状态）。KB-17：同事务落审计。"""
    t = db.get(ForumTopic, tid)
    if t is None:
        raise HTTPException(status_code=404, detail="主题不存在", )
    before = _topic_snapshot(t)
    if body.title is not None:
        t.title = body.title
    if body.tags is not None:
        t.tags = body.tags
    if body.pinned is not None:
        t.pinned = body.pinned
    if body.sticky is not None:
        t.sticky = body.sticky
    if body.locked is not None:
        t.locked = body.locked
    if body.solved is not None:
        t.solved = body.solved
    if body.category is not None:
        t.category = body.category
    if body.status is not None:
        t.status = body.status
    if before != _topic_snapshot(t):
        audit_service.record(
            db, action="update", entity_type="forum_topic", entity_id=t.id, actor_id=admin.id,
            before=before, after=_topic_snapshot(t), reason=f"管理端编辑主题《{t.title}》",
        )
    db.commit()
    db.refresh(t)
    return forum_service.topic_out(t, db)


@router.post("/admin/topics/{tid}/review", response_model=ForumTopicOut)
def admin_review_topic(
    tid: int,
    body: ForumReviewIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    t = db.get(ForumTopic, tid)
    if t is None or t.status != "reviewing":
        raise HTTPException(status_code=404, detail="主题不存在或不在审核中", )
    before = _topic_snapshot(t)
    if body.action == "approve":
        t.status = "normal"
        if t.author and (t.author.need_review or t.author.trust_level < 1):
            t.author.trust_level = 1
            t.author.need_review = False
    else:
        t.status = "hidden"
    audit_service.record(
        db, action="review", entity_type="forum_topic", entity_id=t.id, actor_id=admin.id,
        before=before, after=_topic_snapshot(t),
        reason=f"审核{'通过' if body.action == 'approve' else '隐藏'}主题《{t.title}》",
    )
    db.commit()
    db.refresh(t)
    return forum_service.topic_out(t, db)


@router.post("/admin/replies/{rid}/review", response_model=ForumReplyOut)
def admin_review_reply(
    rid: int,
    body: ForumReviewIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    r = db.get(ForumReply, rid)
    if r is None or r.status != "reviewing":
        raise HTTPException(status_code=404, detail="回复不存在或不在审核中", )
    before_status = r.status
    if body.action == "approve":
        r.status = "normal"
        _bump_reply_count(db, r.topic_id, +1)  # KB-18：原子自增
        if r.author and (r.author.need_review or r.author.trust_level < 1):
            r.author.trust_level = 1
            r.author.need_review = False
    else:
        r.status = "hidden"
    audit_service.record(
        db, action="review", entity_type="forum_reply", entity_id=r.id, actor_id=admin.id,
        before={"status": before_status, "topic_id": r.topic_id},
        after={"status": r.status, "topic_id": r.topic_id},
        reason=f"审核{'通过' if body.action == 'approve' else '隐藏'}回复 #{r.id}",
    )
    db.commit()
    db.refresh(r)
    return forum_service.reply_out(r, db)


@router.delete("/admin/topics/{tid}", status_code=204)
def admin_delete_topic(tid: int, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """删除主题（回复级联）。KB-17：不可逆治理动作必须留痕。"""
    t = db.get(ForumTopic, tid)
    if t is None:
        raise HTTPException(status_code=404, detail="主题不存在", )
    before = {**_topic_snapshot(t), "reply_count": t.reply_count}
    db.query(Announcement).filter(Announcement.topic_id == tid).update({Announcement.topic_id: None})
    community_service.delete_reactions_for_topic(db, tid)
    db.delete(t)  # replies 级联删除
    audit_service.record(
        db, action="delete", entity_type="forum_topic", entity_id=tid, actor_id=admin.id,
        before=before, reason=f"删除主题《{t.title}》",
    )
    db.commit()
    return None


@router.delete("/admin/replies/{rid}", status_code=204)
def admin_delete_reply(rid: int, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    r = db.get(ForumReply, rid)
    if r is None:
        raise HTTPException(status_code=404, detail="回复不存在", )
    topic = db.get(ForumTopic, r.topic_id)
    community_service.delete_reactions_for_reply_tree(db, rid)
    # KB-18：parent_id 带 ondelete=CASCADE，删父回复会连带删整棵子树 ——
    # 旧实现只 `-= 1`，少扣 N-1 条，reply_count 从此偏高（重启时那条全量重算才会自愈）。
    subtree = forum_service.reply_subtree_ids(db, rid)
    removed = (
        db.query(func.count(ForumReply.id))
        .filter(ForumReply.id.in_(subtree), ForumReply.status == "normal")
        .scalar()
        or 0
    )
    db.delete(r)
    db.flush()
    if removed and topic is not None:
        _bump_reply_count(db, topic.id, -removed)
    audit_service.record(
        db, action="delete", entity_type="forum_reply", entity_id=rid, actor_id=admin.id,
        before={"topic_id": r.topic_id, "status": r.status, "subtree_normal_removed": removed},
        reason=f"删除回复 #{rid}（连带子树 {len(subtree)} 条）",
    )
    db.commit()
    return None


@router.post("/admin/users/{uid}/ban", status_code=204)
def admin_ban_user(uid: int, db: Session = Depends(get_db), admin: User = Depends(require_admin)):
    """封禁用户。KB-17：封禁是不可逆治理动作，必须留痕（谁封的、封前状态）。"""
    u = db.get(User, uid)
    if u is None:
        raise HTTPException(status_code=404, detail="用户不存在", )
    if u.role == "admin":
        raise HTTPException(status_code=400, detail="不能封禁管理员", )
    if u.status == "banned":
        raise HTTPException(status_code=409, detail="该用户已被封禁", )
    before = {"username": u.username, "status": u.status, "role": u.role}
    u.status = "banned"
    audit_service.record(
        db, action="status_set", entity_type="user", entity_id=u.id, actor_id=admin.id,
        before=before, after={"username": u.username, "status": u.status, "role": u.role},
        reason=f"封禁用户 {u.username}",
    )
    db.commit()
    return None


@router.get("/admin/reports", response_model=list[ForumReportOut])
def admin_list_reports(
    status: str | None = Query(default=None, pattern="^(open|resolved|dismissed)$"),
    db: Session = Depends(get_db),
    _: User = Depends(require_admin),
):
    q = db.query(ForumReport)
    if status:
        q = q.filter(ForumReport.status == status)
    return q.order_by(ForumReport.created_at.desc()).all()


@router.post("/admin/reports/{rid}/handle", response_model=ForumReportOut)
def admin_handle_report(
    rid: int,
    body: ForumReportHandleIn,
    db: Session = Depends(get_db),
    admin: User = Depends(require_admin),
):
    r = db.get(ForumReport, rid)
    if r is None:
        raise HTTPException(status_code=404, detail="举报不存在", )
    if r.status != "open":
        raise HTTPException(status_code=409, detail="该举报已处理", )
    before = {"status": r.status, "target_type": r.target_type, "target_id": r.target_id}
    r.status = "resolved" if body.action == "resolve" else "dismissed"
    audit_service.record(
        db, action="review", entity_type="forum_report", entity_id=r.id, actor_id=admin.id,
        before=before, after={"status": r.status, "target_type": r.target_type, "target_id": r.target_id},
        reason=f"举报处理：{r.status}",
    )
    db.commit()
    db.refresh(r)
    if r.reporter_id:
        notification_service.create(
            user_id=r.reporter_id,
            type="report_handled",
            actor_id=admin.id,
            topic_id=r.target_id if r.target_type == "topic" else None,
            reply_id=r.target_id if r.target_type == "reply" else None,
        )
    return r
