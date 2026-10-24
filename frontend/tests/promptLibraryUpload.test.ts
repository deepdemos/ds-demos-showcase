import { effectScope, nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import type { PromptSummary } from '../src/api/types'
import { usePromptLibraryUpload } from '../src/composables/usePromptLibraryUpload'

const entry = (id: number): PromptSummary => ({ id, title: `题目 ${id}`, content: `原文 ${id}`, content_excerpt: `原文 ${id}`,
  content_truncated: false, category: null, source_url: null, source: 'contributed', created_at: '2026-10-23T00:00:00Z', demos_total: 0, models_total: 0 })

function setup(hasDraft = false, editing = false) {
  const id = ref<unknown>('1'), prompt = ref(''), title = ref(''), isEditing = ref(editing)
  const fetch = vi.fn(async (n: number) => entry(n))
  const scope = effectScope()
  const state = scope.run(() => usePromptLibraryUpload({ id, prompt, title, editing: isEditing, hasDraft: () => hasDraft, fetch }))!
  return { id, prompt, title, isEditing, fetch, scope, state }
}

describe('提示词库到作品上传的正文保护', () => {
  it('空白新稿自动填入固定原文', async () => {
    const s = setup(); await s.state.load()
    expect(s.prompt.value).toBe('原文 1'); expect(s.title.value).toBe('题目 1'); s.scope.stop()
  })
  it('检测到草稿时等待手动填入，不覆盖作者内容', async () => {
    const s = setup(true); s.prompt.value = '恢复的草稿'; await s.state.load()
    expect(s.prompt.value).toBe('恢复的草稿')
    s.state.apply(); expect(s.prompt.value).toBe('原文 1'); s.scope.stop()
  })
  it('编辑作品不读取或填入库提示词', async () => {
    const s = setup(false, true); await s.state.load()
    expect(s.fetch).not.toHaveBeenCalled(); expect(s.prompt.value).toBe(''); s.scope.stop()
  })
  it('请求期间输入的文字仍保留', async () => {
    const s = setup(); let resolve!: (p: PromptSummary) => void
    s.fetch.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    const pending = s.state.load(); s.prompt.value = '刚刚输入'; resolve(entry(1)); await pending
    expect(s.prompt.value).toBe('刚刚输入'); s.scope.stop()
  })
  it('旧请求无法覆盖新提示词上下文', async () => {
    const s = setup(); let resolve!: (p: PromptSummary) => void
    s.fetch.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    const pending = s.state.load(); s.id.value = '2'; await nextTick(); await nextTick()
    resolve(entry(1)); await pending
    expect(s.state.entry.value?.id).toBe(2); expect(s.prompt.value).toBe('原文 2'); s.scope.stop()
  })
  it('卸载后响应不写入表单', async () => {
    const s = setup(); let resolve!: (p: PromptSummary) => void
    s.fetch.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    const pending = s.state.load(); s.scope.stop(); resolve(entry(1)); await pending
    expect(s.prompt.value).toBe('')
  })
  it('非法 id 不发请求，失败可重试', async () => {
    const s = setup(); s.id.value = '1evil'; await s.state.load()
    expect(s.fetch).not.toHaveBeenCalled(); expect(s.state.error.value).toBeTruthy()
    s.id.value = '1'; await nextTick(); await nextTick()
    expect(s.state.error.value).toBe(''); expect(s.state.entry.value?.id).toBe(1); s.scope.stop()
  })
})
