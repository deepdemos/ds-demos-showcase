from datetime import datetime, timezone

from sqlalchemy import (
    Boolean,
    DateTime,
    Float,
    ForeignKey,
    Index,
    Integer,
    String,
    Text,
    UniqueConstraint,
)
from sqlalchemy.orm import Mapped, mapped_column, relationship

from .database import Base


def utcnow() -> datetime:
    return datetime.now(timezone.utc).replace(tzinfo=None)


# 用户状态全集（KB-22）：模型注释、PATCH 校验、前端下拉共用这一份，别再各写一遍
USER_STATUSES = ("active", "suspended", "banned", "deleted")


class User(Base):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    username: Mapped[str] = mapped_column(String(64), unique=True, index=True, nullable=False)
    password_hash: Mapped[str] = mapped_column(String(256), nullable=False)
    role: Mapped[str] = mapped_column(String(16), default="user", nullable=False)  # user | admin
    status: Mapped[str] = mapped_column(String(16), default="active", nullable=False)  # active | suspended | banned | deleted
    bio: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    # 论坛信任等级：0=新用户（发帖需审核），>=1=正常发帖
    trust_level: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    need_review: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)
    github_bound: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    # 展示名（可空；空则显示 username —— username 与 URL 不变）
    display_name: Mapped[str] = mapped_column(String(64), default="", nullable=False)
    # 头像 URL（空 = 前端用首字母方块兜底）
    avatar_url: Mapped[str] = mapped_column(String(300), default="", nullable=False)
    # 浏览历史开关：关闭后服务端不再记录新历史（已有记录需显式清空）
    history_enabled: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)
    # 社区声望：收到赞 +1、感谢 +2；取消后扣回
    reputation: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    # 收到的赞/感谢原始计数（排行榜用，随互动事务维护）
    received_likes: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    received_thanks: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    demos: Mapped[list["Demo"]] = relationship(back_populates="author")
    comments: Mapped[list["Comment"]] = relationship(back_populates="user")


class Tag(Base):
    __tablename__ = "tags"
    __table_args__ = (UniqueConstraint("key", "value", name="uq_tag_key_value"),)

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    key: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    value: Mapped[str] = mapped_column(String(128), nullable=False, index=True)
    description: Mapped[str] = mapped_column(String(1000), default="", nullable=False)
    # 固定值分组/厂商（如 model 的 "DeepSeek"、"OpenAI"），用于前端分组展示
    group: Mapped[str | None] = mapped_column(String(64), nullable=True)
    parent_id: Mapped[int | None] = mapped_column(ForeignKey("tags.id"), nullable=True, index=True)
    # T3·M5-B2 Tag 状态机（06 附录 B 实施，用户裁决三态）：candidate | active | deprecated
    # 存量迁移见 main._ensure_tag_columns —— 全量视作 active，零行为变化；hidden 未引入
    status: Mapped[str] = mapped_column(String(16), default="active", nullable=False, index=True)

    parent: Mapped["Tag | None"] = relationship(remote_side=[id], back_populates="children")
    children: Mapped[list["Tag"]] = relationship(back_populates="parent", cascade="all, delete-orphan")

    demo_associations: Mapped[list["DemoTag"]] = relationship(back_populates="tag", cascade="all, delete-orphan")


class TagKey(Base):
    """标签键定义：决定该 key 下 value 的填写方式。

    mode:
      - fixed: 固定值（管理员维护，用户只能选择，如 model:dsv4-flash）
      - open:  开放值（key 固定，用户自定义 value，如 game:mc）
      - int:   数字值（key 固定，value 必须是整数，如 rounds:3）
    """

    __tablename__ = "tag_keys"

    key: Mapped[str] = mapped_column(String(64), primary_key=True)
    mode: Mapped[str] = mapped_column(String(16), default="open", nullable=False)
    label: Mapped[str] = mapped_column(String(64), default="", nullable=False)
    description: Mapped[str] = mapped_column(String(1000), default="", nullable=False)
    sort: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    # 重要性分层（v2）：1 核心（model，毕业后由实体接管位置）/ 2 常用（type/category/game）/ 3 扩展
    tier: Mapped[int] = mapped_column(Integer, default=2, nullable=False)


