<script setup lang="ts">
import type { PromptListItem } from '../api/types'
import { api } from '../api'
import { t } from '../i18n'
import CopyButton from './CopyButton.vue'

defineProps<{ prompt: PromptListItem }>()

// 复制时才拉全文：列表项只带 480 字摘要（按需加载契约），点击复制按需取详情。
// 缓存 Promise：已取正文复用、在途请求共享（连点只发一次）；失败不缓存，按钮可重试。
// 上限 20 条：缓存只在本次会话内复用，避免翻遍列表后把所有全文永久留在内存。
const FULL_TEXT_CACHE_MAX = 20
const fullTextCache = new Map<number, Promise<string>>()
function copyText(id: number) {
  let pending = fullTextCache.get(id)
  if (!pending) {
    pending = api.getPrompt(id).then((r) => r.content)
    pending.catch(() => fullTextCache.delete(id))
    fullTextCache.set(id, pending)
    while (fullTextCache.size > FULL_TEXT_CACHE_MAX) {
      const oldest = fullTextCache.keys().next().value
      if (oldest === undefined) break
      fullTextCache.delete(oldest)
    }
  }
  return pending
}
</script>

<template>
  <article class="card card-default pl-card">
    <header class="pl-card-head">
      <div class="pl-card-heading">
        <span v-if="prompt.category" class="pl-card-category">{{ prompt.category }}</span>
        <h2 class="pl-card-title"><RouterLink :to="`/prompts/${prompt.id}`">{{ prompt.title }}</RouterLink></h2>
      </div>
      <CopyButton :text="() => copyText(prompt.id)" :label="t('prompts.copy', '复制提示词')" />
    </header>
    <!-- 摘要已由接口截断（≤480 字），截断时补省略号；不再本地 slice -->
    <p class="pl-card-content">{{ prompt.content_excerpt }}{{ prompt.content_truncated ? '…' : '' }}</p>
    <footer class="pl-card-foot">
      <div class="pl-card-counts">
        <span v-if="prompt.demos_total">{{ t('prompts.resultCount', '{n} 件作品', { n: prompt.demos_total }) }}</span>
        <span v-else>{{ t('prompts.awaitingResults', '等待第一份测试结果') }}</span>
        <span v-if="prompt.models_total">{{ t('prompts.modelCount', '{n} 个模型', { n: prompt.models_total }) }}</span>
      </div>
    </footer>
  </article>
</template>

<style scoped>
.pl-card { display: flex; flex-direction: column; gap: var(--sp-16); min-width: 0; padding: var(--sp-24); }
.pl-card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-12); }
.pl-card-heading { min-width: 0; }
.pl-card-category { display: block; color: var(--ink-soft); font-size: var(--fs-12); margin-bottom: var(--sp-6); }
.pl-card-title { margin: 0; font-family: var(--font-heading); font-size: var(--fs-20); line-height: 1.35; overflow-wrap: anywhere; }
.pl-card-title a { color: inherit; text-decoration: none; }
.pl-card-title a:hover { text-decoration: underline; }
.pl-card-content { flex: 1; margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--font-code); font-size: var(--fs-14); line-height: 1.75; display: -webkit-box; -webkit-line-clamp: 7; -webkit-box-orient: vertical; overflow: hidden; }
.pl-card-foot { border-top: var(--border-w-sm) solid var(--ink); padding-top: var(--sp-12); display: flex; align-items: center; justify-content: space-between; gap: var(--sp-12); flex-wrap: wrap; }
.pl-card-counts { display: flex; gap: var(--sp-12); flex-wrap: wrap; font-size: var(--fs-12); color: var(--ink-soft); }
@media (max-width: 720px) { .pl-card { padding: var(--sp-16); } .pl-card-title { font-size: var(--fs-18); } }
@media (max-width: 480px) { .pl-card-head { flex-wrap: wrap; } }
</style>
