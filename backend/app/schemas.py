from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from .models import USER_STATUSES


class ORMModel(BaseModel):
    model_config = ConfigDict(from_attributes=True)


# ---------- Auth ----------
class RegisterRequest(BaseModel):
    username: str = Field(min_length=3, max_length=32, pattern=r"^[a-zA-Z0-9_]+$")
    password: str = Field(min_length=8, max_length=128)


class LoginRequest(BaseModel):
    username: str
    password: str


class UserOut(ORMModel):
    id: int
    username: str
    role: str
    status: str
    bio: str
    # 展示名与头像：空则前端回落 username / 首字母方块
    display_name: str = ""
    avatar_url: str = ""
    created_at: datetime


class UserPublic(UserOut):
    demo_count: int = 0


class MeOut(UserOut):
    """/auth/me 专用：history_enabled 只对自己可见（别人不需要知道我关没关历史）。"""

    history_enabled: bool = True


class AuthResponse(BaseModel):
    access_token: str
    user: UserOut


class ChangePasswordRequest(BaseModel):
    old_password: str
    new_password: str = Field(min_length=8, max_length=128)


# ---------- Tags ----------
class TagRef(BaseModel):
    key: str
    value: str


class TagOut(ORMModel):
    id: int
    key: str
    value: str
    description: str
    parent_id: int | None
    demo_count: int = 0
    child_count: int = 0
    mode: str = "open"
    # T3·M5-B2：状态机字段（candidate|active|deprecated；存量 DEFAULT active）
    status: str = "active"


class TagDetail(TagOut):
    parent: TagOut | None = None
    children: list[TagOut] = []


class TagStatusIn(BaseModel):
    """T3·M5-B2 Tag 状态跃迁：独立端点 PUT /admin/entities/tag/{id}/status。

    与 Model/Task 同构：status 受机器枚举约束；reason 随审计落行（必填由 service/前端把关）。
    """
    status: str = Field(pattern="^(candidate|active|deprecated)$")
    reason: str = Field(default="", max_length=500)


class TagCreate(BaseModel):
    key: str = Field(min_length=1, max_length=64)
    value: str = Field(min_length=1, max_length=128)
    description: str = ""
    group: str | None = Field(default=None, max_length=64)
    parent_id: int | None = None


class TagKeyValueOut(BaseModel):
    id: int
    value: str
    description: str = ""
    demo_count: int = 0
    group: str | None = None  # 固定值分组/厂商（如 DeepSeek、OpenAI）
    # T3·M5-B2：状态机字段（词表/徽章读同字段——06 附录 B §5）
    status: str = "active"


class TagKeyOut(BaseModel):
    key: str
    mode: str  # fixed | open | int
    label: str
    description: str
    sort: int = 0
    tier: int = 2  # v2 重要性分层：1 核心 / 2 常用 / 3 扩展
    values: list[TagKeyValueOut] = []
    demo_count: int = 0
    min: int | None = None  # int 键：值域下界（供滑条）
    max: int | None = None  # int 键：值域上界（供滑条）


class TagValueSuggestionIn(BaseModel):
    key: str = Field(min_length=1, max_length=64)
    value: str = Field(min_length=1, max_length=128)
    description: str = Field(default="", max_length=1000)
    group: str | None = Field(default=None, max_length=64)
    demo_id: int | None = None


class TagValueSuggestionOut(BaseModel):
    id: int
    key: str
    value: str
    description: str = ""
    group: str | None = None
    status: str = "pending"
    demo_id: int | None = None
    created_at: datetime


class TagSuggestionReview(BaseModel):
    action: str = Field(pattern="^(approve|reject)$")
    group: str | None = Field(default=None, max_length=64)


class AiSuggestIn(BaseModel):
    demo_id: int | None = None
    text: str = Field(default="", max_length=4000)


class TagGroupRename(BaseModel):
    new_group: str = Field(min_length=1, max_length=64)


class TagValueGroupSet(BaseModel):
    group: str | None = Field(default=None, max_length=64)


