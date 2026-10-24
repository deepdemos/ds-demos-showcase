// API 统一出口：默认 Mock 模式；设置 VITE_USE_MOCK=false 切换真实后端。
import { http } from './http'
import { mockApi } from './mock'
import type {
  AdminDemo,
  AdminStats,
  AdminUser,
  Announcement,
  AnnouncementInput,
  AuthResponse,
  CreateDemoPayload,
  DemoCreateResult,
  DemoMeta,
  CurationResult,
  DemoDetail,
  DemoListParams,
  FeaturedPool,
  ForumTopic,
  ForumReply,
  ForumTopicInput,
  ForumTopicAdminUpdate,
  ForumReport,
  ForumReportInput,
  Notification,
  ReactionSummary,
  UserProfile,
  FollowOut,
  DemoSummary,
  Paginated,
  PeekResult,
  SessionLog,
  Settings,
  SiteStats,
  SponsorBoard,
  Tag,
  TagKeyInfo,
  TagKeyValue,
  TagMergeResult,
  TagMergeInput,
  TagSuggestion,
  ThanksBoard,
  UpdateDemoPayload,
  User,
  UserLeaderboardItem,
  UserPublic,
  RecognitionInput,
  RecognitionItem,
  RatingStats,
  LiveStats,
  SiteInfo,
  OssSyncJob,
  ModelDetail,
  PaginatedModels,
  PaginatedTasks,
  TaskDetail,
  TaskProposalIn,
  TaskProposalOut,
  PromptListItem,
  PromptSummary,
  PromptListParams,
  PromptProposalIn,
  PromptProposalOut,
  TaskSuggestItem,
  SamePromptResult,
  PromptClusters,
  ExploreResult,
  AttributionPending,
  AttributeResult,
  TypeDemoPreview,
  TypeDemoQueueResult,
  InspectionResult,
  KnowledgeStats,
  AuditList,
  AdminModelList,
  AdminTaskDetail,
  AdminTaskList,
  MergePreview,
  EntityConflicts,
  MergeHistoryItem,
  UnmergePreview,
  ModelBrief,
  DeriveResult,
  SuggestionItem,
  SuggestionList,
  CollectionItemOut,
  CollectionOut,
  CollectionVisibility,
  FavoriteStatus,
  HistoryItemOut,
  MePatch,
} from './types'

const useMock = (import.meta.env.VITE_USE_MOCK ?? 'true') !== 'false'

export const isMock = useMock

