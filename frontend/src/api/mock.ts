// Mock API —— 前端独立运行时使用的占位数据与逻辑。
// 切换到真实后端：设置环境变量 VITE_USE_MOCK=false，api/index.ts 会改走 axios。

import { createMockPromptStore } from './mockPrompts'
import type {
  AdminDemo,
  AdminStats,
  AdminUser,
  Announcement,
  AnnouncementInput,
  AuthResponse,
  CreateDemoPayload,
  DemoCreateResult,
  CurationResult,
  DemoDetail,
  DemoMeta,
  DemoListParams,
  AdminFeaturedItem,
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
  CollectionOut,
  CollectionItemOut,
  FavoriteStatus,
  HistoryItemOut,
  MePatch,
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
  OssSyncJob,
  SiteInfo,
  ModelDetail,
  ModelSummary,
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
  TaskSummary,
  TaskSuggestItem,
  SamePromptResult,
  PromptCluster,
  PromptClusters,
  SuggestionItem,
  SuggestionList,
  ExploreResult,
  AttributionGroup,
  AttributionItem,
  AttributionPending,
  AttributeResult,
  TypeDemoPreview,
  TypeDemoQueueResult,
  InspectionCheck,
  InspectionResult,
  KnowledgeStats,
  AuditList,
  AuditEntry,
  AdminModelList,
  AdminTaskList,
  MergePreview,
  EntityConflicts,
  MergeHistoryItem,
  UnmergePreview,
  ModelBrief,
  DeriveResult,
  DerivedTag,
} from './types'

const delay = (ms = 180) => new Promise((r) => setTimeout(r, ms))

const forumTopics: ForumTopic[] = [
  { id: 1, title: '欢迎来到讨论区', content: '这里是 **AI 全民制作人** 的讨论区，欢迎交流作品与提示词。\n\n试试贴一个作品：[/demo/demo_粒子星空](/demo/demo_粒子星空)', author: 'tester', author_id: 2, demo_slug: null, category: '交流', tags: ['type:game'], pinned: true, sticky: true, locked: false, solved: false, status: 'normal', reply_count: 2, view_count: 88, like_count: 0, thanks_count: 0, my_reactions: [], created_at: '2026-08-01T10:00:00Z', updated_at: '2026-08-01T10:00:00Z' },
  { id: 2, title: '分享：用灰测模型做的贪吃蛇', content: '最近用 **ds-unknown** 生成了一个贪吃蛇，体验还不错。', author: 'alice', author_id: 3, demo_slug: 'demo_贪吃蛇', category: '分享', tags: ['model:ds-unknown'], pinned: false, sticky: true, locked: false, solved: false, status: 'normal', reply_count: 1, view_count: 42, like_count: 0, thanks_count: 0, my_reactions: [], created_at: '2026-08-02T09:00:00Z', updated_at: '2026-08-02T09:00:00Z' },
  { id: 3, title: '求助：iframe 全屏问题', content: '为什么 F 键有时候没反应？', author: 'tester', author_id: 2, demo_slug: null, category: '求助', tags: [], pinned: false, sticky: false, locked: false, solved: false, status: 'normal', reply_count: 0, view_count: 12, like_count: 0, thanks_count: 0, my_reactions: [], created_at: '2026-08-03T12:00:00Z', updated_at: '2026-08-03T12:00:00Z' },
]
const forumReplies: ForumReply[] = [
  { id: 1, topic_id: 1, author: 'admin', author_id: 1, content: '欢迎！有问题随时发帖。', created_at: '2026-08-01T10:30:00Z' },
  { id: 2, topic_id: 1, author: 'alice', author_id: 3, content: '希望论坛越来越好。', created_at: '2026-08-01T11:00:00Z' },
  { id: 3, topic_id: 2, author: 'tester', author_id: 2, content: '灰测模型效果确实不错。', created_at: '2026-08-02T10:00:00Z' },
]

const forumReports: ForumReport[] = [
  { id: 1, target_type: 'topic', target_id: 2, reason: '疑似违规内容', status: 'open', reporter_id: 3, created_at: '2026-08-04T10:00:00Z' },
]

const notifications: Notification[] = [
  { id: 1, type: 'forum_reply', actor: 'alice', actor_id: 3, demo_slug: null, topic_id: 1, reply_id: 2, read: false, created_at: '2026-08-04T10:00:00Z' },
  { id: 2, type: 'demo_review', actor: null, actor_id: null, demo_slug: 'demo_粒子星空', topic_id: null, reply_id: null, read: false, created_at: '2026-08-04T09:00:00Z' },
  { id: 3, type: 'review_result', actor: null, actor_id: null, demo_slug: 'demo_贪吃蛇', topic_id: null, reply_id: null, read: true, created_at: '2026-08-03T12:00:00Z' },
]

const recognition: RecognitionItem[] = [
  { id: 1, kind: 'sponsor', name: 'Alice', amount: 500, message: '支持 AI 全民制作人！', show_amount: true, sort: 0, active: true },
  { id: 2, kind: 'sponsor', name: 'Bob', amount: 300, message: '作品很棒', show_amount: true, sort: 0, active: true },
  { id: 3, kind: 'thanks', name: '小明', message: '感谢提供了这么好的 demo', show_amount: true, sort: 0, active: true },
]

const announcements: Announcement[] = [
  { id: 1, type: 'manual', title: '站点公告', content: '欢迎来到 AI 全民制作人站，欢迎大家投稿 AI 生成的网页 Demo！', demo_slug: null, created_by: 1, created_at: '2025-01-02T00:00:00Z' },
  { id: 2, type: 'auto', title: '新 Demo 发布', content: '植物大战僵尸（极简版）', demo_slug: 'pvz-demo', created_by: 2, created_at: '2025-03-01T10:00:00Z' },
  { id: 3, type: 'demo_update', title: 'Demo 更新：植物大战僵尸', content: '修复第二关音效不同步的问题', demo_slug: 'pvz-demo', created_by: 2, created_at: '2025-03-02T15:30:00Z' },
  { id: 4, type: 'update', title: '站点更新', content: 'feat: 整站公告系统上线', demo_slug: null, created_by: null, created_at: '2025-03-03T09:00:00Z' },
]

function svgCover(bg: string, text: string, sub: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="480" viewBox="0 0 640 480">
  <rect width="640" height="480" fill="${bg}"/>
  <rect x="14" y="14" width="612" height="452" fill="none" stroke="#000" stroke-width="8"/>
  <text x="320" y="220" font-family="Arial, sans-serif" font-size="96" font-weight="900" text-anchor="middle" fill="#000">${text}</text>
  <text x="320" y="290" font-family="monospace" font-size="22" font-weight="700" text-anchor="middle" fill="#000">${sub}</text>
  <rect x="20" y="430" width="120" height="20" fill="#000"/>
  <rect x="500" y="20" width="120" height="20" fill="#000"/>
</svg>`
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

const users: User[] = [
  { id: 1, username: 'admin', role: 'admin', status: 'active', bio: '站点管理员', display_name: '', avatar_url: '', history_enabled: true, created_at: '2025-01-01T00:00:00Z' },
  { id: 2, username: 'tester', role: 'user', status: 'active', bio: 'AI Demo 爱好者', display_name: '', avatar_url: '', history_enabled: true, created_at: '2025-02-11T08:00:00Z' },
  { id: 3, username: 'alice', role: 'user', status: 'active', bio: '收集各种网页小玩具', display_name: '', avatar_url: '', history_enabled: true, created_at: '2025-03-02T10:30:00Z' },
]

// ── 收藏夹 / 浏览历史（mock 侧内存状态；真实后端见 routers/collections.py） ──
type MockCollection = { id: number; owner: string; title: string; description: string; visibility: 'private' | 'public'; is_default: boolean; updated_at: string }
type MockCollectionItem = { collection_id: number; slug: string; added_at: string }
type MockHistory = { user: string; slug: string; viewed_at: string }

const collections: MockCollection[] = []
const collectionItems: MockCollectionItem[] = []
const viewHistory: MockHistory[] = []
let collectionSeq = 1

/** 默认夹「我的收藏」：第一次用到时自动创建（与后端语义一致：不可删、不可转公开） */
function defaultCollectionOf(username: string): MockCollection {
  let c = collections.find((x) => x.owner === username && x.is_default)
  if (!c) {
    c = {
      id: collectionSeq++,
      owner: username,
      title: '我的收藏',
      description: '',
      visibility: 'private',
      is_default: true,
      updated_at: new Date().toISOString(),
    }
    collections.push(c)
  }
  return c
}

function requireUser(): User {
  if (!currentUser) throw new Error('未登录')
  return currentUser
}

function toCollectionOut(c: MockCollection): CollectionOut {
  const items = collectionItems.filter((i) => i.collection_id === c.id).sort((a, b) => b.added_at.localeCompare(a.added_at))
  const covers: string[] = []
  for (const it of items) {
    const d = demos.find((x) => x.slug === it.slug)
    if (d && !covers.includes(d.cover_url) && covers.length < 3) covers.push(d.cover_url)
  }
  return {
    id: c.id,
    owner_username: c.owner,
    title: c.title,
    description: c.description,
    visibility: c.visibility,
    is_default: c.is_default,
    item_count: items.length,
    cover_urls: covers,
    updated_at: c.updated_at,
  }
}

const passwordOf: Record<string, string> = {
  admin: 'admin123',
  tester: 'password',
  alice: 'password',
}

/** users[] 行 → UserPublic（与后端 _user_public 同形：demo_count 按 mock demo 实时数） */
function toUserPublic(u: User): UserPublic {
  return {
    id: u.id,
    username: u.username,
    role: u.role,
    status: u.status,
    bio: u.bio || '',
    display_name: u.display_name || '',
    avatar_url: u.avatar_url || '',
    created_at: u.created_at,
    demo_count: demos.filter((d) => d.author === u.username).length,
  }
}

let currentUser: User | null = null

const tags: Tag[] = [
  { id: 1, key: 'model', value: 'dsv4', description: '模型版本总类', parent_id: null, demo_count: 6, child_count: 2 },
  { id: 2, key: 'model', value: 'dsv4-flash', description: 'DeepSeek V4 Flash —— 快速推理', parent_id: 1, demo_count: 3, child_count: 0 },
  { id: 3, key: 'model', value: 'dsv4-pro', description: 'DeepSeek V4 Pro —— 强推理', parent_id: 1, demo_count: 0, child_count: 0 },
  { id: 10, key: 'model', value: 'dsv4flash', description: '历史自由值：dsv4-flash 的旧写法', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 18, key: 'model', value: 'ds-unknown', description: '网传灰测版', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 4, key: 'plugin', value: 'routing-suite', description: '路由套件插件', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 11, key: 'plugin', value: 'suite', description: '历史自由值：路由套件的旧写法', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 5, key: 'skills', value: 'J-space', description: 'J-space 技能工作区', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 16, key: 'skills', value: 'j-space', description: '历史自由值：J-space 的旧写法', parent_id: null, demo_count: 1, child_count: 0 },
  { id: 6, key: 'preset', value: 'router-standard', description: '标准路由预设', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 12, key: 'preset', value: 'spec', description: '历史自由值：规格预设', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 7, key: 'type', value: 'effect', description: '视觉特效类', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 8, key: 'type', value: 'widget', description: '小组件类', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 9, key: 'type', value: 'game', description: '小游戏类', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 22, key: 'type', value: 'demo', description: '综合演示类', parent_id: null, demo_count: 8, child_count: 0 },
  { id: 23, key: 'category', value: '图形学', description: '图形学类', parent_id: null, demo_count: 4, child_count: 0 },
  { id: 25, key: 'category', value: '3D建模', description: '3D 建模类', parent_id: null, demo_count: 1, child_count: 0 },
  { id: 26, key: 'category', value: '仿真', description: '仿真类', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 27, key: 'category', value: '动画', description: '动画类', parent_id: null, demo_count: 1, child_count: 0 },
  { id: 13, key: 'author', value: 'tester', description: '系统作者标签', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 14, key: 'author', value: 'alice', description: '系统作者标签', parent_id: null, demo_count: 2, child_count: 0 },
  { id: 12, key: 'author', value: 'admin', description: '系统作者标签', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 19, key: 'author', value: 'DOUBAO', description: '系统作者标签', parent_id: null, demo_count: 3, child_count: 0 },
  { id: 21, key: 'author', value: 'gemini-3.7-flash', description: '系统作者标签', parent_id: null, demo_count: 8, child_count: 0 },
  { id: 15, key: 'author', value: 'sixtyseven', description: '系统作者标签', parent_id: null, demo_count: 6, child_count: 0 },
  { id: 24, key: 'author', value: 'yiheifeikong', description: '系统作者标签', parent_id: null, demo_count: 8, child_count: 0 },
]

const tagKeys: TagKeyInfo[] = [
  { key: 'model', mode: 'fixed', label: '模型', description: 'AI 模型版本（固定值）', sort: 1, values: [
    { value: 'dsv4', description: '模型版本总类', demo_count: 6 },
    { value: 'dsv4-flash', description: 'DeepSeek V4 Flash —— 快速推理', demo_count: 3 },
    { value: 'dsv4-pro', description: 'DeepSeek V4 Pro —— 强推理', demo_count: 0 },
    { value: 'dsv4flash', description: '历史自由值', demo_count: 3 },
    { value: 'ds-unknown', description: '网传灰测版', demo_count: 3 },
  ], demo_count: 6 },
  { key: 'plugin', mode: 'fixed', label: '插件', description: '使用的插件（固定值）', sort: 2, values: [
    { value: 'routing-suite', description: '路由套件插件', demo_count: 2 },
    { value: 'suite', description: '历史自由值', demo_count: 2 },
  ], demo_count: 2 },
  { key: 'type', mode: 'fixed', label: '类型', description: 'Demo 类型（固定值）', sort: 3, values: [
    { value: 'effect', description: '视觉特效类', demo_count: 2 },
    { value: 'widget', description: '小组件类', demo_count: 2 },
    { value: 'game', description: '小游戏类', demo_count: 2 },
    { value: 'demo', description: '综合演示类', demo_count: 8 },
  ], demo_count: 2 },
  { key: 'skills', mode: 'fixed', label: '技能', description: '技能工作区（固定值）', sort: 4, values: [
    { value: 'J-space', description: 'J-space 技能工作区', demo_count: 2 },
    { value: 'j-space', description: '历史自由值', demo_count: 1 },
  ], demo_count: 2 },
  { key: 'preset', mode: 'fixed', label: '预设', description: '预设配置（固定值）', sort: 5, values: [
    { value: 'router-standard', description: '标准路由预设', demo_count: 2 },
    { value: 'spec', description: '历史自由值', demo_count: 2 },
  ], demo_count: 2 },
  { key: 'category', mode: 'fixed', label: '分类', description: '作品分类（固定值）', sort: 6, values: [
    { value: '图形学', description: '图形学类', demo_count: 4 },
    { value: '3D建模', description: '3D 建模类', demo_count: 1 },
    { value: '仿真', description: '仿真类', demo_count: 3 },
    { value: '动画', description: '动画类', demo_count: 1 },
  ], demo_count: 4 },
  { key: 'game', mode: 'open', label: '游戏', description: '游戏名称（自定义值，如 mc / pvz）', sort: 7, values: [
    { value: 'pvz', description: '植物大战僵尸', demo_count: 2 },
    { value: 'mc', description: '我的世界', demo_count: 1 },
  ], demo_count: 2 },
  { key: 'rounds', mode: 'int', label: '轮数', description: '生成轮数（必须为整数）', sort: 8, values: [
    { value: '3', description: '', demo_count: 1 },
  ], demo_count: 1 },
]

let _tagAutoId = 1
for (const _k of tagKeys) for (const _v of _k.values) _v.id = _tagAutoId++

const demos: DemoDetail[] = [
  {
    slug: 'demo_粒子星空',
    title: '粒子星空',
    description: 'Canvas 粒子星空，鼠标移动产生引力扰动，适合作为背景特效。',
    // 与「音频可视化」共用同一句提示词：mock 模式下也能看到 v2「同提示词」模块
    prompt: '用 canvas 做一个全屏动态背景：粒子随鼠标移动产生引力扰动，颜色用高饱和撞色，代码必须单文件自包含。',
    cover_url: svgCover('#4ecdc4', '✦', 'particle starfield'),
    author: 'tester',
    author_id: 2,
    tags: [
      { key: 'model', value: 'dsv4-flash' },
      { key: 'skills', value: 'J-space' },
      { key: 'type', value: 'effect' },
      { key: 'author', value: 'tester' },
    ],
    view_count: 128,
    download_count: 12,
    comment_count: 3,
    created_at: '2025-03-01T09:00:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;overflow:hidden;background:#000}canvas{display:block}</style></head><body><canvas id="c"></canvas><script>
const cv=document.getElementById('c'),x=cv.getContext('2d');let W,H,P=[],M={x:-1e3,y:-1e3};
function rs(){W=cv.width=innerWidth;H=cv.height=innerHeight;P=Array.from({length:180},()=>({x:Math.random()*W,y:Math.random()*H,r:Math.random()*2+0.5,vx:(Math.random()-.5)*.4,vy:(Math.random()-.5)*.4}))}
addEventListener('resize',rs);addEventListener('mousemove',e=>{M.x=e.clientX;M.y=e.clientY});rs();
function tick(){x.fillStyle='rgba(0,0,0,.18)';x.fillRect(0,0,W,H);for(const p of P){p.x+=p.vx;p.y+=p.vy;const dx=p.x-M.x,dy=p.y-M.y,d=Math.hypot(dx,dy);if(d<160){p.x+=dx/d*1.6;p.y+=dy/d*1.6}if(p.x<0||p.x>W)p.vx*=-1;if(p.y<0||p.y>H)p.vy*=-1;x.fillStyle='#ffe66d';x.beginPath();x.arc(p.x,p.y,p.r,0,7);x.fill()}requestAnimationFrame(tick)}tick();
</script></body></html>`,
  },
  {
    slug: 'demo_霓虹时钟',
    title: '霓虹时钟',
    description: '霓虹数字时钟，七段数码管风格，深色底上发光。',
    cover_url: svgCover('#ff6b6b', '21:47', 'neon clock'),
    author: 'tester',
    author_id: 2,
    tags: [
      { key: 'model', value: 'dsv4-pro' },
      { key: 'preset', value: 'router-standard' },
      { key: 'type', value: 'widget' },
      { key: 'author', value: 'tester' },
    ],
    view_count: 96,
    download_count: 8,
    comment_count: 2,
    created_at: '2025-03-02T12:00:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#111;color:#ff6b6b;font-family:monospace;font-size:12vw;font-weight:900;letter-spacing:.08em;text-shadow:0 0 18px currentColor,0 0 42px currentColor}</style></head><body><div id="t">00:00:00</div><script>
function p(n){return String(n).padStart(2,'0')}function u(){const d=new Date();t.textContent=p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds())}setInterval(u,1000);u();
</script></body></html>`,
  },
  {
    slug: 'demo_贪吃蛇',
    title: '贪吃蛇',
    description: '经典贪吃蛇，键盘方向键控制，支持计分与重新开始。',
    cover_url: svgCover('#95e1d3', 'SNAKE', 'keyboard game'),
    author: 'alice',
    author_id: 3,
    tags: [
      { key: 'model', value: 'dsv4-flash' },
      { key: 'plugin', value: 'routing-suite' },
      { key: 'type', value: 'game' },
      { key: 'author', value: 'alice' },
    ],
    view_count: 210,
    download_count: 25,
    comment_count: 5,
    created_at: '2025-03-03T15:30:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;display:grid;place-items:center;height:100vh;background:#ffe66d;font-family:monospace}canvas{border:6px solid #000;background:#fff}</style></head><body><canvas id="c" width="400" height="400"></canvas><script>
const cv=c.getElementById('c'),x=cv.getContext('2d'),S=20,N=20;let snake=[{x:10,y:10}],dir={x:1,y:0},food={x:15,y:15},score=0,dead=false,t=0;
addEventListener('keydown',e=>{const k=e.key;if(k==='ArrowUp'&&dir.y!==1)dir={x:0,y:-1};if(k==='ArrowDown'&&dir.y!==-1)dir={x:0,y:1};if(k==='ArrowLeft'&&dir.x!==1)dir={x:-1,y:0};if(k==='ArrowRight'&&dir.x!==-1)dir={x:1,y:0};if(k==='r'){snake=[{x:10,y:10}];dir={x:1,y:0};food={x:15,y:15};score=0;dead=false}});
function loop(){if(dead)return;t++;if(t%7)return requestAnimationFrame(loop);const h={x:snake[0].x+dir.x,y:snake[0].y+dir.y};if(h.x<0||h.x>=N||h.y<0||h.y>=N||snake.some(s=>s.x===h.x&&s.y===h.y)){dead=true;x.fillStyle='#000';x.fillRect(0,0,400,400);x.fillStyle='#fff';x.font='bold 30px monospace';x.fillText('GAME OVER',80,190);return}snake.unshift(h);if(h.x===food.x&&h.y===food.y){score++;food={x:Math.random()*N|0,y:Math.random()*N|0}}else snake.pop();x.fillStyle='#fff';x.fillRect(0,0,400,400);x.fillStyle='#000';for(const s of snake)x.fillRect(s.x*S,s.y*S,S-2,S-2);x.fillStyle='#ff6b6b';x.fillRect(food.x*S,food.y*S,S,S);x.fillStyle='#000';x.font='bold 14px monospace';x.fillText('score '+score+' (R restart)',10,20);requestAnimationFrame(loop)}loop();
</script></body></html>`,
  },
  {
    slug: 'demo_打字机效果',
    title: '打字机排版',
    description: '打字机逐字输出排版，适合 Story 型页面。',
    cover_url: svgCover('#f38181', 'TYPE', 'typewriter'),
    author: 'tester',
    author_id: 2,
    tags: [
      { key: 'model', value: 'dsv4-pro' },
      { key: 'skills', value: 'J-space' },
      { key: 'type', value: 'widget' },
      { key: 'author', value: 'tester' },
    ],
    view_count: 75,
    download_count: 6,
    comment_count: 1,
    created_at: '2025-03-04T18:00:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#fff;font-family:monospace;padding:40px}pre{font-size:22px;line-height:1.8;border:6px solid #000;padding:32px;background:#ffe66d;box-shadow:8px 8px 0 #000;white-space:pre-wrap;max-width:720px;min-height:180px}</style></head><body><pre id="out"></pre><script>
const txt='你好，这里是 AI 生成的网页 Demo。\n每一行都由模型逐步写出。\n—— AI 全民制作人';let i=0;const out=document.getElementById('out');setInterval(()=>{if(i<=txt.length){out.textContent=txt.slice(0,i)+'▍';i++}else{i=0}},90);
</script></body></html>`,
  },
  {
    slug: 'demo_音频可视化',
    title: '音频可视化',
    description: 'Canvas 频谱条动画，模拟音频可视化的视觉效果。',
    prompt: '用 canvas 做一个全屏动态背景：粒子随鼠标移动产生引力扰动，颜色用高饱和撞色，代码必须单文件自包含。',
    cover_url: svgCover('#4ecdc4', 'WAVE', 'audio visualizer'),
    author: 'alice',
    author_id: 3,
    tags: [
      { key: 'model', value: 'dsv4-flash' },
      { key: 'preset', value: 'router-standard' },
      { key: 'type', value: 'effect' },
      { key: 'author', value: 'alice' },
    ],
    view_count: 143,
    download_count: 14,
    comment_count: 2,
    created_at: '2025-03-05T08:20:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;background:#000;display:grid;place-items:center;height:100vh}canvas{border:6px solid #fff}</style></head><body><canvas id="c" width="600" height="240"></canvas><script>
const cv=document.getElementById('c'),x=cv.getContext('2d');let bars=Array.from({length:48},()=>Math.random()*120);
function tick(){x.fillStyle='#000';x.fillRect(0,0,600,240);for(let i=0;i<bars.length;i++){bars[i]+=(Math.random()*200-100)*0.2;bars[i]=Math.max(10,Math.min(220,bars[i]));const w=8,g=4,xx=i*(w+g);x.fillStyle=['#ff6b6b','#ffe66d','#4ecdc4','#95e1d3'][i%4];x.fillRect(xx,240-bars[i],w,bars[i]);x.strokeStyle='#fff';x.lineWidth=2;x.strokeRect(xx-1,240-bars[i]-1,w+2,bars[i]+2)}requestAnimationFrame(tick)}tick();
</script></body></html>`,
  },
  {
    slug: 'demo_记忆翻牌',
    title: '记忆翻牌游戏',
    description: '记忆翻牌配对小游戏，点击翻牌，配对消除。',
    cover_url: svgCover('#ffe66d', 'MEMO', 'match game'),
    author: 'admin',
    author_id: 1,
    tags: [
      { key: 'model', value: 'dsv4-pro' },
      { key: 'plugin', value: 'routing-suite' },
      { key: 'type', value: 'game' },
      { key: 'author', value: 'admin' },
    ],
    view_count: 187,
    download_count: 19,
    comment_count: 4,
    created_at: '2025-03-06T11:00:00Z',
    status: 'approved',
    session_log_count: 1,
    is_author: false,
    previewHtml: `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#4ecdc4;font-family:monospace}.grid{display:grid;grid-template-columns:repeat(4,80px);gap:12px}.cell{width:80px;height:80px;border:5px solid #000;background:#fff;font-size:36px;display:grid;place-items:center;cursor:pointer;user-select:none}.cell.open{background:#ffe66d}.cell.done{background:#95e1d3;cursor:default}</style></head><body><div class="grid" id="g"></div><script>
const emojis=['A','B','C','D','E','F','G','H'];let cards=[...emojis,...emojis].sort(()=>Math.random()-.5),open=[],done=new Set();const g=document.getElementById('g');
cards.forEach((v,i)=>{const d=document.createElement('div');d.className='cell';d.dataset.i=i;d.textContent='?';d.onclick=()=>{if(open.includes(i)||done.has(i))return;d.textContent=v;d.classList.add('open');open.push(i);if(open.length===2){const [a,b]=open;if(cards[a]===cards[b]){done.add(a);done.add(b);g.children[a].classList.add('done');g.children[b].classList.add('done')}else{setTimeout(()=>{g.children[a].textContent='?';g.children[a].classList.remove('open');g.children[b].textContent='?';g.children[b].classList.remove('open')},500)}open=[]}};g.appendChild(d)});
</script></body></html>`,
  },
]