class TagMergeIn(BaseModel):
    from_key: str = Field(min_length=1, max_length=64)
    from_value: str = Field(min_length=1, max_length=128)
    to_key: str = Field(min_length=1, max_length=64)
    to_value: str = Field(min_length=1, max_length=128)
    dry_run: bool = False


class TagMergeResult(BaseModel):
    merged: int = 0          # 已迁移到目标值的引用数
    removed_dups: int = 0    # 因 demo 已有目标值而删除的重复引用数
    affected_demos: int = 0  # 受影响 demo 数（去重后的 demo 数）
    deleted_source: bool = False
    dry_run: bool = False
    # KB-1：源值不存在时如实回报「没找到」，不再用一串 0 假装合并成功
    found: bool = True


class TagKeyUpsert(BaseModel):
    key: str = Field(min_length=1, max_length=64, pattern=r"^[a-zA-Z0-9_-]+$")
    mode: str = Field(pattern="^(fixed|open|int)$")
    label: str = Field(min_length=1, max_length=64)
    description: str = ""
    sort: int = 0


class TagKeyUpdate(BaseModel):
    """更新标签键：不含 key（key 在 URL 路径里）。"""
    mode: str = Field(pattern="^(fixed|open|int)$")
    label: str = Field(min_length=1, max_length=64)
    description: str = ""
    sort: int = 0


# ---------- Demo ----------
class DemoFromUrlIn(BaseModel):
    """AI agent 友好：JSON 提交，zip 走 URL（后端下载），免 multipart。
    可匿名上传（不登录）：作者固定为 public；带 upload_code 视为可信。"""
    title: str = Field(min_length=1, max_length=200)
    description: str = ""
    tags: list | None = None
    demo_type: str = "web"  # web | zip | link | image（v2 图像作品：file_url 直传图片，file 充当封面）
    external_url: str | None = None
    prompt: str = ""
    video_url: str | None = None
    zip_url: str | None = None
    file_url: str | None = None  # 单文件（.html/.svg）直传 URL，与 zip_url 二选一
    cover_url: str | None = None
    upload_code: str = ""  # 可选：信任通道密钥（未登录时生效，匹配则直接放行）
    idempotency_key: str = ""  # 可选：8~128 位幂等键；重试带同一 key 不重复创建
    task: str = ""  # 可选（v2 B4′）：挑战的题目 slug；只生成待审候选，不直接挂题
    model_hint: str = Field(default="", max_length=500)  # Q2：选兜底型号时的依据留痕
    force: bool = False  # 可选：仅管理员生效，跳过内容重复校验（force=1 强制上传）


class DemoModelBriefOut(BaseModel):
    """demo 关联的模型实体（v2 新增字段，向后兼容默认空）。"""

    id: int
    slug: str
    name: str
    vendor: str | None = None
    status: str = "active"
    # exact/family/unknown/guess —— 卡片与模型页据此打「未定型号/未标注/canary」徽章
    resolution: str = "exact"


class DemoTaskBriefOut(BaseModel):
    """demo 关联的题目实体（v2 新增字段）。"""

    id: int
    slug: str
    title: str


class DemoSummaryOut(BaseModel):
    slug: str
    title: str
    description: str
    cover_url: str
    author: str | None
    author_id: int | None
    tags: list[TagRef]
    view_count: int
    download_count: int
    comment_count: int
    created_at: datetime
    # 内容更新时间（Demo.updated_at 只在内容被改时显式赋值，没有 onupdate；
    # 不能拿它做预览版本号，但作为「这个作品最近一次更新」的口径是可靠的）。
    # 作品库的 demo 组排序需要它：组的时间口径 = 组内最近更新的那一件。
    updated_at: datetime
    status: str
    demo_type: str = "web"
    external_url: str | None = None
    rating_avg: float = 0.0
    rating_count: int = 0
    rating_god: int = 0
    rating_ghost: int = 0
    prompt: str = ""
    # 同提示词结果需显式展示测试条件；未知保持 None，不推断为首轮。
    gen_rounds: int | None = None
    gen_minutes: int | None = None
    models: list[DemoModelBriefOut] = []
    tasks: list[DemoTaskBriefOut] = []


