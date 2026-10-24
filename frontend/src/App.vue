<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from './stores/auth'
import { api } from './api'
import { isMock } from './api'
import { lang, setLang, t } from './i18n'
import { adminExempt, applyServerFunMode, funEffective, titleBase } from './utils/funMode'
import { applyThemePreviewFromUrl, getEffectiveTheme, initTheme, setTheme } from './utils/theme'
import { useQueues } from './composables/adminQueues'
import ConfirmHost from './components/ConfirmHost.vue'
import ToastHost from './components/ToastHost.vue'
import ForumHeader from './components/ForumHeader.vue'
import NotificationBell from './components/NotificationBell.vue'
import AppTabBar from './components/AppTabBar.vue'
import SearchOverlay from './components/SearchOverlay.vue'
import { openSearch } from './composables/useSearch'

const auth = useAuthStore()
const route = useRoute()
const username = computed(() => auth.user?.username ?? '')
// C 项：≤720 移动判定（与站点断点同值；覆盖层/抽屉同款）。用于仅 <720 渲染移动搜索钮，
// 避免 topnav-desktop 被全局隐藏时出现的双入口问题。
const MOB_MQ = '(max-width: 720px)'
const isMobile = ref(typeof window !== 'undefined' && window.matchMedia ? window.matchMedia(MOB_MQ).matches : false)
function onMobMq(e: MediaQueryListEvent) {
  isMobile.value = e.matches
}
let mobMql: MediaQueryList | null = null
const keepAlivePages = ['HomeView', 'DemosView', 'TagListView', 'LeaderboardView', 'ForumListView', 'ModelsView', 'TasksView', 'ExploreView']
// 保留页按 name 做 key（同页返回复用实例）；其他页按 fullPath（参数变化强制重挂载）
// key 只用 path，不用 fullPath：
// fullPath 当 key 会让「同一路径只改 query」（排序/筛选/分页/切面板）**整页重挂** ——
// 表现为滚动弹回顶部、已填内容丢失、请求重发。各视图改为自己 watch 关心的 query。
// 例外：meta.remountOnQuery 的页面（如 /upload?slug=xxx —— 它的身份就是那个 query）保留重挂。
const pageKey = computed(() => {
  if (route.meta.keepAlive) return route.name as string
  return route.meta.remountOnQuery ? route.fullPath : (route.path as string)
})
// 整活模式：品牌文案随全站开关切换（/admin 豁免，恒显真实值）
const funOn = funEffective

watch(
  () => route.fullPath,
  () => {
    adminExempt.value = route.path.startsWith('/admin')
  },
  { immediate: true }, // 直接以 /admin 打开时也要立即生效
)

// 全站整活开关：拉 site-info 校正（60s 缓存，轻量）；失败保持 localStorage 预 seed
onMounted(() => {
  api
    .getSiteInfo()
    .then((info) => applyServerFunMode(!!info.display?.fun_mode))
    .catch(() => undefined)
})

// 用户菜单（03 §2.4）：admin 从顶栏移入下拉，「管理工作台」徽章 = 待办合计，
// 数字走 adminQueues 单一口径（useQueues().totalMust，与后台侧栏/概览台同源，不自算）
const { totalMust: adminQueueTotal, refresh: refreshQueues } = useQueues()
const userMenuOpen = ref(false)
const userMenuRoot = ref<HTMLElement | null>(null)
function toggleUserMenu() {
  userMenuOpen.value = !userMenuOpen.value
}
function onDocClick(e: MouseEvent) {
  if (userMenuOpen.value && userMenuRoot.value && !userMenuRoot.value.contains(e.target as Node)) {
    userMenuOpen.value = false
  }
}
watch(
  () => route.fullPath,
  () => {
    userMenuOpen.value = false
  },
)
onMounted(() => {
  document.addEventListener('click', onDocClick)
  mobMql = window.matchMedia(MOB_MQ)
  mobMql.addEventListener('change', onMobMq)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  mobMql?.removeEventListener('change', onMobMq)
})
// 管理员：进站即拉一次队列计数（badge 合计的数据源；去重逻辑在 composable 内；
// 用 watch 而非 onMounted——首屏时 fetchMe 可能尚未返回，身份就绪后再拉）
watch(
  () => auth.user,
  (u) => {
    if (u?.role === 'admin') void refreshQueues().catch(() => undefined)
  },
  { immediate: true },
)

