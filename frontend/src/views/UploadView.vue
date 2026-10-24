<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useSelectedTags } from '../composables/useSelectedTags'
import { useAuthStore } from '../stores/auth'
import { useTagsStore } from '../stores/tags'
import { useUiStore } from '../stores/ui'
import type { DemoDetail, TaskDetail } from '../api/types'
import TagPicker from '../components/TagPicker.vue'
import type { TagPick } from '../components/TagPicker.vue'
import { t } from '../i18n'
import { compressImage } from '../utils/imageCompress'
import { tagLabel } from '../utils/funMode'
import { useUploadDraft } from '../composables/useUploadDraft'
import { usePromptLibraryUpload } from '../composables/usePromptLibraryUpload'
import { useTaskMount } from '../composables/useTaskMount'
import { useTagSuggest } from '../composables/useTagSuggest'
import { useUploadPlayable } from '../composables/useUploadPlayable'
import { useUploadWizard } from '../composables/useUploadWizard'
import CompletenessDash from '../components/upload/CompletenessDash.vue'
import StepReview from '../components/upload/StepReview.vue'
import StepModelAssert from '../components/upload/StepModelAssert.vue'
import StepDescribe from '../components/upload/StepDescribe.vue'
import PageHero from '../components/PageHero.vue'

const route = useRoute()
const auth = useAuthStore()
const ui = useUiStore()
const editSlug = typeof route.query.slug === 'string' ? route.query.slug : ''
// v2 B4′：从题目页「用你的模型挑战此题」进来 —— 带题面预填 + 一键复制原题
const challengeSlug = typeof route.query.task === 'string' ? route.query.task : ''
const challenge = ref<TaskDetail | null>(null)

const title = ref('')
const description = ref('')
const demoType = ref<'web' | 'zip' | 'link' | 'image'>('web')
const externalUrl = ref('')
const prompt = ref('')
const videoUrl = ref('')
const coverFile = ref<File | null>(null)
const zipFile = ref<File | null>(null)
const commitMessage = ref('')
const keepOldVersion = ref(false)
const submitting = ref(false)
const uploadProgress = ref(0)
const error = ref('')
const dupSlug = ref<string | null>(null)
// P4：编辑态下的重复内容 —— 允许"仍要提交"（带 force=1）
const dupInEdit = ref(false)
const success = ref<{ slug: string; status: string; created?: boolean } | null>(null)
const idempotencyKey = ref('')
const uploadCode = ref('')
const forceUpload = ref(false)
const loading = ref(false)

// 标签选择器（公共 TagPicker）
const tagsStore = useTagsStore()
const tagKeys = computed(() => tagsStore.keys)
const selected = ref<Record<string, { value: string; description: string }[]>>({})
const initialTagsKey = ref('')
const tagsOpen = ref(false)

// RF-3c：展平/计数/型号名统一由 useSelectedTags 派生（原先四处各写一遍）
const {
  list: selectedTagList,
  count: selectedCount,
  modelNames: chosenModelNames,
  toMap: selectedToMap,
} = useSelectedTags(selected)
const selectedTags = computed<TagPick[]>({
  get: () => selectedTagList.value as TagPick[],
  set: (arr) => {
    selected.value = selectedToMap(arr)
  },
})
// 上传场景标签申请（闭环 A）：TagPicker 在 defer 模式下把新 fixed 值留本地待审（此数组为受控源），
// 不入 selected/tags JSON；createDemo 成功拿到 demo_id 后，再逐个 suggestTagValue，然后清空。
const pendingTagApplies = ref<TagPick[]>([])
// 上传提议新题（闭环 B）：有 pickedTask 走 task=，有提议走 propose_task；两路互斥。
const proposeTitle = ref('')
const proposeDesc = ref('')
const proposeCategory = ref('')
// Q2：模型必填，但「不确定」有正门 —— 选了兜底位就多问一句依据（供日后归属工作台收敛）
const FALLBACK_MODEL_RE = /(^|-)unknown$|^unspecified$/i
/** Q2：模型必选 —— 上传页据此把「可选」措辞与校验切成必答态 */
const hasModel = computed(() => (selected.value['model'] || []).length > 0)
const modelUncertain = computed(() =>
  (selected.value['model'] || []).some((m) => FALLBACK_MODEL_RE.test(m.value)),
)
const modelHint = ref('')
// chosenModelNames：selected['model'] 的派生（useUploadPlayable 与 useUploadWizard 共同消费）
/** 标签至少 1 个：与后端 `_require_model_tag`/tags≥1 同规则（模型已含在 tags 内时自然满足）——
 *  提前声明：useUploadWizard 的 readyToSubmit 以 dep 消费 */
const tagsOk = computed(() => selectedCount.value >= 1)

