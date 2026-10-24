<script setup lang="ts">
defineOptions({ name: 'PromptDetailView' })
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import type { DemoSummary, PromptSummary } from '../api/types'
import { t } from '../i18n'
import PageHero from '../components/PageHero.vue'
import CopyButton from '../components/CopyButton.vue'
import DemoCard from '../components/DemoCard.vue'
import PaginationBar from '../components/PaginationBar.vue'
import LoadingRow from '../components/LoadingRow.vue'
import EmptyBox from '../components/EmptyBox.vue'

const route = useRoute()
const prompt = ref<PromptSummary | null>(null)
const loading = ref(false)
const error = ref('')
const demos = ref<DemoSummary[]>([])
const demosLoading = ref(false)
const demosError = ref('')
const demosTotal = ref(0)
const page = ref(1)
const pageSize = 12
let detailGeneration = 0
let demosGeneration = 0

const sourceUrl = computed(() => {
  try {
    const url = new URL(prompt.value?.source_url || '')
    // 新投稿后端已拒绝凭据；这里挡历史/异常数据把 user:pass 渲染成可点击链接
    return ['http:', 'https:'].includes(url.protocol) && url.hostname && !url.username && !url.password ? url.href : null
  } catch { return null }
})

function routeId() {
  const id = Number(route.params.id)
  return Number.isSafeInteger(id) && id > 0 ? id : null
}

async function loadDemos() {
  const id = routeId()
  if (!id) return
  const generation = ++demosGeneration
  demosLoading.value = true
  demosError.value = ''
  try {
    const result = await api.getPromptDemos(id, { page: page.value, page_size: pageSize })
    if (generation !== demosGeneration) return
    demos.value = result.items
    demosTotal.value = result.total
  } catch (e) {
    if (generation === demosGeneration) demosError.value = (e as Error).message
  } finally {
    if (generation === demosGeneration) demosLoading.value = false
  }
}

async function load() {
  const generation = ++detailGeneration
  ++demosGeneration
  prompt.value = null
  demos.value = []
  demosTotal.value = 0
  demosError.value = ''
  page.value = 1
  error.value = ''
  const id = routeId()
  if (!id) {
    error.value = t('prompts.invalidId', '这条提示词的链接无效')
    loading.value = false
    demosLoading.value = false
    return
  }
  loading.value = true
  void loadDemos()
  try {
    const result = await api.getPrompt(id)
    if (generation === detailGeneration) prompt.value = result
  } catch (e) {
    if (generation === detailGeneration) error.value = (e as Error).message
  } finally {
    if (generation === detailGeneration) loading.value = false
  }
}

watch(() => route.params.id, load, { immediate: true })
onBeforeUnmount(() => { ++detailGeneration; ++demosGeneration })
</script>