class TagValueSuggestion(Base):
    """固定值申请建议：用户申请新增 fixed value，管理员审核通过后才创建正式 Tag。"""

    __tablename__ = "tag_value_suggestions"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    key: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    value: Mapped[str] = mapped_column(String(128), nullable=False)
    description: Mapped[str] = mapped_column(String(1000), default="", nullable=False)
    group: Mapped[str | None] = mapped_column(String(64), nullable=True)
    status: Mapped[str] = mapped_column(String(16), default="pending", nullable=False, index=True)  # pending | approved | rejected
    demo_id: Mapped[int | None] = mapped_column(ForeignKey("demos.id", ondelete="SET NULL"), nullable=True)
    created_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    reviewed_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    reviewed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class Demo(Base):
    __tablename__ = "demos"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    slug: Mapped[str] = mapped_column(String(128), unique=True, index=True, nullable=False)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    description: Mapped[str] = mapped_column(Text, default="", nullable=False)
    cover_url: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    # 列表缩略图（最长边 200 的 WebP，命名 <封面名>-thumb.webp）；空 = 无缩略图
    # （default.svg / SVG 封面 / 尚未回填）—— 前端据此不渲染 <img>，不给假封面也不留空洞
    cover_thumb_url: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    # web=网页应用 zip=文件包 link=外部链接
    demo_type: Mapped[str] = mapped_column(String(16), default="web", nullable=False, index=True)
    external_url: Mapped[str | None] = mapped_column(String(2000), nullable=True)
    prompt: Mapped[str] = mapped_column(Text, default="", nullable=False)  # 第一轮提示词
    video_url: Mapped[str | None] = mapped_column(String(2000), nullable=True)  # 介绍视频链接（不存视频）
    status: Mapped[str] = mapped_column(String(16), default="pending", nullable=False, index=True)  # pending | approved | rejected
    view_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    download_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    author_id: Mapped[int | None] = mapped_column(ForeignKey("users.id"), nullable=True, index=True)
    # 匿名（未注册）上传的展示名；author_id 为 NULL 时生效（虚拟 public 身份）
    guest_name: Mapped[str | None] = mapped_column(String(64), nullable=True, index=True)
    # 幂等键：agent 重试去重（非空唯一）
    idempotency_key: Mapped[str | None] = mapped_column(String(128), nullable=True, unique=True, index=True)
    # zip 内容哈希（sha256，按作者去重，普通索引）
    content_hash: Mapped[str | None] = mapped_column(String(64), nullable=True, index=True)
    # 单文件模式：html | svg（直接上传单个自包含文件，非 zip）
    single_file: Mapped[str | None] = mapped_column(String(8), nullable=True)
    # v2：第一轮提示词实体（prompts 表去重缓存）；demos.prompt 原列保留双写
    prompt_id: Mapped[int | None] = mapped_column(ForeignKey("prompts.id"), nullable=True, index=True)
    # Q2：选了兜底型号（family/unknown/guess）时的依据留痕，供日后归属工作台收敛
    # （「不确定」必须带着证据不确定，否则永远不会被收敛）
    model_hint: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    # v2 B5′：Run 语义的元数据从标签体系收编为列（可排序/可聚合，且不污染 fixed 词表）。
    # 标签仍照常写入（`?tag=rounds:3-10` 是已发布 agent 契约），列是「可计算的那一份」。
    gen_rounds: Mapped[int | None] = mapped_column(Integer, nullable=True)      # 生成轮数
    gen_minutes: Mapped[int | None] = mapped_column(Integer, nullable=True)     # 耗时（分钟）
    gen_platform: Mapped[str | None] = mapped_column(String(32), nullable=True)  # 运行平台
    # 多站可见域（astra 橱窗）：逗号分隔枚举 deep | astra | deep,astra；默认 deep = 存量行为不变
    sites: Mapped[str] = mapped_column(String(32), default="deep", nullable=False, index=True)
    # 作品内容语言：zh | en（astra 橱窗策展池要求 en；主站不以此过滤）
    lang: Mapped[str] = mapped_column(String(8), default="zh", nullable=False)
    # 首页策展（07 §2.2）：featured=1 进首页精选/hero 策展池；featured_order = 排序位（1 起连续）
    featured: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    featured_order: Mapped[int | None] = mapped_column(Integer, nullable=True)
    # 评分冗余统计列（榜单排序用，随评分事务更新）
    rating_sum: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    rating_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    rating_avg: Mapped[float] = mapped_column(Float, default=0.0, nullable=False)
    rating_god: Mapped[int] = mapped_column(Integer, default=0, nullable=False)    # score == 5 神作
    rating_ghost: Mapped[int] = mapped_column(Integer, default=0, nullable=False)  # score == 1 鬼作
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    # 注意：不要给 updated_at 加 onupdate —— 它会被浏览/下载/评分等任何对 demo 行的 UPDATE
    # 顺手刷新，而版本化预览 URL 的版本 key 依赖它（2026-09-03 事故：每次浏览换新 URL，
    # CDN 缓存命中率归零 + 读路径写风暴）。updated_at 只应在「内容被改」时显式赋值
    # （update_demo 已显式 `demo.updated_at = datetime.utcnow()`）。
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    author: Mapped["User | None"] = relationship(back_populates="demos")
    tag_associations: Mapped[list["DemoTag"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    comments: Mapped[list["Comment"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    session_logs: Mapped[list["SessionLog"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    ratings: Mapped[list["DemoRating"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    timeline: Mapped[list["DemoTimeline"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    # v2 实体关联（多对多；viewonly 便于 selectinload 预加载，写走 *_links）
    model_links: Mapped[list["DemoModel"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    task_links: Mapped[list["DemoTask"]] = relationship(back_populates="demo", cascade="all, delete-orphan")
    models: Mapped[list["Model"]] = relationship(secondary="demo_models", viewonly=True)
    tasks: Mapped[list["Task"]] = relationship(secondary="demo_tasks", viewonly=True)
    prompt_ref: Mapped["Prompt | None"] = relationship(back_populates="demos")


class DemoTag(Base):
    __tablename__ = "demo_tags"

    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), primary_key=True)
    tag_id: Mapped[int] = mapped_column(ForeignKey("tags.id", ondelete="CASCADE"), primary_key=True)

    demo: Mapped["Demo"] = relationship(back_populates="tag_associations")
    tag: Mapped["Tag"] = relationship(back_populates="demo_associations")


class Comment(Base):
    __tablename__ = "comments"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), nullable=False, index=True)
    user_id: Mapped[int | None] = mapped_column(ForeignKey("users.id"), nullable=True)
    parent_id: Mapped[int | None] = mapped_column(ForeignKey("comments.id"), nullable=True, index=True)
    content: Mapped[str] = mapped_column(Text, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    demo: Mapped["Demo"] = relationship(back_populates="comments")
    user: Mapped["User | None"] = relationship(back_populates="comments")
    parent: Mapped["Comment | None"] = relationship(remote_side=[id], back_populates="children")
    children: Mapped[list["Comment"]] = relationship(back_populates="parent", cascade="all, delete-orphan")


class SessionLog(Base):
    __tablename__ = "session_logs"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), nullable=False, index=True)
    filename: Mapped[str] = mapped_column(String(255), nullable=False)
    file_size: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    demo: Mapped["Demo"] = relationship(back_populates="session_logs")


class DemoTimeline(Base):
    """轻量版本时间线（不依赖 git）：记录 demo 的创建/更新历史。"""

    __tablename__ = "demo_timeline"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), nullable=False, index=True)
    version_label: Mapped[str] = mapped_column(String(32), default="v1", nullable=False)
    message: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    # 若本次更新保留了旧版本，这里指向旧版本 demo 的 slug（可点击跳转）
    old_slug: Mapped[str | None] = mapped_column(String(128), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    demo: Mapped["Demo"] = relationship(back_populates="timeline")


class Setting(Base):
    __tablename__ = "settings"

    key: Mapped[str] = mapped_column(String(64), primary_key=True)
    value: Mapped[str] = mapped_column(String(500), nullable=False)


class VisitDaily(Base):
    """站点访问统计：按天计的原始访问数。

    ips 列为历史遗留（曾存当日访客 IP 的 JSON 数组，UV 备用）：
    已停止写入且无任何消费方，仅保留列避免迁移；新行恒为 "[]"。
    """

    __tablename__ = "visit_daily"

    date: Mapped[str] = mapped_column(String(10), primary_key=True)  # YYYY-MM-DD
    count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    ips: Mapped[str] = mapped_column(Text, default="[]", nullable=False)  # 已停写，历史遗留


class Acknowledgment(Base):
    """赞助 / 致谢榜：单表，kind 区分（sponsor / thanks）。"""

    __tablename__ = "acknowledgments"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    kind: Mapped[str] = mapped_column(String(16), default="sponsor", nullable=False, index=True)  # sponsor | thanks
    name: Mapped[str] = mapped_column(String(64), nullable=False)
    amount: Mapped[int | None] = mapped_column(Integer, nullable=True)  # 仅 sponsor
    message: Mapped[str] = mapped_column(String(200), default="", nullable=False)  # 备注/致谢语
    show_amount: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)  # sponsor 是否公开金额
    sort: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    active: Mapped[bool] = mapped_column(Boolean, default=True, nullable=False)  # 软下架
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)


class DemoRating(Base):
    """用户评分：1~5 分（5=神作，1=鬼作）。登录 user:{id}；匿名 anon:{sha256}。"""

    __tablename__ = "demo_ratings"
    __table_args__ = (UniqueConstraint("demo_id", "rater_key", name="uq_demo_rater"),)

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), nullable=False, index=True)
    user_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True, index=True)
    rater_key: Mapped[str] = mapped_column(String(128), nullable=False)
    score: Mapped[int] = mapped_column(Integer, nullable=False)  # 1~5
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, onupdate=utcnow, nullable=False)

    demo: Mapped["Demo"] = relationship(back_populates="ratings")