// ================= 向导状态机（T15 拆分件 useUploadWizard；逐字迁出行为不变） =================
// 一步只解决一个子问题：① 作品是什么 ② 哪个模型做的 ③ 说清楚 —— 步内不留必填盲区。
// 依赖序：本件产 checklist/stepError 等 → playable 经 getter 惰性消费（渲染期求值）→ 其余件消费 aside/stamp。
// 编辑模式附加状态（提前声明：wizard 的 checklist 需读 currentCover，clearCover 归零 coverPreview）
const demoTitle = ref('')
const currentCover = ref('')
const coverPreview = ref('')
const denied = ref(false)
const initial = ref({ title: '', description: '', demoType: 'web', externalUrl: '', prompt: '', videoUrl: '' })

// 可玩性（T15 拆分件 useUploadPlayable）：旁白 aside / 盖章 stamp 由本件产出，供 tagSuggest/taskMount/draft 与
// pickModel/resetAll 等消费；getter 显式返回类型断开 playable↔wizard 类型推断环（值流：渲染期求值）
const {
  asideOn, lastAside, toggleAside, stamped, rank, expNo, modelStats, statsLoading, drawnTask, drawing, drawTask, aside, stamp,
} = useUploadPlayable({
  checklist: { get value(): { label: string; done: boolean; step: number; must: boolean }[] { return wizard.checklist.value } },
  allDone: { get value(): number { return wizard.allDone.value } },
  mustDone: { get value(): number { return wizard.mustDone.value } },
  chosenModelNames, modelUncertain, hasModel, prompt, idempotencyKey,
})

// ---------- §4.2 标签建议包（T15 拆分件 useTagSuggest；逐字迁出行为不变） ----------
const {
  pack, packLoading, packIgnored, packVisible, addSuggestion, addAllSuggestions, bringBackPack, ignorePack,
} = useTagSuggest({ editSlug, title, description, prompt, selected, stamp, aside })

// 向导状态机（T15 拆分件 useUploadWizard）
const wizard = useUploadWizard({
  editSlug, ui, tagKeys, selected, selectedCount, tagsOk, hasModel, modelUncertain, chosenModelNames,
  title, description, prompt, demoType, externalUrl, videoUrl, zipFile, coverFile, coverPreview, currentCover, modelHint,
  success, aside, stamp,
})
const {
  step, pickModel, descOk, promptOk,
  stepProblems, stepOk, checklist, mustDone, mustTotal, allDone, readyToSubmit,
  stepError, goStep, next, isDirty, onBeforeUnload,
  reviewRows, stepDefs, typeOptions, barPct,
  modelQuery, filteredExact, fbVendorOpen, fileInputKey, clearFile, clearCover, clearModel, selectedList,
  vendorFamilies, unknownValue, guessValue,
} = wizard