class RatingIn(BaseModel):
    score: int = Field(ge=1, le=5)
    device_id: str = Field(default="", max_length=128)


class RatingOut(BaseModel):
    my_score: int | None = None
    avg: float = 0.0
    count: int = 0
    god: int = 0
    ghost: int = 0
    distribution: list[dict] = []  # [{score:1..5, count}] 各档票数，按 score 升序


class DemoTimelineOut(BaseModel):
    id: int
    version_label: str
    message: str
    old_slug: str | None = None
    created_at: datetime


class DemoDetailOut(DemoSummaryOut):
    preview_url: str = ""
    # KB-20：轻量列表（detail=False）不带这两项 → 给默认值，避免响应校验炸掉
    session_log_count: int = 0
    is_author: bool = False
    prompt: str = ""
    # Q2：选了兜底型号时的依据留痕（没记录/灰测不便说/别人传的/多模型混合 + 自由描述）
    model_hint: str = ""
    # KB-29：单个文件作品（'html' | 'svg' | None）。详情页据此决定下载按钮文案；
    # 此前该字段只存在于模型与下载路由、从不进响应，前端只能一直显示「下载 ZIP」。
    single_file: str | None = None
    video_url: str | None = None
    file_size: int | None = None
    storage_size: int | None = None
    inconsistency: bool = False
    timeline: list[DemoTimelineOut] = []


class AdminDemoOut(DemoDetailOut):
    # 策展字段仅管理面暴露（astra 橱窗）；公开 schema 未声明 → pydantic 自动剥离
    sites: str = "deep"
    lang: str = "zh"


class DemoCurationIn(BaseModel):
    """astra 橱窗策展：给 demo 发放站点通行证 + 语言标记。None = 保持不变。"""

    sites: list[Literal["deep", "astra"]] | None = Field(default=None, min_length=1)
    lang: Literal["zh", "en"] | None = None


class Paginated(BaseModel):
    items: list[DemoSummaryOut]
    total: int
    page: int
    page_size: int
    # sort=random 专用：本次列表顺序的会话固定种子。客户端翻页回传 ?rs= 它，
    # 整段翻页建立在同一次洗牌上（跨 60s 时间桶不重复不漏件）；其他 sort 恒 None。
    random_seed: str | None = None


class DemoCreateResult(BaseModel):
    slug: str
    status: str
    created: bool = True  # False = 命中幂等键，返回已有结果（agent 重试去重）
    id: int  # 契约只增：上传成功后前端用它回挂 POST /tags/suggestions.demo_id


# ---------- Comments ----------
class CommentCreate(BaseModel):
    content: str = Field(min_length=1, max_length=5000)
    parent_id: int | None = None


class CommentOut(BaseModel):
    id: int
    demo_id: int
    user_id: int | None
    username: str | None
    parent_id: int | None
    content: str
    created_at: datetime
    children: list["CommentOut"] = []


# ---------- Session Logs ----------
class SessionLogOut(ORMModel):
    id: int
    filename: str
    file_size: int
    created_at: datetime


# ---------- Settings ----------
class SettingsOut(BaseModel):
    auto_approve: bool = True
    auto_approve_public: bool = False
    # 整活模式；PUT 时 None = 保持不变（旧调用方漏带字段不会静默关闭）
    fun_mode: bool | None = None


class SettingsIn(BaseModel):
    """PUT /admin/settings 请求体（KB-22）：**None = 不改该字段**。

    旧实现直接复用 SettingsOut —— 它带默认值（auto_approve=True / auto_approve_public=False），
    只想改 fun_mode 的调用方会把审核策略静默重置。现在三个字段都可缺省，且缺省即不改。
    """

    auto_approve: bool | None = None
    auto_approve_public: bool | None = None
    fun_mode: bool | None = None