class Announcement(Base):
    __tablename__ = "announcements"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    # manual=手动公告 auto=新 demo 自动公告 update=更新公告（内容为 commit 信息） demo_update=作品更新
    type: Mapped[str] = mapped_column(String(16), default="manual", nullable=False, index=True)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    content: Mapped[str] = mapped_column(Text, default="", nullable=False)
    demo_slug: Mapped[str | None] = mapped_column(String(128), nullable=True, index=True)
    # 公告扩展：置顶 / 状态 / 分类 / 定时上下线
    pinned: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    status: Mapped[str] = mapped_column(String(16), default="published", nullable=False, index=True)  # draft | published | offline
    category: Mapped[str] = mapped_column(String(32), default="general", nullable=False, index=True)  # general | system | demo | ...
    published_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    expires_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    # 关联论坛主题（公告 ↔ 讨论互链）
    topic_id: Mapped[int | None] = mapped_column(ForeignKey("forum_topics.id", ondelete="SET NULL"), nullable=True, index=True)
    created_by: Mapped[int | None] = mapped_column(ForeignKey("users.id"), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)


class ForumTopic(Base):
    """论坛主题。"""

    __tablename__ = "forum_topics"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    content: Mapped[str] = mapped_column(Text, default="", nullable=False)  # Markdown 原文
    author_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True, index=True)
    demo_slug: Mapped[str | None] = mapped_column(String(128), nullable=True, index=True)
    category: Mapped[str] = mapped_column(String(32), default="general", nullable=False, index=True)
    tags: Mapped[str] = mapped_column(String(200), default="", nullable=False)  # 逗号分隔
    pinned: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    sticky: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    locked: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    solved: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    status: Mapped[str] = mapped_column(String(16), default="normal", nullable=False, index=True)  # normal | hidden | reviewing
    reply_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    view_count: Mapped[int] = mapped_column(Integer, default=0, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, onupdate=utcnow, nullable=False)

    author: Mapped["User | None"] = relationship()
    replies: Mapped[list["ForumReply"]] = relationship(back_populates="topic", cascade="all, delete-orphan")