/** 清空重来：整页只有一个破坏性动作，所以必须二次确认（跨件编排：清向导态+建议包+草稿，留主件） */
async function resetAll() {
  const ok = await ui.confirm({
    title: t('upload.resetConfirmTitle', '清空重来？'),
    message: t('upload.resetConfirmMsg', '标题、描述、提示词、标签、文件与封面都会被清掉，无法恢复。'),
    confirmText: t('upload.resetDo', '清空'),
    danger: true,
  })
  if (!ok) return
  title.value = ''
  description.value = ''
  prompt.value = ''
  videoUrl.value = ''
  externalUrl.value = ''
  uploadCode.value = ''
  modelHint.value = ''
  pack.value = []
  selected.value = {}
  pendingTagApplies.value = []
  proposeTitle.value = ''
  proposeDesc.value = ''
  proposeCategory.value = ''
  clearFile()
  clearCover()
  error.value = ''
  stepError.value = ''
  success.value = null
  idempotencyKey.value = (crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`) as string
  clearDraft()
  step.value = 1
  aside('reset', t('upload.asReset', '白纸一张。'))
}
/** 挑战横幅可摘掉：不想挂题就不挂，别被 URL 参数绑架 */
const challengeOff = ref(false)
function dropChallenge() {
  challengeOff.value = true
  pickedTask.value = null
  if (title.value.startsWith(`${t('upload.challengePrefix', '挑战')}：`)) title.value = ''
}

// ---------- 挂题（T15 拆分件 useTaskMount；逐字迁出行为不变） ----------
// 唯一状态源：从题目页带 ?task= 进来、或在这里主动选，都写进 pickedTask（loadChallenge 亦写此源）。
const {
  pickedTask, taskQuery, taskHits, taskSearching, taskPickerOpen, pickTask, clearTask, openTaskPicker, simPct,
} = useTaskMount({ title, description, prompt, aside })

// ---------- 草稿持久化（T15 拆分件 useUploadDraft；逐字迁出行为不变） ----------
// 边界（必须如实告知）：File 对象存不进 localStorage，所以文件与封面不恢复，需重选。
const {
  draftFound, resumeDraft, discardDraft, clearDraft, loadDraftRaw, hasDraftContent,
} = useUploadDraft({
  editSlug, title, description, prompt, videoUrl, externalUrl, demoType, modelHint, selected, step, idempotencyKey, aside,
})

// 宽屏（≥1281px）两栏布局：标签面板常驻右侧；窄屏默认收起、点横条展开
const isWide = ref(false)
let wideMq: MediaQueryList | null = null
function onWideChange(e: MediaQueryListEvent) {
  isWide.value = e.matches
}
onMounted(() => {
  wideMq = window.matchMedia('(min-width: 1281px)')
  isWide.value = wideMq.matches
  wideMq.addEventListener('change', onWideChange)
  window.addEventListener('beforeunload', onBeforeUnload)
})
onBeforeUnmount(() => {
  wideMq?.removeEventListener('change', onWideChange)
  window.removeEventListener('beforeunload', onBeforeUnload)
})

// （编辑模式附加状态已在 useUploadWizard 调用前声明——原此处的重复块随 T15 拆分移除）

function collectTags() {
  const out: (string | { key: string; value: string; description?: string })[] = []
  for (const [key, values] of Object.entries(selected.value)) {
    const k = tagKeys.value.find((x) => x.key === key)
    for (const v of values) {
      if (k?.mode === 'fixed') {
        out.push(`${key}:${v.value}`)
      } else {
        out.push({ key, value: v.value, description: v.description || undefined })
      }
    }
  }
  return out
}

function prefillTags(tags: { key: string; value: string }[]) {
  const map: Record<string, { value: string; description: string }[]> = {}
  for (const t of tags) {
    if (t.key === 'author') continue
    if (!tagKeys.value.some((k) => k.key === t.key)) continue
    ;(map[t.key] = map[t.key] || []).push({ value: t.value, description: '' })
  }
  selected.value = map
  initialTagsKey.value = JSON.stringify(map)
}

// v2 B4′：挑战上下文 —— 题面来自 /tasks/{slug}，原题提示词取成员作品里第一条非空的
const challengePrompt = computed(() =>
  (challenge.value?.demos.find((d) => (d.prompt || '').trim())?.prompt || '').trim(),
)
const promptCopied = ref(false)

const library = usePromptLibraryUpload({
  id: computed(() => route.query.prompt),
  editing: computed(() => !!editSlug || typeof route.query.slug === 'string'),
  prompt, title,
  hasDraft: () => !!draftFound.value,
  fetch: (id) => api.getPrompt(id),
})
const libraryPrompt = library.entry
const libraryLoading = library.loading
const libraryError = library.error

function fillLibraryPrompt() {
  library.apply()
  ui.toast(t('upload.promptLibraryCopied', '已填入提示词'), 'success')
}

async function loadChallenge() {
  if (!challengeSlug || editSlug) return
  try {
    challenge.value = await api.getTask(challengeSlug)
    // 带 ?task= 进来时，选择器与横幅共用同一个状态源
    pickedTask.value = { slug: challenge.value.slug, title: challenge.value.title }
    // 有草稿时不抢标题：作者填过的东西优先于系统预填
    if (!title.value.trim() && !draftFound.value) {
      title.value = `${t('upload.challengePrefix', '挑战')}：${challenge.value.title}`
    }
  } catch {
    challenge.value = null
  }
}

function fillChallengePrompt() {
  if (challengePrompt.value) prompt.value = challengePrompt.value
}

async function copyChallengePrompt() {
  if (!challengePrompt.value) return
  try {
    await navigator.clipboard.writeText(challengePrompt.value)
    promptCopied.value = true
    setTimeout(() => (promptCopied.value = false), 1400)
  } catch {
    /* 剪贴板被拒时用户可手动选中复制 */
  }
}

onMounted(async () => {
  idempotencyKey.value = (crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`) as string
  // 只探测、不自动覆盖：是否续填由作者决定（误恢复比误清空更难被发现）
  if (!editSlug) {
    const p = loadDraftRaw()
    if (p && hasDraftContent(p)) draftFound.value = { savedAt: p.savedAt || 0, title: (p.title || '').slice(0, 24) }
  }
  void loadChallenge()
  void library.load()
  try {
    await tagsStore.load()
  } catch {
    /* 静默 */
  }
  if (editSlug) {
    loading.value = true
    try {
      const demo: DemoDetail = await api.getDemo(editSlug)
      demoTitle.value = demo.title
      currentCover.value = demo.cover_url
      title.value = demo.title
      description.value = demo.description
      demoType.value = demo.demo_type || 'web'
      externalUrl.value = demo.external_url || ''
      prompt.value = demo.prompt || ''
      videoUrl.value = demo.video_url || ''
      prefillTags(demo.tags)
      initial.value = {
        title: demo.title,
        description: demo.description,
        demoType: demo.demo_type || 'web',
        externalUrl: demo.external_url || '',
        prompt: demo.prompt || '',
        videoUrl: demo.video_url || '',
      }
      const canEdit = auth.user?.role === 'admin' || !!demo.is_author
      if (!canEdit) {
        denied.value = true
        error.value = t('upload.errDenied', '你没有权限编辑这个 Demo')
      } else {
        // 编辑的意图几乎总是改信息，直接落在第 3 步，省掉两次空点击
        step.value = 3
      }
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }
})