# ---------- 赞助/致谢 ----------
class RecognitionIn(BaseModel):
    kind: str = Field(pattern="^(sponsor|thanks)$")
    name: str = Field(min_length=1, max_length=64)
    amount: int | None = Field(default=None, ge=0)
    message: str = Field(default="", max_length=200)
    show_amount: bool = True
    sort: int = 0
    active: bool = True


# ---------- Admin ----------
class AdminUserOut(UserPublic):
    pass


class ReviewAction(BaseModel):
    action: str = Field(pattern="^(approve|reject)$")


class UserPatch(BaseModel):
    role: str | None = Field(default=None, pattern="^(user|admin)$")
    # KB-22：状态集来自 models.USER_STATUSES（旧写法漏了 banned，而封禁端点写的正是 banned）
    status: str | None = Field(default=None, pattern="^(" + "|".join(USER_STATUSES) + ")$")


class DemoCounts(BaseModel):
    total: int
    approved: int
    pending: int
    rejected: int


class StorageStatusOut(BaseModel):
    oss_enabled: bool
    mode: str  # oss | oss_backup | local
    local_demos: int
    local_files: int
    local_size_bytes: int


class AdminStatsOut(BaseModel):
    demos: DemoCounts
    users: int
    storage: StorageStatusOut


# ---------- Announcements ----------
class AnnouncementOut(ORMModel):
    id: int
    type: str  # manual | auto | update | demo_update
    title: str
    content: str
    demo_slug: str | None = None
    pinned: bool = False
    status: str = "published"  # draft | published | offline
    category: str = "general"  # general | system | demo | ...
    published_at: datetime | None = None
    expires_at: datetime | None = None
    topic_id: int | None = None
    topic_title: str | None = None
    created_by: int | None = None
    created_at: datetime


class AnnouncementUpsert(BaseModel):
    title: str = Field(min_length=1, max_length=200)
    content: str = ""
    demo_slug: str | None = None
    pinned: bool = False
    status: str = Field(default="published", pattern="^(draft|published|offline)$")
    category: str = Field(default="general", max_length=32)
    published_at: datetime | None = None
    expires_at: datetime | None = None
    topic_id: int | None = None


# ---------- 作品 meta（富卡片） ----------
class DemoMetaOut(BaseModel):
    slug: str
    title: str
    cover_url: str
    author: str


class SamePromptOut(BaseModel):
    """同提示词的其他作品（v2 B2′）：prompt_id 精确共享 = 严格复现对比。

    与 Task 构成粗细两档：同 prompt = 同一句话交给不同模型；同 task = 同一题材。
    本模块零 Task 依赖，是 v2 第一个用户可见的价值。
    """

    prompt: str = ""
    prompt_id: int | None = None
    items: list["DemoSummaryOut"] = []


# ---------- 论坛 ----------
class ForumTopicOut(BaseModel):
    id: int
    title: str
    content: str = ""
    author: str | None = None
    author_id: int | None = None
    demo_slug: str | None = None
    category: str = "general"
    tags: list[str] = []
    pinned: bool = False
    sticky: bool = False
    locked: bool = False
    solved: bool = False
    status: str = "normal"
    reply_count: int = 0
    view_count: int = 0
    like_count: int = 0
    thanks_count: int = 0
    my_reactions: list[str] = []
    created_at: datetime
    updated_at: datetime


class ForumTopicPage(BaseModel):
    items: list[ForumTopicOut]
    total: int
    page: int
    page_size: int



class ForumReplyOut(BaseModel):
    id: int
    topic_id: int
    author: str | None = None
    author_id: int | None = None
    content: str
    status: str = "normal"
    parent_id: int | None = None
    like_count: int = 0
    thanks_count: int = 0
    my_reactions: list[str] = []
    created_at: datetime
    # 仅管理端全局列表填充：跨主题的回复必须知道属于哪个帖子
    topic_title: str | None = None


class ForumReplyPage(BaseModel):
    items: list[ForumReplyOut]
    total: int
    page: int
    page_size: int