class ForumReply(Base):
    """论坛回复。"""

    __tablename__ = "forum_replies"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    topic_id: Mapped[int] = mapped_column(ForeignKey("forum_topics.id", ondelete="CASCADE"), nullable=False, index=True)
    author_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True, index=True)
    content: Mapped[str] = mapped_column(Text, nullable=False)  # Markdown 原文
    status: Mapped[str] = mapped_column(String(16), default="normal", nullable=False, index=True)  # normal | hidden | reviewing
    parent_id: Mapped[int | None] = mapped_column(ForeignKey("forum_replies.id", ondelete="CASCADE"), nullable=True, index=True)
    # 历史评论迁移标记：指向来源 comment.id（幂等去重用）
    source_comment_id: Mapped[int | None] = mapped_column(Integer, nullable=True, index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    topic: Mapped["ForumTopic"] = relationship(back_populates="replies")
    author: Mapped["User | None"] = relationship()


class ForumReaction(Base):
    """论坛互动：主题/回复的赞与感谢。同一用户对同一目标同一类型只能有一条。"""

    __tablename__ = "forum_reactions"
    __table_args__ = (
        UniqueConstraint("user_id", "target_type", "target_id", "reaction_type", name="uq_forum_reaction"),
    )

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    target_type: Mapped[str] = mapped_column(String(16), nullable=False, index=True)  # topic | reply
    target_id: Mapped[int] = mapped_column(Integer, nullable=False, index=True)
    reaction_type: Mapped[str] = mapped_column(String(16), nullable=False, index=True)  # like | thanks
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    user: Mapped["User"] = relationship()


class UserFollow(Base):
    """用户关注关系。"""

    __tablename__ = "user_follows"
    __table_args__ = (
        UniqueConstraint("follower_id", "following_id", name="uq_user_follow"),
    )

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    follower_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    following_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    follower: Mapped["User"] = relationship(foreign_keys=[follower_id])
    following: Mapped["User"] = relationship(foreign_keys=[following_id])


class ForumReport(Base):
    """举报表。"""

    __tablename__ = "forum_reports"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    target_type: Mapped[str] = mapped_column(String(16), nullable=False, index=True)  # topic | reply
    target_id: Mapped[int] = mapped_column(Integer, nullable=False, index=True)
    reporter_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    reason: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    status: Mapped[str] = mapped_column(String(16), default="open", nullable=False, index=True)  # open | resolved | dismissed
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)