/**
 * 封面选图：**端侧先压**再上传。
 *
 * 原先这里零校验零压缩 —— 一张 12MB 的手机照原样上传（慢、费流量），而带宽是本站的明确约束
 * （docs/预览架构与排坑记录.md「带宽与成本意识」）。现在统一走 utils/imageCompress：
 * 最长边 1600 / webp q0.82（服务端 compress_cover 再归一到 1280，两级参数刻意错开避免叠加失真）。
 *
 * 三条不变量：① 任何压缩失败都回退原文件继续上传，绝不阻断；② 小图原样直通（幂等）；
 * ③ 预览用压缩后的 blob（更快、内存更小）。不给用户看体积数字（"无感"）。
 */
const coverOptimizing = ref(false)
async function onCoverChange(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0] || null
  coverPreview.value = ''
  if (!file) {
    coverFile.value = null
    return
  }
  coverFile.value = file // 先占位：压缩失败也不至于丢文件
  coverOptimizing.value = true
  try {
    const res = await compressImage(file, { maxEdge: 1600 })
    coverFile.value = res.file
    coverPreview.value = URL.createObjectURL(res.file)
  } catch {
    // 理论上 compressImage 内部已兜底，这里只是最后一道保险
    coverPreview.value = URL.createObjectURL(file)
  } finally {
    coverOptimizing.value = false
  }
}

function onZipChange(e: Event) {
  zipFile.value = (e.target as HTMLInputElement).files?.[0] || null
}

const hasChanges = computed(() => {
  if (!editSlug) return true
  return (
    title.value.trim() !== initial.value.title ||
    description.value.trim() !== initial.value.description ||
    demoType.value !== initial.value.demoType ||
    externalUrl.value.trim() !== initial.value.externalUrl ||
    prompt.value.trim() !== initial.value.prompt ||
    videoUrl.value.trim() !== initial.value.videoUrl ||
    JSON.stringify(selected.value) !== initialTagsKey.value ||
    !!coverFile.value ||
    !!zipFile.value ||
    !!commitMessage.value.trim()
  )
})