class ForumTopicIn(BaseModel):
    title: str = Field(min_length=1, max_length=200)
    content: str = Field(default="", max_length=20000)
    demo_slug: str | None = Field(default=None, max_length=128)
    category: str = Field(default="general", max_length=32)
    tags: str = Field(default="", max_length=200)  # 逗号分隔


class ForumReplyIn(BaseModel):
    content: str = Field(min_length=1, max_length=10000)
    parent_id: int | None = None


class ForumTopicAdminUpdate(BaseModel):
    title: str | None = Field(default=None, min_length=1, max_length=200)
    tags: str | None = Field(default=None, max_length=200)
    pinned: bool | None = None
    sticky: bool | None = None
    locked: bool | None = None
    solved: bool | None = None
    category: str | None = Field(default=None, max_length=32)
    status: str | None = Field(default=None, pattern="^(normal|hidden|reviewing)$")


class ForumReviewIn(BaseModel):
    action: str = Field(pattern="^(approve|reject)$")


class ForumReportIn(BaseModel):
    target_type: str = Field(pattern="^(topic|reply)$")
    target_id: int
    reason: str = Field(min_length=1, max_length=500)


class ForumReportOut(BaseModel):
    id: int
    target_type: str
    target_id: int
    reporter_id: int | None = None
    reason: str = ""
    status: str = "open"
    created_at: datetime


class ForumReportHandleIn(BaseModel):
    action: str = Field(pattern="^(resolve|dismiss)$")


# ---------- 社区互动 ----------
class ReactionToggleIn(BaseModel):
    target_type: str = Field(pattern="^(topic|reply)$")
    target_id: int
    reaction_type: str = Field(pattern="^(like|thanks)$")


class ReactionSummary(BaseModel):
    target_type: str
    target_id: int
    like_count: int = 0
    thanks_count: int = 0
    my_reactions: list[str] = []


class ReactionToggleOut(ReactionSummary):
    active: bool = False


class FollowOut(BaseModel):
    following: bool
    followers_count: int = 0
    following_count: int = 0


class UserProfileOut(BaseModel):
    id: int
    username: str
    role: str
    status: str
    bio: str
    created_at: datetime
    reputation: int = 0
    demo_count: int = 0
    topic_count: int = 0
    reply_count: int = 0
    follower_count: int = 0
    following_count: int = 0
    is_following: bool = False
    is_self: bool = False


class UserLeaderboardOut(BaseModel):
    id: int
    username: str
    bio: str = ""
    reputation: int = 0
    received_likes: int = 0
    received_thanks: int = 0
    demo_count: int = 0
    topic_count: int = 0
    reply_count: int = 0
    follower_count: int = 0


class UserLeaderboardPage(BaseModel):
    items: list[UserLeaderboardOut]
    total: int
    page: int
    page_size: int


# ---------- 通知 ----------
class NotificationOut(BaseModel):
    id: int
    type: str
    actor: str | None = None
    actor_id: int | None = None
    demo_slug: str | None = None
    topic_id: int | None = None
    reply_id: int | None = None
    read: bool = False
    created_at: datetime


class NotificationReadIn(BaseModel):
    id: int


class UnreadCountOut(BaseModel):
    count: int = 0

# ---------- v2 实体：Model / Task / Explore ----------


class ModelSummaryOut(BaseModel):
    id: int
    slug: str
    name: str
    vendor: str | None = None
    status: str = "active"
    resolution: str = "exact"
    description: str = ""
    demo_count: int = 0
    rating_avg: float | None = None  # 等权均分（旧语义，保留兼容）
    # 收缩后的社区分 = (票数加权和 + m·C)/(票数 + m)；零票为 None（没证据 ≠ 0 分）
    score: float | None = None
    votes: int = 0
    sample_level: str = "none"  # none | low | mid | high
    created_at: datetime


class ModelListOut(BaseModel):
    items: list[ModelSummaryOut]
    total: int
    page: int
    page_size: int


class ModelTaskRefOut(BaseModel):
    id: int
    slug: str
    title: str
    demo_count: int = 0