// 语言切换：ref 响应式驱动全站（含 keepAlive 页）；<html lang> 同步
const switchLang = () => setLang(lang.value === 'en' ? 'zh' : 'en')
watchEffect(() => {
  document.documentElement.lang = lang.value === 'en' ? 'en' : 'zh-CN'
})

// 主题（04 §3.5/03 §11.3 仲裁版）：接管 FOUC + ?theme= 预览；按钮 paper↔ink 循环（语言按钮同规格）
const themeNow = ref<ReturnType<typeof getEffectiveTheme>>(getEffectiveTheme())
const themeTitle = computed(() =>
  themeNow.value === 'ink'
    ? t('app.theme.toPaper', '当前墨黑，点击换纸白')
    : t('app.theme.toInk', '当前纸白，点击换墨黑'),
)
function cycleTheme() {
  setTheme(themeNow.value, { cycle: true })
  themeNow.value = getEffectiveTheme()
}

// P2-1 转场例外名（03 §12.6）：forum 双皮壳（自带 forum-takeover 登场）/ upload remountOnQuery
// （query 即身份，向导步进不重播转场）→ page-cut 空名 = 零类定义硬切。
const pageTransitionName = computed(() => (route.meta.forum || route.meta.remountOnQuery ? 'page-cut' : 'page'))
onMounted(() => {
  initTheme()
  applyThemePreviewFromUrl()
  themeNow.value = getEffectiveTheme()
})
</script>