class Notification(Base):
    """站内通知：作品/论坛/管理动作通知相关用户。"""

    __tablename__ = "notifications"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True)
    type: Mapped[str] = mapped_column(String(32), nullable=False, index=True)  # forum_reply | forum_reaction | demo_review | review_result | report_handled | system
    actor_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    demo_slug: Mapped[str | None] = mapped_column(String(128), nullable=True)
    topic_id: Mapped[int | None] = mapped_column(Integer, nullable=True)
    reply_id: Mapped[int | None] = mapped_column(Integer, nullable=True)
    read: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    user: Mapped["User"] = relationship(foreign_keys="Notification.user_id")
    actor: Mapped["User | None"] = relationship(foreign_keys="Notification.actor_id")


# ---------------- v2 实体：Model / Task / Prompt（从 Tag 毕业的一等实体） ----------------


class Model(Base):
    """模型实体（v2）：由 Tag(key=model) 毕业而来。

    status:
      - candidate:  自动新建/用户申请，待管理员确认
      - active:     已确认上架
      - unverified: 灰测/未验证模型（如 ds-unknown），照常展示 + 前端灰测徽章
      - deprecated: 已合并/退役（merged_into_id 指向新实体，不物理删除）
    """

    __tablename__ = "models"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    slug: Mapped[str] = mapped_column(String(128), unique=True, index=True, nullable=False)
    name: Mapped[str] = mapped_column(String(128), unique=True, nullable=False)
    vendor: Mapped[str | None] = mapped_column(String(64), nullable=True)  # 厂商（沿用 tag.group）
    status: Mapped[str] = mapped_column(String(16), default="candidate", nullable=False, index=True)
    # 断言强度（Q2 决议，与 status 正交：status 管生命周期，resolution 管「有多确定」）
    #   exact   精确型号（A）
    #   family  知厂商不知型号（B）：vendor 有值、slug 形如 <vendor>-unknown
    #   unknown 完全不知（C）：全局 unspecified
    #   guess   有猜测未证实（D）：网传灰测 ds-unknown，可被「揭晓」改映射
    resolution: Mapped[str] = mapped_column(String(16), default="exact", nullable=False, index=True)
    merged_into_id: Mapped[int | None] = mapped_column(ForeignKey("models.id"), nullable=True)
    description: Mapped[str] = mapped_column(String(1000), default="", nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, onupdate=utcnow, nullable=False)

    aliases: Mapped[list["ModelAlias"]] = relationship(back_populates="model", cascade="all, delete-orphan")


