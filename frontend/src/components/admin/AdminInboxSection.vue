<script setup lang="ts">
// 治理收件箱（v2 B4′ 最小版）：所有知识变更建议都从这里过一遍人工。
// 规则：approve 才由后端 service 真正执行（挂题/建实体/合并），本面板不改任何本地状态猜测。
// M2-t4 批量消化（02 P5/03 §9.3）：按 kind 分节+全选/多选批量批准/驳回。
// 【批量端点实测 2026-02】后端仅 POST /admin/suggestions/{sid}/review 单条端点，
// 无批量端点 → 批量走前端限速循环 5/s（BATCH_INTERVAL_MS=200）；「前端批量」已在 UI 标注，
// 后端协作清单：POST /admin/suggestions/batch-review{action, ids}（事务内逐条落审计）。
// 治理语义（可撤销才给批量）：批准效果逐条可审计回溯（unmerge/删实体/撤挂均有端点），
// 驳回不销毁数据（引擎可再次提名）——故两种批量都提供，批准走强确认（影响面 N 条预览）。
// 顺手修：后端 GET /suggestions 的 kind pattern 不含 retag_demo（传了 422）→ 该 kind 改客户端过滤。
defineOptions({ name: 'AdminInboxSection' })
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api'
import type { SuggestionItem } from '../../api/types'
import { useUiStore } from '../../stores/ui'
import { parseDate } from '../../utils/time'
import { INBOX_KINDS, inboxKindLabel } from '../../utils/adminLabels'
import { entityHrefTo, promptSuggestionSource, suggestionEntityHref } from '../../utils/entityDeepLink'
import LoadingRow from '../LoadingRow.vue'
import EmptyBox from '../EmptyBox.vue'
import { t } from '../../i18n'

const ui = useUiStore()
const route = useRoute()
const router = useRouter()

const items = ref<SuggestionItem[]>([])
const counts = ref<Record<string, number>>({})
const reviewFloor = ref(0.6)
const status = ref<'pending' | 'approved' | 'rejected' | 'all'>('pending')
const kind = ref('')
const loading = ref(false)
const busy = ref<Record<number, boolean>>({})
const error = ref('')

const KIND_LABELS = INBOX_KINDS // 单一源（utils/adminLabels），概览台 kind 直达链同词表

// ---- M2-t4 批量选择态 ----
const selected = ref<Set<number>>(new Set())
const batchRunning = ref(false)
const batchProgress = ref<{ done: number; total: number } | null>(null)
const batchLastAction = ref<'approve' | 'reject' | null>(null)
const failed = ref<{ id: number; kind: string; brief: string; message: string }[]>([])


const total = computed(() => Object.values(counts.value).reduce((a, b) => a + b, 0))

function brief(s: SuggestionItem): string {
  const p = s.payload as Record<string, unknown>
  const str = (v: unknown) => (v == null ? '' : String(v))
  switch (s.kind) {
    case 'task_match':
      return `《${str(p.demo_title) || str(p.demo_slug) || p.demo_id}》→ ${str(p.task_title) || str(p.task_id)}`
    case 'new_model':
    case 'alias':
      return str(p.name) || str(p.alias) || str(p.model_id)
    case 'retag_demo':
      return `《${str(p.demo_title) || str(p.demo_slug)}》type:${str(p.remove || 'demo')} → type:${str(p.add)}${
        Array.isArray(p.matched) && p.matched.length ? `（命中：${p.matched.slice(0, 3).join('、')}）` : ''
      }`
    case 'new_task':
    case 'new_prompt':
      return str(p.title) || str(p.task_id)
    default:
      return `${str(p.source_id ?? p.source ?? '')} → ${str(p.target_id ?? p.target ?? '')}`
  }
}

function effectOf(s: SuggestionItem): string {
  if (s.kind === 'task_match') return t('admin.inbox.effectAttach', '批准后该作品进入同题对比')
  if (s.kind === 'new_model') return t('admin.inbox.effectModel', '通过后创建模型实体（active）')
  if (s.kind === 'new_task') return t('admin.inbox.effectTask', '批准后创建题目并按建议挂题')
  if (s.kind === 'new_prompt') return t('admin.inbox.effectPrompt', '批准后收录到提示词库')
  if (s.kind === 'retag_demo') return t('admin.inbox.effectRetag', '通过后仅替换该作品的 type 值，其他标签不动')
  return t('admin.inbox.effectMerge', '通过后迁移引用并废弃源实体（可审计回溯）')
}