class ModelTagDistOut(BaseModel):
    """模型行为档案：某标签键的值分布（常见类型/常见玩法）。"""

    value: str
    demos: int


class ModelDetailOut(ModelSummaryOut):
    aliases: list[str] = []
    tasks: list[ModelTaskRefOut] = []
    recent_demos: list["DemoSummaryOut"] = []
    merged_into: int | None = None
    type_dist: list[ModelTagDistOut] = []
    game_dist: list[ModelTagDistOut] = []
    # 先验透明化：{C, m} 一起给出，读者能自己验算"为什么 6 票的 4.9 排在 412 票的 4.6 后面"
    prior: dict = Field(default_factory=dict)


class ModelBriefOut(BaseModel):
    id: int
    slug: str
    name: str
    vendor: str | None = None
    status: str = "active"


class CompareBestDemoOut(BaseModel):
    slug: str
    title: str
    rating_avg: float = 0.0


class CompareRowOut(BaseModel):
    model: ModelBriefOut
    demo_count: int = 0
    avg_rating: float | None = None
    # v2 B5′：run 元数据两指标（未填者不参与 AVG，返回 None 而不是 0）
    avg_rounds: float | None = None
    avg_minutes: float | None = None
    best_demo: CompareBestDemoOut | None = None


class TaskSummaryOut(BaseModel):
    id: int
    slug: str
    title: str
    description: str = ""
    # 题面摘录：无描述时取该题下第一件作品的提示词（列表页要能看懂题目是什么）
    prompt_excerpt: str = ""
    # 题目行的代表封面（200px 缩略图）：该题下排序第一且有缩略图的已上架作品；
    # 空 = 该题没有任何带缩略图的作品（如全是 default.svg）→ 前端不渲染图片
    cover_thumb_url: str = ""
    # 同一件代表作品的**原图** URL：卡片网格按它推导 640 档（缺文件时 CoverImg 自身回落）
    cover_url: str = ""
    category: str | None = None
    status: str = "active"
    demo_count: int = 0
    created_at: datetime


class TaskListOut(BaseModel):
    items: list[TaskSummaryOut]
    total: int
    page: int
    page_size: int


# ---------- Prompt library ----------
class PromptListItemOut(BaseModel):
    """列表项：正文只给 480 字摘要，全文按 id 单取（最坏一页从 ~2MB 降到 ~100KB）。"""

    id: int
    title: str
    content_excerpt: str
    content_truncated: bool
    category: str | None = None
    source_url: str | None = None
    source: Literal["contributed", "demo"]
    created_at: datetime
    demos_total: int
    models_total: int


class PromptSummaryOut(PromptListItemOut):
    """详情：摘要字段 + 完整正文。"""

    content: str


class PromptListOut(BaseModel):
    items: list[PromptListItemOut]
    total: int
    page: int
    page_size: int


class PromptProposalIn(BaseModel):
    title: str = Field(min_length=1, max_length=200)
    content: str = Field(min_length=1, max_length=20000)
    category: str | None = Field(default=None, max_length=64)
    source_url: str | None = Field(default=None, max_length=2048)


class PromptProposalOut(BaseModel):
    status: Literal["pending", "existing"]
    suggestion_id: int | None = None
    prompt_id: int | None = None


class TaskProposalIn(BaseModel):
    """独立出题（登录用户，不依赖上传作品）：title 必填，描述/分类可选。"""

    title: str = Field(min_length=1, max_length=200)
    description: str = Field(default="", max_length=4000)
    category: str | None = Field(default=None, max_length=64)


class TaskProposalOut(BaseModel):
    """出题受理结果：只进待审收件箱（复用 EntitySuggestion 审核路径），不直建 Task。"""

    status: Literal["pending"]
    suggestion_id: int
    # duplicate=True = 同一用户已提交过同题名的待审提案，本次幂等返回不堆箱
    duplicate: bool = False