<template>
  <!-- P3-fix：宽档挂在外壳根上（自定义属性 --w-shell 由此继承给顶栏/主区/页脚三处）——
       原先只把 main 换成 --w-wide，顶栏与页脚仍是 1280：1440 屏内容比顶栏宽 57px、
       1920 屏宽 80px（用户实测报的就是这条）。外壳宽度只能有一个真源。 -->
  <div class="app-shell" :class="{ 'forum-shell': route.meta.forum, 'shell-wide': route.meta.wide, 'no-tabbar': route.meta.hideTabBar }">
    <!-- P2：meta.bare（独立预览页）连顶栏都不渲染 —— 那一页的目标是"无干扰地玩" -->
    <template v-if="!route.meta.forum && !route.meta.bare">
    <header class="topbar container">
      <RouterLink to="/" class="brand">
        <span class="brand-mark" aria-hidden="true"></span>
        <!-- <br> 写模板字面量（{{ }} 插值会转义 HTML） -->
        <span v-if="funOn && lang === 'en'" class="brand-name">astra canary<br />collection</span>
        <span v-else-if="funOn" class="brand-name">astra 灰测<br />作品收集</span>
        <span v-else-if="lang === 'en'" class="brand-name">AI Demo<br />Makers</span>
        <span v-else class="brand-name">AI 全民<br />制作人</span>
      </RouterLink>

      <!-- T7 v3（用户二轮反馈①③）：论坛出顶栏（首页纸条+footer+404 地图+TabBar 社区承接），
           ⌕ 占论坛原槽位（SearchOverlay openSearch 复用——/ 与 ⌘K 仍由覆盖层自持）。
           提示词库为一级入口（供直接复制与复测）；「关于」迁出顶栏 → footer + 用户菜单 + 404 地图。
           IA 约定（PR #17 review）：桌面顶栏主入口 ≤4（作品库/提示词库/探索/⌕）——排行榜迁 footer 承接
           （/leaderboard 路由与键不动）；T9 反馈②：⌕ 去卡片化=裸图标链（hover 变色+3px 下划线），
           去 btn-outline 边框卡片感（06 §P2 的『唯一带框件』分工被用户实测否决） -->
      <nav class="topnav topnav-desktop">
        <RouterLink class="nav-link" to="/demos">{{ t('app.nav.demos', '作品库') }}</RouterLink>
        <RouterLink class="nav-link" to="/prompts">{{ t('app.nav.prompts', '提示词库') }}</RouterLink>
        <RouterLink class="nav-link" to="/tags">{{ t('app.nav.explore', '探索') }}</RouterLink>
        <button class="nav-link topnav-search" type="button" :aria-label="t('search.title', '全局搜索')" :title="t('search.openTip', '全局搜索（快捷键 /）')" @click="openSearch">
          <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
            <circle cx="10.5" cy="10.5" r="6.5" fill="none" stroke="currentColor" stroke-width="2.6" />
            <path d="M15.5 15.5 21 21" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" />
          </svg>
        </button>
      </nav>

      <!-- M2-1 右簇降级修正：工具簇不再整体隐藏（topnav-desktop 会连主题/语言一起藏掉，
           违反「<720 顶栏只留品牌+主题/语言」）——改用 topnav-tools 常显，子项各自降级：
           auth-cluster（App.vue scoped @720 隐藏）+ CTA（topnav-desktop 类，全局 @720 隐藏）；
           桌面 .topnav 基础 flex + scoped gap10 与原逐字节等价。 -->
      <div class="topnav topnav-tools">
        <!-- T7 v3：工具簇 ⌕ 撤销（迁 nav 位，别留双入口）——右簇瘦身=主题/语言/铃铛+用户菜单+CTA -->
        <!-- C 项：移动全局搜索入口。<720 时 nav.topnav-desktop 被全局隐藏（styles/layouts/topbar.css），
             Nav 里的 ⌕ 随之消失，触屏又无硬件键盘的 ⌘K / / —— 此前移动端全局搜索实际不可达。
             这里在常显的 .topnav-tools 里补同一个⌕，只在 <720 渲染（断点型 isMobile 判断），
             ≥721 由上面 nav.topnav-desktop 里的同款承接——同一时刻只存在一个入口。 -->
        <button
          v-if="isMobile"
          class="nav-link topnav-search"
          type="button"
          :aria-label="t('search.title', '全局搜索')"
          :title="t('search.openTip', '全局搜索（快捷键 /）')"
          @click="openSearch"
        >
          <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
            <circle cx="10.5" cy="10.5" r="6.5" fill="none" stroke="currentColor" stroke-width="2.6" />
            <path d="M15.5 15.5 21 21" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" />
          </svg>
        </button>
        <button class="btn btn-sm btn-outline" type="button" :title="themeTitle" @click="cycleTheme">
          {{ themeNow === 'ink' ? '纸' : '墨' }}
        </button>
        <button class="btn btn-sm btn-outline" type="button" :title="lang === 'en' ? '切换到中文' : 'Switch to English'" @click="switchLang">
          {{ lang === 'en' ? '中文' : 'EN' }}
        </button>
        <!-- M1-fix-10：GitHub 图标链移出 header → footer（仓库链接是页脚惯例，右缘工具区只留主题/语言/铃铛+用户菜单+CTA） -->
        <!-- M2-1 顶栏移动降级（任务书）：<720 只留品牌+主题/语言——auth 簇与 CTA 收起：
             铃铛/用户菜单/登录注册 → 「我的」Tab 内聚；CTA → TabBar 中央 FAB。
             display:contents 保桌面 flex 布局逐字节不变（子项仍直接参与 space-between/gap）。 -->
        <template v-if="auth.isLoggedIn()">
          <span class="auth-cluster">
            <!-- T9 反馈③：铃铛并入 btn-sm 描边族（用例级 class 合并——TabBar/forum 壳的 .notif-bell 旧档不受影响） -->
            <NotificationBell class="btn btn-sm btn-outline" />
            <!-- 用户菜单（03 §2.4）：admin 从顶栏移入下拉；徽章合计走 adminQueues 单一口径；
                 T9 反馈③：trigger 并入 btn-sm 描边族（同盒法规，裸字形态废除） -->
            <div ref="userMenuRoot" class="user-menu">
              <button class="user-menu-trigger btn btn-sm btn-outline" type="button" :aria-expanded="userMenuOpen" @click="toggleUserMenu">
                {{ username }}
                <span v-if="auth.isAdmin() && adminQueueTotal > 0" class="user-menu-dot" aria-hidden="true"></span>
                <span class="user-menu-caret" aria-hidden="true">▾</span>
              </button>
              <Transition name="user-menu-pop">
                <div v-if="userMenuOpen" class="user-menu-panel">
                  <!-- M2-1 顺手修：en 词表里该词条在 app.profile（原键 app.menu.profile 走查中永远回落中文） -->
                  <RouterLink class="user-menu-item" :to="`/user/${username}`">{{ t('app.profile', '个人主页') }}</RouterLink>
                  <RouterLink v-if="auth.isAdmin()" class="user-menu-item" to="/admin">
                    {{ t('app.nav.workbench', '管理工作台') }}
                    <span v-if="adminQueueTotal > 0" class="user-menu-badge">{{ adminQueueTotal }}</span>
                  </RouterLink>
                  <!-- t36 收纳去处：窄桌面（≤1120）顶栏「关于」收进用户菜单（键复用 app.nav.about） -->
                  <RouterLink class="user-menu-item" to="/about">{{ t('app.nav.about', '关于本站') }}</RouterLink>
                  <button class="user-menu-item user-menu-quit" type="button" @click="auth.logout()">{{ t('app.nav.logout', '退出') }}</button>
                </div>
              </Transition>
            </div>
          </span>
        </template>
        <template v-else>
          <span class="auth-cluster">
            <RouterLink class="nav-link" to="/login">{{ t('app.nav.login', '登录') }}</RouterLink>
            <RouterLink class="nav-link" to="/register">{{ t('app.nav.register', '注册') }}</RouterLink>
          </span>
        </template>
        <!-- 上传 Demo 升为主 CTA（03 §2.4：单一主动作，不与信息项竞争；URL /upload 不变）；移动端由 TabBar FAB 承接 -->
        <RouterLink class="btn btn-sm btn-primary topnav-cta topnav-desktop" to="/upload">{{ t('app.nav.upload', '上传 Demo') }}</RouterLink>
      </div>
    </header>
    </template>

    <ForumHeader v-if="route.meta.forum" />

    <div v-if="isMock" class="container">
      <div class="notice notice-warn" style="margin-top: var(--sp-14)">
        <span v-if="lang === 'en'">{{ t('app.mockNotice', '') }}</span>
        <span v-else><strong>Mock 模式</strong>：当前使用内置占位数据，未连接后端。设置 <code>VITE_USE_MOCK=false</code> 后切换到真实 API。</span>
      </div>
    </div>

    <!-- P3：宽度真源是 --w-shell（顶栏/主区/页脚同宽，由 .app-shell 上的 .shell-wide 决定）。
         P2：meta.bare（独立预览页）不套 .container —— 那一页要满视口，不能被 1280/1440 截住。 -->
    <main
      :class="route.meta.bare ? 'bare-main' : 'container'"
      :style="{ flex: 1 }"
    >
      <RouterView v-slot="{ Component }">
        <!-- P2-1 页面转场（04 §2.3.1 可抄范式）：Transition 必须包在 KeepAlive 外层，
             mode="out-in" = 旧页 0ms 硬切消失（.page-leave-active transition:none）+ 新页 stamp-lite 250ms 登场；
             KeepAlive 命中复用不重播 enter、重新插入时播（符合「转场=页面级登场」语义）；
             scrollBehavior 协同：savedPosition/top:0 在新页插入时生效（leave 0ms 无延迟），入场动画叠加不抢滚动。 -->
        <Transition :name="pageTransitionName" mode="out-in">
            <KeepAlive :include="keepAlivePages">
              <component :is="Component" :key="pageKey" />
            </KeepAlive>
        </Transition>
      </RouterView>
    </main>

    <template v-if="!route.meta.forum && !route.meta.bare">
      <footer class="footer container">
        <!-- M1-fix-10 减法去处：关于/GitHub 从顶栏迁入页脚（低频件+仓库链接是页脚惯例；
             forum 双皮壳页无 footer——仓库链在该壳缺席，回执记录） -->
        <div class="footer-links">
          <RouterLink class="footer-link" to="/about">{{ t('app.nav.about', '关于本站') }}</RouterLink>
          <span class="footer-links-div" aria-hidden="true"></span>
          <!-- T7 v3：论坛出顶栏后的 footer 承接（可达性集=footer+论坛直链+404 地图+首页纸条） -->
          <RouterLink class="footer-link" to="/forum">{{ t('app.nav.forum', '论坛') }}</RouterLink>
          <span class="footer-links-div" aria-hidden="true"></span>
          <!-- IA 收纳（PR #17 review）：排行榜出顶栏进 footer，/leaderboard 路由与键不变 -->
          <RouterLink class="footer-link" to="/leaderboard">{{ t('app.nav.leaderboard', '排行榜') }}</RouterLink>
          <span class="footer-links-div" aria-hidden="true"></span>
          <a class="footer-link" href="https://github.com/liceses/ds-demos-showcase" target="_blank" rel="noopener" :title="t('app.github', 'GitHub 仓库')">
            GitHub <span class="footer-ext" aria-hidden="true">↗</span>
          </a>
        </div>
        <div class="mono">{{ titleBase }} · {{ t('app.footerTail', 'AI 网页 Demo 作品集') }}</div>
        <div class="mono">{{ t('app.footerDisclaimer', '时间线仅表示创建/更新记录，不等同于 AI 生成真实性证明') }}</div>
      </footer>
    </template>

    <!-- M2-1 移动 TabBar（03 §10.2）：非 forum 双皮壳壳层挂载；<720 渲染，桌面零渲染。
         meta.hideTabBar：作品详情自带贴底动作条（含返回），两条叠加会占掉手机屏约 15% -->
    <AppTabBar v-if="!route.meta.forum && !route.meta.bare && !route.meta.hideTabBar" />

    <ConfirmHost />
    <ToastHost />
    <!-- M2-3 全局搜索覆盖层（03 §12.1）：App 根一次挂载 + 组件内 Teleport to body——全路由可用（含 forum 双皮壳） -->
    <SearchOverlay />
  </div>