/** 后端 GET /suggestions 的 kind 过滤 pattern 不含 retag_demo（传了 422）→ 客户端过滤 */
function isClientFilteredKind(k: string): boolean {
  return k === 'retag_demo'
}

function promptContent(s: SuggestionItem): string {
  return typeof s.payload.content === 'string' ? s.payload.content : ''
}

// ---- new_prompt 展开全文：列表口 payload.content 超 480 字已被后端截断（content_truncated），
// 「查看全文」时才调详情接口取全量；已取条目按 id 缓存，避免重复请求（带宽与 DOM 双省） ----
const fullContents = ref(new Map<number, string>())
const fullLoading = ref<Record<number, boolean>>({})

/** <pre> 显示的正文：优先已取回的全量，否则用列表里的（可能被截断的）payload.content */
function promptText(s: SuggestionItem): string {
  return fullContents.value.get(s.id) ?? promptContent(s)
}

async function loadFullPrompt(s: SuggestionItem) {
  if (fullContents.value.has(s.id) || fullLoading.value[s.id]) return
  fullLoading.value[s.id] = true
  try {
    const full = await api.getSuggestion(s.id)
    // 仅缓存真正的字符串全文；payload 异常时不写缓存，保持「查看全文」可重试
    if (typeof full.payload.content === 'string') fullContents.value.set(s.id, full.payload.content)
    else ui.toast(t('admin.inbox.promptFullFailed', '全文读取失败'), 'error')
  } catch (e) {
    ui.toast((e as Error).message, 'error')
  } finally {
    fullLoading.value[s.id] = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const clientFiltered = kind.value !== '' && isClientFilteredKind(kind.value)
    const res = await api.listSuggestions({ status: status.value, kind: clientFiltered ? undefined : kind.value || undefined })
    items.value = clientFiltered ? res.items.filter((s) => s.kind === kind.value) : res.items
    counts.value = res.pending_by_kind
    reviewFloor.value = res.thresholds.review
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

// ---- M2-t4：待处理视图按 kind 分节（组内保持 confidence/id 排序；组间按积压量降序）----
const kindGroups = computed(() => {
  if (status.value !== 'pending') return null
  const map = new Map<string, SuggestionItem[]>()
  for (const s of items.value) {
    const list = map.get(s.kind) || []
    list.push(s)
    map.set(s.kind, list)
  }
  return [...map.entries()].sort((a, b) => b[1].length - a[1].length)
})

function pendingOf(kindKey: string): SuggestionItem[] {
  return items.value.filter((s) => s.kind === kindKey)
}

function toggle(id: number) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function toggleKind(kindKey: string) {
  const next = new Set(selected.value)
  const rows = pendingOf(kindKey)
  const allOn = rows.length > 0 && rows.every((s) => next.has(s.id))
  for (const s of rows) {
    if (allOn) next.delete(s.id)
    else next.add(s.id)
  }
  selected.value = next
}

function kindAllSelected(kindKey: string): boolean {
  const rows = pendingOf(kindKey)
  return rows.length > 0 && rows.every((s) => selected.value.has(s.id))
}

function clearSelection() {
  selected.value = new Set()
}

const selectedRows = computed(() => items.value.filter((s) => selected.value.has(s.id)))

/** 批量执行（前端限速循环 5/s）：失败列表可重试；进度实时可见 */
async function runBatch(action: 'approve' | 'reject', idsOverride?: number[]) {
  const rows = idsOverride ? items.value.filter((s) => idsOverride.includes(s.id)) : selectedRows.value
  const ids = rows.map((r) => r.id)
  if (!ids.length || batchRunning.value) return
  if (action === 'approve') {
    // 影响面 N 条预览 + 治理语义注记（可撤销才给批量：批准逐条可审计回溯，驳回可由引擎再次提名）
    const preview = rows.slice(0, 5).map((s) => `· ${brief(s)}\n  ${effectOf(s)}`).join('\n')
    const ok = await ui.confirm({
      title: t('admin.inbox.batchConfirmTitle', '批量批准 {n} 条建议？', { n: ids.length }),
      message:
        `${preview}${rows.length > 5 ? `\n…${t('admin.inbox.batchMore', '等共 {n} 条', { n: rows.length })}` : ''}\n\n` +
        (rows.some((s) => s.kind === 'new_prompt')
          ? t('admin.inbox.promptBatchNote', '提示词批准后公开收录，请先核对完整正文和来源。')
          : t('admin.inbox.batchUndoNote', '治理语义：批准效果逐条可审计回溯（合并可 unmerge/实体可删/挂题可撤）；驳回不销毁数据，引擎可再次提名。')),
      confirmText: t('admin.inbox.batchApprove', '批量批准'),
    })
    if (!ok) return
  }
  batchRunning.value = true
  batchLastAction.value = action
  failed.value = []
  // M3-B5 真批量端点（t4 前端限速循环退役）：POST /admin/suggestions/batch-review——逐条独立提交+审计，单条失败不拖垮整批
  const res = await api.batchReviewSuggestions(action, ids)
  const fails: typeof failed.value = res.results
    .filter((x) => !x.ok)
    .map((x) => {
      const item = rows.find((r) => r.id === x.id)
      return { id: x.id, kind: item?.kind || '', brief: item ? brief(item) : `#${x.id}`, message: x.error || '失败' }
    })
  failed.value = fails
  batchRunning.value = false
  batchProgress.value = null
  selected.value = new Set()
  const okCount = res.ok
  ui.toast(
    t('admin.inbox.batchDone', '批量{action}完成：成功 {ok} · 失败 {bad}', {
      action: action === 'approve' ? t('admin.inbox.approve', '批准') : t('admin.inbox.reject', '驳回'),
      ok: okCount,
      bad: fails.length,
    }),
    fails.length ? 'error' : 'success',
  )
  await load()
}

function retryFailed() {
  if (!failed.value.length || !batchLastAction.value) return
  void runBatch(batchLastAction.value, failed.value.map((f) => f.id))
}

function goEntity(s: SuggestionItem) {
  const href = suggestionEntityHref(s)
  if (!href) return
  const loc = entityHrefTo(href)
  void router.push(loc)
}

async function review(s: SuggestionItem, action: 'approve' | 'reject') {
  if (action === 'approve') {
    const ok = await ui.confirm({
      title: t('admin.inbox.confirmApprove', '批准这条建议？'),
      message: `${brief(s)}\n${effectOf(s)}\n${s.kind === 'new_prompt'
        ? t('admin.inbox.promptOpenHint', '批准后可打开提示词，查看同正文作品。')
        : t('admin.inbox.digestThenEntity', '队列只消化待办；批准后深链回实体页改字段。')}`,
      confirmText: t('admin.inbox.approve', '批准'),
    })
    if (!ok) return
  }
  busy.value[s.id] = true
  try {
    const done = await api.reviewSuggestion(s.id, action)
    ui.toast(
      action === 'approve'
        ? done.result || t('admin.inbox.doneApprove', '已批准并执行')
        : t('admin.inbox.doneReject', '已驳回'),
      'success',
    )
    if (action === 'approve') {
      const merged = { ...s, ...done, payload: done.payload || s.payload, result: done.result || s.result }
      if (suggestionEntityHref(merged)) {
        goEntity(merged)
        return
      }
    }
    await load()
  } catch (e) {
    ui.toast((e as Error).message, 'error')
  } finally {
    busy.value[s.id] = false
  }
}

onMounted(() => {
  // M2-t4 深链：?tab=inbox&filter=<kind>（概览台 kind 计数直达）；未知 filter 忽略
  const f = String(route.query.filter || '')
  if (f && INBOX_KINDS[f]) kind.value = f
  void load()
})
</script>

<template>
  <div>
    <div class="filter-row" style="margin-bottom: var(--sp-12); flex-wrap: wrap">
      <span class="filter-label">{{ t('admin.inbox.hint', '低置信度建议默认不进此视图（阈值以下只记录不骚扰）') }}</span>
      <select v-model="status" class="input" style="max-width: 130px" @change="load">
        <option value="pending">{{ t('admin.inbox.pending', '待处理') }}</option>
        <option value="approved">{{ t('admin.inbox.approved', '已批准') }}</option>
        <option value="rejected">{{ t('admin.inbox.rejected', '已驳回') }}</option>
        <option value="all">{{ t('common.all', '全部') }}</option>
      </select>
      <select v-model="kind" class="input" style="max-width: 150px" @change="load">
        <option value="">{{ t('admin.inbox.allKinds', '全部类型') }}</option>
        <option v-for="(_v, k) in KIND_LABELS" :key="k" :value="k">{{ inboxKindLabel(k) }}</option>
      </select>
      <button class="btn btn-sm btn-secondary" type="button" :disabled="loading" @click="load">{{ t('common.refresh', '刷新') }}</button>
      <span v-if="status === 'pending'" class="mini-stat"><b>{{ total }}</b> {{ t('admin.inbox.total', '条待处理') }}</span>
      <span v-for="(n, k) in counts" :key="k" class="tag-chip mode-open">{{ inboxKindLabel(k) }} <b class="count">{{ n }}</b></span>
    </div>

    <div v-if="error" class="notice notice-error">{{ error }}</div>
    <LoadingRow v-if="loading && !items.length" :text="t('admin.inbox.loading', '加载收件箱…')" />
    <EmptyBox v-else-if="!items.length" :text="t('admin.inbox.empty', '队列是空的 —— 没有需要人工判断的建议')" />

    <!-- M2-t4 批量工具条：选中即现；批量走前端限速循环（后端批量端点=协作清单），影响面先确认 -->
    <div v-if="status === 'pending' && (selected.size > 0 || batchRunning || failed.length)" class="inbox-batch card card-default" data-cdp="inbox-batch">
      <template v-if="batchRunning">
        <b>{{ t('admin.inbox.batchRunning', '批量执行中…') }}</b>
      </template>
      <template v-else>
        <div class="filter-row" style="margin: 0; flex-wrap: wrap; align-items: center">
          <b>{{ t('admin.inbox.batchSelected', '已选 {n} 条', { n: selected.size }) }}</b>
          <button class="btn btn-sm btn-primary" type="button" @click="runBatch('approve')">
            {{ t('admin.inbox.batchApprove', '批量批准') }}
          </button>
          <button class="btn btn-sm btn-dark" type="button" @click="runBatch('reject')">
            {{ t('admin.inbox.batchReject', '批量驳回') }}
          </button>
          <button class="btn btn-sm btn-outline" type="button" @click="clearSelection">
            {{ t('admin.inbox.batchClear', '清空选择') }}
          </button>
          <span class="hint">{{ t('admin.inbox.batchRealNote', '批量=真批量端点（逐条独立提交+审计，单条失败不拖垮整批）。') }}</span>
        </div>
        <div v-if="failed.length" class="inbox-failed">
          <b>{{ t('admin.inbox.batchFailTitle', '失败 {n} 条：', { n: failed.length }) }}</b>
          <span v-for="f in failed" :key="f.id" class="mono">#{{ f.id }}（{{ f.message }}）</span>
          <button class="btn btn-sm btn-outline" type="button" @click="retryFailed">{{ t('admin.inbox.batchRetry', '重试失败项') }}</button>
        </div>
      </template>
    </div>

    <!-- M2-t4：待处理视图按 kind 分节+多选；其余状态保持平铺只读 -->
    <template v-if="kindGroups">
      <section v-for="[kindKey, rows] in kindGroups" :key="kindKey" class="inbox-kind-section">
        <header class="inbox-kind-head">
          <b>{{ inboxKindLabel(kindKey) }}</b>
          <span class="mini-stat"><b>{{ rows.length }}</b> {{ t('admin.inbox.total', '条待处理') }}</span>
          <button class="btn btn-sm btn-outline" type="button" @click="toggleKind(kindKey)">
            {{ kindAllSelected(kindKey) ? t('admin.inbox.deselectKind', '取消全选') : t('admin.inbox.selectAll', '全选本节') }}
          </button>
        </header>
        <article v-for="s in rows" :key="s.id" class="inbox-row card card-default" :class="{ 'inbox-picked': selected.has(s.id) }">
          <div class="inbox-head">
            <label class="inbox-check">
              <input type="checkbox" :checked="selected.has(s.id)" @change="toggle(s.id)" />
              <span class="cluster-badge cb-exact">{{ inboxKindLabel(s.kind) }}</span>
            </label>
            <span class="inbox-source mono">{{ s.source }}</span>
            <span v-if="s.confidence != null" class="mini-stat"><b>{{ (s.confidence * 100).toFixed(0) }}%</b> {{ t('admin.inbox.confidence', '置信度') }}</span>
            <span v-if="status === 'pending' && s.confidence != null && s.confidence < reviewFloor" class="hint">
              {{ t('admin.inbox.belowFloor', '低于默认阈值，仅显式筛出时可见') }}
            </span>
            <span class="muted mono inbox-time">{{ parseDate(s.created_at).toLocaleString('zh-CN') }}</span>
          </div>
          <div class="inbox-brief">{{ brief(s) }}</div>
          <!-- 默认收起：列表正文是摘要（超 480 字后端截断），审核员展开按需看/取全量 -->
          <details v-if="s.kind === 'new_prompt'" class="inbox-prompt-details">
            <summary>{{ t('admin.inbox.promptContent', '提示词全文') }}</summary>
            <pre class="inbox-prompt-text mono">{{ promptText(s) }}</pre>
            <button
              v-if="s.payload.content_truncated && !fullContents.has(s.id)"
              class="btn btn-sm btn-outline"
              type="button"
              :disabled="fullLoading[s.id]"
              @click="loadFullPrompt(s)"
            >
              {{ fullLoading[s.id] ? t('admin.inbox.promptFullLoading', '加载中…') : t('admin.inbox.promptFull', '查看全文') }}
            </button>
            <p v-if="s.payload.source_url" class="inbox-prompt-source">
              <b>{{ t('admin.inbox.promptSource', '来源链接') }}：</b>
              <a v-if="promptSuggestionSource(s)" :href="promptSuggestionSource(s)!" target="_blank" rel="noopener noreferrer">{{ s.payload.source_url }}</a>
              <span v-else>{{ s.payload.source_url }}</span>
            </p>
          </details>
          <div class="filter-row" style="margin: var(--sp-8) 0 0">
            <span class="hint">{{ effectOf(s) }}</span>
            <template v-if="s.status === 'pending'">
              <button class="btn btn-sm btn-primary" type="button" :disabled="busy[s.id]" @click="review(s, 'approve')">
                {{ busy[s.id] ? t('admin.inbox.working', '执行中…') : t('admin.inbox.approve', '批准') }}
              </button>
              <button class="btn btn-sm btn-dark" type="button" :disabled="busy[s.id]" @click="review(s, 'reject')">
                {{ t('admin.inbox.reject', '驳回') }}
              </button>
            </template>
            <span v-else class="cluster-covered-tag">{{ s.status === 'approved' ? t('admin.inbox.approved', '已批准') : t('admin.inbox.rejected', '已驳回') }}</span>
            <button v-if="suggestionEntityHref(s)" class="btn btn-sm btn-outline" type="button" @click="goEntity(s)">
              {{ s.kind === 'new_prompt' ? t('admin.inbox.openPrompt', '打开提示词') : t('admin.inbox.openEntity', '打开实体 →') }}
            </button>
          </div>
        </article>
      </section>
    </template>
    <div v-else class="inbox-list">
      <article v-for="s in items" :key="s.id" class="inbox-row card card-default">
        <div class="inbox-head">
          <span class="cluster-badge cb-exact">{{ inboxKindLabel(s.kind) }}</span>
          <span class="inbox-source mono">{{ s.source }}</span>
          <span v-if="s.confidence != null" class="mini-stat"><b>{{ (s.confidence * 100).toFixed(0) }}%</b> {{ t('admin.inbox.confidence', '置信度') }}</span>
          <span class="muted mono inbox-time">{{ parseDate(s.created_at).toLocaleString('zh-CN') }}</span>
        </div>
        <div class="inbox-brief">{{ brief(s) }}</div>
        <!-- 默认收起：列表正文是摘要（超 480 字后端截断），审核员展开按需看/取全量 -->
        <details v-if="s.kind === 'new_prompt'" class="inbox-prompt-details">
          <summary>{{ t('admin.inbox.promptContent', '提示词全文') }}</summary>
          <pre class="inbox-prompt-text mono">{{ promptText(s) }}</pre>
          <button
            v-if="s.payload.content_truncated && !fullContents.has(s.id)"
            class="btn btn-sm btn-outline"
            type="button"
            :disabled="fullLoading[s.id]"
            @click="loadFullPrompt(s)"
          >
            {{ fullLoading[s.id] ? t('admin.inbox.promptFullLoading', '加载中…') : t('admin.inbox.promptFull', '查看全文') }}
          </button>
          <p v-if="s.payload.source_url" class="inbox-prompt-source">
            <b>{{ t('admin.inbox.promptSource', '来源链接') }}：</b>
            <a v-if="promptSuggestionSource(s)" :href="promptSuggestionSource(s)!" target="_blank" rel="noopener noreferrer">{{ s.payload.source_url }}</a>
            <span v-else>{{ s.payload.source_url }}</span>
          </p>
        </details>
        <div class="filter-row" style="margin: var(--sp-8) 0 0">
          <span class="hint">{{ effectOf(s) }}</span>
          <template v-if="s.status === 'pending'">
            <button class="btn btn-sm btn-primary" type="button" :disabled="busy[s.id]" @click="review(s, 'approve')">
              {{ busy[s.id] ? t('admin.inbox.working', '执行中…') : t('admin.inbox.approve', '批准') }}
            </button>
            <button class="btn btn-sm btn-dark" type="button" :disabled="busy[s.id]" @click="review(s, 'reject')">
              {{ t('admin.inbox.reject', '驳回') }}
            </button>
          </template>
          <span v-else class="cluster-covered-tag">{{ s.status === 'approved' ? t('admin.inbox.approved', '已批准') : t('admin.inbox.rejected', '已驳回') }}</span>
          <button v-if="suggestionEntityHref(s)" class="btn btn-sm btn-outline" type="button" @click="goEntity(s)">
            {{ s.kind === 'new_prompt' ? t('admin.inbox.openPrompt', '打开提示词') : t('admin.inbox.openEntity', '打开实体 →') }}
          </button>
        </div>
      </article>
    </div>
  </div>
</template>

<style scoped>
.inbox-prompt-details {
  margin-top: var(--sp-8);
}
.inbox-prompt-details > summary {
  padding: var(--sp-8) 0;
  min-height: 44px;
  box-sizing: border-box;
  cursor: pointer;
  font-weight: 800;
}
.inbox-prompt-details > summary:focus-visible {
  outline: var(--border-w) solid var(--ink);
  outline-offset: var(--sp-2);
}
.inbox-prompt-text {
  margin: 0;
  padding: var(--sp-12);
  background: var(--paper);
  border: var(--border-w) solid var(--ink);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: var(--fs-14);
  line-height: 1.6;
}
.inbox-prompt-source {
  margin: var(--sp-8) 0 0;
  overflow-wrap: anywhere;
}
/* ---- M2-t4 批量消化（admin scoped 纪律：styles/ 零新增块）---- */
/* kind 分节头：节名+计数+全选（44px 触达线内按钮复用 btn-sm 既有触达） */
.inbox-kind-section {
  margin-bottom: 22px;
}
.inbox-kind-head {
  display: flex;
  align-items: center;
  gap: var(--sp-12);
  padding: var(--sp-8) 0;
  border-bottom: var(--border-w, 4px) solid var(--ink, #000);
  margin-bottom: var(--sp-12);
}
/* 勾选框：行首 18px 可点目标包 44px 热区（触达线） */
.inbox-check {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-8);
  cursor: pointer;
  min-height: 44px;
}
.inbox-check input {
  width: 18px;
  height: 18px;
  accent-color: var(--ink, #000);
  cursor: pointer;
}
.inbox-picked {
  border-color: var(--yellow, #ffe66d);
}
/* 批量工具条：选中即现；失败列表逐条可重试 */
.inbox-batch {
  margin-bottom: var(--sp-16);
}
.inbox-failed {
  margin-top: var(--sp-10);
  padding-top: var(--sp-10);
  border-top: 2px solid var(--ink, #000);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-10);
  font-size: var(--fs-12);
}
</style>