class TaskChainRowOut(BaseModel):
    """证据表的一行 = 一件作品，列即链条环节（模型 → 题面 → 生成过程 → 评分）。"""

    slug: str
    title: str
    cover_url: str = ""  # 图像作品一期：「并排看」的图片 URL（存储层 cover_url，可能是 webp 或外链）
    # 并排看的比较语义：image=看图；web/zip/link=看封面+点进作品
    demo_type: str = "web"
    models: list[ModelBriefOut] = []
    prompt_id: int | None = None
    # None = 该作品未填提示词，一致性未知（既不算一致也不算不一致）
    same_prompt: bool | None = None
    prompt_excerpt: str = ""
    rounds: int | None = None
    minutes: int | None = None
    rating_avg: float | None = None
    rating_count: int = 0


class TaskChainOut(BaseModel):
    brief: str = ""
    # description=作者写的题面；prompt=回落到基准提示词（必须标注，不冒充作者描述）
    brief_source: str = ""
    prompt_id: int | None = None
    prompt_variants: int = 0
    no_prompt_count: int = 0
    rows: list[TaskChainRowOut] = []


class TaskDetailOut(BaseModel):
    id: int
    slug: str
    title: str
    description: str = ""
    category: str | None = None
    status: str = "active"
    demos_total: int = 0
    compare: list[CompareRowOut] = []
    demos: list["DemoSummaryOut"] = []
    # 链条视图（题目页主形态）：题面 + 逐作品的证据行
    chain: TaskChainOut | None = None
    created_at: datetime


class TaskSuggestItemOut(BaseModel):
    task_id: int
    # 只有 id 的建议没法显示 —— 补齐可读字段（上传页挂题选择器用）
    slug: str
    title: str
    category: str | None = None
    demo_count: int = 0
    score: float


class ExploreGroupOut(BaseModel):
    total: int = 0
    items: list[ModelSummaryOut] = []
    # 兜底位作品数（未定型号/未标注/灰测），前端渲染为「其他 · 未定 N」折叠行
    fallback_demos: int = 0


class ExploreTagValuesOut(BaseModel):
    value: str
    demos: int


class ExploreOut(BaseModel):
    models: ExploreGroupOut
    tasks_total: int = 0
    tasks: list[TaskSummaryOut] = []
    tags: dict[str, list[ExploreTagValuesOut]] = {}


# ---------- v2 B1.5：治理写接口请求体（admin only） ----------


class ModelCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)
    vendor: str | None = Field(default=None, max_length=64)
    description: str = Field(default="", max_length=1000)
    status: str = Field(default="active", pattern="^(candidate|active|unverified|deprecated)$")


class ModelUpdate(BaseModel):
    name: str | None = Field(default=None, min_length=1, max_length=128)
    vendor: str | None = Field(default=None, max_length=64)
    description: str | None = Field(default=None, max_length=1000)
    # 改 slug：ASCII 安全值，旧 slug 自动转为别名（匹配层继续认，但对外链接会变）
    slug: str | None = Field(default=None, min_length=1, max_length=100)


class ModelStatusIn(BaseModel):
    status: str = Field(pattern="^(candidate|active|unverified|deprecated)$")
    reason: str = Field(default="", max_length=500)


class DeriveIn(BaseModel):
    """标签建议包的输入：作者已经写下的那几样东西。"""

    title: str = Field(default="", max_length=200)
    description: str = Field(default="", max_length=4000)
    prompt: str = Field(default="", max_length=8000)
    limit: int = Field(default=8, ge=1, le=20)


class DeriveItemOut(BaseModel):
    key: str
    value: str
    label: str = ""
    confidence: float = 0.0
    reason: str = ""
    demo_count: int | None = None


class DeriveOut(BaseModel):
    items: list[DeriveItemOut] = []
    note: str = ""


class AliasIn(BaseModel):
    alias: str = Field(min_length=1, max_length=128)


class MergeIn(BaseModel):
    """合并请求（model / task 同构）：dry_run=True 先预览影响面，确认后才真合。"""

    target_id: int
    dry_run: bool = False
    reason: str = Field(default="", max_length=500)