const sessionLogs: Record<string, SessionLog[]> = {
  demo_粒子星空: [{ id: 1, filename: '生成会话.md', file_size: 1840, created_at: '2025-03-01T09:00:00Z' }],
  demo_霓虹时钟: [{ id: 2, filename: '生成会话.md', file_size: 1210, created_at: '2025-03-02T12:00:00Z' }],
  demo_贪吃蛇: [{ id: 3, filename: '生成会话.md', file_size: 2360, created_at: '2025-03-03T15:30:00Z' }],
  demo_打字机效果: [{ id: 4, filename: '生成会话.md', file_size: 980, created_at: '2025-03-04T18:00:00Z' }],
  demo_音频可视化: [{ id: 5, filename: '生成会话.md', file_size: 1530, created_at: '2025-03-05T08:20:00Z' }],
  demo_记忆翻牌: [{ id: 6, filename: '生成会话.md', file_size: 2070, created_at: '2025-03-06T11:00:00Z' }],
}

const sessionTexts: Record<string, string> = {
  'demo_粒子星星/生成会话.md': `# 生成会话：粒子星空

## 模型
- model:dsv4-flash
- plugin:routing-suite

## Prompt 摘要
“做一个 canvas 粒子星空背景，鼠标移动产生引力扰动。”

## 生成步骤
1. 初始化 canvas 与粒子数组
2. 实现粒子运动与边界反弹
3. 加入鼠标扰动
4. 添加背景淡出拖尾效果
5. 适配窗口 resize

## 迭代记录
- commit 1: 骨架与粒子运动
- commit 2: 鼠标扰动
- commit 3: 拖尾与视觉打磨`,
  'demo_霓虹时钟/生成会话.md': `# 生成会话：霓虹时钟

## 模型
- model:dsv4-pro
- preset:router-standard

## Prompt 摘要
“做一个霓虹数字时钟，七段数码管发光风格。”

## 生成步骤
1. 布局与字体
2. 时间逻辑
3. 霓虹光晕样式

## 迭代记录
- commit 1: 基础时钟
- commit 2: 霓虹样式
- commit 3: 响应式字号`,
  'demo_贪吃蛇/生成会话.md': `# 生成会话：贪吃蛇

## 模型
- model:dsv4-flash
- plugin:routing-suite

## Prompt 摘要
“经典贪吃蛇，方向键控制，计分与重开。”

## 生成步骤
1. 蛇与食物数据结构
2. 移动与碰撞
3. 计分与重开
4. 视觉与手感打磨

## 迭代记录
- commit 1: 蛇身移动
- commit 2: 食物与计分
- commit 3: 碰撞检测
- commit 4: 视觉打磨`,
  'demo_打字机效果/生成会话.md': `# 生成会话：打字机排版

## 模型
- model:dsv4-pro
- skills:J-space

## Prompt 摘要
“打字机逐字输出效果，适合 Story 页面。”

## 生成步骤
1. 文本与光标
2. 定时输出
3. 循环播放

## 迭代记录
- commit 1: 打字机逻辑
- commit 2: 视觉与循环`,
  'demo_音频可视化/生成会话.md': `# 生成会话：音频可视化

## 模型
- model:dsv4-flash
- preset:router-standard

## Prompt 摘要
“canvas 频谱条动画，模拟音频可视化。”

## 生成步骤
1. 频谱条布局
2. 动画随机波动
3. 配色与描边

## 迭代记录
- commit 1: 频谱条绘制
- commit 2: 波动动画
- commit 3: 配色打磨`,
  'demo_记忆翻牌/生成会话.md': `# 生成会话：记忆翻牌

## 模型
- model:dsv4-pro
- plugin:routing-suite

## Prompt 摘要
“记忆翻牌配对游戏，点击翻牌，配对消除。”

## 生成步骤
1. 卡片布局
2. 翻牌逻辑
3. 配对判定
4. 完成状态与视觉

## 迭代记录
- commit 1: 卡片生成
- commit 2: 翻牌与配对
- commit 3: 延迟回翻
- commit 4: 完成态视觉`,
}


const pendingDemos: DemoDetail[] = [
  {
    slug: 'demo_待审示例',
    title: '待审示例',
    description: '这是一个等待管理员审核的示例 Demo。',
    cover_url: svgCover('#ff6b6b', 'PEND', 'pending review'),
    author: 'tester',
    author_id: 2,
    tags: [
      { key: 'model', value: 'dsv4-flash' },
      { key: 'type', value: 'widget' },
      { key: 'author', value: 'tester' },
    ],
    view_count: 0,
    download_count: 0,
    comment_count: 0,
    created_at: '2025-03-07T08:00:00Z',
    status: 'pending',
    session_log_count: 0,
    is_author: false,
    previewHtml: '<!doctype html><html><body style="margin:0;font-family:monospace;display:grid;place-items:center;height:100vh;background:#ffe66d"><h1>待审核</h1></body></html>',
  },
]

let settings: Settings = { auto_approve: true, auto_approve_public: false }

// v2 B3′：mock 题目池（adminCreateTask 会写入，列表页与治理面板立刻可见）
const mockTasks: TaskSummary[] = [
  { id: 1, slug: 'mc-web', title: '我的世界网页版', description: '用网页技术复刻我的世界核心玩法', category: '游戏', status: 'active', demo_count: 3, created_at: '2026-08-05T00:00:00Z' },
]
let mockTaskSeq = 2
// 独立出题的 mock 待审提案（与真接口同语义：同题名 pending 幂等，不直建题目）
const mockTaskProposals: { id: number; title: string; description: string; category: string | null; status: 'pending' }[] = []
// 身份绑定闭环 A：createDemo 成功后才带 demo_id suggestTagValue，需要一个稳定 demo id
let mockDemoIdSeq = 1

// v2 B4′：mock 收件箱（带 task 的上传会真的入队，能完整演一遍批准流程）
const mockSuggestions: SuggestionItem[] = []
let mockSuggestionSeq = 1
const mockPromptStore = createMockPromptStore({
  demos: () => demos,
  userId: () => currentUser?.id ?? null,
  suggestions: mockSuggestions,
  nextSuggestionId: () => mockSuggestionSeq++,
})

/** 收件箱列表口对超长 new_prompt 正文做摘要（与后端契约一致：>480 字截断 + content_truncated）。
 *  只截克隆返回值，存储对象不动 —— approve 仍用全量 payload 执行，语义不变。 */
function excerptSuggestion(s: SuggestionItem): SuggestionItem {
  if (s.kind !== 'new_prompt' || typeof s.payload.content !== 'string') return s
  const content = s.payload.content
  if (content.length <= 480) return s
  return { ...s, payload: { ...s.payload, content: content.slice(0, 480), content_truncated: true } }
}
// C：TagValueSuggestion 与 EntitySuggestion 分表；概览台并语义合成
const mockTagSuggestions: TagSuggestion[] = [
  { id: 1, key: 'plugin', value: 'routing-lite', description: '用户申请的固定值', group: null, status: 'pending', demo_id: null, created_at: '2026-08-20T10:00:00Z' },
]
// M3-B3 挂摘演示态：task slug → 已挂 demo 行（mock 无 DemoTask 表，内存映射代偿）
const taskAttached: Record<string, { id: number; slug: string; title: string; status: string }[]> = {}
let attachSeq = 9000
// M2-t4 演示种子：kind 分节/批量消化的可见样例（mock 占位数据性质；id 9001+ 不与上传入队冲突）
void (() => {
  const seed = (id: number, kind: SuggestionItem['kind'], payload: Record<string, unknown>, confidence: number, status: SuggestionItem['status'] = 'pending', created_at = '2026-08-20T10:00:00Z') =>
    mockSuggestions.push({ id, kind, payload, confidence, source: 'ai', status, created_at })
  seed(9001, 'retag_demo', { demo_title: '重力迷宫', demo_slug: 'demo_重力迷宫', remove: 'demo', add: 'puzzle', matched: ['puzzle', 'maze', 'grid'] }, 0.72)
  seed(9002, 'retag_demo', { demo_title: '粒子星空', demo_slug: 'demo_粒子星空', remove: 'demo', add: 'visual', matched: ['canvas', 'shader'] }, 0.68)
  seed(9003, 'retag_demo', { demo_title: '霓虹打字机', demo_slug: 'demo_霓虹打字机', remove: 'demo', add: 'effect', matched: ['text-fx'] }, 0.66)
  seed(9004, 'task_match', { demo_title: '粒子星空', demo_slug: 'demo_粒子星空', task_title: '我的世界网页版', task_id: 1 }, 0.81)
  seed(9005, 'alias', { name: 'dsv4flash', alias: 'DSV4 Flash（旧写法）', model_id: 'dsv4flash' }, 0.77)
  seed(9006, 'new_model', { name: 'kimi-k3.5', model_id: 'kimi-k3.5' }, 0.55, 'rejected')
})()

// 合并向导 / 别名中心的可变数据源（提到模块级，避免每处各抄一份假数据）
const mockModels: ModelSummary[] = [
  { id: 1, slug: 'dsv4-flash', name: 'dsv4-flash', vendor: 'DeepSeek', status: 'active', resolution: 'exact', description: 'DeepSeek V4 Flash —— 快速推理', demo_count: 8, rating_avg: 4.4, score: 4.31, votes: 128, sample_level: 'high', created_at: '2026-08-01T00:00:00Z' },
  { id: 2, slug: 'dsv4-pro', name: 'dsv4-pro', vendor: 'DeepSeek', status: 'active', resolution: 'exact', description: 'DeepSeek V4 Pro —— 强推理', demo_count: 6, rating_avg: 4.7, score: 4.62, votes: 74, sample_level: 'high', created_at: '2026-08-01T00:00:00Z' },
  { id: 3, slug: 'ds-unknown', name: 'ds-unknown', vendor: null, status: 'unverified', resolution: 'guess', description: '灰测模型', demo_count: 12, rating_avg: 4.1, score: 3.98, votes: 9, sample_level: 'low', created_at: '2026-08-02T00:00:00Z' },
  { id: 4, slug: 'dsv4flash', name: 'dsv4flash', vendor: 'DeepSeek', status: 'active', resolution: 'exact', description: '同一型号的另一种写法（用来演示合并向导）', demo_count: 2, rating_avg: 5.0, score: 4.02, votes: 1, sample_level: 'low', created_at: '2026-08-02T00:00:00Z' },
]
const mockAliases: Record<string, string[]> = { 'dsv4-flash': ['dsv4 flash'] }