const realApi = {
  // 列表只回正文摘要（content_excerpt ≤480 字），全量走 getPrompt —— 列表页不再拖全量正文
  async listPrompts(params: PromptListParams = {}): Promise<Paginated<PromptListItem>> {
    const { data } = await http.get('/prompts', { params })
    return data
  },
  async getPrompt(id: number): Promise<PromptSummary> {
    const { data } = await http.get(`/prompts/${id}`)
    return data
  },
  async getPromptDemos(id: number, params: { page?: number; page_size?: number } = {}): Promise<Paginated<DemoSummary>> {
    const { data } = await http.get(`/prompts/${id}/demos`, { params })
    return data
  },
  async proposePrompt(payload: PromptProposalIn): Promise<PromptProposalOut> {
    const { data } = await http.post('/prompts', payload)
    return data
  },
  // ---------- 认证 ----------
  async login(username: string, password: string): Promise<AuthResponse> {
    const { data } = await http.post('/auth/login', { username, password })
    return data
  },
  async register(username: string, password: string): Promise<AuthResponse> {
    const { data } = await http.post('/auth/register', { username, password })
    return data
  },
  async logout(): Promise<void> {
    await http.post('/auth/logout')
  },
  async me(): Promise<User> {
    const { data } = await http.get('/auth/me')
    return data
  },
  async changePassword(old_password: string, new_password: string): Promise<void> {
    await http.post('/auth/change-password', { old_password, new_password })
  },
  async getUser(username: string): Promise<User & { demo_count: number }> {
    const { data } = await http.get(`/users/${encodeURIComponent(username)}`)
    return data
  },

  // ---------- 标签 ----------
  async getTag(key: string, value: string): Promise<Tag> {
    const { data } = await http.get(`/tags/${encodeURIComponent(`${key}:${value}`)}`)
    return data
  },
  async createTag(key: string, value: string, description?: string, parent_id?: number | null, group?: string): Promise<Tag> {
    const { data } = await http.post('/tags', { key, value, description, parent_id, group })
    return data
  },
  async suggestTagValue(payload: { key: string; value: string; description?: string; group?: string; demo_id?: number | null }): Promise<TagSuggestion> {
    const { data } = await http.post('/tags/suggestions', payload)
    return data
  },
  async listTagSuggestions(status?: 'pending' | 'approved' | 'rejected'): Promise<TagSuggestion[]> {
    const { data } = await http.get('/tags/admin/suggestions', { params: status ? { status } : {} })
    return data
  },
  async renameTagGroup(key: string, group: string, newGroup: string): Promise<{ updated: number; new_group: string }> {
    const { data } = await http.put(`/tags/admin/groups/${encodeURIComponent(key)}/${encodeURIComponent(group)}`, { new_group: newGroup })
    return data
  },
  async clearTagGroup(key: string, group: string): Promise<{ cleared: number }> {
    const { data } = await http.delete(`/tags/admin/groups/${encodeURIComponent(key)}/${encodeURIComponent(group)}`)
    return data
  },
  async setTagGroup(tagId: number, group: string | null): Promise<TagKeyValue> {
    const { data } = await http.put(`/tags/admin/values/${tagId}/group`, { group })
    return data
  },
  async mergeTags(payload: TagMergeInput): Promise<TagMergeResult> {
    const { data } = await http.post('/tags/admin/merge', payload)
    return data
  },

  async reviewTagSuggestion(id: number, action: 'approve' | 'reject', group?: string): Promise<TagSuggestion> {
    const { data } = await http.post(`/tags/admin/suggestions/${id}/review`, { action, group })
    return data
  },
  async fetchModels(): Promise<{ created: number; note: string }> {
    const { data } = await http.post('/tags/admin/fetch-models')
    return data
  },
  async aiSuggest(payload: { demo_id?: number; text?: string }): Promise<{ suggestions: { key: string; value: string; reason: string }[]; note: string }> {
    const { data } = await http.post('/tags/admin/ai-suggest', payload)
    return data
  },
  async listTagKeys(): Promise<TagKeyInfo[]> {
    const { data } = await http.get('/tags/tag-keys')
    return data
  },
  /** T3·M5-B2：管理端词表全量（含 deprecated + status 徽章）——知识中心总表/详情导航数据源 */
  async adminListTagKeys(): Promise<TagKeyInfo[]> {
    const { data } = await http.get('/tags/admin/tag-keys')
    return data
  },
  async createTagKey(payload: { key: string; mode: 'fixed' | 'open' | 'int'; label: string; description?: string; sort?: number }): Promise<TagKeyInfo> {
    const { data } = await http.post('/tags/admin/tag-keys', payload)
    return data
  },
  async updateTagKey(key: string, payload: { mode: 'fixed' | 'open' | 'int'; label: string; description?: string; sort?: number }): Promise<TagKeyInfo> {
    const { data } = await http.put(`/tags/admin/tag-keys/${encodeURIComponent(key)}`, payload)
    return data
  },
  async deleteTagKey(key: string): Promise<void> {
    await http.delete(`/tags/admin/tag-keys/${encodeURIComponent(key)}`)
  },
  async deleteTagValue(key: string, value: string): Promise<void> {
    await http.delete(`/tags/admin/tag-keys/${encodeURIComponent(key)}/values/${encodeURIComponent(value)}`)
  },

  // ---------- Demo ----------
  async listDemos(params: DemoListParams = {}): Promise<Paginated<DemoSummary>> {
    const { data } = await http.get('/demos', {
      params: {
        status: params.status,
        tag: params.tags,
        q: params.q,
        author: params.author,
        model: params.model,
        task: params.task,
        sort: params.sort,
        rs: params.rs,
        page: params.page,
        page_size: params.page_size,
        featured: params.featured,
      },
      paramsSerializer: { indexes: null },
    })
    return data
  },
  async getDemo(slug: string): Promise<DemoDetail> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}`)
    return data
  },
  /**
   * 按题目批量取**整组**（作品库 demo 组）。
   * 主列表按页切片，组会被页边界切开；前端拿到整组才能画完整的堆叠卡、
   * 并按「组内最近更新 / 浏览最多 / 最高分」算组级排序。
   */
  async demosByTasks(taskIds: number[]): Promise<DemoSummary[]> {
    if (!taskIds.length) return []
    const { data } = await http.get('/demos/by-tasks', {
      params: { task_ids: taskIds },
      paramsSerializer: { indexes: null },
    })
    return data
  },
  /** KB-29：卡片专用轻量口（不增加 view_count）。凡「只要 slug/title/封面/作者」的
   *  卡片场景都必须用它 —— 用 getDemo 会顺手把该作品的浏览量刷上去。 */
  async demoMeta(slug: string): Promise<DemoMeta> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/meta`)
    return data
  },
  async getRelated(slug: string): Promise<DemoSummary[]> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/related`, { params: { limit: 30 } })
    return data
  },
  async getSamePrompt(slug: string, limit = 12): Promise<SamePromptResult> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/same-prompt`, { params: { limit } })
    return data
  },
  async getRating(slug: string, deviceId?: string): Promise<RatingStats> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/rating`, { params: deviceId ? { device_id: deviceId } : {} })
    return data
  },
  async rateDemo(slug: string, score: number, deviceId?: string): Promise<RatingStats> {
    const { data } = await http.post(`/demos/${encodeURIComponent(slug)}/rating`, { score, device_id: deviceId || '' })
    return data
  },
  async unrateDemo(slug: string, deviceId?: string): Promise<RatingStats> {
    const { data } = await http.delete(`/demos/${encodeURIComponent(slug)}/rating`, { params: deviceId ? { device_id: deviceId } : {} })
    return data
  },
  async getLeaderboard(
    sort: 'avg' | 'god' | 'ghost' | 'net' | 'count' | 'heat',
    page = 1,
    pageSize = 20,
    range: 'all' | 'week' | 'month' = 'all',
  ): Promise<Paginated<DemoSummary>> {
    const { data } = await http.get('/leaderboard', { params: { sort, page, page_size: pageSize, range } })
    return data
  },
  async userLeaderboard(
    sort: 'reputation' | 'likes' | 'thanks' | 'topics' | 'replies' | 'demos' | 'followers',
    page = 1,
    pageSize = 20,
  ): Promise<Paginated<UserLeaderboardItem>> {
    const { data } = await http.get('/users/leaderboard', { params: { sort, page, page_size: pageSize } })
    return data
  },
  async getForumTopic(id: number): Promise<ForumTopic | null> {
    try {
      const { data } = await http.get(`/forum/topics/${id}`)
      return data
    } catch {
      return null
    }
  },
  async toggleReaction(targetType: 'topic' | 'reply', targetId: number, reactionType: 'like' | 'thanks'): Promise<ReactionSummary & { active: boolean }> {
    const { data } = await http.post('/forum/reactions', { target_type: targetType, target_id: targetId, reaction_type: reactionType })
    return data
  },
  async getUserProfile(username: string): Promise<UserProfile> {
    const { data } = await http.get(`/users/${encodeURIComponent(username)}/profile`)
    return data
  },
  async toggleFollow(userId: number): Promise<FollowOut> {
    const { data } = await http.post(`/users/${userId}/follow`)
    return data
  },
  async listFollowers(username: string): Promise<UserPublic[]> {
    const { data } = await http.get(`/users/${encodeURIComponent(username)}/followers`)
    return data
  },
  async listFollowing(username: string): Promise<UserPublic[]> {
    const { data } = await http.get(`/users/${encodeURIComponent(username)}/following`)
    return data
  },
  async listForumTopics(params: { q?: string; category?: string; tag?: string; demo?: string; sort?: 'newest' | 'popular' | 'replies' | 'hot'; sticky?: boolean; participated?: boolean; followed?: boolean; kind?: 'general' | 'demo'; page?: number; page_size?: number } = {}): Promise<Paginated<ForumTopic>> {
    const { data } = await http.get('/forum/topics', { params })
    return data
  },
  async listForumReplies(topicId: number): Promise<ForumReply[]> {
    const { data } = await http.get(`/forum/topics/${topicId}/replies`)
    // 后端已改成分页对象 { items, total, page, page_size }；兼容旧版数组返回
    return Array.isArray(data) ? data : (data.items ?? [])
  },
  async listForumRepliesPage(topicId: number, page = 1, pageSize = 50): Promise<Paginated<ForumReply>> {
    const { data } = await http.get(`/forum/topics/${topicId}/replies`, { params: { page, page_size: pageSize } })
    return data
  },
  async createForumTopic(payload: ForumTopicInput): Promise<ForumTopic> {
    const { data } = await http.post('/forum/topics', {
      title: payload.title,
      content: payload.content || '',
      demo_slug: payload.demo_slug ?? null,
      category: payload.category || 'general',
      tags: (payload.tags || []).join(','),
    })
    return data
  },
  async createForumReply(topicId: number, content: string, parentId?: number): Promise<ForumReply> {
    const { data } = await http.post(`/forum/topics/${topicId}/replies`, { content, parent_id: parentId ?? null })
    return data
  },
  async adminListForumTopics(params: { q?: string; status?: string; category?: string; pinned?: boolean; page?: number; page_size?: number } = {}): Promise<Paginated<ForumTopic>> {
    const { data } = await http.get('/forum/admin/topics', { params })
    return data
  },
  async adminUpdateForumTopic(id: number, patch: ForumTopicAdminUpdate): Promise<ForumTopic> {
    const { data } = await http.put(`/forum/admin/topics/${id}`, patch)
    return data
  },
  async adminDeleteForumTopic(id: number): Promise<void> {
    await http.delete(`/forum/admin/topics/${id}`)
  },
  async adminDeleteForumReply(id: number): Promise<void> {
    await http.delete(`/forum/admin/replies/${id}`)
  },
  async adminReviewForumTopic(id: number, action: 'approve' | 'reject'): Promise<ForumTopic> {
    const { data } = await http.post(`/forum/admin/topics/${id}/review`, { action })
    return data
  },
  async adminReviewForumReply(id: number, action: 'approve' | 'reject'): Promise<ForumReply> {
    const { data } = await http.post(`/forum/admin/replies/${id}/review`, { action })
    return data
  },
  async adminListForumReplies(params: { topic_id?: number; status?: string; q?: string; limit?: number } = {}): Promise<ForumReply[]> {
    const { data } = await http.get('/forum/admin/replies', { params })
    return data
  },

  async listForumReports(): Promise<ForumReport[]> {
    const { data } = await http.get('/forum/admin/reports')
    return data
  },
  async handleForumReport(id: number, action: 'resolve' | 'dismiss'): Promise<ForumReport> {
    const { data } = await http.post(`/forum/admin/reports/${id}/handle`, { action })
    return data
  },
  async createForumReport(payload: ForumReportInput): Promise<ForumReport> {
    const { data } = await http.post('/forum/reports', payload)
    return data
  },
  async adminBanUser(id: number): Promise<void> {
    await http.post(`/forum/admin/users/${id}/ban`)
  },
  async listNotifications(params: { unread_only?: boolean; page?: number; page_size?: number } = {}): Promise<Notification[]> {
    const { data } = await http.get('/notifications', { params })
    return data
  },
  async getUnreadCount(): Promise<{ count: number }> {
    const { data } = await http.get('/notifications/unread-count')
    return data
  },
  async markNotificationRead(id: number): Promise<Notification> {
    const { data } = await http.post('/notifications/read', { id })
    return data
  },
  async markAllNotificationsRead(): Promise<void> {
    await http.post('/notifications/read-all')
  },



  async createDemo(payload: CreateDemoPayload, onProgress?: (percent: number) => void): Promise<DemoCreateResult> {
    const form = new FormData()
    form.append('title', payload.title)
    if (payload.description) form.append('description', payload.description)
    if (payload.tags?.length) form.append('tags', JSON.stringify(payload.tags))
    if (payload.demo_type) form.append('demo_type', payload.demo_type)
    if (payload.external_url) form.append('external_url', payload.external_url)
    if (payload.prompt) form.append('prompt', payload.prompt)
    if (payload.video_url) form.append('video_url', payload.video_url)
    if (payload.cover) form.append('cover', payload.cover)
    if (payload.file) form.append('file', payload.file)
    if (payload.idempotency_key) form.append('idempotency_key', payload.idempotency_key)
    if (payload.task) form.append('task', payload.task)
    if (payload.propose_task) form.append('propose_task', JSON.stringify(payload.propose_task))
    if (payload.model_hint) form.append('model_hint', payload.model_hint)
    if (payload.upload_code) form.append('upload_code', payload.upload_code)
    if (payload.force) form.append('force', 'true')
    // 上传含解压 + OSS 传输，放宽超时（默认 15s 不够）；onUploadProgress 给前端进度条
    const { data } = await http.post('/demos', form, {
      timeout: 120000,
      onUploadProgress: onProgress
        ? (e) => {
            if (e.total) onProgress(Math.min(100, Math.round((e.loaded / e.total) * 100)))
          }
        : undefined,
    })
    return data
  },
  async updateDemo(
    slug: string,
    payload: UpdateDemoPayload,
    onProgress?: (percent: number) => void,
    force = false,
  ): Promise<void> {
    const form = new FormData()
    if (payload.title) form.append('title', payload.title)
    if (payload.description !== undefined) form.append('description', payload.description)
    if (payload.tags) form.append('tags', JSON.stringify(payload.tags))
    if (payload.demo_type) form.append('demo_type', payload.demo_type)
    if (payload.external_url !== undefined) form.append('external_url', payload.external_url || '')
    if (payload.prompt !== undefined) form.append('prompt', payload.prompt)
    if (payload.video_url !== undefined) form.append('video_url', payload.video_url || '')
    if (payload.cover) form.append('cover', payload.cover)
    if (payload.file) form.append('file', payload.file)
    if (payload.commit_message) form.append('commit_message', payload.commit_message)
    if (payload.keep_old_version) form.append('keep_old_version', 'true')
    // P4：编辑路径原先拿不到 force —— 与新建路径不对称，导致"重复内容 409"只能改内容绕开。
    // 后端 PUT /demos/{slug} 接受 force=1（demos.py），这里按需带上（默认不带，保持原行为）。
    await http.put(`/demos/${encodeURIComponent(slug)}${force ? '?force=1' : ''}`, form, {
      timeout: 120000,
      onUploadProgress: onProgress
        ? (e) => {
            if (e.total) onProgress(Math.min(100, Math.round((e.loaded / e.total) * 100)))
          }
        : undefined,
    })
  },
  async deleteDemo(slug: string): Promise<void> {
    await http.delete(`/demos/${encodeURIComponent(slug)}`)
  },
  async downloadDemo(slug: string): Promise<void> {
    // 直接整页导航下载：后端 307 → OSS（Content-Disposition: attachment）触发浏览器原生下载。
    // 不经过 XHR/blob，因此不需要 OSS CORS 放行主站源；大 zip 走流式下载也不占内存。
    window.location.assign(`/api/v1/demos/${encodeURIComponent(slug)}/download`)
  },

  // ---------- 评论 ----------
  /** KB-31：删除零引用实体（有引用/挂载时后端 409，文案直接透传给用户）。 */
  async adminDeleteModel(ident: string): Promise<void> {
    await http.delete(`/admin/models/${encodeURIComponent(ident)}`)
  },
  async adminDeleteTask(ident: string): Promise<void> {
    await http.delete(`/admin/tasks/${encodeURIComponent(ident)}`)
  },
  /** KB-30：从 models.dev 同步模型字典（新模型进待审建议）。
   *  后端要抓外部网络（最长 30s），这里单独放宽超时，否则前端 15s 先断、后端还在跑。 */
  async syncModels(): Promise<{ providers: number; total_models: number; new_pending: number; updated_group: number; note: string }> {
    const { data } = await http.post('/tags/admin/sync-models', undefined, { timeout: 60000 })
    return data
  },
  // KB-33：旧评论通道已废弃（后端 410 迁论坛），这里不再暴露必失败的包装。
  // ---------- Session Logs ----------
  async listSessionLogs(slug: string): Promise<SessionLog[]> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/session-logs`)
    return data
  },
  async getSessionLog(slug: string, filename: string): Promise<string> {
    const { data } = await http.get(`/demos/${encodeURIComponent(slug)}/session-logs/${encodeURIComponent(filename)}`, {
      responseType: 'text',
    })
    return data
  },

  // ---------- Admin ----------
  async adminDemos(): Promise<AdminDemo[]> {
    const { data } = await http.get('/admin/demos')
    return data
  },
  async adminUsers(): Promise<AdminUser[]> {
    const { data } = await http.get('/admin/users')
    return data
  },
  async adminReview(): Promise<DemoDetail[]> {
    const { data } = await http.get('/admin/review')
    return data
  },
  async adminApprove(idOrSlug: string | number, action: 'approve' | 'reject'): Promise<void> {
    await http.post(`/admin/review/${idOrSlug}`, { action })
  },
  /** astra 橱窗策展：发放/回收站点通行证 + 语言标记（None 字段后端保持不变） */
  async setCuration(slug: string, body: { sites?: string[]; lang?: 'zh' | 'en' }): Promise<CurationResult> {
    const { data } = await http.put(`/admin/demos/${encodeURIComponent(slug)}/curation`, body)
    return data
  },
  // ---------- 首页策展池（07 §2.2 / T5·M5-F1）----------
  /** 精选池列表（含 featured_order；面板数据源） */
  async listFeatured(): Promise<FeaturedPool> {
    const { data } = await http.get('/admin/featured')
    return data
  },
  /** 加入精选池（尾部追加 order）；仅已上架作品可进（后端校验） */
  async addFeaturedDemo(payload: { slug?: string; demo_id?: number }): Promise<{ ok: boolean; slug: string; featured_order: number; total: number }> {
    const { data } = await http.post('/admin/featured', payload)
    return data
  },
  /** 移出精选池（剩余自动重排） */
  async removeFeaturedDemo(demoId: number): Promise<{ ok: boolean; slug: string }> {
    const { data } = await http.delete(`/admin/featured/${demoId}`)
    return data
  },
  /** 池内上移/下移 */
  async moveFeaturedDemo(demoId: number, direction: 'up' | 'down'): Promise<{ ok: boolean; slug: string; featured_order: number }> {
    const { data } = await http.put(`/admin/featured/${demoId}/order`, { direction })
    return data
  },
  /** 置顶为 hero（=池首，首页大卡取 order 最小） */
  async heroFeaturedDemo(demoId: number): Promise<{ ok: boolean; slug: string; featured_order: number }> {
    const { data } = await http.put(`/admin/featured/${demoId}/hero`)
    return data
  },
  async getSettings(): Promise<Settings> {
    const { data } = await http.get('/admin/settings')
    return data
  },
  async updateSettings(next: Settings): Promise<Settings> {
    const { data } = await http.put('/admin/settings', next)
    return data
  },
  async ossSync(force = false): Promise<{ started: boolean; job: OssSyncJob }> {
    // 后台任务：立即返回，前端轮询 /admin/oss-sync-status 看进度
    const { data } = await http.post('/admin/oss-sync', null, { params: force ? { force: true } : {} })
    return data
  },
  async getOssSyncStatus(): Promise<OssSyncJob> {
    const { data } = await http.get('/admin/oss-sync-status')
    return data
  },
  async getAdminStats(): Promise<AdminStats> {
    const { data } = await http.get('/admin/stats')
    return data
  },
  async storageStatus(): Promise<{ oss_enabled: boolean; mode: string; local_demos: number; local_files: number; local_size_bytes: number }> {
    const { data } = await http.get('/admin/storage-status')
    return data
  },
  async updateUser(id: number, patch: Partial<Pick<User, 'role' | 'status'>>): Promise<User> {
    const { data } = await http.patch(`/users/${id}`, patch)
    return data
  },

  // ---------- 站点统计 / 赞助榜（关于本站页） ----------
  async getSiteStats(): Promise<SiteStats> {
    const { data } = await http.get('/stats/visits')
    return data
  },
  async reportVisit(): Promise<void> {
    // 页面访问打点：一次路由切换 = 一次浏览（原始 PV +1）；失败静默
    http.post('/stats/visit').catch(() => undefined)
  },
  async reportHeartbeat(): Promise<void> {
    // 实时在线心跳：每 30s 一次；失败静默
    http.post('/stats/heartbeat').catch(() => undefined)
  },
  async getLiveStats(): Promise<LiveStats> {
    const { data } = await http.get('/stats/live')
    return data
  },
  // 站点公开概况（后端 60s 缓存 + CDN 可缓存）：内容/社区/流量/热门一次拿全
  // refresh=true 仅 admin 生效：强刷后端缓存（切整活开关后立刻广播新状态）
  async getSiteInfo(opts?: { refresh?: boolean }): Promise<SiteInfo> {
    const { data } = await http.get('/meta/site-info', { params: opts?.refresh ? { refresh: 1 } : {} })
    return data
  },

  // ---------- v2 实体：Model / Task ----------
  async listModels(params: { status?: string; vendor?: string; q?: string; sort?: 'demos' | 'score' | 'rating' | 'votes' | 'new' | 'name'; page?: number; page_size?: number } = {}): Promise<PaginatedModels> {
    const { data } = await http.get('/models', { params })
    return data
  },
  async getExplore(): Promise<ExploreResult> {
    const { data } = await http.get('/explore')
    return data
  },
  async getModel(slug: string): Promise<ModelDetail> {
    const { data } = await http.get(`/models/${encodeURIComponent(slug)}`)
    return data
  },
  /** 模型页作品清单（详情接口只带首屏 12 件，看全部走这里） */
  /** 侧滑"瞄一眼"：紧凑摘要（比详情接口轻得多，见 API_CONTRACT §27） */
  async peek(kind: 'model' | 'task' | 'demo', slug: string): Promise<PeekResult> {
    const { data } = await http.get(`/peek/${kind}/${encodeURIComponent(slug)}`)
    return data
  },
  async getModelDemos(
    slug: string,
    params: { sort?: 'newest' | 'score' | 'popular'; type?: string; game?: string; page?: number; page_size?: number } = {},
  ): Promise<Paginated<DemoSummary>> {
    const { data } = await http.get(`/models/${encodeURIComponent(slug)}/demos`, { params })
    return data
  },
  async listTasks(params: { status?: string; q?: string; category?: string; sort?: 'demos' | 'newest'; page?: number; page_size?: number } = {}): Promise<PaginatedTasks> {
    const { data } = await http.get('/tasks', { params })
    return data
  },
  /** 规则层相似题目建议（上传页挂题选择器；纯读、无 LLM） */
  async suggestTasks(q: string, limit = 6): Promise<TaskSuggestItem[]> {
    const { data } = await http.get('/tasks/suggest', { params: { q, limit } })
    return data
  },
  async getTask(slug: string): Promise<TaskDetail> {
    const { data } = await http.get(`/tasks/${encodeURIComponent(slug)}`)
    return data
  },
  /** 独立出题（登录用户，不依赖上传作品）：只进待审收件箱，管理员批准才公开 */
  async proposeTask(payload: TaskProposalIn): Promise<TaskProposalOut> {
    const { data } = await http.post('/tasks', payload)
    return data
  },
  // ---------- v2 B3′：治理台（admin only） ----------
  async getPromptClusters(opts: { refresh?: boolean; minScore?: number } = {}): Promise<PromptClusters> {
    const { data } = await http.get('/admin/prompt-clusters', {
      params: { refresh: opts.refresh ? 1 : undefined, min_score: opts.minScore },
    })
    return data
  },
  async adminCreateTask(payload: {
    title: string
    description?: string
    category?: string
    status?: string
    demo_ids?: number[]
    demo_slugs?: string[]
  }): Promise<{ id: number; slug: string; title: string; status: string; attached: number }> {
    const { data } = await http.post('/admin/tasks', payload)
    return data
  },
  async listSuggestions(params: { status?: string; kind?: string } = {}): Promise<SuggestionList> {
    const { data } = await http.get('/admin/suggestions', { params })
    return data
  },
  async reviewSuggestion(id: number, action: 'approve' | 'reject'): Promise<SuggestionItem> {
    const { data } = await http.post(`/admin/suggestions/${id}/review`, { action })
    return data
  },
  // 详情口：列表里 new_prompt 的 payload.content 超 480 字会被截断，审核员点「查看全文」从这里取全量
  async getSuggestion(id: number): Promise<SuggestionItem> {
    const { data } = await http.get(`/admin/suggestions/${id}`)
    return data
  },
  async getAttributionPending(): Promise<AttributionPending> {
    const { data } = await http.get('/admin/attribution/pending')
    return data
  },
  async attributeDemos(payload: { demo_ids: number[]; target_id: number; reason?: string }): Promise<AttributeResult> {
    const { data } = await http.post('/admin/attribution', payload)
    return data
  },
  async getTypeDemoPreview(params: { limit?: number; min_confidence?: number } = {}): Promise<TypeDemoPreview> {
    const { data } = await http.get('/admin/type-demo/preview', { params })
    return data
  },
  async queueTypeDemo(payload: { limit?: number; min_confidence?: number } = {}): Promise<TypeDemoQueueResult> {
    const { data } = await http.post('/admin/type-demo/queue', null, { params: payload })
    return data
  },
  async getInspection(params: { sample_limit?: number } = {}): Promise<InspectionResult> {
    const { data } = await http.get('/admin/inspection', { params })
    return data
  },
  async queueInspection(checkId: string, minConfidence = 0.8): Promise<{ check: string; proposed: number; queued: number }> {
    const { data } = await http.post(`/admin/inspection/${encodeURIComponent(checkId)}/queue`, null, {
      params: { min_confidence: minConfidence },
    })
    return data
  },
  async getKnowledgeStats(): Promise<KnowledgeStats> {
    const { data } = await http.get('/admin/knowledge/stats')
    return data
  },
  async getAudit(
    params: { action?: string; entity_type?: string; entity_id?: number; q?: string; page?: number; page_size?: number } = {},
  ): Promise<AuditList> {
    const { data } = await http.get('/admin/audit', { params })
    return data
  },
  async adminListModels(params: { q?: string; status?: string; page_size?: number } = {}): Promise<AdminModelList> {
    const { data } = await http.get('/admin/models', { params })
    return data
  },
  /** KB-28：管理端新建模型实体（POST /admin/models）。后端同时把 model:<name> 写入词表，
   *  所以建完立即可在上传选择器里选到；重名或别名冲突返回 409。 */
  async adminCreateModel(payload: {
    name: string
    vendor?: string
    description?: string
    status?: string
  }): Promise<{ id: number; slug: string; name: string; vendor?: string | null; status: string }> {
    const { data } = await http.post('/admin/models', payload)
    return data
  },
  async adminListEntityTasks(params: { q?: string; status?: string; page_size?: number } = {}): Promise<AdminTaskList> {
    const { data } = await http.get('/admin/tasks', { params })
    return data
  },
  async mergeEntity(kind: 'models' | 'tasks', ident: string, payload: { target_id: number; dry_run?: boolean; reason?: string }): Promise<MergePreview> {
    const { data } = await http.post(`/admin/${kind}/${encodeURIComponent(ident)}/merge`, payload)
    return data
  },
  async addModelAlias(ident: string, alias: string): Promise<{ ok: boolean }> {
    const { data } = await http.post(`/admin/models/${encodeURIComponent(ident)}/aliases`, { alias })
    return data
  },
  async removeModelAlias(ident: string, alias: string): Promise<void> {
    await http.delete(`/admin/models/${encodeURIComponent(ident)}/aliases/${encodeURIComponent(alias)}`)
  },
  async getEntityConflicts(): Promise<EntityConflicts> {
    const { data } = await http.get('/admin/entity-conflicts')
    return data
  },
  async updateModel(ident: string | number, payload: { name?: string; vendor?: string; description?: string; slug?: string }): Promise<ModelBrief> {
    const { data } = await http.put(`/admin/models/${encodeURIComponent(String(ident))}`, payload)
    return data
  },
  /** M3-3 直改权：Model 状态跃迁（candidate/active/unverified/deprecated，理由必填走审计）——既有端点 PUT /admin/models/{ident}/status */
  async setModelStatus(ident: string | number, payload: { status: string; reason?: string }): Promise<{ id: number; slug: string; status: string }> {
    const { data } = await http.put(`/admin/models/${encodeURIComponent(String(ident))}/status`, payload)
    return data
  },
  /** T3·M5-B2 直改权：Tag 状态跃迁（candidate/active/deprecated，理由必填走审计）——独立端点 PUT /admin/entities/tag/{id}/status（不并入 PATCH） */
  async setTagStatus(tagId: number, payload: { status: string; reason?: string }): Promise<{ id: number; key: string; value: string; status: string }> {
    const { data } = await http.put(`/admin/entities/tag/${tagId}/status`, payload)
    return data
  },
  /** M3-3 直改权：Task 题面字段直改（title/description/category/status）——既有端点 PUT /admin/tasks/{ident}（服务端落 update 审计） */
  async updateTask(ident: string | number, payload: { title?: string; description?: string; category?: string | null; status?: string; reason?: string }): Promise<{ id: number; slug: string; title: string; status: string }> {
    const { data } = await http.put(`/admin/tasks/${encodeURIComponent(String(ident))}`, payload)
    return data
  },
  /** M3-B3 管理端题目详情（任何状态含 merged/hidden + 归属作品全量含 pending/rejected——挂摘 UI 数据源） */
  async getAdminTaskDetail(ident: string | number): Promise<AdminTaskDetail> {
    const { data } = await http.get(`/admin/tasks/${encodeURIComponent(String(ident))}`)
    return data
  },
  /** M3-B3 直改权统一入口：PATCH /admin/entities/{type}/{id}（白名单逐实体，服务端落审计） */
  async patchEntity(entityType: 'model' | 'task' | 'tag', ident: string | number, fields: Record<string, unknown>): Promise<Record<string, unknown>> {
    const { data } = await http.patch(`/admin/entities/${entityType}/${encodeURIComponent(String(ident))}`, fields)
    return data
  },
  /** M3-B3 收件箱批量审核（t4 前端限速循环→真批量端点；ids 1..500） */
  async batchReviewSuggestions(action: 'approve' | 'reject', ids: number[]): Promise<{ action: string; ok: number; failed: number; results: { id: number; ok: boolean; error?: string }[] }> {
    const { data } = await http.post('/admin/suggestions/batch-review', { action, ids })
    return data
  },
  /** M3-B3 按 slug 挂题（实体详情关联作品区；未知 slug 整批 404） */
  async attachTaskDemoBySlug(ident: string | number, demoSlug: string): Promise<{ task_id: number; attached: number }> {
    const { data } = await http.post(`/admin/tasks/${encodeURIComponent(String(ident))}/demos`, { demo_slugs: [demoSlug] })
    return data
  },
  /** M3-B3 按 slug 摘题（不在该题下 404） */
  async detachTaskDemoBySlug(ident: string | number, demoSlug: string): Promise<void> {
    await http.delete(`/admin/tasks/${encodeURIComponent(String(ident))}/demos/slug/${encodeURIComponent(demoSlug)}`)
  },
  async getMergeHistory(): Promise<{ items: MergeHistoryItem[] }> {
    const { data } = await http.get('/admin/models/merge-history')
    return data
  },
  async unmergeEntity(ident: string | number, payload: { dry_run?: boolean; reason?: string }): Promise<UnmergePreview> {
    const { data } = await http.post(`/admin/models/${encodeURIComponent(String(ident))}/unmerge`, payload)
    return data
  },
  /** 上传第 2 步的建议包：匿名可用、纯只读（不写库） */
  async deriveTags(payload: { title?: string; description?: string; prompt?: string; limit?: number }): Promise<DeriveResult> {
    const { data } = await http.post('/tags/derive', payload)
    return data
  },
  async getSponsors(): Promise<SponsorBoard> {
    const { data } = await http.get('/stats/sponsors')
    return data
  },
  async getThanks(): Promise<ThanksBoard> {
    const { data } = await http.get('/stats/thanks')
    return data
  },
  // 管理：赞助 / 致谢
  async listRecognition(): Promise<{ items: RecognitionItem[] }> {
    const { data } = await http.get('/stats/recognition')
    return data
  },
  async createRecognition(payload: RecognitionInput): Promise<{ id: number }> {
    const { data } = await http.post('/stats/recognition', payload)
    return data
  },
  async updateRecognition(id: number, payload: RecognitionInput): Promise<{ id: number }> {
    const { data } = await http.put(`/stats/recognition/${id}`, payload)
    return data
  },
  async deleteRecognition(id: number): Promise<void> {
    await http.delete(`/stats/recognition/${id}`)
  },

  // ---------- 公告 ----------
  async listAnnouncements(): Promise<Announcement[]> {
    const { data } = await http.get('/announcements')
    return data
  },
  async adminListAnnouncements(params: { status?: string; category?: string; pinned?: boolean } = {}): Promise<Announcement[]> {
    const { data } = await http.get('/admin/announcements', { params })
    return data
  },
  async createAnnouncement(payload: AnnouncementInput): Promise<Announcement> {
    const { data } = await http.post('/admin/announcements', payload)
    return data
  },
  async updateAnnouncement(id: number, payload: AnnouncementInput): Promise<Announcement> {
    const { data } = await http.put(`/admin/announcements/${id}`, payload)
    return data
  },
  async deleteAnnouncement(id: number): Promise<void> {
    await http.delete(`/admin/announcements/${id}`)
  },

  // ── 收藏夹 ───────────────────────────────────────────────
  async listMyCollections(): Promise<CollectionOut[]> {
    const { data } = await http.get('/me/collections')
    return data.items
  },
  async createCollection(payload: { title: string; description?: string; visibility?: CollectionVisibility }): Promise<CollectionOut> {
    const { data } = await http.post('/me/collections', payload)
    return data
  },
  async updateCollection(id: number, patch: { title?: string; description?: string; visibility?: CollectionVisibility }): Promise<CollectionOut> {
    const { data } = await http.patch(`/me/collections/${id}`, patch)
    return data
  },
  async deleteCollection(id: number): Promise<void> {
    await http.delete(`/me/collections/${id}`)
  },
  async listCollectionItems(id: number, params: { page?: number; pageSize?: number } = {}): Promise<Paginated<CollectionItemOut>> {
    const { data } = await http.get(`/me/collections/${id}/items`, { params: { page: params.page, page_size: params.pageSize } })
    return data
  },
  async addToCollection(id: number, slug: string): Promise<void> {
    await http.post(`/me/collections/${id}/items`, { slug })
  },
  async removeFromCollection(id: number, slug: string): Promise<void> {
    await http.delete(`/me/collections/${id}/items/${encodeURIComponent(slug)}`)
  },
  async getFavoriteStatus(slug: string): Promise<FavoriteStatus> {
    const { data } = await http.get('/me/favorites/status', { params: { slug } })
    return data
  },
  async toggleFavorite(slug: string, collectionId?: number): Promise<FavoriteStatus> {
    const { data } = await http.post('/me/favorites/toggle', { slug, collection_id: collectionId })
    return data
  },
  async listPublicCollections(username: string): Promise<CollectionOut[]> {
    const { data } = await http.get(`/users/${encodeURIComponent(username)}/collections`)
    return data.items
  },
  async getPublicCollection(id: number): Promise<CollectionOut> {
    const { data } = await http.get(`/collections/${id}`)
    return data
  },
  async listPublicCollectionItems(id: number, params: { page?: number; pageSize?: number } = {}): Promise<Paginated<CollectionItemOut>> {
    const { data } = await http.get(`/collections/${id}/items`, { params: { page: params.page, page_size: params.pageSize } })
    return data
  },

  // ── 浏览历史 ─────────────────────────────────────────────
  async recordView(slug: string): Promise<void> {
    // silent401：这是**后台可选调用**（未登录就不该记服务端历史），
    // 不能因为匿名 401 把正在看作品的读者弹去登录页（2026-09 线上事故）。
    await http.post(`/me/history/${encodeURIComponent(slug)}`, undefined, { silent401: true })
  },
  async listHistory(params: { page?: number; pageSize?: number } = {}): Promise<Paginated<HistoryItemOut>> {
    const { data } = await http.get('/me/history', { params: { page: params.page, page_size: params.pageSize } })
    return data
  },
  async clearHistory(): Promise<void> {
    await http.delete('/me/history')
  },
  async deleteHistoryItem(slug: string): Promise<void> {
    await http.delete(`/me/history/${encodeURIComponent(slug)}`)
  },

  // ── 资料与隐私 ───────────────────────────────────────────
  async updateMe(patch: MePatch): Promise<User> {
    const { data } = await http.patch('/auth/me', patch)
    return data
  },
  async uploadAvatar(file: File): Promise<{ avatar_url: string }> {
    const fd = new FormData()
    fd.append('file', file)
    const { data } = await http.post('/auth/me/avatar', fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    return data
  },
  async removeAvatar(): Promise<void> {
    await http.delete('/auth/me/avatar')
  },
}

export const api = useMock ? mockApi : realApi