</template>

<style scoped>
/* ---- M2-1 移动 TabBar 壳层配合（03 §10.2 + 任务书降级裁决）----
   <720 顶栏只留品牌+主题/语言：auth 簇（铃铛/用户菜单/登录注册）display:none 收进
   「我的」Tab 内聚，CTA 隐藏（topnav-desktop 类）由中央 FAB 承接；
   桌面 display:contents——子项照旧直接参与 .topnav 的 flex/gap，布局逐字节不变。 */
.auth-cluster {
  display: contents;
}
/* T7 v3 三段对称栅格：[1fr auto 1fr]=品牌|中导航|右簇——中导航几何居中（CDP 验收偏差 ≤8px）。
   scoped display:grid 特异性压过全局 .topbar 的 flex；全局媒体规则里的 flex-direction 随 display:grid 失效无害。
   三个子项必须显式指定列：display:none 的项不占格，<720 隐藏中导航后右簇会被自动放进 auto 中列，
   品牌被挤在左列里折成三行。 */
.topbar {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
}
.topbar > .brand {
  grid-column: 1;
  justify-self: start;
}
.topbar > nav.topnav-desktop {
  grid-column: 2;
  justify-self: center;
}
.topbar > .topnav-tools {
  grid-column: 3;
  justify-self: end;
}
@media (max-width: 720px) {
  .topbar {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .topbar > .topnav-tools {
    grid-column: 2;
  }
  .auth-cluster {
    display: none;
  }
  /* 固定底栏让位：footer 尾部不被 TabBar 遮挡（forum 壳、hideTabBar 页无 TabBar 不让位） */
  .app-shell:not(.forum-shell):not(.no-tabbar) {
    padding-bottom: var(--tabbar-h-safe);
  }
}

/* 用户菜单（M1-1，03 §2.4）：样式组件级（style.css 冻结令生效中，令牌经 var() 引用全局既有值并带回落） */
.user-menu {
  position: relative;
}
/* T9 反馈③：右簇统一 btn-sm 描边族——铃铛/trigger 的盒法规收敛（scoped 压过全局 .notif-bell
   2px 纸底旧档；该旧档仍服务 TabBar/forum 壳用例，不删）。CTA=btn-primary 红底同盒法规（同高同描边，
   只以色/底区分主次）无需改。统一后右簇=同边框律/同 padding/同影档/同 hover（抬起+影），只字色与图标异。 */
.topbar .notif-bell {
  padding: 7px 13px;
  border: var(--border-w, 3px) solid var(--ink, #000);
  background: var(--wash-yellow, #ffe66d);
  box-shadow: var(--shadow-lg, 6px 6px 0 0 #000);
  /* T17 验收复扫（B1 残差实锤）：铃铛 18px svg 图标内容驱动 40>39，min-height 兜不住下限之上的内容
     （svg 18+14 padding+8 border=40；字面钮内容 17 恰 39）——盒高钉回 39 主档：图标 flex 居中不动，
     越出 1px 收进 7px padding 内不可见，右簇四钮等高。 */
  height: 39px;
}
/* T16（M4-2c）右簇盒高残差归一：t9 实测墨 39/EN 38/CTA 39——同 padding/边框/字号下
   line-height:normal 随 CJK/拉丁字体度量浮动 1px。min-height 归一到 39px 主档，
   内容 flex 居中，字形渲染差异不再进盒高。 */
.topbar .topnav-tools .btn {
  min-height: 39px;
}
@media (hover: hover) {
  .topbar .notif-bell:hover {
    transform: translate(-2px, -2px);
    box-shadow: var(--shadow-lg, 6px 6px 0 0 #000);
  }
}
.user-menu-trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-6);
  cursor: pointer;
}
.user-menu-caret {
  font-size: var(--fs-10);
}
/* 管理员有待办时的红点提示（点击展开看合计） */
.user-menu-dot {
  width: 8px;
  height: 8px;
  background: var(--red, #ff6b6b);
  border: 2px solid var(--ink, #000);
}
.user-menu-panel {
  position: absolute;
  right: 0;
  top: calc(100% + 8px);
  z-index: var(--z-dropdown);
  min-width: 180px;
  display: flex;
  flex-direction: column;
  background: var(--paper, #fff);
  border: var(--border-w, 4px) solid var(--ink, #000);
  box-shadow: 6px 6px 0 0 rgba(0, 0, 0, 1);
}
.user-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-10);
  padding: var(--sp-10) var(--sp-14);
  color: var(--ink, #000);
  text-decoration: none;
  font-weight: 700;
  font-size: var(--fs-14);
  background: none;
  border: none;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
}
.user-menu-item:hover {
  background: var(--paper-deep, #f2eee6);
}
.user-menu-quit {
  border-top: 2px solid var(--ink, #000);
}
.user-menu-badge {
  min-width: 22px;
  padding: 1px var(--sp-6);
  text-align: center;
  background: var(--red, #ff6b6b);
  color: var(--on-accent, #000);
  border: 2px solid var(--ink, #000);
  font-size: var(--fs-12);
  font-weight: 900;
}
.topnav-cta {
  margin-left: var(--sp-14); /* t36 实测：CTA 与认证簇仅隔 2px——转化主件脱离按钮堆，独立成组 */
}
/* ---- M1-fix-10 topnav 间距重排（t36 CDP 实测驱动）----
   实况：容器 1280 有 356px 余量但簇内全挤——nav 簇 gap 8px（文字间距 32px）、
   右簇 gap 8px、CTA 距注册 2px。修法=簇内呼吸，不动 space-between 的簇间分配。 */
.topbar nav.topnav {
  gap: var(--sp-14); /* 导航簇 8→14：文字间距 32→38px，静默文字链需要呼吸感 */
}
.topbar .topnav.topnav-tools {
  gap: var(--sp-10); /* 右簇（功能钮）8→10：边框件之间留一线（原 topnav-desktop 选择器随 M2-1 右簇改名而更新） */
}

/* 弹层登场（编排类豁免口径，R7 白名单内） */
.user-menu-pop-enter-active {
  transition: opacity 150ms ease, transform 150ms ease;
}
.user-menu-pop-enter-from {
  opacity: 0;
  transform: translateY(-4px);
}
.user-menu-pop-leave-active {
  transition: none;
}
.user-menu-pop-leave-to {
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .user-menu-pop-enter-active {
    transition: none;
  }
}

/* ---- M1-H1 header 静默化（05 §2.1/§5）：覆盖全局 nav-link 的荧光黄框 → 静默文字链 ----
   muted→ink 色阶；当前页 = ink + 3px 实线下划；边框只留给功能件（CTA/主题/铃铛）。 */
.topbar .nav-link {
  border-color: transparent;
  background: none;
  color: var(--ink-soft, #555);
}
.topbar .nav-link:hover {
  color: var(--ink, #000);
  border-color: transparent;
  background: none;
  text-decoration: underline;
  text-decoration-thickness: 3px;
  text-underline-offset: 6px;
}
/* exact-active：避免「/」链接在全站都被 inclusive 匹配点亮（首页只在本页亮） */
.topbar .nav-link.router-link-exact-active {
  color: var(--ink, #000);
  border-color: transparent;
  background: none;
  text-decoration: underline;
  text-decoration-thickness: 3px;
  text-underline-offset: 6px;
}
/* T9 反馈②：⌕ 裸图标链——图标块状对齐；hover/焦点=3px 墨条下划（图标无文本，
   text-decoration 不生效，借 nav-link 既有 3px 透明底边框翻色=同语汇的图标形态） */
.topbar .nav-link.topnav-search {
  display: inline-flex;
  align-items: center;
}
.topbar .nav-link.topnav-search svg {
  display: block;
}
.topbar .nav-link.topnav-search:hover,
.topbar .nav-link.topnav-search:focus-visible {
  border-bottom-color: var(--ink, #000);
  text-decoration: none;
}


/* ---- M1-fix-10 header 减法（用户二次裁决：不是间距是数量）----
   顶栏信息项 6→4（作品库/探索/排行榜/论坛）：品牌 logo=回首页、「关于」迁 footer、
   GitHub 迁 footer；间距节奏（gap 14/10+CTA 24）保留。「关于」去处三路：
   footer 链接行 + 首页条带 05 + 404 站点地图——顶栏不再承担低频项。 */
.footer-links {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--sp-12);
  margin-bottom: var(--sp-8);
}
.footer-link {
  color: var(--ink-soft, #555);
  text-decoration: none;
  font-weight: 700;
  font-size: var(--fs-13);
}
.footer-link:hover {
  color: var(--ink, #000);
  text-decoration: underline;
  text-underline-offset: 4px;
}
.footer-links-div {
  width: 2px;
  height: 14px;
  background: var(--ink, #000);
}
.footer-ext {
  font-size: var(--fs-11);
}</style>