class UnmergeIn(BaseModel):
    """撤销合并：dry_run=True 先看能迁回多少（合并后可能又做过归属，必须预览）。"""

    dry_run: bool = False
    reason: str = Field(default="", max_length=500)


class TaskCreateIn(BaseModel):
    title: str = Field(min_length=1, max_length=200)
    description: str = Field(default="", max_length=5000)
    category: str | None = Field(default=None, max_length=64)
    status: str = Field(default="active", pattern="^(candidate|active|merged|hidden)$")
    # 建题即挂题（prompt 簇「成题」一次点击完成：新建 + 批量挂载）
    demo_ids: list[int] = Field(default_factory=list, max_length=500)
    # M3-B2：按 slug 初始挂载（实体详情/知识中心直建题用；与 demo_ids 可混用）
    demo_slugs: list[str] = Field(default_factory=list, max_length=500)


class TaskUpdateIn(BaseModel):
    title: str | None = Field(default=None, min_length=1, max_length=200)
    description: str | None = Field(default=None, max_length=5000)
    category: str | None = Field(default=None, max_length=64)
    status: str | None = Field(default=None, pattern="^(candidate|active|merged|hidden)$")
    # M3-B1 跃迁理由：落 update 审计的 reason（此前审计理由恒为「编辑题目」——协作项落地）
    reason: str | None = Field(default=None, max_length=500)


class AttachDemosIn(BaseModel):
    """挂题：demo_ids（既有，簇成题用）或 demo_slugs（M3-B2，实体详情按 slug 挂）至少给一种。"""

    demo_ids: list[int] = Field(default_factory=list, max_length=500)
    demo_slugs: list[str] = Field(default_factory=list, max_length=500)


class AttributeIn(BaseModel):
    """归属工作台：把作品从兜底位迁到真实型号。"""

    demo_ids: list[int] = Field(min_length=1, max_length=200)
    target_id: int
    reason: str = Field(default="", max_length=500)


class SuggestionReviewIn(BaseModel):
    action: str = Field(pattern="^(approve|reject)$")


class BatchReviewIn(BaseModel):
    """M3-B3 收件箱批量审核：事务内逐条走 suggestion_service.review（每条独立落审计，单条失败不拖垮整批）。"""

    action: str = Field(pattern="^(approve|reject)$")
    ids: list[int] = Field(min_length=1, max_length=500)


# ---------- 收藏夹 / 浏览历史 / 账号资料（本轮） ----------
class CollectionOut(BaseModel):
    id: int
    owner_username: str
    title: str
    description: str = ""
    visibility: str = "private"
    is_default: bool = False
    item_count: int = 0
    # 夹内前 3 件作品封面（列表缩略用），避免前端为每个夹各发一次请求
    cover_urls: list[str] = []
    updated_at: datetime


class CollectionCreateIn(BaseModel):
    title: str = Field(min_length=1, max_length=60)
    description: str = Field(default="", max_length=200)
    visibility: str = "private"


class CollectionUpdateIn(BaseModel):
    title: str | None = Field(default=None, max_length=60)
    description: str | None = Field(default=None, max_length=200)
    visibility: str | None = None


class CollectionItemIn(BaseModel):
    slug: str


class CollectionItemOut(BaseModel):
    demo: DemoSummaryOut
    added_at: datetime


class CollectionItemPage(BaseModel):
    items: list[CollectionItemOut]
    total: int
    page: int
    page_size: int


class FavoriteStatus(BaseModel):
    favorited: bool
    collection_ids: list[int] = []


class FavoriteToggleIn(BaseModel):
    slug: str
    # 不传 = 走默认夹（"收藏"永远只点一下）
    collection_id: int | None = None


class HistoryItemOut(BaseModel):
    demo: DemoSummaryOut
    viewed_at: datetime


class HistoryPage(BaseModel):
    items: list[HistoryItemOut]
    total: int
    page: int
    page_size: int


class MePatch(BaseModel):
    display_name: str | None = Field(default=None, max_length=64)
    bio: str | None = Field(default=None, max_length=500)
    history_enabled: bool | None = None