// 撤销合并的可变状态：dsv4flash 已被合进 dsv4-flash（与真实库里那次的形状一致）
let mockMergeHistory: MergeHistoryItem[] = [
  {
    source: { id: 4, slug: 'dsv4flash', name: 'dsv4flash' },
    target: { id: 1, slug: 'dsv4-flash', name: 'dsv4-flash' },
    moved_total: 2,
    movable_back: 2,
    reliable: true,
    reason: '合并入 dsv4-flash（id=1），迁移 2 个作品引用',
    restored_status: 'active',
  },
]

// Q2 第三步：mock 归属工作台（懒构造：要用到 demos，避开模块初始化顺序问题）
const mockAttrTargets = [
  { id: 1, slug: 'dsv4-flash', name: 'dsv4-flash', vendor: 'DeepSeek' },
  { id: 2, slug: 'dsv4-pro', name: 'dsv4-pro', vendor: 'DeepSeek' },
  { id: 4, slug: 'hy4', name: 'HY4', vendor: 'Tencent' },
]
let mockAttribution: AttributionGroup[] = []

function ensureAttribution(): AttributionGroup[] {
  if (mockAttribution.length) return mockAttribution
  const pool = demos.filter((d) => d.status === 'approved').slice(0, 5)
  const modelOf = (slug: string, name: string, resolution: string, vendor: string | null, n: number): ModelSummary => ({
    id: 900 + mockAttribution.length + n,
    slug,
    name,
    vendor,
    status: 'active',
    resolution,
    description: '',
    demo_count: n,
    rating_avg: null,
    created_at: '2026-08-05T00:00:00Z',
  })
  const itemsOf = (list: typeof pool, guessSlug?: string, offset = 0): AttributionItem[] =>
    list.map((d, i) => ({
      id: 7000 + offset + i, // mock 的 DemoDetail 不带 id，工作台只用它做选中键
      slug: d.slug,
      title: d.title,
      model_hint: i === 0 ? '作者备注：看着像 dsv4-flash 出的' : '',
      rating_avg: d.rating_avg ?? 0,
      rating_count: d.rating_count ?? 0,
      guess: guessSlug ? mockAttrTargets.find((t) => t.slug === guessSlug) ?? null : null,
    }))
  mockAttribution = [
    { model: modelOf('unspecified', 'unspecified', 'unknown', null, 3), demos: itemsOf(pool.slice(0, 3), 'dsv4-flash', 0) },
    { model: modelOf('deepseek-unknown', 'deepseek-unknown', 'family', 'DeepSeek', 2), demos: itemsOf(pool.slice(3, 5), undefined, 10) },
    // M2-t4 灰测池样例（guess 档）：概览台池卡有实数可演（件数取 demo_count=12）
    { model: modelOf('ds-unknown', 'ds-unknown', 'guess', null, 12), demos: itemsOf(pool.slice(0, 2), 'dsv4-flash', 20) },
  ]
  return mockAttribution
}
// astra 橱窗策展（mock 态记忆）：slug -> {sites, lang}
const curationMap = new Map<string, { sites: string[]; lang: 'zh' | 'en' }>()
// 首页策展池（07 §2.2 mock 态记忆）：池序 entries（真实后端以 demo 主键为操作键，mock 用合成 id 模拟）
let featuredNextId = 5000
// RF-5a：原先这里是空数组 —— 于是 mock 模式下首页策展池恒为空（只能看「池空回落随机」），
// 后台「精选管理」面板也永远是空的，整条策展流离线无法演示。
// 补三条与 demo 夹具同 slug 的初始池（顺序即 hero→尾部）。
const featuredEntries: { id: number; slug: string }[] = [
  { id: 1, slug: 'demo_粒子星空' },
  { id: 2, slug: 'demo_霓虹时钟' },
  { id: 3, slug: 'demo_贪吃蛇' },
]

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

function findDemo(slug: string): DemoDetail | undefined {
  return [...demos, ...pendingDemos].find((d) => d.slug === slug)
}

function tagOf(t: { key: string; value: string }) {
  return `${t.key}:${t.value}`
}

function toTagRef(t: string | { key: string; value: string; description?: string }) {
  if (typeof t === 'string') {
    const [key, ...rest] = t.split(':')
    return { key, value: rest.join(':') }
  }
  return { key: t.key, value: t.value }
}

function mockRatingKey(slug: string, deviceId: string) {
  return `ds_mock_rating_${slug}_${deviceId}`
}
function readMyScore(slug: string, deviceId: string): number | null {
  try {
    const v = localStorage.getItem(mockRatingKey(slug, deviceId))
    return v ? Number(v) : null
  } catch {
    return null
  }
}
function ratingStore(d: DemoDetail) {
  return {
    avg: d.rating_avg || 4.0,
    count: d.rating_count ?? 12,
    god: d.rating_god || 6,
    ghost: d.rating_ghost || 1,
  }
}
function ratingDistribution(d: DemoDetail) {
  return [
    { score: 1, count: d.rating_ghost || 1 },
    { score: 2, count: 2 },
    { score: 3, count: 3 },
    { score: 4, count: 2 },
    { score: 5, count: d.rating_god || 6 },
  ]
}


function demosApproved() {
  return clone(demos.filter((d) => d.status === 'approved')) as DemoSummary[]
}

