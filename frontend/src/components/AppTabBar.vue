<script setup lang="ts">
// M2-1 移动底部 TabBar（03 §10.2 + 任务书裁决）：Tab + 中央上传 FAB。
// 手机首页可发现性（PR #17 review）：恢复「首页」为第一槽位——六槽 首页/作品/提示词/+FAB/社区/我的
// （.tb 为 flex:1，390px 视口可容）；<720 启用（与既有移动断点同值），桌面 display:none 零影响；
// forum 双皮壳壳层不挂载（App.vue v-if）。
// 我的位内聚：登录→/user/:u（页内 hero 自我工具排：通知（未读红点镜像）/设置/工作台(admin)/退出，
// M2-1 落地于 UserView isSelf 分支）；未登录→/login（页内含注册路径）。
// 未读红点镜像：与 NotificationBell 同源 notifications store 单一口径（startPolling 幂等）。
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useNotificationsStore } from '../stores/notifications'
import { t } from '../i18n'

const route = useRoute()
const auth = useAuthStore()
const notif = useNotificationsStore()

const username = computed(() => auth.user?.username ?? '')

// 我的位去向（03 §10.2：我的=登录?/user/:u）
const meTo = computed(() => (auth.isLoggedIn() ? `/user/${username.value}` : '/login'))
// 我的位点亮：本人用户页（含粉丝/关注子页）/设置/通知 同属「我的」语义域；
// 精确前缀（/user/a 不得误吞 /user/abc）。
const meActive = computed(() => {
  if (auth.isLoggedIn()) {
    const u = `/user/${username.value}`
    return route.path === u || route.path.startsWith(`${u}/`) || route.path === '/settings' || route.path === '/notifications'
  }
  return route.path === '/login' || route.path === '/register'
})

onMounted(() => {
  if (auth.isLoggedIn()) notif.startPolling()
})
</script>

<template>
  <nav class="tabbar" :aria-label="t('app.tabbar.label', '底部导航')">
    <!-- 首页槽：exact 匹配（/ 不抢其他路由的点亮）；手机端首页可发现性的兜底入口 -->
    <RouterLink class="tb" :class="{ active: route.path === '/' }" to="/">
      {{ t('app.tabbar.home', '首页') }}
    </RouterLink>
    <RouterLink
      class="tb"
      :class="{ active: route.path.startsWith('/demos') || route.path.startsWith('/demo/') }"
      to="/demos"
    >
      {{ t('app.tabbar.works', '作品') }}
    </RouterLink>
    <RouterLink class="tb" :class="{ active: route.path.startsWith('/prompts') }" to="/prompts">
      {{ t('app.tabbar.prompts', '提示词') }}
    </RouterLink>
    <!-- 中央上传 FAB：转化主件的物理地位（菲茨：拇指热区 56px 反色大目标）；CTA 随顶栏减法退到此处 -->
    <div class="fab-slot">
      <RouterLink class="fab" to="/upload" :aria-label="t('app.nav.upload', '上传 Demo')" :title="t('app.nav.upload', '上传 Demo')">
        <span class="fab-glyph" aria-hidden="true">+</span>
      </RouterLink>
    </div>
    <RouterLink class="tb" :class="{ active: route.path.startsWith('/forum') }" to="/forum">
      {{ t('app.tabbar.community', '社区') }}
    </RouterLink>
    <RouterLink class="tb tb-me" :class="{ active: meActive }" :to="meTo">
      {{ auth.isLoggedIn() ? t('app.tabbar.me', '我的') : t('app.tabbar.login', '登录') }}
      <template v-if="auth.isLoggedIn() && notif.unreadCount > 0">
        <span class="tb-dot" aria-hidden="true"></span>
        <span class="sr-only">{{ t('home.side.unread', '{n} 条未读', { n: notif.unreadCount }) }}</span>
      </template>
    </RouterLink>
  </nav>
</template>

<style scoped>
/* 桌面（≥721）零渲染；<720 固定底栏。全部 scoped——全局 styles/ 零新增块。 */
.tabbar {
  display: none;
}
@media (max-width: 720px) {
  .tabbar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: var(--z-tabbar); /* 弹层之下（modal 1000/toast 1100），内容之上 */
    display: flex;
    align-items: stretch;
    background: var(--paper, #fff);
    border-top: var(--border-w, 4px) solid var(--ink, #000);
    padding-bottom: env(safe-area-inset-bottom); /* iPhone 底部横条（03 §10.2） */
    /* P0-2：高度真源 = tokens/primitives.css 的 --tabbar-h（.tb 的 min-height 引用它）；
       P1 底栏占位契约会让页内同高 fixed 条统一 bottom: var(--tabbar-h-safe)。 */
  }
}
.tb {
  position: relative;
  flex: 1 1 0;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: var(--tabbar-h); /* ≥44 触达线（03 §10.1），留余量；P0-2 起高度入令牌 */
  font-size: var(--fs-12);
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.02em;
  text-decoration: none;
  color: var(--ink-soft, #555);
  background: none;
  border: none;
}
/* active=反色章（03 §10.2 语汇，不新增花活）；hover 仅限指针设备，触屏用 :active 压平反馈 */
@media (hover: hover) {
  .tb:hover {
    color: var(--ink, #000);
  }
}
.tb:active {
  color: var(--ink, #000);
}
.tb.active {
  background: var(--ink, #000);
  color: var(--paper, #fff);
}
/* 未读红点镜像（通知不占 Tab，红点上「我的」位） */
.tb-dot {
  position: absolute;
  top: 8px;
  right: calc(50% - 18px);
  width: 8px;
  height: 8px;
  background: var(--red, #ff6b6b);
  border: 2px solid var(--ink, #000);
}
/* 中央上传 FAB：56px 反色（墨面纸字），上探出栏沿一档 */
.fab-slot {
  flex: 1 1 0;
  display: flex;
  justify-content: center;
  align-items: flex-start;
}
.fab {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  margin-top: -16px;
  background: var(--ink, #000);
  color: var(--paper, #fff);
  box-shadow: 3px 3px 0 0 var(--ink, #000); /* 硬影贴色：挤出贴纸质感（纸白=黑影/墨黑=白墨影，双主题自洽） */
  text-decoration: none;
}
.fab-glyph {
  font-size: 28px;
  font-weight: 900;
  line-height: 1;
}
@media (hover: hover) {
  .fab:hover {
    background: var(--red, #ff6b6b);
    color: var(--on-accent, #000);
  }
}
.fab:active {
  background: var(--red, #ff6b6b);
  color: var(--on-accent, #000);
}
</style>