async function submit(force = false) {
  // 防抖：提交中忽略重复触发（连点 / 回车连按）
  if (submitting.value) return
  // 向导门禁：先定位到第一个不满足的必答步，把错误带到问题现场（而不是只弹一条横幅）
  const badStep = [1, 2, 3].find((n) => !stepOk.value[n])
  if (badStep) {
    step.value = badStep
    stepError.value = stepProblems.value[badStep][0] || t('upload.errStep', '这一步还差必填项')
    return
  }
  if (!title.value.trim()) {
    error.value = t('upload.errTitle', '请填写标题')
    return
  }
  if (demoType.value === 'link') {
    if (!/^https?:\/\//.test(externalUrl.value.trim())) {
      error.value = t('upload.errLink', '链接类型需要填写 http(s) 地址')
      return
    }
  } else if (!editSlug && !zipFile.value) {
    error.value = t('upload.errFile', '请上传文件（zip 或单个 .html/.svg）')
    return
  }
  const tags = collectTags()
  // Q2：模型必选先校验（与后端同规则）；失败时展开标签面板 —— 选择器默认就落在「模型」键上
  if (!hasModel.value && !editSlug) {
    error.value = t('upload.errModel', '请选择模型：不确定就选「未标注 / 未定型号」这类兜底值，别空着')
    tagsOpen.value = true
    return
  }
  if (!tags.length) {
    error.value = t('upload.errTags', '请至少选择一个标签（标签是作品分类的关键）')
    tagsOpen.value = true
    return
  }

  // 编辑模式：没有任何改动时阻止提交，避免生成空公告
  if (editSlug && !hasChanges.value) {
    error.value = t('upload.errNoChanges', '没有任何修改，未提交')
    return
  }

  submitting.value = true
  uploadProgress.value = 0
  error.value = ''
  dupSlug.value = null
  success.value = null
  const onProgress = (p: number) => {
    uploadProgress.value = p
  }
  try {
    if (editSlug) {
      await api.updateDemo(
        editSlug,
        {
          title: title.value.trim(),
          description: description.value.trim(),
          tags,
          demo_type: demoType.value,
          external_url: demoType.value === 'link' ? externalUrl.value.trim() : externalUrl.value.trim() || undefined,
          prompt: prompt.value.trim(),
          video_url: videoUrl.value.trim() || undefined,
          cover: coverFile.value,
          file: zipFile.value,
          commit_message: commitMessage.value.trim() || undefined,
          keep_old_version: keepOldVersion.value,
        },
        onProgress,
        force,
      )
      success.value = { slug: editSlug, status: 'updated' }
    } else {
      const created = await api.createDemo(
        {
          title: title.value.trim(),
          description: description.value.trim(),
          tags,
          demo_type: demoType.value,
          external_url: demoType.value === 'link' ? externalUrl.value.trim() : externalUrl.value.trim() || undefined,
          prompt: prompt.value.trim(),
          video_url: videoUrl.value.trim() || undefined,
          cover: coverFile.value,
          file: zipFile.value,
          idempotency_key: idempotencyKey.value || undefined,
          upload_code: uploadCode.value.trim() || undefined,
          // 挂题两路互斥：有 pickedTask 走 task=，否则有提议走 propose_task（闭环 B）
          task: pickedTask.value?.slug,
          propose_task:
            !pickedTask.value && proposeTitle.value.trim()
              ? {
                  title: proposeTitle.value.trim(),
                  description: proposeDesc.value.trim() || undefined,
                  category: proposeCategory.value.trim() || undefined,
                }
              : undefined,
          model_hint: modelUncertain.value ? modelHint.value.trim() || undefined : undefined,
          force: forceUpload.value || undefined,
        },
        onProgress,
      )
      success.value = created
      // 闭环 A：createDemo 成功后再把待审新 fixed 值带 demo_id 逐个提交申请，然后清空本地待审
      if (created.id && pendingTagApplies.value.length) {
        await Promise.all(
          pendingTagApplies.value.map((p) =>
            api.suggestTagValue({
              key: p.key,
              value: p.value,
              description: p.description || undefined,
              demo_id: created.id,
            }),
          ),
        )
        pendingTagApplies.value = []
      }
      // 成功了就别再留草稿：下次进来该是白纸，而不是"继续上次那件已发布的"
      clearDraft()
    }
  } catch (e) {
    error.value = (e as Error).message
    // 409 内容重复：后端 detail 含 /demo/<slug>，解析出已有 demo 供跳转
    const m = /\/demo\/([^/\s]+)/.exec((e as Error).message)
    dupSlug.value = m ? m[1] : null
    // P4：编辑路径原先没有 bypass —— 与新建路径不对称（POST 一直带 force=1），
    // 于是"我要更新这件、内容确实没大改"会被 409 卡死。这里给出第二个出口。
    dupInEdit.value = !!(m && editSlug)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="route-page">  <PageHero>
    <span class="eyebrow">{{ editSlug ? t('upload.editEyebrow', '编辑 Demo') : t('app.nav.upload', '上传 Demo') }}</span>
    <h1 class="page-title">{{ editSlug ? demoTitle || t('upload.edit', '编辑') : t('upload.new', '上传') }}</h1>
    <p class="sub">
      {{ editSlug
        ? t('upload.editSub', '改信息或重新上传文件；改动会写入时间线并生成更新公告。')
        : t('upload.newSub', '三步：先说清是什么、再说哪个模型做的、最后把它讲明白。未登录也能以公开用户身份发布。') }}
    </p>
    <p v-if="!editSlug" class="notice notice-info upload-ai-hint">
      {{ t('upload.aiHint', '不会打包、不确定 HTML 能不能单独打开？把文件丢给 AI，让它按指南检查「自包含 / zip 能跑 / 封面可截图」再代传。') }}
      <a href="/api/v1/meta/agent-guide" target="_blank" rel="noopener">{{ t('upload.aiHintLink', 'AI 自动上传指南 →') }}</a>
    </p>
  </PageHero>

  <section class="section" style="padding-top: var(--sp-8)">
    <div v-if="loading" class="loading-row"><span class="spinner"></span> {{ t('demo.loading', '加载 Demo…') }}</div>

    <div v-else-if="denied" class="empty-box" style="max-width: 560px">
      <p style="margin-bottom: 18px">{{ error }}</p>
      <RouterLink class="btn btn-outline" to="/">{{ t('notFound.back', '返回首页') }}</RouterLink>
    </div>

    <div v-else class="upload-grid" :class="{ 'panel-open': tagsOpen }">
      <div class="card card-default upload-form-card" style="padding: var(--sp-24)">
        <!-- 草稿恢复：只探测不自动覆盖（误恢复比误清空更难被发现，所以必须让作者点头） -->
        <div v-if="draftFound && !success" class="uw-draft">
          <span>
            {{ t('upload.draftFound', '发现上次没填完的草稿') }}
            <b v-if="draftFound.title">「{{ draftFound.title }}…」</b>
            <span class="muted">· {{ t('upload.draftNoFile', '文件与封面需重新选') }}</span>
          </span>
          <!-- 按钮语义不能倒置：安全动作（继续填）用主色，破坏性动作（丢弃）用弱色 + 确认 -->
          <button type="button" class="btn btn-sm btn-secondary" @click="resumeDraft">{{ t('upload.draftResume', '继续填') }}</button>
          <button type="button" class="btn btn-sm btn-ghost" @click="discardDraft">{{ t('upload.draftDiscard', '丢弃') }}</button>
        </div>

        <!-- 步骤条：一步只解决一个子问题；可任意回退，状态不丢 -->
        <ol class="uw-steps">
          <li v-for="s in stepDefs" :key="s.n">
            <button
              type="button"
              class="uw-step"
              :class="{ active: step === s.n, ok: s.n <= 3 && stepOk[s.n] }"
              :aria-current="step === s.n ? 'step' : undefined"
              @click="goStep(s.n)"
            >
              <span class="uw-step-n mono">{{ s.n }}</span>
              <span class="uw-step-body">
                <b>{{ s.title }}</b>
                <span class="uw-step-hint">{{ s.hint }}</span>
              </span>
            </button>
          </li>
        </ol>

        <!-- 完成度仪表盘（T15 拆分件 CompletenessDash）：goal-gradient —— 让人一直看得见"还剩几步"，点击直达 -->
        <CompletenessDash
          :rank="rank"
          :all-done="allDone"
          :checklist="checklist"
          :must-done="mustDone"
          :must-total="mustTotal"
          :bar-pct="barPct"
          :step="step"
          :aside-on="asideOn"
          @go="goStep"
          @toggle-aside="toggleAside"
        />
        <p v-if="lastAside" class="uw-aside" aria-live="polite">{{ lastAside }}</p>

        <!-- P4：写成 submit() 而不是 submit —— submit 现在带 force 参数，
             直接绑会把 submit 事件当 force 传进去（永远是 truthy），等于编辑一律强推。 -->
        <form class="form-stack" @submit.prevent="submit()">
        <div v-if="!editSlug && (libraryPrompt || libraryLoading || libraryError)" class="card card-mint challenge-card" aria-live="polite">
          <p v-if="libraryLoading" class="hint">{{ t('common.loading', '加载中…') }}</p>
          <template v-else-if="libraryPrompt">
            <div class="filter-row" style="margin-bottom: var(--sp-6)">
              <span class="mini-stat">{{ t('upload.promptLibraryTitle', '来自提示词库') }}</span>
              <b>{{ libraryPrompt.title }}</b>
              <button class="btn btn-sm btn-primary" type="button" @click="fillLibraryPrompt">{{ t('upload.promptLibraryApply', '填入这条提示词') }}</button>
              <RouterLink class="btn btn-sm btn-outline" :to="`/prompts/${libraryPrompt.id}`">{{ t('upload.promptLibraryBack', '查看提示词') }}</RouterLink>
            </div>
            <p class="challenge-prompt mono">{{ libraryPrompt.content }}</p>
            <p class="hint">{{ t('upload.promptLibraryHint', '提交作品时保留原文，可关联到同提示词结果。') }}</p>
          </template>
          <template v-else>
            <p class="hint">{{ t('upload.promptLibraryError', '提示词加载失败，请重试') }}</p>
            <button class="btn btn-sm btn-outline" type="button" @click="library.load()">{{ t('common.retry', '重试') }}</button>
          </template>
        </div>
        <!-- v2 B4′：挑战上下文（跨步保留，因为题面同时喂给标题与提示词） -->
        <div v-if="challenge && !challengeOff" class="card card-mint challenge-card">
          <div class="filter-row" style="margin-bottom: var(--sp-6)">
            <span class="cluster-badge cb-exact">CHALLENGE</span>
            <b>{{ challenge.title }}</b>
            <span class="mini-stat"><b>{{ challenge.demos_total }}</b> {{ t('upload.challengeWorks', '个已有作品') }}</span>
            <button v-if="challengePrompt" class="btn btn-sm btn-secondary" type="button" style="margin-left: auto" @click="fillChallengePrompt">
              {{ t('upload.challengeFill', '填入原题面') }}
            </button>
            <button v-if="challengePrompt" class="btn btn-sm btn-outline" type="button" @click="copyChallengePrompt">
              {{ promptCopied ? t('upload.copied', '已复制') : t('upload.challengeCopy', '复制原题面') }}
            </button>
            <!-- 挂了题面就该能摘掉：不想进同题对比时别被 URL 参数绑架 -->
            <button class="btn btn-sm btn-dark" type="button" style="margin-left: auto" @click="dropChallenge">✕ {{ t('upload.dropChallenge', '不挑战这题了') }}</button>
          </div>
          <p v-if="challenge.description" class="muted" style="margin: 0 0 var(--sp-6)">{{ challenge.description }}</p>
          <p v-if="challengePrompt" class="challenge-prompt mono">{{ challengePrompt }}</p>
          <p class="hint" style="margin: var(--sp-6) 0 0">{{ t('upload.challengeHint', '提交后挂题请求进入管理员确认队列，通过才会出现在同题对比里。') }}</p>
        </div>

        <!-- ============ ① 作品是什么 ============ -->
        <fieldset v-show="step === 1" class="uw-panel">
          <legend>{{ t('upload.s1Legend', '这个作品是什么？') }}</legend>
          <!-- 类型用大卡片：选项全可见（省一次展开的记忆负担）+ 命中区域大（Fitts） -->
          <div class="uw-types">
            <button
              v-for="o in typeOptions"
              :key="o.value"
              type="button"
              class="uw-type b-lift"
              :class="{ active: demoType === o.value }"
              :aria-pressed="demoType === o.value"
              data-step-focus="1"
              @click="demoType = o.value"
            >
              <b>{{ o.label }}</b>
              <span class="uw-type-hint">{{ o.hint }}</span>
            </button>
          </div>

          <label v-if="demoType === 'link'" class="field">
            {{ t('upload.linkUrl', '外部链接地址（必填）') }}
            <input
              v-model="externalUrl"
              class="input"
              :class="{ 'uw-field-bad': externalUrl.trim() && !/^https?:\/\//.test(externalUrl.trim()) }"
              :aria-invalid="externalUrl.trim() && !/^https?:\/\//.test(externalUrl.trim()) ? true : undefined"
              placeholder="https://…"
              data-step-focus="1"
            />
            <span class="hint">
              {{ /^https?:\/\//.test(externalUrl.trim()) ? t('upload.linkOk', '地址格式没问题 · 直接跳转打开，服务器不存储内容') : t('upload.linkHint', '直接跳转打开，服务器不存储内容') }}
            </span>
          </label>
          <label v-else class="field">
            {{ demoType === 'image' ? t('upload.fileImage', '作品图片') : t('upload.file', '文件') }}{{ demoType === 'image' ? '（必填）' : editSlug ? t('upload.fileEdit', '（可选，不选则保留原文件）') : '' }}
            <!-- key 重挂才能真的清空原生 file input（直接置 null 在部分浏览器无效） -->
            <input :key="fileInputKey" class="input" type="file" :accept="demoType === 'image' ? 'image/png,image/jpeg,image/webp' : '.zip,application/zip,.html,.htm,.svg,text/html,image/svg+xml'" data-step-focus="1" @change="onZipChange" />
            <span class="hint">
              {{ demoType === 'image'
                ? t('upload.fileImageHint', '即图为作品，不需另传封面；站内展示 1280 WebP，不保留上传原图')
                : demoType === 'web'
                  ? t('upload.fileWebHint', '支持 zip（根目录需含 index.html）或单个 .html/.svg；单 HTML 必须自包含（内联 CSS/JS，双击可直接打开）')
                  : t('upload.fileZipHint', 'zip 文件包（不要求 index.html）') }}
            </span>
            <span v-if="zipFile" class="uw-picked">
              <span class="mono">{{ zipFile.name }}</span>
              <span class="muted mono">{{ (zipFile.size / 1048576).toFixed(1) }} MB</span>
              <button type="button" class="uw-x" :aria-label="t('upload.removeFile', '移除文件')" @click="clearFile">✕</button>
            </span>
          </label>
        </fieldset>

        <!-- ============ ② 哪个模型做的（必答）（T15 拆分件 StepModelAssert） ============ -->
        <StepModelAssert
          v-show="step === 2"
          v-model:model-query="modelQuery"
          v-model:model-hint="modelHint"
          v-model:fb-vendor-open="fbVendorOpen"
          :filtered-exact="filteredExact"
          :chosen-model-names="chosenModelNames"
          :vendor-families="vendorFamilies"
          :unknown-value="unknownValue"
          :guess-value="guessValue"
          :model-uncertain="modelUncertain"
          :stamped="stamped"
          :model-stats="modelStats"
          :stats-loading="statsLoading"
          @pick="pickModel"
          @clear="clearModel"
        />

        <!-- ============ ③ 说清楚（T15 拆分件 StepDescribe） ============ -->
        <StepDescribe
          v-show="step === 3"
          v-model:title="title"
          v-model:description="description"
          v-model:prompt="prompt"
          v-model:video-url="videoUrl"
          v-model:commit-message="commitMessage"
          v-model:keep-old-version="keepOldVersion"
          v-model:upload-code="uploadCode"
          v-model:force-upload="forceUpload"
          v-model:tags-open="tagsOpen"
          v-model:task-query="taskQuery"
          v-model:task-picker-open="taskPickerOpen"
          v-model:propose-title="proposeTitle"
          v-model:propose-desc="proposeDesc"
          v-model:propose-category="proposeCategory"
          :desc-ok="descOk"
          :prompt-ok="promptOk"
          :edit-slug="editSlug"
          :demo-type="demoType"
          :zip-file="zipFile"
          :cover-file="coverFile"
          :current-cover="currentCover"
          :cover-preview="coverPreview"
          :is-wide="isWide"
          :is-admin="auth.isAdmin()"
          :idempotency-key="idempotencyKey"
          :exp-no="expNo"
          :picked-task="pickedTask"
          :task-hits="taskHits"
          :task-searching="taskSearching"
          :sim-pct="simPct"
          :pack-visible="packVisible"
          :pack-loading="packLoading"
          :pack-ignored="packIgnored"
          :drawn-task="drawnTask"
          :drawing="drawing"
          :chosen-model-names="chosenModelNames"
          :selected-list="selectedList"
          :selected-count="selectedCount"
          @pick-task="pickTask"
          @clear-task="clearTask"
          @open-task-picker="openTaskPicker"
          @add-suggestion="addSuggestion"
          @add-all-suggestions="addAllSuggestions"
          @bring-back-pack="bringBackPack"
          @ignore-pack="ignorePack"
          @draw-task="drawTask"
          @cover-change="onCoverChange"
          @clear-cover="clearCover"
        />

        <!-- ============ ④ 核对并提交（T15 拆分件 StepReview） ============ -->
        <StepReview
          v-show="step === 4"
          :review-rows="reviewRows"
          :error="error"
          :dup-slug="dupSlug"
          :dup-in-edit="dupInEdit"
          @force-submit="submit(true)"
          :success="success"
          :challenge="challenge"
          :challenge-off="challengeOff"
          :edit-slug="editSlug"
          :submitting="submitting"
          :upload-progress="uploadProgress"
          @go="goStep"
          @reset="resetAll"
        />

          <!-- 步内问题：醒目红块。原先是裸 bullet，和上方清单长得一样，读起来像"又一项待办"而不是"这里卡住了" -->
          <p v-if="stepError" class="uw-err" role="alert" aria-live="assertive">{{ stepError }}</p>
          <div v-else-if="stepProblems[step]?.length" class="uw-problems" aria-live="polite">
            <b>{{ t('upload.stillMissing', '这一步还差') }}</b>
            <ul>
              <li v-for="p in stepProblems[step]" :key="p">{{ p }}</li>
            </ul>
          </div>

          <div class="uw-nav">
            <button v-if="step > 1" class="btn btn-outline" type="button" @click="goStep(step - 1)">← {{ t('upload.prev', '上一步') }}</button>
            <!-- 破坏性动作远离主行动按钮：并排放着迟早有人误点 -->
            <button v-if="!editSlug && isDirty && !success" class="btn btn-ghost" type="button" @click="resetAll">{{ t('upload.resetAll', '清空重来') }}</button>
            <span class="uw-nav-spacer"></span>
            <button v-if="step < 4" class="btn btn-secondary btn-lg" type="button" @click="next">{{ t('upload.next', '下一步') }} →</button>
            <button
              v-else
              class="btn btn-primary btn-lg"
              :class="{ 'is-uploading': submitting }"
              type="submit"
              :disabled="submitting || !readyToSubmit"
              :title="readyToSubmit ? '' : t('upload.needMust', '必答项还没齐：' + checklist.filter((c) => c.must && !c.done).map((c) => c.label).join('、'))"
            >
              {{ submitting ? (uploadProgress >= 100 ? t('upload.processingShort', '处理中…') : t('upload.uploadingN', '上传中 {n}%', { n: uploadProgress })) : editSlug ? t('upload.saveChanges', '保存修改') : t('upload.submit', '确认发布') }}
            </button>
          </div>
        </form>
      </div>

      <Teleport to="body">
        <div v-if="tagsOpen" class="tag-modal">
          <div class="tag-modal-mask" @click="tagsOpen = false"></div>
          <div class="tag-modal-panel">
            <div class="tag-modal-head">
              <span class="filter-label">{{ t('upload.tagPicker', '标签选择') }}</span>
              <button class="btn btn-sm btn-dark" type="button" @click="tagsOpen = false">{{ t('common.close', '关闭') }}</button>
            </div>
        <div class="tag-drawer-head">
          <!-- 模型与标签推导已归位到步骤 ② ③；抽屉只保留「全词表编辑器」这一职责，不再重复同一信息 -->
          <span class="hint">
            {{ t('upload.tagPickerHint', '固定值点选 · 自定义值输入添加 · 数字值填整数 · author 系统保留') }}
            <span v-if="chosenModelNames.length" class="uw-drawer-model">
              · {{ t('upload.drawerModelNow', '模型') }}：{{ chosenModelNames.map((m) => tagLabel(m)).join(' / ') }}
            </span>
          </span>
        </div>

        <TagPicker v-model="selectedTags" :defer-apply="true" v-model:pending-apply="pendingTagApplies" />
          </div>
        </div>
      </Teleport>
    </div>
  </section>
  </div>
</template>