export const mockApi = {
  async listPrompts(params: PromptListParams = {}): Promise<Paginated<PromptListItem>> {
    await delay(120)
    return clone(mockPromptStore.list(params))
  },
  async getPrompt(id: number): Promise<PromptSummary> {
    await delay(100)
    return clone(mockPromptStore.get(id))
  },
  async getPromptDemos(id: number, params: { page?: number; page_size?: number } = {}): Promise<Paginated<DemoSummary>> {
    await delay(120)
    return clone(mockPromptStore.demos(id, params))
  },
  async proposePrompt(payload: PromptProposalIn): Promise<PromptProposalOut> {
    await delay(150)
    return clone(mockPromptStore.propose(payload))
  },
  // ---------- 认证 ----------
  async login(username: string, password: string): Promise<AuthResponse> {
    await delay(300)
    const user = users.find((u) => u.username === username)
    if (!user || passwordOf[username] !== password) {
      throw new Error('用户名或密码错误')
    }
    if (user.status !== 'active') {
      throw new Error('账号不可用')
    }
    currentUser = clone(user)
    return { access_token: 'mock-token', user: clone(user) }
  },

  async register(username: string, password: string): Promise<AuthResponse> {
    await delay(300)
    if (!/^[a-zA-Z0-9_]{3,32}$/.test(username)) {
      throw new Error('用户名需为 3-32 位字母数字下划线')
    }
    if (password.length < 8) {
      throw new Error('密码至少 8 位')
    }
    if (users.some((u) => u.username === username)) {
      throw new Error('用户名已存在')
    }
    const user: User = { id: users.length + 1, username, role: 'user', status: 'active', bio: '', display_name: '', avatar_url: '', history_enabled: true, created_at: new Date().toISOString() }
    users.push(user)
    passwordOf[username] = password
    currentUser = clone(user)
    return { access_token: 'mock-token', user: clone(user) }
  },

  async logout(): Promise<void> {
    await delay(100)
    currentUser = null
  },

  async me(): Promise<User> {
    await delay(100)
    if (!currentUser) throw new Error('未登录')
    return clone(currentUser)
  },

  async getUser(username: string): Promise<User & { demo_count: number }> {
    await delay()
    const u = users.find((x) => x.username === username)
    if (!u) throw new Error('用户不存在')
    return { ...clone(u), demo_count: demos.filter((d) => d.author === username).length }
  },

  async changePassword(old_password: string, new_password: string): Promise<void> {
    await delay(200)
    if (!currentUser) throw new Error('未登录')
    if (passwordOf[currentUser.username] !== old_password) throw new Error('原密码错误')
    passwordOf[currentUser.username] = new_password
  },

  // ---------- 标签 ----------
  async getTag(key: string, value: string): Promise<Tag> {
    await delay()
    const t = tags.find((x) => x.key === key && x.value === value)
    if (!t) throw new Error('标签不存在')
    const result = clone(t)
    result.children = tags.filter((x) => x.parent_id === t.id).map((x) => clone(x))
    result.parent = t.parent_id ? tags.find((x) => x.id === t.parent_id) ?? null : null
    return result
  },

  async createTag(key: string, value: string, description?: string, parent_id?: number | null, _group?: string): Promise<Tag> {
    await delay(200)
    if (key === 'author') throw new Error('author 为保留 key')
    if (tags.some((t) => t.key === key && t.value === value)) throw new Error('标签已存在')
    const tag: Tag = {
      id: Math.max(...tags.map((t) => t.id)) + 1,
      key,
      value,
      description: description || '',
      parent_id: parent_id ?? null,
      demo_count: 0,
      child_count: 0,
    }
    tags.push(tag)
    return clone(tag)
  },
  async suggestTagValue(payload: { key: string; value: string; description?: string; group?: string; demo_id?: number | null }): Promise<TagSuggestion> {
    await delay(200)
    const s: TagSuggestion = {
      id: Date.now(),
      key: payload.key,
      value: payload.value,
      description: payload.description || '',
      group: payload.group || null,
      status: 'pending',
      demo_id: payload.demo_id ?? null,
      created_at: new Date().toISOString(),
    }
    // RF-5a：真正入队 —— 原实现只造了个对象就返回，于是「上传时申请新固定值」在 mock 模式下
    // 管理端永远看不到（真实后端是落 pending 队列，管理员在「固定值申请」里审）
    mockTagSuggestions.unshift(s)
    return clone(s)
  },
  async listTagSuggestions(status?: 'pending' | 'approved' | 'rejected'): Promise<TagSuggestion[]> {
    await delay()
    const all: TagSuggestion[] = mockTagSuggestions
    return clone(status ? all.filter((s) => s.status === status) : all)
  },
  async renameTagGroup(key: string, group: string, newGroup: string): Promise<{ updated: number; new_group: string }> {
    await delay()
    const k = tagKeys.find((x) => x.key === key)
    let updated = 0
    if (k) for (const v of k.values) if (v.group === group) { v.group = newGroup; updated++ }
    return { updated, new_group: newGroup }
  },
  async clearTagGroup(key: string, group: string): Promise<{ cleared: number }> {
    await delay()
    const k = tagKeys.find((x) => x.key === key)
    let cleared = 0
    if (k) for (const v of k.values) if (v.group === group) { v.group = null; cleared++ }
    return { cleared }
  },
  async setTagGroup(tagId: number, group: string | null): Promise<TagKeyValue> {
    await delay()
    const v = tagKeys.flatMap((k) => k.values).find((x) => x.id === tagId)
    if (!v) throw new Error('标签不存在')
    v.group = group
    return clone(v)
  },
  async mergeTags(payload: TagMergeInput): Promise<TagMergeResult> {
    await delay(250)
    const from = payload.from_value
    const to = payload.to_value
    const affected = from.length + to.length
    return { merged: affected, removed_dups: 2, affected_demos: Math.max(1, Math.floor(affected / 2)), deleted_source: true, dry_run: payload.dry_run }
  },

  async reviewTagSuggestion(id: number, action: 'approve' | 'reject', group?: string): Promise<TagSuggestion> {
    await delay(200)
    const s = mockTagSuggestions.find((x) => x.id === id)
    if (!s) throw new Error('建议不存在')
    s.status = action === 'approve' ? 'approved' : 'rejected'
    if (group) s.group = group
    if (action === 'approve') {
      const k = tagKeys.find((x) => x.key === s.key)
      if (k && !k.values.some((v) => v.value === s.value)) {
        k.values.push({ id: 8000 + s.id, value: s.value, description: s.description, demo_count: 0, group: s.group, status: 'active' })
      }
    }
    return clone(s)
  },
  async fetchModels(): Promise<{ created: number; note: string }> {
    await delay(300)
    return { created: 0, note: 'mock' }
  },
  async aiSuggest(_payload: { demo_id?: number; text?: string }): Promise<{ suggestions: { key: string; value: string; reason: string }[]; note: string }> {
    await delay(300)
    return { suggestions: [], note: 'mock' }
  },

  async listTagKeys(): Promise<TagKeyInfo[]> {
    await delay()
    // M3-2/M3-3 实体总表/详情按 value.id 定位（真实 TagKeyOut 自带 id）——mock 出口补确定性 id（1000+ 按序，会话内稳定）
    const out = clone(tagKeys)
    let n = 1000
    for (const k of out) for (const v of k.values) if (v.id == null) v.id = n++
    return out
  },
  // T3·M5-B2：管理端词表全量（mock 与公开词表同数据，逐值补 status 默认 active）
  async adminListTagKeys(): Promise<TagKeyInfo[]> {
    const out = await this.listTagKeys()
    for (const k of out) for (const v of k.values) if (!v.status) v.status = 'active'
    return out
  },
  async createTagKey(payload: { key: string; mode: 'fixed' | 'open' | 'int'; label: string; description?: string; sort?: number }): Promise<TagKeyInfo> {
    await delay(200)
    if (tagKeys.some((k) => k.key === payload.key)) throw new Error('标签键已存在')
    const info: TagKeyInfo = {
      key: payload.key,
      mode: payload.mode,
      label: payload.label,
      description: payload.description || '',
      sort: payload.sort ?? 0,
      values: [],
      demo_count: 0,
    }
    tagKeys.push(info)
    return clone(info)
  },
  async updateTagKey(key: string, payload: { mode: 'fixed' | 'open' | 'int'; label: string; description?: string; sort?: number }): Promise<TagKeyInfo> {
    await delay(200)
    const k = tagKeys.find((x) => x.key === key)
    if (!k) throw new Error('标签键不存在')
    k.mode = payload.mode
    k.label = payload.label
    if (payload.description !== undefined) k.description = payload.description
    if (payload.sort !== undefined) k.sort = payload.sort
    return clone(k)
  },
  async deleteTagKey(key: string): Promise<void> {
    await delay(200)
    const idx = tagKeys.findIndex((x) => x.key === key)
    if (idx < 0) throw new Error('标签键不存在')
    if (key === 'author' || key === 'version-of') throw new Error('保留 key 禁止删除')
    if (tagKeys[idx].demo_count > 0) throw new Error('该键下有标签正被 demo 引用，禁止删除')
    tagKeys.splice(idx, 1)
    const toRemove = tags.filter((t) => t.key === key)
    for (const t of toRemove) {
      const ti = tags.indexOf(t)
      if (ti >= 0) tags.splice(ti, 1)
    }
  },
  async deleteTagValue(key: string, value: string): Promise<void> {
    await delay(200)
    if (key === 'author' || key === 'version-of') throw new Error('保留 key 禁止删除')
    const t = tags.find((x) => x.key === key && x.value === value)
    if (!t) throw new Error('标签值不存在')
    if (t.demo_count > 0) throw new Error('该标签正被 demo 引用，禁止删除')
    const idx = tags.indexOf(t)
    tags.splice(idx, 1)
  },

  // ---------- Demo ----------
  async listDemos(params: DemoListParams = {}): Promise<Paginated<DemoSummary>> {
    await delay()
    // RF-5a：补上真接口支持的 author/model/task 三个筛选（原先静默丢弃 → mock 模式给假结论），
    // 并把 featured 从「提前 return 绕过其余筛选」改为「在既有查询上收窄」
    // （后端 demos.py:438 的语义：只出 featured 行，status/q/可见域照常生效）
    const {
      tags: tagFilters = [],
      q = '',
      sort = 'newest',
      page = 1,
      page_size = 20,
      status = 'approved',
      featured,
      author,
      model,
      task,
    } = params
    let items = [...demos, ...(status === 'pending' ? pendingDemos : [])].filter((d) => !status || d.status === status)
    if (author) {
      // 与后端一致：author=public 是「所有未注册上传」的虚拟身份
      items = author === 'public' ? items.filter((d) => d.author === 'public') : items.filter((d) => d.author === author)
    }
    if (model) {
      items = items.filter((d) => d.tags.some((x) => tagOf(x) === `model:${model}`))
    }
    if (task) {
      // mock 的作品夹具没有题目关联，这里如实返回空（而不是忽略参数给假结果）
      items = items.filter((d) => (d.tasks || []).some((x) => x.slug === task))
    }
    if (tagFilters.length) {
      items = items.filter((d) => tagFilters.every((tf) => d.tags.some((t) => tagOf(t) === tf)))
    }
    if (q) {
      const lower = q.toLowerCase()
      items = items.filter(
        (d) =>
          d.title.toLowerCase().includes(lower) ||
          d.description.toLowerCase().includes(lower) ||
          d.tags.some((t) => tagOf(t).toLowerCase().includes(lower)),
      )
    }
    if (featured === 1) {
      // 首页策展池：只出池内行（在既有筛选结果上收窄），并按池序（hero=首件）
      const idx = new Map(featuredEntries.map((e, i) => [e.slug, i]))
      items = items.filter((d) => idx.has(d.slug)).sort((a, b) => idx.get(a.slug)! - idx.get(b.slug)!)
      const total = items.length
      const start = (page - 1) * page_size
      return { items: items.slice(start, start + page_size), total, page, page_size }
    }
    // 稳定排序：主键 + 次级键（同时间/同热度时按 slug 兜底），保证刷新后顺序可复现
    const bySlug = (a: DemoDetail, b: DemoDetail) => a.slug.localeCompare(b.slug)
    if (sort === 'random') {
      // 随机推荐（首页精选「换一批」，Fisher-Yates 洗牌）
      items = [...items]
      for (let i = items.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1))
        ;[items[i], items[j]] = [items[j], items[i]]
      }
    } else if (sort === 'prompt') {
      // 提示词模式：填了提示词的 demo 优先，同组按最新
      items = [...items].sort(
        (a, b) =>
          Number(Boolean(b.prompt)) - Number(Boolean(a.prompt)) ||
          b.created_at.localeCompare(a.created_at) ||
          bySlug(a, b),
      )
    } else if (sort === 'popular') {
      items = [...items].sort(
        (a, b) => b.view_count - a.view_count || b.created_at.localeCompare(a.created_at) || bySlug(a, b),
      )
    } else {
      items = [...items].sort((a, b) => b.created_at.localeCompare(a.created_at) || bySlug(a, b))
    }
    const total = items.length
    const start = (page - 1) * page_size
    const pageItems = items.slice(start, start + page_size)
    return { items: pageItems, total, page, page_size }
  },

  /** KB-29：卡片专用轻量口（真接口不增 view_count，mock 同口径） */
  async demoMeta(slug: string): Promise<DemoMeta> {
    await delay(60)
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    return { slug: d.slug, title: d.title, cover_url: d.cover_url, author: d.author ?? 'public' }
  },

  async demosByTasks(taskIds: number[]): Promise<DemoSummary[]> {
    await delay(80)
    if (!taskIds.length) return []
    const ids = new Set(taskIds)
    // 与真接口同口径：不限分页、按题目取整组（mock 夹具里只有极少数作品带题）
    return demos.filter((d) => (d.tasks ?? []).some((tk) => ids.has(tk.id))).map((d) => clone(d))
  },

  async getDemo(slug: string): Promise<DemoDetail> {
    await delay()
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    d.view_count += 1
    const out = clone(d)
    // 夹具不逐条写 demo_type（与 site_info 里 demos_by_type 的 `d.demo_type || 'web'` 同口径）：
    // detail 这处漏了缺省 → demo_type 为 undefined → 详情页落到「外部链接」分支，
    // 带 previewHtml 的夹具在 mock 下永远看不到预览（键盘焦点提示也就无从演）。
    out.demo_type = out.demo_type || 'web'
    out.timeline = [
      {
        id: 1,
        version_label: 'v2',
        message: '优化性能与视觉细节',
        old_slug: `${slug}-v1`,
        created_at: '2025-03-02T15:30:00Z',
      },
      {
        id: 2,
        version_label: 'v1',
        message: '创建',
        old_slug: null,
        created_at: '2025-03-01T09:00:00Z',
      },
    ]
    return out
  },

  async getRelated(slug: string): Promise<DemoSummary[]> {
    await delay(200)
    const cur = findDemo(slug)
    if (!cur) throw new Error('Demo 不存在')
    // 按标签重合粗略推荐（mock 简单实现）
    const curKeys = new Set(cur.tags.filter((t) => t.key !== 'author').map((t) => t.key + ':' + t.value))
    const others = [...demos.filter((d) => d.slug !== slug)]
      .map((d) => {
        let score = d.tags.filter((t) => curKeys.has(t.key + ':' + t.value)).length
        score += d.view_count * 0.001
        score += Math.random() * 0.5
        return { score, d }
      })
      .sort((a, b) => b.score - a.score)
      .slice(0, 30)
      .map((x) => x.d)
    return clone(others)
  },
  async getSamePrompt(slug: string, limit = 12): Promise<SamePromptResult> {
    await delay(150)
    const cur = findDemo(slug)
    if (!cur) throw new Error('Demo 不存在')
    const prompt = (cur.prompt || '').trim()
    if (!prompt) return { prompt: '', prompt_id: null, items: [] }
    // mock 按提示词文本精确分组（真实接口按 prompt_id，语义一致）
    const items = demos.filter((d) => d.slug !== slug && d.status === 'approved' && (d.prompt || '').trim() === prompt)
    return { prompt, prompt_id: 1, items: clone(items.slice(0, limit)) }
  },
  async getRating(slug: string, deviceId?: string): Promise<RatingStats> {
    await delay(100)
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    const st = ratingStore(d)
    const my = deviceId ? readMyScore(slug, deviceId) : null
    return { my_score: my, avg: st.avg, count: st.count, god: st.god, ghost: st.ghost, distribution: ratingDistribution(d) }
  },
  async rateDemo(slug: string, score: number, deviceId?: string): Promise<RatingStats> {
    await delay(150)
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    if (deviceId) {
      const prev = readMyScore(slug, deviceId)
      localStorage.setItem(mockRatingKey(slug, deviceId), String(score))
      if (prev) {
        // 改分：人数不变，神/鬼票回退旧值再累计新值
        if (prev === 5) d.rating_god = Math.max(0, (d.rating_god || 0) - 1)
        if (prev === 1) d.rating_ghost = Math.max(0, (d.rating_ghost || 0) - 1)
      } else {
        d.rating_count = (d.rating_count ?? 12) + 1
      }
      d.rating_avg = score // mock 简化：不精确维护历史均值
      if (score === 5) d.rating_god = (d.rating_god || 0) + 1
      if (score === 1) d.rating_ghost = (d.rating_ghost || 0) + 1
    }
    return this.getRating(slug, deviceId)
  },
  async unrateDemo(slug: string, deviceId?: string): Promise<RatingStats> {
    await delay(100)
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    if (deviceId) {
      const prev = readMyScore(slug, deviceId)
      localStorage.removeItem(mockRatingKey(slug, deviceId))
      if (prev) {
        d.rating_count = Math.max(0, (d.rating_count ?? 12) - 1)
        if (prev === 5) d.rating_god = Math.max(0, (d.rating_god || 0) - 1)
        if (prev === 1) d.rating_ghost = Math.max(0, (d.rating_ghost || 0) - 1)
      }
    }
    return this.getRating(slug, deviceId)
  },
  async getLeaderboard(
    sort: 'avg' | 'god' | 'ghost' | 'net' | 'count' | 'heat',
    page = 1,
    pageSize = 20,
    range: 'all' | 'week' | 'month' = 'all',
  ): Promise<Paginated<DemoSummary>> {
    await delay(200)
    const now = Date.now()
    const week = 7 * 86400000
    const month = 30 * 86400000
    let items = [...demos].filter((d) => {
      if (d.status !== 'approved') return false
      if (range === 'all') return true
      return now - new Date(d.created_at).getTime() <= (range === 'week' ? week : month)
    })
    // RF-5a：与后端 ratings.py:149 对齐 —— 质量榜（avg/god/ghost/net）排除 0 评。
    // 原实现把 0 评作品也排进榜（还会因为 rating_avg 缺省 0 而垫底出现在榜尾），
    // 与真接口「没证据不上榜」的口径相反。
    if (sort === 'avg' || sort === 'god' || sort === 'ghost' || sort === 'net') {
      items = items.filter((d) => (d.rating_count || 0) > 0)
    }
    if (sort === 'god') items.sort((a, b) => (b.rating_god || 0) - (a.rating_god || 0))
    else if (sort === 'ghost') items.sort((a, b) => (b.rating_ghost || 0) - (a.rating_ghost || 0))
    else if (sort === 'count') items.sort((a, b) => (b.rating_count || 0) - (a.rating_count || 0))
    else if (sort === 'heat') items.sort((a, b) => (b.view_count + b.download_count) - (a.view_count + a.download_count))
    else if (sort === 'net') items.sort((a, b) => ((b.rating_god || 0) - (b.rating_ghost || 0)) - ((a.rating_god || 0) - (a.rating_ghost || 0)))
    else items.sort((a, b) => (b.rating_avg || 0) - (a.rating_avg || 0))
    const start = (page - 1) * pageSize
    return { items: clone(items.slice(start, start + pageSize)), total: items.length, page, page_size: pageSize }
  },

  async userLeaderboard(
    sort: 'reputation' | 'likes' | 'thanks' | 'topics' | 'replies' | 'demos' | 'followers',
    page = 1,
    pageSize = 20,
  ): Promise<Paginated<UserLeaderboardItem>> {
    await delay()
    // 占位统计表：mock 用户无真实声望数据，按 id 给一组确定值（与后端 UserLeaderboardOut 同形）
    const stats: Record<number, { reputation: number; received_likes: number; received_thanks: number; topic_count: number; reply_count: number; follower_count: number }> = {
      1: { reputation: 120, received_likes: 45, received_thanks: 12, topic_count: 2, reply_count: 8, follower_count: 6 },
      2: { reputation: 66, received_likes: 30, received_thanks: 5, topic_count: 1, reply_count: 6, follower_count: 2 },
      3: { reputation: 88, received_likes: 51, received_thanks: 9, topic_count: 3, reply_count: 11, follower_count: 4 },
    }
    const demoCountOf = (id: number) => demos.filter((d) => d.status === 'approved' && (d as unknown as { author_id?: number }).author_id === id).length
    const rows: UserLeaderboardItem[] = users.map((u) => ({
      id: u.id,
      username: u.username,
      bio: u.bio || '',
      reputation: stats[u.id]?.reputation ?? 0,
      received_likes: stats[u.id]?.received_likes ?? 0,
      received_thanks: stats[u.id]?.received_thanks ?? 0,
      demo_count: demoCountOf(u.id),
      topic_count: stats[u.id]?.topic_count ?? 0,
      reply_count: stats[u.id]?.reply_count ?? 0,
      follower_count: stats[u.id]?.follower_count ?? 0,
    }))
    const keyOf = (r: UserLeaderboardItem): number =>
      sort === 'likes' ? r.received_likes
      : sort === 'thanks' ? r.received_thanks
      : sort === 'topics' ? r.topic_count
      : sort === 'replies' ? r.reply_count
      : sort === 'demos' ? r.demo_count
      : sort === 'followers' ? r.follower_count
      : r.reputation
    rows.sort((a, b) => (keyOf(b) - keyOf(a)) || (b.id - a.id))
    const start = (page - 1) * pageSize
    return { items: clone(rows.slice(start, start + pageSize)), total: rows.length, page, page_size: pageSize }
  },


  async getSiteStats(): Promise<SiteStats> {
    await delay()
    const now = new Date()
    const d = (n: number) => { const x = new Date(now); x.setDate(x.getDate() - n); return x.toISOString().slice(0, 10) }
    const last7 = [6, 5, 4, 3, 2, 1, 0].map((n, i) => ({ date: d(n), count: 40 + ((i * 37) % 60) }))
    return {
      today: 168,
      yesterday: 132,
      total: 45678,
      last7,
    }
  },
  async reportVisit(): Promise<void> {
    return
  },
  async reportHeartbeat(): Promise<void> {
    return
  },
  async getLiveStats(): Promise<LiveStats> {
    await delay(100)
    return { online: 12, last1min: 8, last5min: 35, today: 168 }
  },
  async getSiteInfo(): Promise<SiteInfo> {
    await delay(100)
    const approved = demos.filter((d) => d.status === 'approved')
    return {
      site: { name: 'AI 全民制作人', description: 'AI 网页 Demo 作品集', info_version: 1 },
      display: { fun_mode: false },
      content: {
        demos_total: approved.length,
        demos_by_type: approved.reduce<Record<string, number>>((m, d) => {
          const t = d.demo_type || 'web' // demo_type 可空，不能直接做索引键
          m[t] = (m[t] || 0) + 1
          return m
        }, {}),
        authors_total: 6,
        uploads_last_7d: approved.length,
        tags: { keys: 10, values: 210 },
        forum_topics: forumTopics.length,
      },
      community: { users_total: 20, users_active_week: 5 },
      traffic: { pv_today: 168, pv_yesterday: 210, pv_total: 12345, online_now: 12 },
      hot: {
        top_models: [
          { value: 'ds-unknown', demos: 12 },
          { value: 'dsv4-flash', demos: 6 },
        ],
        top_games: [{ value: '我的世界', demos: 4 }],
        latest_demo: approved.length
          ? { slug: approved[0].slug, title: approved[0].title, created_at: approved[0].created_at }
          : null,
      },
      capabilities: {
        upload: {
          anonymous: true,
          guide: '/api/v1/meta/agent-guide',
          tag_keys: '/api/v1/tags/tag-keys',
          idempotency: true,
        },
        features: { forum: true, ratings: true, session_logs: true, preview: 'versioned-url' },
      },
      generated_at: new Date().toISOString(),
    }
  },

  // ---------- v2 实体（Mock） ----------
  async listModels(
    params: { status?: string; vendor?: string; q?: string; sort?: 'demos' | 'score' | 'rating' | 'votes' | 'new' | 'name'; page?: number; page_size?: number } = {},
  ): Promise<PaginatedModels> {
    await delay()
    const items: ModelSummary[] = mockModels.map((m) => ({ ...m }))
    const lq = params.q?.toLowerCase()
    const filtered = items.filter(
      (m) =>
        (!params.status || m.status === params.status) &&
        (!params.vendor || m.vendor === params.vendor) &&
        // 真实后端 model 搜索含别名；mock 同口径（防上传「别名缺口」在离线态失真的关键）
        (!lq ||
          m.name.toLowerCase().includes(lq) ||
          (mockAliases[m.slug] || []).some((a) => a.toLowerCase().includes(lq))),
    )
    if (params.sort === 'rating') filtered.sort((a, b) => (b.rating_avg ?? 0) - (a.rating_avg ?? 0))
    else if (params.sort === 'score' || params.sort === 'votes')
      filtered.sort((a, b) => (b.votes ?? 0) - (a.votes ?? 0) || (b.score ?? 0) - (a.score ?? 0))
    else if (params.sort === 'name') filtered.sort((a, b) => a.name.localeCompare(b.name))
    else if (params.sort === 'new') filtered.sort((a, b) => b.created_at.localeCompare(a.created_at))
    else filtered.sort((a, b) => b.demo_count - a.demo_count)
    return { items: filtered, total: filtered.length, page: params.page ?? 1, page_size: params.page_size ?? 20 }
  },
  async getModel(slug: string): Promise<ModelDetail> {
    await delay()
    const base = (await this.listModels({})).items.find((m) => m.slug === slug)
    if (!base) throw new Error('模型不存在')
    const demos = demosApproved().slice(0, 6)
    return {
      ...base,
      aliases: [...(mockAliases[slug] || [])],
      tasks: [{ id: 1, slug: 'mc-web', title: '我的世界网页版', demo_count: 3 }],
      recent_demos: demos,
      merged_into: null,
      type_dist: [
        { value: 'game', demos: 5 },
        { value: 'demo', demos: 3 },
      ],
      game_dist: [{ value: '我的世界', demos: 3 }],
    }
  },
  async peek(kind: 'model' | 'task' | 'demo', slug: string): Promise<PeekResult> {
    await delay()
    if (kind === 'model') {
      const m = mockModels.find((x) => x.slug === slug)
      const list = demosApproved().slice(0, 3)
      return {
        kind: 'model',
        slug,
        name: m?.name || slug,
        vendor: m?.vendor ?? null,
        resolution: m?.resolution || 'exact',
        status: m?.status || 'active',
        description: m?.description || '',
        demo_count: m?.demo_count ?? list.length,
        score: m?.score ?? null,
        votes: m?.votes ?? 0,
        sample_level: m?.sample_level || 'none',
        demos: list.map((d) => ({ slug: d.slug, title: d.title, rating_avg: d.rating_avg ?? null, rating_count: d.rating_count ?? 0, cover_url: d.cover_url ?? null })),
        full_path: `/models/${slug}`,
      }
    }
    if (kind === 'task') {
      return {
        kind: 'task',
        slug,
        name: slug.replace(/-/g, ' · '),
        description: '（mock）这道题要求做出一个可玩的网页作品，题面摘自第一件作品的提示词。',
        is_prompt_excerpt: true,
        demo_count: 4,
        model_count: 3,
        demos: demosApproved().slice(0, 3).map((d) => ({ slug: d.slug, title: d.title, rating_avg: d.rating_avg ?? null, rating_count: d.rating_count ?? 0, cover_url: d.cover_url ?? null })),
        full_path: `/tasks/${slug}`,
      }
    }
    const d = demosApproved().find((x) => x.slug === slug) || demosApproved()[0]
    return {
      kind: 'demo',
      slug: d.slug,
      name: d.title,
      description: d.description,
      demo_type: d.demo_type,
      rating_avg: d.rating_avg,
      rating_count: d.rating_count,
      cover_url: d.cover_url,
      models: d.models || [],
      demos: [],
      full_path: `/demo/${d.slug}`,
    }
  },
  async getModelDemos(
    slug: string,
    params: { sort?: 'newest' | 'score' | 'popular'; type?: string; game?: string; page?: number; page_size?: number } = {},
  ): Promise<Paginated<DemoSummary>> {
    await delay()
    const base = demosApproved().filter((d) => {
      const names = new Set([...(d.models ?? []).map((m) => m.slug), ...d.tags.filter((x) => x.key === 'model').map((x) => x.value)])
      return names.has(slug)
    })
    // mock 作品未逐个挂模型：一个都没有时退回全量，好让「加载更多 / 排序」能离线演示
    const pool = base.length ? base : demosApproved()
    const sorted = [...pool].sort((a, b) => {
      if (params.sort === 'score') return (b.rating_avg ?? 0) - (a.rating_avg ?? 0)
      if (params.sort === 'popular') return b.view_count + b.download_count - (a.view_count + a.download_count)
      return b.created_at.localeCompare(a.created_at)
    })
    const size = params.page_size ?? 24
    const page = params.page ?? 1
    return {
      items: sorted.slice((page - 1) * size, page * size).map((d) => ({ ...d })),
      total: sorted.length,
      page,
      page_size: size,
    }
  },
  async getExplore(): Promise<ExploreResult> {
    await delay()
    const models = await this.listModels({ sort: 'demos', page_size: 12 })
    const tasks = await this.listTasks({ sort: 'demos', page_size: 8 })
    // 描述性标签：从 mock 作品实际统计（兜底位不参与，与真接口口径一致）
    const tally = (key: string) => {
      const c = new Map<string, number>()
      for (const d of demos) {
        if (d.status !== 'approved') continue
        for (const t of d.tags) if (t.key === key) c.set(t.value, (c.get(t.value) ?? 0) + 1)
      }
      return [...c.entries()].sort((a, b) => b[1] - a[1]).slice(0, 12).map(([value, demos]) => ({ value, demos }))
    }
    return {
      models: { total: models.total, items: models.items, fallback_demos: tally('model').reduce((a, b) => a + b.demos, 0) },
      tasks_total: tasks.total,
      tasks: tasks.items,
      tags: { category: tally('category'), type: tally('type'), game: tally('game') },
    }
  },
  async listTasks(params: { status?: string; q?: string; category?: string; sort?: string; page?: number; page_size?: number } = {}): Promise<PaginatedTasks> {
    await delay()
    const q = (params.q || '').toLowerCase()
    let items = mockTasks.filter(
      (t) =>
        (!params.status || t.status === params.status) &&
        (!params.category || t.category === params.category) &&
        (!q || t.title.toLowerCase().includes(q) || t.description.toLowerCase().includes(q)),
    )
    if (params.sort === 'newest') items = [...items].sort((a, b) => b.created_at.localeCompare(a.created_at))
    else items = [...items].sort((a, b) => b.demo_count - a.demo_count)
    return { items: clone(items), total: items.length, page: params.page ?? 1, page_size: params.page_size ?? 20 }
  },
  async suggestTasks(q: string, limit = 6): Promise<TaskSuggestItem[]> {
    await delay()
    const s = (q || '').toLowerCase()
    return mockTasks
      .map((t) => {
        const hay = `${t.title} ${t.description}`.toLowerCase()
        const hit = hay.split(/[\s，。、·:；]/).filter((w) => w.length > 1 && s.includes(w)).length
        return { t, score: hit ? Math.min(0.95, 0.25 + hit * 0.2) : hay && s && hay.includes(s.slice(0, 4)) ? 0.3 : 0 }
      })
      .filter((x) => x.score > 0.05)
      .sort((a, b) => b.score - a.score)
      .slice(0, limit)
      .map((x) => ({ task_id: x.t.id, slug: x.t.slug, title: x.t.title, category: x.t.category, demo_count: x.t.demo_count, score: Number(x.score.toFixed(3)) }))
  },
  /** 独立出题（mock）：同题名 pending 幂等返回，与真接口同形 */
  async proposeTask(payload: TaskProposalIn): Promise<TaskProposalOut> {
    await delay()
    const title = (payload.title || '').trim()
    if (!title) throw new Error('title 必填（不能全是空白）')
    const dup = mockTaskProposals.find((p) => p.title === title && p.status === 'pending')
    if (dup) return { status: 'pending', suggestion_id: dup.id, duplicate: true }
    const id = mockTaskProposals.length ? Math.max(...mockTaskProposals.map((p) => p.id)) + 1 : 1
    mockTaskProposals.push({ id, title, description: payload.description || '', category: payload.category || null, status: 'pending' })
    return { status: 'pending', suggestion_id: id, duplicate: false }
  },
  async getTask(slug: string): Promise<TaskDetail> {
    await delay()
    const task = mockTasks.find((t) => t.slug === slug)
    if (!task) throw new Error('题目不存在')
    const demos = demosApproved().slice(0, 4)
    return {
      id: task.id, slug: task.slug, title: task.title, description: task.description,
      category: task.category, status: task.status, created_at: task.created_at, demos_total: demos.length,
      compare: [
        { model: { id: 1, slug: 'dsv4-flash', name: 'dsv4-flash', vendor: 'DeepSeek', status: 'active' }, demo_count: 2, avg_rating: 4.4, avg_rounds: 1.5, avg_minutes: 32, best_demo: demos[0] ? { slug: demos[0].slug, title: demos[0].title, rating_avg: 4.4 } : null },
        { model: { id: 3, slug: 'ds-unknown', name: 'ds-unknown', vendor: null, status: 'unverified' }, demo_count: 1, avg_rating: 4.0, avg_rounds: null, avg_minutes: null, best_demo: null },
      ],
      demos,
      // 图像作品一期：并排看的数据源与真接口同形（rows 带 cover_url；真接口只出 approved）
      chain: {
        brief: task.description || '',
        brief_source: task.description ? 'description' : '',
        prompt_id: null,
        prompt_variants: 0,
        no_prompt_count: demos.filter((d) => !(d.prompt || '').trim()).length,
        rows: demos.map((d) => ({
          slug: d.slug,
          title: d.title,
          cover_url: d.cover_url,
          demo_type: d.demo_type ?? 'web',
          models: d.models ?? [],
          prompt_id: null,
          same_prompt: null,
          prompt_excerpt: (d.prompt || '').slice(0, 120),
          rounds: null,
          minutes: null,
          rating_avg: d.rating_count ? d.rating_avg ?? null : null,
          rating_count: d.rating_count ?? 0,
        })),
      },
    }
  },
  async getPromptClusters(_opts: { refresh?: boolean; minScore?: number } = {}): Promise<PromptClusters> {
    await delay()
    // mock 按「同一句提示词」出 exact 簇（粒子星空 / 音频可视化 故意共用一句，面板可见效果）
    const by = new Map<string, DemoDetail[]>()
    for (const d of demos) {
      const p = (d.prompt || '').trim()
      if (!p) continue
      by.set(p, [...(by.get(p) ?? []), d])
    }
    const toCluster = (members: DemoDetail[]): PromptCluster => ({
      kind: 'exact',
      score: null,
      demo_count: members.length,
      models: [...new Set(members.flatMap((m) => m.tags.filter((t) => t.key === 'model').map((t) => t.value)))].sort(),
      distinct_models: 0,
      covered: false,
      suggested_title: members[0]?.title ?? '',
      sample_prompt: (members[0]?.prompt ?? '').slice(0, 600),
      demos: members.map((m) => ({
        demo_id: 0,
        slug: m.slug,
        title: m.title,
        models: m.tags.filter((t) => t.key === 'model').map((t) => t.value),
        rating_avg: m.rating_avg ?? 0,
        rating_count: m.rating_count ?? 0,
        covered: false,
      })),
    })
    const exact = [...by.values()].filter((a) => a.length >= 2).map(toCluster)
    for (const c of exact) c.distinct_models = c.models.length
    return {
      exact,
      similar: [],
      stats: {
        demos_with_prompt: [...by.values()].reduce((a, b) => a + b.length, 0),
        unique_prompts: by.size,
        exact_clusters: exact.length,
        similar_clusters: 0,
      },
    }
  },
  async adminCreateTask(payload: {
    title: string
    description?: string
    category?: string
    status?: string
    demo_ids?: number[]
    demo_slugs?: string[]
  }): Promise<{ id: number; slug: string; title: string; status: string; attached: number }> {
    await delay(200)
    const slug = `mock-task-${mockTaskSeq}`
    mockTasks.unshift({
      id: mockTaskSeq++,
      slug,
      title: payload.title,
      description: payload.description || '',
      category: payload.category || null,
      status: payload.status || 'active',
      demo_count: payload.demo_ids?.length ?? 0,
      created_at: new Date().toISOString(),
    })
    // M3-B3 建题即挂（demo_slugs → 解析+挂载映射）
    if (payload.demo_slugs?.length) {
      const list = (taskAttached[slug] = taskAttached[slug] || [])
      for (const ds of payload.demo_slugs) {
        const d = demos.find((x) => x.slug === ds)
        if (!d) throw new Error('demo slug 不存在: ' + ds)
        if (!list.some((x) => x.slug === ds)) list.unshift({ id: ++attachSeq, slug: ds, title: d.title || ds, status: d.status || 'approved' })
      }
    }
    return { id: mockTaskSeq - 1, slug, title: payload.title, status: payload.status || 'active', attached: payload.demo_ids?.length ?? 0 }
  },
  async getSponsors(): Promise<SponsorBoard> {
    await delay()
    return {
      total_amount: '¥ 1280',
      updated_at: '2026-08-19',
      sponsors: [
        { name: 'Alice', amount: '¥ 500', message: '支持 AI 全民制作人！' },
        { name: 'Bob', amount: '¥ 300', message: '作品很棒，继续加油' },
        { name: 'Charlie', amount: '¥ 200' },
        { name: 'Dave', amount: '¥ 100' },
      ],
    }
  },
  async getThanks(): Promise<ThanksBoard> {
    await delay()
    const items = recognition.filter((r) => r.kind === 'thanks' && r.active)
    return {
      updated_at: new Date().toISOString().slice(0, 10),
      thanks: items.map((r) => ({ name: r.name, ...(r.message ? { message: r.message } : {}) })),
    }
  },
  async listRecognition(): Promise<{ items: RecognitionItem[] }> {
    await delay()
    return { items: clone(recognition) }
  },
  async createRecognition(payload: RecognitionInput): Promise<{ id: number }> {
    await delay()
    const item: RecognitionItem = {
      id: Date.now(),
      kind: payload.kind,
      name: payload.name,
      amount: payload.kind === 'sponsor' ? payload.amount ?? 0 : null,
      message: payload.message || '',
      show_amount: payload.kind === 'sponsor' ? !!payload.show_amount : true,
      sort: payload.sort || 0,
      active: payload.active ?? true,
    }
    recognition.push(item)
    return { id: item.id }
  },
  async updateRecognition(id: number, payload: RecognitionInput): Promise<{ id: number }> {
    await delay()
    const r = recognition.find((x) => x.id === id)
    if (!r) throw new Error('记录不存在')
    r.kind = payload.kind
    r.name = payload.name
    r.amount = payload.kind === 'sponsor' ? payload.amount ?? 0 : null
    r.message = payload.message || ''
    r.show_amount = payload.kind === 'sponsor' ? !!payload.show_amount : true
    r.sort = payload.sort || 0
    r.active = payload.active ?? true
    return { id }
  },
  async deleteRecognition(id: number): Promise<void> {
    await delay()
    const i = recognition.findIndex((x) => x.id === id)
    if (i >= 0) recognition.splice(i, 1)
  },

  async createDemo(payload: CreateDemoPayload, onProgress?: (percent: number) => void): Promise<DemoCreateResult> {
    await delay(500)
    onProgress?.(50)
    await delay(500)
    onProgress?.(100)
    if (!currentUser) throw new Error('请先登录')
    if (payload.demo_type === 'image') {
      if (!payload.file) throw new Error('请上传图像文件')
      if (!/\.(png|jpg|jpeg|webp)$/i.test(payload.file.name)) throw new Error('图像作品仅支持 PNG / JPG / JPEG / WEBP')
    } else if (payload.demo_type !== 'link' && !payload.file) throw new Error('请上传 zip 文件')
    const id = mockDemoIdSeq++
    const slug = 'demo_' + Math.random().toString(16).slice(2, 10)
    // 图像作品：图即作品，直接用本地对象 URL 展示上传的那张图（真后端落 /media/covers/*.webp）；
    // srcset 派生档只对 /media/covers/*.webp 生效 → CoverImg 会自动回落到本对象 URL。
    const imageCover = payload.demo_type === 'image' && payload.file ? URL.createObjectURL(payload.file) : ''
    const demo: DemoDetail = {
      slug,
      title: payload.title,
      description: payload.description || '',
      cover_url: imageCover || svgCover('#95e1d3', 'NEW', 'just uploaded'),
      author: currentUser.username,
      author_id: currentUser.id,
      tags: [...(payload.tags || []).map((t) => toTagRef(t)), { key: 'author', value: currentUser.username }],
      demo_type: payload.demo_type || 'web',
      external_url: payload.external_url || null,
      prompt: payload.prompt || '',
      video_url: payload.video_url || null,
      view_count: 0,
      download_count: 0,
      comment_count: 0,
      created_at: new Date().toISOString(),
      status: settings.auto_approve ? 'approved' : 'pending',
      session_log_count: 0,
        is_author: true,
      previewHtml: payload.demo_type === 'web' ? '<!doctype html><html><body style="margin:0;font-family:monospace;display:grid;place-items:center;height:100vh;background:#4ecdc4"><h1>已上传 Demo</h1></body></html>' : undefined,
    }
    ;(settings.auto_approve ? demos : pendingDemos).push(demo)
    // v2 B4′：带 task 的上传只入候选队列（与真实后端一致，不直接挂题）
    if (payload.task) {
      mockSuggestions.unshift({
        id: mockSuggestionSeq++,
        kind: 'task_match',
        payload: { task_title: payload.task, demo_title: demo.title, demo_slug: demo.slug },
        confidence: 0.98,
        source: 'user',
        status: 'pending',
        created_at: new Date().toISOString(),
      })
    }
    // 身份绑定闭环 B：带 propose_task 的上传只落 new_task 候选，不建 Task（对应 test_propose_task_queues_new_task_without_creating_task）
    if (payload.propose_task) {
      mockSuggestions.unshift({
        id: mockSuggestionSeq++,
        kind: 'new_task',
        payload: {
          title: payload.propose_task.title,
          description: payload.propose_task.description || '',
          category: payload.propose_task.category || null,
        },
        confidence: 0.98,
        source: 'user',
        status: 'pending',
        created_at: new Date().toISOString(),
      })
    }
    return { id, slug: demo.slug, status: demo.status as string, created: true }
  },

  async listSuggestions(params: { status?: string; kind?: string } = {}): Promise<SuggestionList> {
    await delay(150)
    const st = params.status || 'pending'
    const items = mockSuggestions.filter(
      (s) => (st === 'all' || s.status === st) && (!params.kind || s.kind === params.kind),
    )
    const byKind: Record<string, number> = {}
    for (const s of mockSuggestions) if (s.status === 'pending') byKind[s.kind] = (byKind[s.kind] || 0) + 1
    // 列表出口降级：new_prompt 超长正文截断（存储不动）；全量走 getSuggestion
    return { items: clone(items).map(excerptSuggestion), pending_by_kind: byKind, thresholds: { auto_accept: 0.99, review: 0.6 } }
  },
  // 详情口：返回未截断的完整建议（payload.content 为全量），收件箱「查看全文」数据源
  async getSuggestion(id: number): Promise<SuggestionItem> {
    await delay(120)
    const s = mockSuggestions.find((x) => x.id === id)
    if (!s) throw new Error('建议不存在')
    return clone(s)
  },
  // M3-3 直改权演示：Model 状态跃迁/Task 题面直改（mock 内存态，与真实端点同形状）
  async setModelStatus(ident: string, payload: { status: string; reason?: string }): Promise<{ id: number; slug: string; status: string }> {
    await delay(200)
    const m = mockModels.find((x) => x.slug === ident || String(x.id) === ident)
    if (!m) throw new Error('模型实体不存在')
    m.status = payload.status
    return { id: m.id, slug: m.slug, status: m.status }
  },
  async updateTask(ident: string, payload: { title?: string; description?: string; category?: string | null; status?: string }): Promise<{ id: number; slug: string; title: string; status: string }> {
    await delay(200)
    const k = mockTasks.find((x) => x.slug === ident || String(x.id) === ident)
    if (!k) throw new Error('题目不存在')
    if (payload.title) k.title = payload.title
    if (payload.description != null) k.description = payload.description
    if (payload.category !== undefined) k.category = payload.category
    if (payload.status) k.status = payload.status
    return { id: k.id, slug: k.slug, title: k.title, status: k.status }
  },
  // T3·M5-B2 Tag 状态跃迁（mock 内存态：按 value.id 定位并写回词表 status）
  async setTagStatus(tagId: number, payload: { status: string; reason?: string }): Promise<{ id: number; key: string; value: string; status: string }> {
    await delay(200)
    for (const k of tagKeys) for (const v of k.values) {
      if (v.id === tagId) {
        if (v.status === payload.status) throw new Error('该标签已是 ' + payload.status + ' 状态')
        v.status = payload.status
        return { id: tagId, key: k.key, value: v.value, status: v.status }
      }
    }
    throw new Error('标签值不存在')
  },
  // M3-B3 直改权/挂摘/批量（与真实端点同形状；mock 内存态）
  async getAdminTaskDetail(ident: string): Promise<{ id: number; slug: string; title: string; description: string; category: string | null; status: string; merged_into_id: number | null; created_at: string; demos: { id: number; slug: string; title: string; status: string }[] }> {
    await delay(150)
    const k = mockTasks.find((x) => x.slug === ident || String(x.id) === ident)
    if (!k) throw new Error('题目不存在')
    return { id: k.id, slug: k.slug, title: k.title, description: k.description, category: k.category ?? null, status: k.status, merged_into_id: null, created_at: k.created_at, demos: taskAttached[k.slug] ?? [] }
  },
  async patchEntity(entityType: 'model' | 'task' | 'tag', ident: string | number, fields: Record<string, unknown>): Promise<Record<string, unknown>> {
    await delay(180)
    if (entityType === 'model') {
      const m = mockModels.find((x) => x.slug === ident || String(x.id) === ident)
      if (!m) throw new Error('模型实体不存在')
      if (fields.name) m.name = String(fields.name)
      if (fields.vendor !== undefined) m.vendor = fields.vendor as string | null
      if (fields.description !== undefined) m.description = String(fields.description)
      return { type: 'model', id: m.id, slug: m.slug, updated: Object.keys(fields) }
    }
    if (entityType === 'task') {
      const k = mockTasks.find((x) => x.slug === ident || String(x.id) === ident)
      if (!k) throw new Error('题目不存在')
      const f = fields as { title?: string; description?: string; category?: string | null; status?: string }
      if (f.title) k.title = f.title
      if (f.description !== undefined) k.description = f.description
      if (f.category !== undefined) k.category = f.category
      if (f.status) k.status = f.status
      return { type: 'task', id: k.id, slug: k.slug, updated: Object.keys(f) }
    }
    if (entityType === 'tag') {
      const id = Number(ident)
      for (const k of tagKeys) for (const v of k.values) {
        if (v.id === id) {
          if (fields.description !== undefined && fields.description !== null) v.description = String(fields.description)
          if (fields.group !== undefined) v.group = (fields.group as string | null) || null
          return { type: 'tag', id, key: k.key, value: v.value, updated: Object.keys(fields) }
        }
      }
      throw new Error('标签值不存在')
    }
    throw new Error('未知实体类型 ' + entityType)
  },
  async batchReviewSuggestions(action: 'approve' | 'reject', ids: number[]): Promise<{ action: string; ok: number; failed: number; results: { id: number; ok: boolean; error?: string }[] }> {
    await delay(300)
    const results: { id: number; ok: boolean; error?: string }[] = []
    for (const sid of ids) {
      try {
        await mockApi.reviewSuggestion(sid, action)
        results.push({ id: sid, ok: true })
      } catch (e) {
        results.push({ id: sid, ok: false, error: (e as Error).message })
      }
    }
    return { action, ok: results.filter((r) => r.ok).length, failed: results.filter((r) => !r.ok).length, results }
  },
  async attachTaskDemoBySlug(ident: string, demoSlug: string): Promise<{ task_id: number; attached: number }> {
    await delay(180)
    const k = mockTasks.find((x) => x.slug === ident || String(x.id) === ident)
    if (!k) throw new Error('题目不存在')
    const d = demos.find((x) => x.slug === demoSlug)
    if (!d) throw new Error('demo slug 不存在: ' + demoSlug)
    const list = (taskAttached[k.slug] = taskAttached[k.slug] || [])
    if (!list.some((x) => x.slug === demoSlug)) list.unshift({ id: ++attachSeq, slug: demoSlug, title: d.title || demoSlug, status: d.status || 'approved' })
    k.demo_count = (k.demo_count || 0) + 1
    return { task_id: k.id, attached: 1 }
  },
  async detachTaskDemoBySlug(ident: string, demoSlug: string): Promise<void> {
    await delay(180)
    const k = mockTasks.find((x) => x.slug === ident || String(x.id) === ident)
    if (!k) throw new Error('题目不存在')
    const list = taskAttached[k.slug] || []
    const i = list.findIndex((x) => x.slug === demoSlug)
    if (i < 0) throw new Error('该作品不在此题目下')
    list.splice(i, 1)
    k.demo_count = Math.max(0, (k.demo_count || 0) - 1)
  },
  async reviewSuggestion(id: number, action: 'approve' | 'reject'): Promise<SuggestionItem> {
    await delay(200)
    const s = mockSuggestions.find((x) => x.id === id)
    if (!s) throw new Error('建议不存在')
    if (s.status !== 'pending') throw new Error('该建议已处理')
    s.status = action === 'approve' ? 'approved' : 'rejected'
    s.reviewed_at = new Date().toISOString()
    if (action === 'approve' && s.kind === 'new_prompt') mockPromptStore.approve(s)
    // 批准挂题：mock 里把对应题目的作品数 +1，面板反馈可见
    if (action === 'approve' && s.kind === 'task_match') {
      const title = String(s.payload.task_title || '')
      const t = mockTasks.find((x) => x.slug === title || x.title === title || x.id === Number(s.payload.task_id))
      if (t) t.demo_count += 1
    }
    if (action === 'approve' && s.kind === 'new_task') {
      const slug = String(s.payload.slug || 'new-task')
      s.result = `题目 ${slug} 挂 0 个作品`
    }
    if (action === 'approve' && s.kind === 'new_model') {
      const name = String(s.payload.name || s.payload.slug || 'model')
      s.result = `模型 ${name} → ${name}`
    }
    return clone(s)
  },

  async getAttributionPending(): Promise<AttributionPending> {
    await delay(150)
    return clone({ groups: ensureAttribution(), targets: mockAttrTargets })
  },
  async attributeDemos(payload: { demo_ids: number[]; target_id: number; reason?: string }): Promise<AttributeResult> {
    await delay(200)
    const target = mockAttrTargets.find((t) => t.id === payload.target_id)
    if (!target) throw new Error('目标型号不存在')
    const moved: number[] = []
    for (const g of mockAttribution) {
      const hits = g.demos.filter((d) => payload.demo_ids.includes(d.id))
      if (!hits.length) continue
      moved.push(...hits.map((d) => d.id))
      g.demos = g.demos.filter((d) => !payload.demo_ids.includes(d.id))
      g.model.demo_count = g.demos.length
    }
    mockAttribution = mockAttribution.filter((g) => g.demos.length)
    if (!moved.length) throw new Error('这些作品已归属过')
    return { moved: moved.length, demo_ids: moved, target: { ...target } }
  },

  async getTypeDemoPreview(params: { limit?: number; min_confidence?: number } = {}): Promise<TypeDemoPreview> {
    await delay(200)
    // mock 版规则：与后端同一批关键词的极简副本，只为离线演示「预览→入队→批准」链路
    const rules: [string, string[]][] = [
      ['simulation', ['仿真', '模拟', '物理', '引力']],
      ['music', ['音乐', '钢琴', '节奏', 'audio']],
      ['utility', ['工具', '计算', '转换', '二维码']],
      ['visualization', ['可视化', '图表', '数据']],
      ['education', ['教学', '科普', '学习']],
    ]
    const minConf = params.min_confidence ?? 0.7
    const inDemo = demos.filter((d) => d.status === 'approved' && d.tags.some((x) => x.key === 'type' && x.value === 'demo'))
    const samples: TypeDemoPreview['samples'] = []
    const byTarget: Record<string, number> = {}
    for (const d of [...inDemo, ...demos.slice(0, 4)]) {
      const text = `${d.title} ${d.description} ${d.prompt || ''}`
      const hit = rules.find(([, ws]) => ws.some((w) => text.includes(w)))
      if (!hit) continue
      const conf = 0.82
      if (conf < minConf) continue
      byTarget[hit[0]] = (byTarget[hit[0]] || 0) + 1
      if (samples.length < 40) {
        samples.push({
          demo_slug: d.slug,
          demo_title: d.title,
          add: hit[0],
          alt: [],
          confidence: conf,
          matched: rules.find(([, ws]) => ws.some((w) => text.includes(w)))?.[1].filter((w) => text.includes(w)) || [],
          label_zh: hit[0],
        })
      }
    }
    const dist = new Map<string, number>()
    for (const d of demos.filter((x) => x.status === 'approved')) {
      for (const tg of d.tags) if (tg.key === 'type') dist.set(tg.value, (dist.get(tg.value) || 0) + 1)
    }
    const approved = demos.filter((d) => d.status === 'approved').length
    return {
      stats: {
        approved,
        demo_share: (dist.get('demo') || 0) / (approved || 1),
        type_dist: [...dist.entries()].map(([value, n]) => ({ value, demos: n, rate: n / (approved || 1) })),
      },
      scanned: params.limit ?? 500,
      proposed: samples.length,
      by_target: byTarget,
      samples,
    }
  },
  async queueTypeDemo(): Promise<TypeDemoQueueResult> {
    await delay(200)
    return { proposed: 0, queued: 0 }
  },

  async getInspection(): Promise<InspectionResult> {
    await delay(200)
    const approved = demos.filter((d) => d.status === 'approved').length
    const noType = demos.filter((d) => d.status === 'approved' && !d.tags.some((x) => x.key === 'type')).length
    const noPrompt = demos.filter((d) => d.status === 'approved' && !(d.prompt || '').trim()).length
    const rows: InspectionCheck[] = [
      { id: 'type_missing', label: '作品没有 type 标签', level: 'action', hint: '规则可从提示词/标题推断，生成补值候选', count: noType, can_queue: true, rate: noType / (approved || 1), samples: demos.slice(0, 3).map((d) => ({ slug: d.slug, title: d.title })) },
      { id: 'type_multi', label: '作品挂了多个 type', level: 'action', hint: '只处理含 demo 的组合（删垃圾桶留具体值）', count: 2, can_queue: true, fixable: 1, samples: [] },
      { id: 'demo_left', label: '仍挂 type:demo 且规则无信号', level: 'warn', hint: '机器没把握，需人工逐件看', count: 4, can_queue: false, samples: [] },
      { id: 'no_prompt', label: '缺第一轮提示词', level: 'warn', hint: '只能靠作者/管理员补，机器编不出来', count: noPrompt, can_queue: false, rate: noPrompt / (approved || 1) },
      { id: 'model_fallback', label: '挂在兜底型号上的作品', level: 'warn', hint: '去「归属工作台」处理', count: mockAttribution.reduce((n, g) => n + g.demos.length, 0), can_queue: false },
      { id: 'fixed_no_desc', label: '固定值缺少介绍', level: 'warn', hint: '词表补课：悬浮提示与搜索都依赖它', count: 6, can_queue: false },
      { id: 'orphan_values', label: '零引用的标签值', level: 'info', hint: '可能错拼或已废弃，人工决定清理', count: 3, can_queue: false, samples: [] },
      { id: 'dup_model_slug', label: '重复 slug 的模型实体', level: 'warn', hint: '违反实体唯一性，需要合并', count: 0, can_queue: false },
      { id: 'inbox_pending', label: '收件箱积压', level: 'info', hint: '待人工批准的候选数', count: mockSuggestions.filter((s) => s.status === 'pending').length, can_queue: false },
    ]
    return { approved, total_findings: rows.reduce((n, c) => n + c.count, 0), checks: rows }
  },
  async queueInspection(checkId: string): Promise<{ check: string; proposed: number; queued: number }> {
    await delay(220)
    if (checkId !== 'type_missing' && checkId !== 'type_multi') throw new Error('该巡检项没有可自动执行的补救动作')
    return { check: checkId, proposed: 0, queued: 0 }
  },

  async deriveTags(payload: { title?: string; description?: string; prompt?: string; limit?: number }): Promise<DeriveResult> {
    await delay(180)
    const text = `${payload.title || ''} ${payload.description || ''} ${payload.prompt || ''}`
    if (text.trim().length < 4) return { items: [], note: '' }
    const rules: [string, string, string[], number, string][] = [
      ['type', 'simulation', ['仿真', '模拟', '物理', '轨道'], 0.85, '描述命中：仿真、物理'],
      ['type', 'music', ['音乐', '钢琴', '节奏'], 0.85, '描述命中：音乐'],
      ['type', 'puzzle', ['解谜', '拼图', '消消乐'], 0.85, '描述命中：解谜'],
      ['model', 'dsv4-flash', ['dsv4', 'deepseek'], 0.9, '文本里出现该型号名'],
      ['category', '3D建模', ['3d', '三维', '建模'], 0.77, '名称命中「3D建模」（163 件在用）'],
    ]
    const items: DerivedTag[] = []
    for (const [key, value, kws, conf, reason] of rules) {
      if (!kws.some((k) => text.toLowerCase().includes(k.toLowerCase()))) continue
      items.push({ key, value, label: value, confidence: conf, reason })
      if (key === 'type') break // type 单值语义：只给最合适的一个
    }
    items.sort((a, b) => b.confidence - a.confidence)
    return { items: items.slice(0, payload.limit ?? 8), note: items.length ? '规则推导，仅供参考；不收也不影响提交。' : '' }
  },
  async adminListModels(params: { q?: string; status?: string; page_size?: number } = {}): Promise<AdminModelList> {
    await delay(160)
    const items = mockModels.filter(
      (m) => (!params.q || m.name.toLowerCase().includes(params.q.toLowerCase())) && (!params.status || m.status === params.status),
    )
    const count = (s: string) => mockModels.filter((m) => m.status === s).length
    return { items: items.map((m) => ({ ...m })), total: items.length, status_counts: { candidate: count('candidate'), active: count('active'), unverified: count('unverified'), deprecated: count('deprecated') } }
  },
  /** KB-28：与真接口同口径（重名 409；建完进 mock 池，列表与选择器立即可见） */
  async adminCreateModel(payload: { name: string; vendor?: string; description?: string; status?: string }): Promise<{ id: number; slug: string; name: string; vendor?: string | null; status: string }> {
    await delay(200)
    const name = payload.name.trim()
    if (!name) throw new Error('名称不能为空')
    if (mockModels.some((m) => m.name.toLowerCase() === name.toLowerCase())) {
      throw new Error(`模型「${name}」已存在（或其别名已指向某实体），请改用合并或加别名`)
    }
    const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || `model-${mockModels.length + 1}`
    const row: ModelSummary = {
      id: Math.max(0, ...mockModels.map((m) => m.id)) + 1,
      slug,
      name,
      vendor: payload.vendor?.trim() || null,
      status: payload.status || 'active',
      resolution: 'exact',
      description: payload.description || '',
      demo_count: 0,
      rating_avg: null,
      score: null,
      votes: 0,
      sample_level: 'none',
      created_at: new Date().toISOString(),
    }
    mockModels.unshift(row)
    return { id: row.id, slug: row.slug, name: row.name, vendor: row.vendor, status: row.status }
  },
  /** KB-31：删除零引用实体（有引用时真接口 409，mock 同口径） */
  async adminDeleteModel(ident: string): Promise<void> {
    await delay(120)
    const i = mockModels.findIndex((m) => m.slug === ident || m.name === ident)
    if (i < 0) throw new Error('模型不存在')
    if (mockModels[i].demo_count > 0) throw new Error(`该模型仍有 ${mockModels[i].demo_count} 个作品引用，请使用合并而不是删除`)
    mockModels.splice(i, 1)
  },
  async adminDeleteTask(ident: string): Promise<void> {
    await delay(120)
    const i = mockTasks.findIndex((t) => t.slug === ident)
    if (i < 0) throw new Error('题目不存在')
    if ((mockTasks[i].demo_count || 0) > 0) throw new Error('该题目仍挂着作品，请使用合并或下架')
    mockTasks.splice(i, 1)
  },
  /** KB-30：models.dev 同步（mock 只回一个可读结果） */
  async syncModels(): Promise<{ providers: number; total_models: number; new_pending: number; updated_group: number; note: string }> {
    await delay(600)
    return { providers: 12, total_models: 240, new_pending: 3, updated_group: 5, note: '新模型已写入 pending 建议，需人工审核后生效' }
  },
  async adminListEntityTasks(params: { q?: string; status?: string; page_size?: number } = {}): Promise<AdminTaskList> {
    await delay(160)
    const items = mockTasks.filter((x) => (!params.q || x.title.includes(params.q)) && (!params.status || x.status === params.status))
    return { items, total: items.length, page: 1, page_size: params.page_size ?? 50, status_counts: { candidate: 0, active: items.length, merged: 0, hidden: 0 } }
  },
  async getEntityConflicts(): Promise<EntityConflicts> {
    await delay(160)
    // 造一组真冲突给向导演示：dsv4-flash 与 dsv4flash 规范化后同键
    const alive = mockModels.filter((m) => m.status !== 'deprecated')
    const groups: Record<string, typeof alive> = {}
    for (const m of alive) {
      const key = m.name.toLowerCase().replace(/[^a-z0-9]/g, '')
      ;(groups[key] ||= []).push(m)
    }
    const models = Object.entries(groups)
      .filter(([, v]) => v.length > 1)
      .map(([key, v]) => ({ key, items: v.map((m) => ({ id: m.id, label: m.name, demos: m.demo_count })) }))
    return { models, tasks: [], groups: models.length }
  },
  async mergeEntity(kind: 'models' | 'tasks', ident: string, payload: { target_id: number; dry_run?: boolean; reason?: string }): Promise<MergePreview> {
    await delay(240)
    // M3-B5：ident 支持 id 或 slug（管理端合并 UI 传 slug）
    const srcList = kind === 'models' ? mockModels : mockTasks
    const srcObj = srcList.find((x) => x.slug === ident || String(x.id) === ident)
    const srcId = srcObj?.id ?? Number(ident)
    if (!Number.isFinite(srcId) || srcId === payload.target_id) throw new Error('不能合并到自身')
    const ref = (id: number): MergePreview['source'] => {
      const m = mockModels.find((x) => x.id === id)
      if (m) return { id, slug: m.slug, name: m.name }
      const t = mockTasks.find((x) => x.id === id)
      return { id, slug: t?.slug, title: t?.title || `#${id}` }
    }
    const affected = kind === 'models' ? mockModels.find((m) => m.id === srcId)?.demo_count ?? 0 : 1
    if (payload.dry_run) {
      return { source: ref(srcId), target: ref(payload.target_id), affected_demos: affected, aliases_moved: 1, dry_run: true }
    }
    const row = mockModels.find((m) => m.id === srcId)
    const target = mockModels.find((m) => m.id === payload.target_id)
    if (row && target) {
      row.status = 'deprecated'
      target.demo_count += row.demo_count
      mockAliases[target.slug] = [...new Set([...(mockAliases[target.slug] || []), row.name, ...(mockAliases[row.slug] || [])])]
    }
    return { source: ref(srcId), target: ref(payload.target_id), affected_demos: affected, aliases_moved: 1, dry_run: false }
  },
  async addModelAlias(ident: string, alias: string): Promise<{ ok: boolean }> {
    await delay(180)
    const row = mockModels.find((m) => m.id === Number(ident))
    if (!row) throw new Error('实体不存在')
    mockAliases[row.slug] = [...new Set([...(mockAliases[row.slug] || []), alias])]
    return { ok: true }
  },
  async removeModelAlias(ident: string, alias: string): Promise<void> {
    await delay(180)
    const row = mockModels.find((m) => m.id === Number(ident))
    if (row) mockAliases[row.slug] = (mockAliases[row.slug] || []).filter((a) => a !== alias)
  },
  async getMergeHistory(): Promise<{ items: MergeHistoryItem[] }> {
    await delay(160)
    return { items: mockMergeHistory.map((x) => ({ ...x })) }
  },
  async unmergeEntity(ident: string | number, payload: { dry_run?: boolean; reason?: string }): Promise<UnmergePreview> {
    await delay(220)
    const id = Number(ident)
    const rec = mockMergeHistory.find((x) => x.source.id === id)
    if (!rec) throw new Error('该实体不处于「已被合并」状态，无需撤销')
    const src = mockModels.find((m) => m.id === id)
    const tgt = mockModels.find((m) => m.id === rec.target?.id)
    const view: UnmergePreview = {
      source: { id, slug: src?.slug, name: src?.name },
      target: rec.target ?? { id: 0 },
      moved_total: rec.moved_total,
      will_restore: rec.movable_back,
      already_moved_away: rec.moved_total - rec.movable_back,
      restored_status: rec.restored_status,
      reliable: rec.reliable,
      dry_run: !!payload.dry_run,
    }
    if (!payload.dry_run) {
      if (src) {
        src.status = rec.restored_status
        if (tgt) {
          tgt.demo_count = Math.max(0, tgt.demo_count - rec.movable_back)
          src.demo_count += rec.movable_back
        }
      }
      mockMergeHistory = mockMergeHistory.filter((x) => x.source.id !== id)
      view.unmerged = true
    }
    return view
  },
  async updateModel(ident: string | number, payload: { name?: string; vendor?: string; description?: string; slug?: string }): Promise<ModelBrief> {
    await delay(200)
    const row = mockModels.find((m) => m.id === Number(ident) || m.slug === String(ident))
    if (!row) throw new Error('模型不存在')
    if (payload.slug) {
      const clean = payload.slug.trim().toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/-{2,}/g, '-').replace(/^-|-$/g, '')
      if (!clean) throw new Error('slug 必须是 ASCII（字母数字与连字符）')
      if (clean !== payload.slug.trim()) throw new Error(`slug 含非法字符，建议用「${clean}」`)
      if (mockModels.some((m) => m.slug === clean && m.id !== row.id)) throw new Error(`slug「${clean}」已被其他实体占用`)
      mockAliases[clean] = [...new Set([...(mockAliases[clean] || []), row.slug])]
      row.slug = clean
    }
    if (payload.vendor !== undefined) row.vendor = payload.vendor
    if (payload.description !== undefined) row.description = payload.description
    if (payload.name && payload.name !== row.name) {
      mockAliases[row.slug] = [...new Set([...(mockAliases[row.slug] || []), row.name])]
      row.name = payload.name
    }
    return { id: row.id, slug: row.slug, name: row.name, vendor: row.vendor, status: row.status, resolution: row.resolution }
  },
  async getKnowledgeStats(): Promise<KnowledgeStats> {
    await delay(200)
    const approved = demos.filter((d) => d.status === 'approved').length
    const cov = (key: string) => {
      const n = demos.filter((d) => d.status === 'approved' && d.tags.some((x) => x.key === key)).length
      return { label: key, tier: key === 'model' ? 1 : key === 'type' ? 2 : 3, demos: n, rate: n / (approved || 1) }
    }
    return {
      demos_approved: approved,
      coverage: { model: cov('model'), type: cov('type'), category: cov('category'), game: cov('game'), rounds: cov('rounds') },
      model_entity: { demos: cov('model').demos, rate: cov('model').rate, total_models: 6, active: 5, candidate: 0, unverified: 1, deprecated: 0 },
      task: { total: mockTasks.length, active: mockTasks.length, candidate: 0 },
      inbox: { pending: mockSuggestions.filter((s) => s.status === 'pending').length, pending_actionable: {} },
      duplicate_slugs: 0,
    }
  },
  async getAudit(
    params: { action?: string; entity_type?: string; entity_id?: number; q?: string; page?: number; page_size?: number } = {},
  ): Promise<AuditList> {
    await delay(200)
    const items: AuditEntry[] = [
      // M2-t4：灰测池揭晓审计样例（100 天前自 ds-unknown 揭晓 → 概览台池卡 90 天红线可演）
      { id: 4, actor_type: 'user', actor_id: 1, actor: 'admin', action: 'attribute', entity_type: 'model', entity_id: 1, reason: '归属 3 个作品到 dsv4-flash（自 ds-unknown 揭晓）', created_at: new Date(Date.now() - 100 * 86400000).toISOString(), before: null, after: { target: 'dsv4-flash', moved: 3, from: ['ds-unknown'] } },
      { id: 3, actor_type: 'user', actor_id: 1, actor: 'admin', action: 'review', entity_type: 'suggestion', entity_id: 1, reason: '批准挂题请求', created_at: new Date().toISOString(), before: { status: 'pending' }, after: { status: 'approved', result: '挂题成功' } },
      { id: 2, actor_type: 'user', actor_id: 1, actor: 'admin', action: 'attribute', entity_type: 'model', entity_id: 1, reason: '归属 2 个作品到 dsv4-flash', created_at: new Date().toISOString(), before: null, after: { target: 'dsv4-flash', moved: 2, from: ['unspecified'] } },
      { id: 1, actor_type: 'user', actor_id: 1, actor: 'admin', action: 'create', entity_type: 'task', entity_id: 1, reason: '从题目候选成题', created_at: new Date().toISOString(), before: null, after: { slug: 'mc-web' } },
    ]
    const filtered = items.filter(
      (x) => (!params.action || x.action === params.action) && (!params.entity_type || x.entity_type === params.entity_type) && (!params.q || (x.reason || '').includes(params.q)),
    )
    return {
      items: filtered,
      total: filtered.length,
      page: params.page ?? 1,
      page_size: params.page_size ?? 50,
      actions: ['create', 'update', 'status_set', 'merge', 'alias_add', 'alias_remove', 'attach', 'detach', 'delete', 'review', 'attribute'],
      entity_types: ['model', 'task', 'suggestion'],
    }
  },

  async getForumTopic(id: number): Promise<ForumTopic | null> {
    await delay(150)
    return forumTopics.find((t) => t.id === id) || null
  },
  async listForumTopics(params: { q?: string; category?: string; tag?: string; demo?: string; sort?: 'newest' | 'popular' | 'replies' | 'hot'; sticky?: boolean; participated?: boolean; followed?: boolean; kind?: 'general' | 'demo'; page?: number; page_size?: number } = {}): Promise<Paginated<ForumTopic>> {
    await delay()
    const { q = '', category, tag, demo, sort = 'newest', page = 1, page_size = 20 } = params
    let items = [...forumTopics].filter((t) => t.status === 'normal')
    if (q) items = items.filter((t) => t.title.includes(q) || t.content.includes(q))
    if (category) items = items.filter((t) => t.category === category)
    if (tag) items = items.filter((t) => t.tags.includes(tag))
    if (demo) items = items.filter((t) => t.demo_slug === demo)
    if (params.kind === 'demo') items = items.filter((t) => !!t.demo_slug)
    if (params.kind === 'general') items = items.filter((t) => !t.demo_slug)
    // RF-5a：与后端 forum.py:126-136 对齐 —— popular=浏览、replies=回复数、
    // hot=回复数+浏览/50（时间衰减用 created_at 兜底）；原先 replies/hot 都被静默降级成置顶排序
    const byPinned = (a: ForumTopic, b: ForumTopic) => Number(b.sticky) - Number(a.sticky) || b.created_at.localeCompare(a.created_at)
    if (sort === 'popular') items.sort((a, b) => b.view_count - a.view_count || byPinned(a, b))
    else if (sort === 'replies') items.sort((a, b) => b.reply_count - a.reply_count || byPinned(a, b))
    else if (sort === 'hot')
      items.sort((a, b) => b.reply_count + b.view_count / 50 - (a.reply_count + a.view_count / 50) || byPinned(a, b))
    else items.sort((a, b) => Number(b.pinned) - Number(a.pinned) || byPinned(a, b))
    const start = (page - 1) * page_size
    return { items: clone(items.slice(start, start + page_size)), total: items.length, page, page_size }
  },
  async listForumReplies(topicId: number): Promise<ForumReply[]> {
    await delay()
    return clone(forumReplies.filter((r) => r.topic_id === topicId).sort((a, b) => a.created_at.localeCompare(b.created_at)))
  },
  async listForumRepliesPage(topicId: number, page = 1, pageSize = 50): Promise<Paginated<ForumReply>> {
    await delay()
    const items = clone(forumReplies.filter((r) => r.topic_id === topicId).sort((a, b) => a.created_at.localeCompare(b.created_at)))
    const start = (page - 1) * pageSize
    return { items: items.slice(start, start + pageSize), total: items.length, page, page_size: pageSize }
  },
  async createForumTopic(payload: ForumTopicInput): Promise<ForumTopic> {
    await delay(300)
    const t: ForumTopic = {
      id: Math.max(0, ...forumTopics.map((x) => x.id)) + 1,
      title: payload.title,
      content: payload.content || '',
      author: 'tester',
      author_id: 2,
      demo_slug: payload.demo_slug ?? null,
      category: payload.category || 'general',
      tags: payload.tags || [],
      pinned: false,
      sticky: false,
      locked: false,
      solved: false,
      status: 'normal',
      reply_count: 0,
      view_count: 0,
      like_count: 0,
      thanks_count: 0,
      my_reactions: [],
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
    forumTopics.unshift(t)
    return clone(t)
  },
  async createForumReply(topicId: number, content: string, parentId?: number): Promise<ForumReply> {
    await delay(200)
    const r: ForumReply = { id: Math.max(0, ...forumReplies.map((x) => x.id)) + 1, topic_id: topicId, author: 'tester', author_id: 2, content, parent_id: parentId ?? null, status: 'normal', created_at: new Date().toISOString() }
    forumReplies.push(r)
    const t = forumTopics.find((x) => x.id === topicId)
    if (t) t.reply_count += 1
    return clone(r)
  },
  async adminListForumTopics(params: { status?: string; category?: string; pinned?: boolean; page?: number; page_size?: number } = {}): Promise<Paginated<ForumTopic>> {
    await delay()
    let items = [...forumTopics]
    if (params.status) items = items.filter((t) => t.status === params.status)
    if (params.category) items = items.filter((t) => t.category === params.category)
    if (params.pinned !== undefined) items = items.filter((t) => t.pinned === params.pinned)
    return { items: clone(items), total: items.length, page: 1, page_size: items.length || 1 }
  },
  async adminUpdateForumTopic(id: number, patch: ForumTopicAdminUpdate): Promise<ForumTopic> {
    await delay()
    const t = forumTopics.find((x) => x.id === id)
    if (!t) throw new Error('主题不存在')
    Object.assign(t, patch)
    return clone(t)
  },
  async adminDeleteForumTopic(id: number): Promise<void> {
    await delay()
    const i = forumTopics.findIndex((x) => x.id === id)
    if (i >= 0) forumTopics.splice(i, 1)
  },
  async adminDeleteForumReply(id: number): Promise<void> {
    await delay()
    const i = forumReplies.findIndex((x) => x.id === id)
    if (i >= 0) forumReplies.splice(i, 1)
  },
  async toggleReaction(targetType: 'topic' | 'reply', targetId: number, reactionType: 'like' | 'thanks'): Promise<ReactionSummary & { active: boolean }> {
    await delay()
    const items = targetType === 'topic' ? forumTopics : forumReplies
    const it = items.find((x) => x.id === targetId) as any
    if (!it) throw new Error('内容不存在')
    it.like_count = it.like_count ?? 0
    it.thanks_count = it.thanks_count ?? 0
    it.my_reactions = it.my_reactions ?? []
    const idx = it.my_reactions.indexOf(reactionType)
    let active: boolean
    if (idx >= 0) {
      it.my_reactions.splice(idx, 1)
      active = false
      if (reactionType === 'like') it.like_count = Math.max(0, it.like_count - 1)
      else it.thanks_count = Math.max(0, it.thanks_count - 1)
    } else {
      it.my_reactions.push(reactionType)
      active = true
      if (reactionType === 'like') it.like_count += 1
      else it.thanks_count += 1
    }
    return { target_type: targetType, target_id: targetId, like_count: it.like_count, thanks_count: it.thanks_count, my_reactions: [...it.my_reactions], active }
  },
  async getUserProfile(username: string): Promise<UserProfile> {
    await delay()
    const u = users.find((x) => x.username === username)
    if (!u) throw new Error('用户不存在')
    return { id: u.id, username: u.username, role: u.role, status: u.status, bio: u.bio || '', display_name: u.display_name || '', avatar_url: u.avatar_url || '', created_at: u.created_at, reputation: 42, demo_count: u.demo_count ?? 0, topic_count: 2, reply_count: 5, follower_count: 3, following_count: 1, is_following: false, is_self: username === currentUser?.username }
  },
  async toggleFollow(_userId: number): Promise<FollowOut> {
    await delay()
    return { following: true, followers_count: 3, following_count: 1 }
  },
  async listFollowers(username: string): Promise<UserPublic[]> {
    await delay()
    const ids = username === 'alice' ? [1, 2] : [1, 3]
    return ids.map((id) => toUserPublic(users.find((u) => u.id === id)!))
  },
  async listFollowing(username: string): Promise<UserPublic[]> {
    await delay()
    const ids = username === 'admin' ? [2, 3] : [1]
    return ids.map((id) => toUserPublic(users.find((u) => u.id === id)!))
  },

  async adminReviewForumTopic(id: number, action: 'approve' | 'reject'): Promise<ForumTopic> {
    await delay()
    const t = forumTopics.find((x) => x.id === id)
    if (!t) throw new Error('主题不存在')
    t.status = action === 'approve' ? 'normal' : 'hidden'
    return clone(t)
  },
  async adminReviewForumReply(id: number, _action: 'approve' | 'reject'): Promise<ForumReply> {
    await delay()
    const r = forumReplies.find((x) => x.id === id)
    if (!r) throw new Error('回复不存在')
    return clone(r)
  },
  async adminListForumReplies(params: { topic_id?: number; status?: string } = {}): Promise<ForumReply[]> {
    await delay()
    let items = [...forumReplies]
    if (params.topic_id != null) items = items.filter((r) => r.topic_id === params.topic_id)
    if (params.status) items = items.filter((r) => r.status === params.status)
    return clone(items)
  },

  async listForumReports(): Promise<ForumReport[]> {
    await delay()
    return clone(forumReports)
  },
  async handleForumReport(id: number, action: 'resolve' | 'dismiss'): Promise<ForumReport> {
    await delay()
    const r = forumReports.find((x) => x.id === id)
    if (!r) throw new Error('举报不存在')
    r.status = action === 'resolve' ? 'resolved' : 'dismissed'
    return clone(r)
  },
  async createForumReport(payload: ForumReportInput): Promise<ForumReport> {
    await delay()
    const r: ForumReport = { id: Math.max(0, ...forumReports.map((x) => x.id)) + 1, target_type: payload.target_type, target_id: payload.target_id, reason: payload.reason, status: 'open', reporter_id: 2, created_at: new Date().toISOString() }
    forumReports.push(r)
    return clone(r)
  },
  async adminBanUser(_id: number): Promise<void> {
    await delay()
  },
  async listNotifications(params: { unread_only?: boolean; page?: number; page_size?: number } = {}): Promise<Notification[]> {
    await delay()
    let items = [...notifications]
    if (params.unread_only) items = items.filter((n) => !n.read)
    const start = ((params.page || 1) - 1) * (params.page_size || 20)
    return clone(items.slice(start, start + (params.page_size || 20)))
  },
  async getUnreadCount(): Promise<{ count: number }> {
    await delay(50)
    return { count: notifications.filter((n) => !n.read).length }
  },
  async markNotificationRead(id: number): Promise<Notification> {
    await delay()
    const n = notifications.find((x) => x.id === id)
    if (!n) throw new Error('通知不存在')
    n.read = true
    return clone(n)
  },
  async markAllNotificationsRead(): Promise<void> {
    await delay()
    for (const n of notifications) n.read = true
  },



  async updateDemo(slug: string, payload: UpdateDemoPayload, onProgress?: (percent: number) => void): Promise<void> {
    await delay(400)
    onProgress?.(100)
    const d = findDemo(slug)
    if (!d) throw new Error('Demo 不存在')
    if (payload.title) d.title = payload.title
    if (payload.description !== undefined) d.description = payload.description
    if (payload.demo_type) d.demo_type = payload.demo_type
    if (payload.external_url !== undefined) d.external_url = payload.external_url || null
    if (payload.prompt !== undefined) d.prompt = payload.prompt
    if (payload.video_url !== undefined) d.video_url = payload.video_url || null
    if (payload.tags) d.tags = payload.tags.map((t) => toTagRef(t))
  },

  async deleteDemo(slug: string): Promise<void> {
    await delay(200)
    const idx = demos.findIndex((d) => d.slug === slug)
    if (idx >= 0) demos.splice(idx, 1)
  },

  async downloadDemo(slug: string): Promise<void> {
    await delay(200)
    const d = findDemo(slug)
    if (d) d.download_count += 1
  },

  // KB-33：旧评论通道已废弃（后端 410 迁论坛），mock 不再提供这两个接口。

  // ---------- Session Logs ----------
  async listSessionLogs(slug: string): Promise<SessionLog[]> {
    await delay()
    return clone(sessionLogs[slug] || [])
  },

  async getSessionLog(slug: string, filename: string): Promise<string> {
    await delay()
    const key = `${slug}/${filename}`
    return sessionTexts[key] || `# ${filename}\n\n（暂无内容）`
  },

  // ---------- Admin ----------
  async adminDemos(): Promise<AdminDemo[]> {
    await delay()
    return clone(
      [...demos, ...pendingDemos].map((d) => {
        const cur = curationMap.get(d.slug)
        return {
          ...d,
          storage_size: 1024 * 20,
          inconsistency: false,
          sites: cur ? cur.sites.join(',') : 'deep',
          lang: cur?.lang ?? 'zh',
        }
      }),
    )
  },

  async adminUsers(): Promise<AdminUser[]> {
    await delay()
    return clone(users.map((u) => ({ ...u, demo_count: demos.filter((d) => d.author_id === u.id).length })))
  },

  // ---------- 首页策展池（07 §2.2 / T5·M5-F1；mock 内存态）----------
  async listFeatured(): Promise<FeaturedPool> {
    await delay(150)
    const items = featuredEntries.map((e, i) => {
      const d = findDemo(e.slug)
      if (!d) return null
      const base: DemoSummary = clone(d)
      return { ...base, id: e.id, featured_order: i + 1 }
    }).filter((x): x is AdminFeaturedItem => x !== null)
    return { items, total: items.length }
  },
  async addFeaturedDemo(payload: { slug?: string; demo_id?: number }): Promise<{ ok: boolean; slug: string; featured_order: number; total: number }> {
    await delay(200)
    const target = payload.slug ? findDemo(payload.slug) : undefined
    if (!target) throw new Error('Demo 不存在')
    if (target.status !== 'approved') throw new Error('仅已上架（approved）作品可进首页策展池')
    if (featuredEntries.some((e) => e.slug === target.slug)) throw new Error(`已在精选池：${target.slug}`)
    featuredEntries.push({ id: featuredNextId++, slug: target.slug })
    return { ok: true, slug: target.slug, featured_order: featuredEntries.length, total: featuredEntries.length }
  },
  async removeFeaturedDemo(demoId: number): Promise<{ ok: boolean; slug: string }> {
    await delay(150)
    const i = featuredEntries.findIndex((e) => e.id === demoId)
    if (i < 0) throw new Error('该作品不在精选池')
    const [gone] = featuredEntries.splice(i, 1)
    return { ok: true, slug: gone.slug }
  },
  async moveFeaturedDemo(demoId: number, direction: 'up' | 'down'): Promise<{ ok: boolean; slug: string; featured_order: number }> {
    await delay(150)
    const i = featuredEntries.findIndex((e) => e.id === demoId)
    if (i < 0) throw new Error('该作品不在精选池')
    const j = direction === 'up' ? i - 1 : i + 1
    if (j < 0 || j >= featuredEntries.length) throw new Error('已在边界，不能再移')
    ;[featuredEntries[i], featuredEntries[j]] = [featuredEntries[j], featuredEntries[i]]
    return { ok: true, slug: featuredEntries[i].slug, featured_order: i + 1 }
  },
  async heroFeaturedDemo(demoId: number): Promise<{ ok: boolean; slug: string; featured_order: number }> {
    await delay(150)
    const i = featuredEntries.findIndex((e) => e.id === demoId)
    if (i < 0) throw new Error('该作品不在精选池')
    if (i === 0) throw new Error('它已是 hero（池内第一件）')
    const [moved] = featuredEntries.splice(i, 1)
    featuredEntries.unshift(moved)
    return { ok: true, slug: moved.slug, featured_order: 1 }
  },

  async adminReview(): Promise<DemoDetail[]> {
    await delay()
    return clone(pendingDemos)
  },

  async adminApprove(idOrSlug: string | number, action: 'approve' | 'reject'): Promise<void> {
    await delay(200)
    const d = pendingDemos.find((x) => x.slug === idOrSlug)
    if (!d) throw new Error('待审 Demo 不存在')
    if (action === 'approve') {
      d.status = 'approved'
      demos.push(d)
      pendingDemos.splice(pendingDemos.indexOf(d), 1)
    } else {
      d.status = 'rejected'
      pendingDemos.splice(pendingDemos.indexOf(d), 1)
    }
  },

  // astra 橱窗策展（mock 记忆在 map；缺省 deep/zh 与后端默认一致）
  async setCuration(slug: string, body: { sites?: string[]; lang?: 'zh' | 'en' }): Promise<CurationResult> {
    await delay(200)
    const cur = curationMap.get(slug) || { sites: ['deep'], lang: 'zh' as const }
    const sites = body.sites ?? cur.sites
    if (!sites.length) throw new Error('sites 需为 deep/astra 的非空子集')
    const lang = body.lang ?? cur.lang
    curationMap.set(slug, { sites, lang })
    return { slug, sites: sites.join(','), lang }
  },

  async getSettings(): Promise<Settings> {
    await delay()
    return clone(settings)
  },

  async updateSettings(next: Settings): Promise<Settings> {
    await delay()
    settings = { ...settings, ...next }
    return clone(settings)
  },

  async ossSync(_force = false): Promise<{ started: boolean; job: OssSyncJob }> {
    await delay(300)
    return {
      started: true,
      job: {
        running: false,
        force: _force,
        total: demos.length,
        done: demos.length,
        ok: demos.length,
        fail: 0,
        covers_ok: 0,
        covers_fail: 0,
        current: '',
        last_error: '',
        started_at: Date.now(),
        finished_at: Date.now(),
      },
    }
  },

  async getOssSyncStatus(): Promise<OssSyncJob> {
    await delay(100)
    return {
      running: false,
      force: false,
      total: demos.length,
      done: demos.length,
      ok: demos.length,
      fail: 0,
      covers_ok: 0,
      covers_fail: 0,
      current: '',
      last_error: '',
      started_at: Date.now(),
      finished_at: Date.now(),
    }
  },

  async getAdminStats(): Promise<AdminStats> {
    await delay()
    const demos = forumTopics.length // 随便用现成数据表示
    return {
      demos: { total: demos, approved: 10, pending: 2, rejected: 1 },
      users: users.length,
      storage: { oss_enabled: false, mode: 'local', local_demos: demos, local_files: 0, local_size_bytes: 0 },
    }
  },
  async storageStatus(): Promise<{ oss_enabled: boolean; mode: string; local_demos: number; local_files: number; local_size_bytes: number }> {
    await delay(200)
    return { oss_enabled: false, mode: 'local', local_demos: demos.length, local_files: 0, local_size_bytes: 0 }
  },

  async updateUser(id: number, patch: Partial<Pick<User, 'role' | 'status'>>): Promise<User> {
    await delay(200)
    const u = users.find((x) => x.id === id)
    if (!u) throw new Error('用户不存在')
    Object.assign(u, patch)
    return clone(u)
  },

  // ---------- 公告 ----------
  async listAnnouncements(): Promise<Announcement[]> {
    await delay()
    return clone(announcements)
  },
  async adminListAnnouncements(params: { status?: string; category?: string; pinned?: boolean } = {}): Promise<Announcement[]> {
    await delay()
    let items = [...announcements]
    if (params.status) items = items.filter((a) => (a.status || 'published') === params.status)
    if (params.category) items = items.filter((a) => (a.category || 'general') === params.category)
    if (params.pinned !== undefined) items = items.filter((a) => !!a.pinned === params.pinned)
    return clone(items)
  },
  async createAnnouncement(payload: AnnouncementInput): Promise<Announcement> {
    await delay(200)
    const ann: Announcement = {
      id: Math.max(0, ...announcements.map((a) => a.id)) + 1,
      type: 'manual',
      title: payload.title,
      content: payload.content || '',
      demo_slug: payload.demo_slug ?? null,
      pinned: payload.pinned ?? false,
      status: payload.status ?? 'published',
      category: payload.category ?? 'general',
      published_at: payload.published_at ?? null,
      expires_at: payload.expires_at ?? null,
      created_by: currentUser?.id ?? null,
      created_at: new Date().toISOString(),
    }
    announcements.unshift(ann)
    return clone(ann)
  },
  async updateAnnouncement(id: number, payload: AnnouncementInput): Promise<Announcement> {
    await delay(200)
    const a = announcements.find((x) => x.id === id)
    if (!a) throw new Error('公告不存在')
    a.title = payload.title
    if (payload.content !== undefined) a.content = payload.content
    if (payload.demo_slug !== undefined) a.demo_slug = payload.demo_slug
    if (payload.pinned !== undefined) a.pinned = payload.pinned
    if (payload.status !== undefined) a.status = payload.status
    if (payload.category !== undefined) a.category = payload.category
    if (payload.published_at !== undefined) a.published_at = payload.published_at
    if (payload.expires_at !== undefined) a.expires_at = payload.expires_at
    return clone(a)
  },
  async deleteAnnouncement(id: number): Promise<void> {
    await delay(200)
    const idx = announcements.findIndex((a) => a.id === id)
    if (idx >= 0) announcements.splice(idx, 1)
  },
  // ── 收藏夹 ───────────────────────────────────────────────
  async listMyCollections(): Promise<CollectionOut[]> {
    await delay()
    const me = requireUser()
    defaultCollectionOf(me.username) // 保证默认夹存在（UI 一进来就能看到它）
    return collections.filter((c) => c.owner === me.username).map(toCollectionOut).sort((a, b) => Number(b.is_default) - Number(a.is_default) || b.updated_at.localeCompare(a.updated_at))
  },
  async createCollection(payload: { title: string; description?: string; visibility?: 'private' | 'public' }): Promise<CollectionOut> {
    await delay()
    const me = requireUser()
    const title = payload.title.trim()
    if (!title) throw new Error('名称不能为空')
    if (collections.filter((c) => c.owner === me.username).length >= 20) throw new Error('最多 20 个收藏夹')
    if (collections.some((c) => c.owner === me.username && c.title === title)) throw new Error('已有同名收藏夹')
    const c: MockCollection = {
      id: collectionSeq++,
      owner: me.username,
      title,
      description: payload.description?.trim() || '',
      visibility: payload.visibility || 'private',
      is_default: false,
      updated_at: new Date().toISOString(),
    }
    collections.push(c)
    return toCollectionOut(c)
  },
  async updateCollection(id: number, patch: { title?: string; description?: string; visibility?: 'private' | 'public' }): Promise<CollectionOut> {
    await delay()
    const me = requireUser()
    const c = collections.find((x) => x.id === id)
    if (!c || c.owner !== me.username) throw new Error('收藏夹不存在')
    if (patch.title !== undefined) {
      if (c.is_default) throw new Error('默认收藏夹不可改名')
      const t = patch.title.trim()
      if (!t) throw new Error('名称不能为空')
      c.title = t
    }
    if (patch.description !== undefined) c.description = patch.description.trim()
    if (patch.visibility !== undefined) {
      if (c.is_default) throw new Error('默认收藏夹不可转公开')
      c.visibility = patch.visibility
    }
    c.updated_at = new Date().toISOString()
    return toCollectionOut(c)
  },
  async deleteCollection(id: number): Promise<void> {
    await delay()
    const me = requireUser()
    const c = collections.find((x) => x.id === id)
    if (!c || c.owner !== me.username) throw new Error('收藏夹不存在')
    if (c.is_default) throw new Error('默认收藏夹不可删除')
    collections.splice(collections.indexOf(c), 1)
    for (let i = collectionItems.length - 1; i >= 0; i--) if (collectionItems[i].collection_id === id) collectionItems.splice(i, 1)
  },
  async listCollectionItems(id: number, params: { page?: number; pageSize?: number } = {}): Promise<Paginated<CollectionItemOut>> {
    await delay()
    const me = requireUser()
    const c = collections.find((x) => x.id === id)
    if (!c || c.owner !== me.username) throw new Error('收藏夹不存在')
    const page = params.page || 1
    const pageSize = params.pageSize || 20
    const rows = collectionItems.filter((i) => i.collection_id === id).sort((a, b) => b.added_at.localeCompare(a.added_at))
    const slice = rows.slice((page - 1) * pageSize, page * pageSize)
    return {
      items: slice.map((it) => ({ demo: clone(demos.find((d) => d.slug === it.slug)!), added_at: it.added_at })).filter((x) => x.demo),
      total: rows.length,
      page,
      page_size: pageSize,
    }
  },
  async addToCollection(id: number, slug: string): Promise<void> {
    await delay(120)
    const me = requireUser()
    const c = collections.find((x) => x.id === id)
    if (!c || c.owner !== me.username) throw new Error('收藏夹不存在')
    if (!demos.some((d) => d.slug === slug)) throw new Error('作品不存在')
    if (collectionItems.some((i) => i.collection_id === id && i.slug === slug)) return // 幂等
    const inThis = collectionItems.filter((i) => i.collection_id === id).length
    if (inThis >= 500) throw new Error('收藏夹已满（最多 500 件）')
    collectionItems.push({ collection_id: id, slug, added_at: new Date().toISOString() })
    c.updated_at = new Date().toISOString()
  },
  async removeFromCollection(id: number, slug: string): Promise<void> {
    await delay(120)
    const me = requireUser()
    const c = collections.find((x) => x.id === id)
    if (!c || c.owner !== me.username) throw new Error('收藏夹不存在')
    const i = collectionItems.findIndex((x) => x.collection_id === id && x.slug === slug)
    if (i >= 0) collectionItems.splice(i, 1)
    c.updated_at = new Date().toISOString()
  },
  async getFavoriteStatus(slug: string): Promise<FavoriteStatus> {
    await delay(80)
    if (!currentUser) return { favorited: false, collection_ids: [] }
    const ids = collections
      .filter((c) => c.owner === currentUser!.username && collectionItems.some((i) => i.collection_id === c.id && i.slug === slug))
      .map((c) => c.id)
    return { favorited: ids.length > 0, collection_ids: ids }
  },
  async toggleFavorite(slug: string, collectionId?: number): Promise<FavoriteStatus> {
    await delay(150)
    const me = requireUser()
    const target = collectionId ? collections.find((c) => c.id === collectionId && c.owner === me.username) : defaultCollectionOf(me.username)
    if (!target) throw new Error('收藏夹不存在')
    const has = collectionItems.some((i) => i.collection_id === target.id && i.slug === slug)
    if (has) await (this as unknown as { removeFromCollection: (a: number, b: string) => Promise<void> }).removeFromCollection(target.id, slug)
    else await (this as unknown as { addToCollection: (a: number, b: string) => Promise<void> }).addToCollection(target.id, slug)
    return (this as unknown as { getFavoriteStatus: (s: string) => Promise<FavoriteStatus> }).getFavoriteStatus(slug)
  },
  async listPublicCollections(username: string): Promise<CollectionOut[]> {
    await delay()
    return collections.filter((c) => c.owner === username && c.visibility === 'public').map(toCollectionOut)
  },
  async getPublicCollection(id: number): Promise<CollectionOut> {
    await delay()
    const c = collections.find((x) => x.id === id)
    // 私密夹对他人/匿名一律 404（不泄露"存在但无权"）
    if (!c) throw new Error('收藏夹不存在')
    const mine = currentUser && c.owner === currentUser.username
    if (c.visibility !== 'public' && !mine) throw new Error('收藏夹不存在')
    return toCollectionOut(c)
  },
  async listPublicCollectionItems(id: number, params: { page?: number; pageSize?: number } = {}): Promise<Paginated<CollectionItemOut>> {
    await delay()
    const c = collections.find((x) => x.id === id)
    const mine = currentUser && c && c.owner === currentUser.username
    if (!c || (c.visibility !== 'public' && !mine)) throw new Error('收藏夹不存在')
    const page = params.page || 1
    const pageSize = params.pageSize || 24
    const rows = collectionItems.filter((i) => i.collection_id === id).sort((a, b) => b.added_at.localeCompare(a.added_at))
    return {
      items: rows.slice((page - 1) * pageSize, page * pageSize).map((it) => ({ demo: clone(demos.find((d) => d.slug === it.slug)!), added_at: it.added_at })),
      total: rows.length,
      page,
      page_size: pageSize,
    }
  },

  // ── 浏览历史（服务端侧；匿名只写本机 localStorage，见 useLocalHistory） ──
  async recordView(slug: string): Promise<void> {
    await delay(60)
    if (!currentUser) return // 匿名：服务端一无所知
    if (!currentUser.history_enabled) return // 开关关闭：不新增（已有记录保留，清空需显式操作）
    const me = currentUser.username
    const i = viewHistory.findIndex((h) => h.user === me && h.slug === slug)
    const now = new Date().toISOString()
    if (i >= 0) viewHistory[i].viewed_at = now
    else viewHistory.push({ user: me, slug, viewed_at: now })
    const mine = viewHistory.filter((h) => h.user === me).sort((a, b) => b.viewed_at.localeCompare(a.viewed_at))
    for (const old of mine.slice(200)) viewHistory.splice(viewHistory.indexOf(old), 1) // 上限 200
  },
  async listHistory(params: { page?: number; pageSize?: number } = {}): Promise<Paginated<HistoryItemOut>> {
    await delay()
    const me = requireUser()
    const page = params.page || 1
    const pageSize = params.pageSize || 20
    const rows = viewHistory.filter((h) => h.user === me.username).sort((a, b) => b.viewed_at.localeCompare(a.viewed_at))
    return {
      items: rows.slice((page - 1) * pageSize, page * pageSize).map((h) => ({ demo: clone(demos.find((d) => d.slug === h.slug)!), viewed_at: h.viewed_at })).filter((x) => x.demo),
      total: rows.length,
      page,
      page_size: pageSize,
    }
  },
  async clearHistory(): Promise<void> {
    await delay(150)
    const me = requireUser()
    for (let i = viewHistory.length - 1; i >= 0; i--) if (viewHistory[i].user === me.username) viewHistory.splice(i, 1)
  },
  async deleteHistoryItem(slug: string): Promise<void> {
    await delay(100)
    const me = requireUser()
    const i = viewHistory.findIndex((h) => h.user === me.username && h.slug === slug)
    if (i >= 0) viewHistory.splice(i, 1)
  },

  // ── 资料与隐私 ───────────────────────────────────────────
  async updateMe(patch: MePatch): Promise<User> {
    await delay(150)
    const me = requireUser()
    if (patch.display_name !== undefined) me.display_name = patch.display_name.trim().slice(0, 64)
    if (patch.bio !== undefined) me.bio = patch.bio.trim().slice(0, 500)
    if (patch.history_enabled !== undefined) me.history_enabled = patch.history_enabled
    const live = users.find((u) => u.id === me.id)
    if (live) Object.assign(live, { display_name: me.display_name, bio: me.bio, history_enabled: me.history_enabled })
    return clone(me)
  },
  async uploadAvatar(file: File): Promise<{ avatar_url: string }> {
    await delay(400)
    const me = requireUser()
    // 口径与封面一致（后端 max_file_size=200MB）：端侧已先压成 512×512，这里只拦异常文件
    if (file.size > 200 * 1024 * 1024) throw new Error('图片过大（上限 200MB）')
    if (!/^image\/(png|jpe?g|webp)$/.test(file.type)) throw new Error('只支持 png/jpg/webp')
    // mock：直接用对象 URL（真实后端会裁成 256×256 落 /media/avatars/…）
    const url = URL.createObjectURL(file)
    me.avatar_url = url
    const live = users.find((u) => u.id === me.id)
    if (live) live.avatar_url = url
    return { avatar_url: url }
  },
  async removeAvatar(): Promise<void> {
    await delay(150)
    const me = requireUser()
    me.avatar_url = ''
    const live = users.find((u) => u.id === me.id)
    if (live) live.avatar_url = ''
  },
}