class ModelAlias(Base):
    """模型别名：上传匹配用（"dsv4flash"→canonical）。规范化写法本身也存一条，查询 O(1)。"""

    __tablename__ = "model_aliases"

    alias: Mapped[str] = mapped_column(String(128), primary_key=True)
    model_id: Mapped[int] = mapped_column(ForeignKey("models.id", ondelete="CASCADE"), nullable=False, index=True)

    model: Mapped["Model"] = relationship(back_populates="aliases")


class Task(Base):
    """题目实体（v2）：Benchmark = 固定 Task 比较多 Model。"""

    __tablename__ = "tasks"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    slug: Mapped[str] = mapped_column(String(128), unique=True, index=True, nullable=False)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    description: Mapped[str] = mapped_column(Text, default="", nullable=False)
    category: Mapped[str | None] = mapped_column(String(64), nullable=True)  # 对齐 category 标签值
    # candidate=规则/用户建议待审 | active=已确认 | merged=已并入 | hidden=下架
    status: Mapped[str] = mapped_column(String(16), default="candidate", nullable=False, index=True)
    merged_into_id: Mapped[int | None] = mapped_column(ForeignKey("tasks.id"), nullable=True)
    created_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, onupdate=utcnow, nullable=False)


class DemoModel(Base):
    """demo ↔ model 多对多。"""

    __tablename__ = "demo_models"
    __table_args__ = (Index("ix_demo_models_model_id", "model_id"),)

    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), primary_key=True)
    model_id: Mapped[int] = mapped_column(ForeignKey("models.id", ondelete="CASCADE"), primary_key=True)

    demo: Mapped["Demo"] = relationship(back_populates="model_links")
    model: Mapped["Model"] = relationship()


class DemoTask(Base):
    """demo ↔ task 多对多（决策 D1）。"""

    __tablename__ = "demo_tasks"
    __table_args__ = (Index("ix_demo_tasks_task_id", "task_id"),)

    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id", ondelete="CASCADE"), primary_key=True)
    task_id: Mapped[int] = mapped_column(ForeignKey("tasks.id", ondelete="CASCADE"), primary_key=True)

    demo: Mapped["Demo"] = relationship(back_populates="task_links")
    task: Mapped["Task"] = relationship()


