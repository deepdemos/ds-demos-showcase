<script setup lang="ts">
defineOptions({ name: 'PromptsView' })
import { onMounted, ref } from 'vue'
import { api } from '../api'
import type { PromptListItem } from '../api/types'
import { t } from '../i18n'
import { isImeComposing } from '../utils/ime'
import { useListPage } from '../composables/useListPage'
import PageHero from '../components/PageHero.vue'
import PromptLibraryCard from '../components/PromptLibraryCard.vue'
import PaginationBar from '../components/PaginationBar.vue'
import LoadingRow from '../components/LoadingRow.vue'
import EmptyBox from '../components/EmptyBox.vue'

const q = ref('')
const category = ref('')
const sort = ref<'newest' | 'most_tested'>('newest')
const results = ref<'all' | 'tested' | 'untested'>('all')
// Applied text is separate from the draft: changing pages keeps the same search.
const applied = ref({ q: '', category: '' })
const { items, total, page, pageSize, loading, error, load, apply } = useListPage<PromptListItem>(
  ({ page, page_size }) => api.listPrompts({
    q: applied.value.q || undefined,
    category: applied.value.category || undefined,
    sort: sort.value,
    has_results: results.value === 'all' ? undefined : results.value === 'tested',
    page,
    page_size,
  }),
  18,
)

function search() {
  applied.value = { q: q.value.trim(), category: category.value.trim() }
  void apply()
}

function clear() {
  q.value = ''
  category.value = ''
  sort.value = 'newest'
  results.value = 'all'
  search()
}

onMounted(load)
</script>

<template>
  <div class="route-page">
    <PageHero>
      <h1 class="page-title">{{ t('prompts.title', '提示词库') }}</h1>
      <p class="sub">{{ t('prompts.subtitle', '找一句值得测试的提示词，复制去跑，再看看不同模型交出的作品。') }}</p>
      <div class="filter-row pl-hero-actions">
        <RouterLink class="btn btn-primary" to="/prompts/new">{{ t('prompts.contribute', '收录提示词') }}</RouterLink>
        <span class="mini-stat"><b>{{ total }}</b> {{ t('prompts.countUnit', '条提示词') }}</span>
      </div>
    </PageHero>

    <section class="section pl-section" :aria-label="t('prompts.title', '提示词库')">
      <form class="pl-toolbar" @submit.prevent="search">
        <label class="pl-search">
          <span class="sr-only">{{ t('prompts.search', '搜索提示词') }}</span>
          <input v-model="q" class="input" type="search" maxlength="200" enterkeyhint="search"
            :placeholder="t('prompts.searchPlaceholder', '搜索标题或提示词正文')"
            @keydown.enter.prevent="!isImeComposing($event) && search()" @search="!q && search()" />
        </label>
        <label class="pl-category">
          <span class="sr-only">{{ t('prompts.category', '分类') }}</span>
          <input v-model="category" class="input" maxlength="64" :placeholder="t('prompts.categoryPlaceholder', '分类（可选）')"
            @keydown.enter.prevent="!isImeComposing($event) && search()" />
        </label>
        <button class="btn btn-secondary" type="submit">{{ t('common.search', '搜索') }}</button>
      </form>
      <div class="pl-options">
        <label class="pl-option">
          <span>{{ t('prompts.sortLabel', '排序') }}</span>
          <select v-model="sort" class="input" @change="apply()">
            <option value="newest">{{ t('prompts.newest', '最新收录') }}</option>
            <option value="most_tested">{{ t('prompts.mostTested', '作品最多') }}</option>
          </select>
        </label>
        <label class="pl-option">
          <span>{{ t('prompts.resultsLabel', '测试结果') }}</span>
          <select v-model="results" class="input" @change="apply()">
            <option value="all">{{ t('prompts.allResults', '全部') }}</option>
            <option value="tested">{{ t('prompts.withResults', '已有作品') }}</option>
            <option value="untested">{{ t('prompts.withoutResults', '等待测试') }}</option>
          </select>
        </label>
      </div>

      <EmptyBox v-if="error" kind="error" :text="error" @retry="load" />
      <LoadingRow v-else-if="loading && !items.length" :text="t('prompts.loading', '正在加载提示词…')" />
      <template v-else-if="items.length">
        <p v-if="loading" class="pl-refresh muted" role="status">{{ t('prompts.refreshing', '正在更新结果…') }}</p>
        <div class="pl-grid" :aria-busy="loading">
          <PromptLibraryCard v-for="prompt in items" :key="prompt.id" :prompt="prompt" />
        </div>
        <PaginationBar :page="page" :total="total" :page-size="pageSize" @change="(p) => { page = p; load() }" />
      </template>
      <EmptyBox v-else :text="applied.q || applied.category || results !== 'all' ? t('prompts.noMatches', '没有找到符合条件的提示词') : t('prompts.empty', '提示词库还没有内容，先收录一条想测试的提示词。')">
        <template #action>
          <button v-if="applied.q || applied.category || results !== 'all'" class="btn btn-outline" type="button" @click="clear">{{ t('prompts.clearFilters', '清空筛选') }}</button>
          <RouterLink class="btn btn-primary" to="/prompts/new">{{ t('prompts.contribute', '收录提示词') }}</RouterLink>
        </template>
      </EmptyBox>
    </section>
  </div>
</template>

<style scoped>
.pl-section { padding-top: var(--sp-8); }
.pl-hero-actions { margin-top: var(--sp-20); }
.pl-toolbar { display: flex; gap: var(--sp-10); flex-wrap: wrap; align-items: center; margin-bottom: var(--sp-16); }
.pl-search { flex: 1 1 280px; min-width: 0; }
.pl-category { flex: 0 1 180px; min-width: 0; }
.pl-options { display: flex; gap: var(--sp-20); flex-wrap: wrap; margin-bottom: var(--sp-24); }
.pl-option { display: flex; align-items: center; gap: var(--sp-8); font-size: var(--fs-13); font-weight: 700; }
.pl-option .input { width: auto; }
.pl-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--sp-24); }
.pl-refresh { margin-bottom: var(--sp-12); font-size: var(--fs-13); }
@media (max-width: 720px) { .pl-grid { grid-template-columns: minmax(0, 1fr); gap: var(--sp-16); } .pl-options { gap: var(--sp-12); } .pl-search { flex-basis: 100%; } .pl-category { flex: 1; } }
</style>