<template>
  <div class="route-page">
    <RouterLink class="pd-back" to="/prompts">{{ t('prompts.back', '返回提示词库') }}</RouterLink>
    <LoadingRow v-if="loading" :text="t('prompts.loading', '正在加载提示词…')" />
    <EmptyBox v-else-if="error" kind="error" :text="error" @retry="load" />
    <template v-else-if="prompt">
      <PageHero>
        <h1 class="page-title pd-title">{{ prompt.title }}</h1>
        <div class="filter-row pd-meta">
          <span v-if="prompt.category" class="mini-stat">{{ prompt.category }}</span>
          <span class="mini-stat">{{ t('prompts.resultCount', '{n} 件作品', { n: prompt.demos_total }) }}</span>
          <span class="mini-stat">{{ t('prompts.modelCount', '{n} 个模型', { n: prompt.models_total }) }}</span>
          <span class="muted">{{ prompt.source === 'demo' ? t('prompts.fromWorks', '来自站内作品') : t('prompts.fromContribution', '独立收录') }}</span>
        </div>
      </PageHero>

      <section class="card card-default pd-original" aria-labelledby="prompt-original-title">
        <header class="pd-original-head">
          <h2 id="prompt-original-title" class="pd-heading">{{ t('prompts.original', '提示词原文') }}</h2>
          <CopyButton :text="prompt.content" :label="t('prompts.copy', '复制提示词')" />
        </header>
        <pre class="pd-content">{{ prompt.content }}</pre>
        <a v-if="sourceUrl" class="pd-source" :href="sourceUrl" target="_blank" rel="noopener noreferrer">{{ t('prompts.viewSource', '查看原始来源') }}</a>
      </section>
      <div class="pd-test-actions">
        <RouterLink class="btn btn-primary" :to="{ path: '/upload', query: { prompt: prompt.id } }">{{ t('prompts.uploadResult', '提交测试作品') }}</RouterLink>
        <p class="muted">{{ t('prompts.testHint', '复制原文到你使用的模型里测试，完成后把作品交回来。') }}</p>
      </div>

      <section class="section" aria-labelledby="prompt-results-title">
        <h2 id="prompt-results-title" class="section-title">{{ t('prompts.sameResults', '同一提示词的作品') }}</h2>
        <p class="pd-comparison-note">{{ t('prompts.comparisonNote', '这里按第一轮提示词原文关联作品。模型版本、追加轮次、参考素材和人工调整仍会影响结果，请结合每件作品的记录比较。') }}</p>
        <EmptyBox v-if="demosError" kind="error" :text="demosError" @retry="loadDemos" />
        <LoadingRow v-else-if="demosLoading && !demos.length" :text="t('prompts.loadingResults', '正在加载测试作品…')" />
        <template v-else-if="demos.length">
          <p v-if="demosLoading" class="muted pd-results-refresh" role="status">{{ t('prompts.refreshing', '正在更新结果…') }}</p>
          <div class="pd-results" :aria-busy="demosLoading">
            <div v-for="demo in demos" :key="demo.slug" class="pd-result">
              <DemoCard :demo="demo" />
              <div class="pd-run-meta">
                <span>{{ demo.gen_rounds === 1 ? t('prompts.firstRound', '首轮结果') : demo.gen_rounds != null && demo.gen_rounds > 1 ? t('prompts.multipleRounds', '{n} 轮生成', { n: demo.gen_rounds }) : t('prompts.roundsUnknown', '轮次未填写') }}</span>
                <span>{{ demo.gen_minutes != null ? t('prompts.minutes', '用时 {n} 分钟', { n: demo.gen_minutes }) : t('prompts.timeUnknown', '用时未填写') }}</span>
              </div>
            </div>
          </div>
          <PaginationBar :page="page" :total="demosTotal" :page-size="pageSize" @change="(p) => { page = p; loadDemos() }" />
        </template>
        <EmptyBox v-else :text="t('prompts.noResults', '还没有测试作品。你可以成为第一个用这条提示词交出结果的人。')">
          <template #action>
            <RouterLink class="btn btn-primary" :to="{ path: '/upload', query: { prompt: prompt.id } }">{{ t('prompts.uploadResult', '提交测试作品') }}</RouterLink>
          </template>
        </EmptyBox>
      </section>
    </template>
  </div>
</template>

<style scoped>
.pd-back { display: inline-block; margin-top: var(--sp-24); font-size: var(--fs-13); font-weight: 800; color: var(--ink); }
.pd-title { overflow-wrap: anywhere; }
.pd-meta { margin-top: var(--sp-16); font-size: var(--fs-13); }
.pd-original { padding: var(--sp-24); }
.pd-original-head { display: flex; justify-content: space-between; gap: var(--sp-12); align-items: center; flex-wrap: wrap; padding-bottom: var(--sp-16); border-bottom: var(--border-w-sm) solid var(--ink); }
.pd-heading { margin: 0; font-family: var(--font-heading); font-size: var(--fs-18); }
.pd-content { font-family: var(--font-code); font-size: var(--fs-15); line-height: 1.8; white-space: pre-wrap; overflow-wrap: anywhere; margin: var(--sp-24) 0; max-width: 80ch; }
.pd-source { color: var(--ink); font-size: var(--fs-13); font-weight: 700; }
.pd-test-actions { display: flex; gap: var(--sp-16); flex-wrap: wrap; align-items: center; margin-top: var(--sp-24); }
.pd-test-actions p { font-size: var(--fs-13); margin: 0; }
.pd-comparison-note { max-width: 80ch; font-size: var(--fs-13); line-height: 1.7; margin-bottom: var(--sp-24); color: var(--ink-soft); }
.pd-results { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--sp-24); }
.pd-result { min-width: 0; }
.pd-run-meta { display: flex; justify-content: space-between; gap: var(--sp-8); flex-wrap: wrap; padding-top: var(--sp-12); font-size: var(--fs-12); color: var(--ink-soft); }
.pd-results-refresh { font-size: var(--fs-13); margin-bottom: var(--sp-12); }
@media (max-width: 1024px) { .pd-results { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 720px) { .pd-original { padding: var(--sp-16); } .pd-content { font-size: var(--fs-14); } .pd-results { gap: var(--sp-16); } }
@media (max-width: 480px) { .pd-results { grid-template-columns: minmax(0, 1fr); } }
</style>