class Prompt(Base):
    """提示词实体（v2）：demos.prompt 规范化去重后的缓存；相似检索/「同提示词」模块的语料。"""

    __tablename__ = "prompts"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    content_hash: Mapped[str] = mapped_column(String(64), unique=True, index=True, nullable=False)
    content: Mapped[str] = mapped_column(Text, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    demos: Mapped[list["Demo"]] = relationship(back_populates="prompt_ref")


class PromptLibraryEntry(Base):
    """审核后的提示词收录信息；正文仍由不可变的 Prompt 实体保存。"""

    __tablename__ = "prompt_library_entries"
    __table_args__ = (UniqueConstraint("prompt_id", "scope", name="uq_prompt_library_scope"),)

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    prompt_id: Mapped[int] = mapped_column(ForeignKey("prompts.id"), nullable=False, index=True)
    scope: Mapped[str] = mapped_column(String(16), default="deep", nullable=False, index=True)
    status: Mapped[str] = mapped_column(String(16), default="approved", nullable=False, index=True)
    title: Mapped[str] = mapped_column(String(200), nullable=False)
    category: Mapped[str | None] = mapped_column(String(64), nullable=True)
    source_url: Mapped[str | None] = mapped_column(String(2048), nullable=True)
    created_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)


# ---------------- v2 治理地基：审计 + 统一建议收件箱 ----------------


class AuditLog(Base):
    """知识变更审计（治理铁律第 6 条「任何自动变化可追溯」）。

    与实体变更**同事务**写入：审计写失败则业务回滚，绝不允许「合并无痕」。
    before/after 存 JSON 快照（仅关键字段，见 audit_service.snapshot_*），
    合并类操作靠 before 快照 + merged_into 指针即可人工回退。
    """

    __tablename__ = "audit_log"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    actor_type: Mapped[str] = mapped_column(String(16), default="user", nullable=False)  # user | system
    actor_id: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True, index=True)
    action: Mapped[str] = mapped_column(String(32), nullable=False, index=True)  # create|update|status_set|merge|alias_add|alias_remove|attach|detach|delete|review
    entity_type: Mapped[str] = mapped_column(String(32), nullable=False)  # model | task | suggestion | demo_model | demo_task
    entity_id: Mapped[int] = mapped_column(Integer, nullable=False)
    before: Mapped[str | None] = mapped_column(Text, nullable=True)  # JSON
    after: Mapped[str | None] = mapped_column(Text, nullable=True)  # JSON
    reason: Mapped[str] = mapped_column(String(500), default="", nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False, index=True)

    __table_args__ = (
        Index("ix_audit_entity", "entity_type", "entity_id"),
    )


# 建议类型（EntitySuggestion.kind）
SUGGESTION_KINDS = ("new_model", "new_task", "new_prompt", "task_match", "merge_model", "merge_task", "alias", "retag_demo")
# 审计动作全集：路由的 action 过滤白名单、前端下拉都从这里取，
# 避免「加了新写入动作但忘了补白名单」导致审计记录筛不出来（attribute 曾漏在这里）。
AUDIT_ACTIONS = (
    "create",
    "update",
    "status_set",
    "merge",
    "alias_add",
    "alias_remove",
    "attach",
    "detach",
    "delete",
    "review",
    "attribute",
    "unmerge",
    "slug_set",
    # 首页策展（07 §2.2 / T5·M5-F1）：精选池 添加/移除/排序/置顶 hero
    "featured_add",
    "featured_remove",
    "featured_order",
    "featured_hero",
)
# 建议来源六值（评审与重排.md 裁决 R2）
SUGGESTION_SOURCES = ("user", "admin", "ai", "inferred", "external", "imported")

