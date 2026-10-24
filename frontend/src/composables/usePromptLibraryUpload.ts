import { onScopeDispose, ref, watch, type Ref } from 'vue'
import type { PromptSummary } from '../api/types'

/** 异步预填仅用于空白新稿；恢复草稿、编辑和在途输入都由作者保有。 */
export function usePromptLibraryUpload(deps: {
  id: Readonly<Ref<unknown>>
  editing: Readonly<Ref<boolean>>
  prompt: Ref<string>
  title: Ref<string>
  hasDraft: () => boolean
  fetch: (id: number) => Promise<PromptSummary>
}) {
  const entry = ref<PromptSummary | null>(null)
  const loading = ref(false)
  const error = ref('')
  let generation = 0
  let disposed = false

  function apply() {
    if (!entry.value || deps.editing.value || disposed) return
    deps.prompt.value = entry.value.content
    if (!deps.title.value.trim()) deps.title.value = entry.value.title
  }

  async function load() {
    const mine = ++generation
    entry.value = null
    error.value = ''
    loading.value = false
    const raw = deps.id.value
    if (deps.editing.value || raw === undefined || raw === null || raw === '' || disposed) return
    const id = typeof raw === 'string' && /^\d+$/.test(raw) ? Number(raw) : NaN
    if (!Number.isSafeInteger(id) || id < 1) {
      error.value = 'invalid-id'
      return
    }
    loading.value = true
    try {
      const result = await deps.fetch(id)
      if (mine !== generation || disposed || deps.editing.value) return
      entry.value = result
      if (!deps.hasDraft() && !deps.prompt.value.trim()) apply()
    } catch {
      if (mine === generation && !disposed) error.value = 'load-failed'
    } finally {
      if (mine === generation && !disposed) loading.value = false
    }
  }

  watch([deps.id, deps.editing], () => { void load() })
  onScopeDispose(() => { disposed = true; generation++ })
  return { entry, loading, error, load, apply }
}
