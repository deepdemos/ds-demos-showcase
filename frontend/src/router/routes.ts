// 路由表（M4-E2 抽出：vitest 快照测试可零 DOM 导入；router/index.ts 消费同一份数据）
import type { RouteRecordRaw } from 'vue-router'

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: () => import('../views/HomeView.vue'), meta: { title: '首页', keepAlive: true } },
  { path: '/about', name: 'about', component: () => import('../views/AboutView.vue'), meta: { title: '关于本站' } },
  { path: '/demos', name: 'demos', component: () => import('../views/DemosView.vue'), meta: { title: '作品库', keepAlive: true, wide: true } },
  { path: '/prompts', name: 'prompts', component: () => import('../views/PromptsView.vue'), meta: { title: '提示词库', keepAlive: true, wide: true } },
  { path: '/prompts/new', name: 'prompt-new', component: () => import('../views/PromptNewView.vue'), meta: { title: '收录提示词', requiresAuth: true } },
  { path: '/prompts/:id', name: 'prompt-detail', component: () => import('../views/PromptDetailView.vue'), meta: { title: '提示词' } },
  { path: '/leaderboard', name: 'leaderboard', component: () => import('../views/LeaderboardView.vue'), meta: { title: '排行榜', keepAlive: true, wide: true } },
  { path: '/forum', name: 'forum', component: () => import('../views/ForumListView.vue'), meta: { title: '讨论区', forum: true, keepAlive: true } },
  { path: '/forum/topic/:id', name: 'forum-topic', component: () => import('../views/ForumTopicView.vue'), props: true, meta: { title: '主题', forum: true } },
  { path: '/forum/new', name: 'forum-new', component: () => import('../views/ForumNewView.vue'), meta: { title: '发帖', forum: true, requiresAuth: true } },
  // 独立预览页（P2）：无站点外壳（meta.bare）、一个键都不绑 —— 键盘全交给作品。
  // 放在 /demo/:slug 之前；props 传 slug；不 keepAlive（每次进来都该是干净的一次预览）。
  { path: '/demo/:slug/play', name: 'demo-play', component: () => import('../views/DemoPlayView.vue'), props: true, meta: { title: '预览', bare: true } },
  { path: '/demo/:slug', name: 'demo', component: () => import('../views/DemoView.vue'), props: true, meta: { title: 'Demo', hideTabBar: true } },
  { path: '/models', name: 'models', component: () => import('../views/ModelsView.vue'), meta: { title: '模型', keepAlive: true, wide: true } },
  { path: '/models/:slug', name: 'model-detail', component: () => import('../views/ModelDetailView.vue'), props: true, meta: { title: '模型' } },
  { path: '/tasks', name: 'tasks', component: () => import('../views/TasksView.vue'), meta: { title: '题目', keepAlive: true, wide: true } },
  // 独立出题放在 /tasks/:slug 之前：静态段优先，避免被参数路由吞并
  { path: '/tasks/new', name: 'task-new', component: () => import('../views/TaskNewView.vue'), meta: { title: '出题', requiresAuth: true } },
  { path: '/tasks/:slug', name: 'task-detail', component: () => import('../views/TaskDetailView.vue'), props: true, meta: { title: '题目' } },
  // v2 D3：/tags 原地升级为「探索」（URL 不变保外链兼容），旧的键浏览页下移到 /tags/keys
  { path: '/tags', name: 'explore', component: () => import('../views/ExploreView.vue'), meta: { title: '探索', keepAlive: true, wide: true } },
  { path: '/tags/keys', name: 'tag-keys', component: () => import('../views/TagListView.vue'), meta: { title: '标签', keepAlive: true } },
  { path: '/tag/:k/:v', name: 'tag-detail', component: () => import('../views/TagDetailView.vue'), props: true, meta: { title: '标签详情' } },
  { path: '/login', name: 'login', component: () => import('../views/LoginView.vue'), meta: { title: '登录' } },
  { path: '/register', name: 'register', component: () => import('../views/RegisterView.vue'), meta: { title: '注册' } },
  { path: '/user/:username', name: 'user', component: () => import('../views/UserView.vue'), props: true, meta: { title: '用户' } },
  // M0 名单页：与 /user/:u 同语义公开可看（路由段数不同，无参数吞并冲突）
  { path: '/user/:username/followers', name: 'user-followers', component: () => import('../views/FollowListView.vue'), props: (route) => ({ username: String(route.params.username), mode: 'followers' as const }), meta: { title: '粉丝' } },
  { path: '/user/:username/following', name: 'user-following', component: () => import('../views/FollowListView.vue'), props: (route) => ({ username: String(route.params.username), mode: 'following' as const }), meta: { title: '关注' } },
  { path: '/author/public', name: 'public-author', component: () => import('../views/PublicView.vue'), meta: { title: '公开用户' } },
  // 收藏夹（需登录）—— /me/history 不在此列：匿名也要能看"本机记录"（混合存储的匿名半边）
  { path: '/me/collections', name: 'me-collections', component: () => import('../views/MeCollectionsView.vue'), meta: { title: '收藏夹', requiresAuth: true } },
  { path: '/me/collections/:id', name: 'me-collection-detail', component: () => import('../views/MeCollectionDetailView.vue'), props: true, meta: { title: '收藏夹', requiresAuth: true } },
  { path: '/me/history', name: 'me-history', component: () => import('../views/MeHistoryView.vue'), meta: { title: '浏览历史' } },
  // 公开侧：某人的公开收藏夹 + 分享落地页（都是可匿名的）
  { path: '/user/:username/collections', name: 'user-collections', component: () => import('../views/PublicCollectionsView.vue'), props: true, meta: { title: '公开收藏夹' } },
  { path: '/collections/:id', name: 'collection', component: () => import('../views/PublicCollectionView.vue'), props: true, meta: { title: '收藏夹' } },
  { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue'), meta: { title: '账户设置', requiresAuth: true } },
  { path: '/notifications', name: 'notifications', component: () => import('../views/NotificationsView.vue'), meta: { title: '通知', requiresAuth: true } },
  { path: '/upload', name: 'upload', component: () => import('../views/UploadView.vue'), meta: { title: '上传 Demo', remountOnQuery: true } },
  { path: '/admin', name: 'admin', component: () => import('../views/AdminView.vue'), meta: { title: '管理后台', requiresAuth: true, requiresAdmin: true } },
  // 赞助/致谢已并入管理后台面板；旧地址保留重定向（书签与外链不该突然死）
  { path: '/admin/sponsors', redirect: { path: '/admin', query: { tab: 'sponsors' } } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('../views/NotFoundView.vue'), meta: { title: '404' } },
]