# 审计实体类型全集（KB-17）：写入侧与审计页筛选白名单共用这一份，
# 避免「加了新写入动作但忘了补白名单」导致审计记录筛不出来（tag/user/setting 曾各漏一次）。
AUDIT_ENTITY_TYPES = (
    "model",
    "task",
    "tag",
    "suggestion",
    "demo",
    "forum_topic",
    "forum_reply",
    "forum_report",
    "user",
    "setting",
)


class EntitySuggestion(Base):
    """统一建议收件箱（治理文档 §三「管理员最需要的是待处理队列」）。

    规则层（v2.0）与将来 LLM 层**共用本表**，靠 source + confidence 区分：
      - confidence ≥ 0.99 → 自动接受但必须留日志（audit_log）
      - 0.6 ~ 0.99        → 进收件箱待人工审核
      - < 0.6             → 只记录不骚扰（列表默认不展示，admin 可显式筛）
    approve 才由 suggestion_service 调对应 service 落库执行，本表永不直接改实体。
    """

    __tablename__ = "entity_suggestions"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    kind: Mapped[str] = mapped_column(String(32), nullable=False, index=True)
    payload: Mapped[str] = mapped_column(Text, default="", nullable=False)  # JSON：建议内容 + 证据摘要
    confidence: Mapped[float | None] = mapped_column(Float, nullable=True)  # 规则阶段 = TF-IDF 相似度
    # 来源六值（评审与重排.md 裁决 R2）；规则召回记 inferred，脚本导入记 imported
    source: Mapped[str] = mapped_column(String(16), default="inferred", nullable=False)
    status: Mapped[str] = mapped_column(String(16), default="pending", nullable=False, index=True)  # pending|approved|rejected
    # 关联对象（按 kind 语义复用：task_match→目标 task；merge_*→源/目标；new_*→触发 demo）
    demo_id: Mapped[int | None] = mapped_column(ForeignKey("demos.id", ondelete="SET NULL"), nullable=True)
    ref_id: Mapped[int | None] = mapped_column(Integer, nullable=True)  # 非 FK：kind 决定指向哪张表
    created_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    reviewed_by: Mapped[int | None] = mapped_column(ForeignKey("users.id", ondelete="SET NULL"), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    reviewed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class Collection(Base):
    """收藏夹（可命名、可公开分享）。每个用户有一个 is_default 的「我的收藏」。"""

    __tablename__ = "collections"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    owner_id: Mapped[int] = mapped_column(ForeignKey("users.id"), index=True, nullable=False)
    title: Mapped[str] = mapped_column(String(60), nullable=False)
    description: Mapped[str] = mapped_column(String(200), default="", nullable=False)
    # private | public（公开的任何人可看，可分享链接；私密的对他人一律 404）
    visibility: Mapped[str] = mapped_column(String(8), default="private", nullable=False, index=True)
    # 默认夹：自动创建、不可删除、不可转公开、不可改名
    is_default: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)
    updated_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)

    owner: Mapped["User"] = relationship()


class CollectionItem(Base):
    """收藏夹条目。同一夹内同一作品只允许一行（重复加入 = 幂等）。"""

    __tablename__ = "collection_items"
    __table_args__ = (UniqueConstraint("collection_id", "demo_id", name="uq_collection_item"),)

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    collection_id: Mapped[int] = mapped_column(ForeignKey("collections.id"), index=True, nullable=False)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id"), index=True, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False)


class DemoView(Base):
    """浏览历史（服务端侧）。

    隐私口径与 services/visits.py 的「不收集访客 IP」一致：**只存 user_id / demo_id / 时间**，
    不存 IP、不存 UA、不存来源页。同一作品只保留最近一次（unique + upsert 刷新 viewed_at）。
    """

    __tablename__ = "demo_views"
    __table_args__ = (UniqueConstraint("user_id", "demo_id", name="uq_demo_view"),)

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id"), index=True, nullable=False)
    demo_id: Mapped[int] = mapped_column(ForeignKey("demos.id"), index=True, nullable=False)
    viewed_at: Mapped[datetime] = mapped_column(DateTime, default=utcnow, nullable=False, index=True)
