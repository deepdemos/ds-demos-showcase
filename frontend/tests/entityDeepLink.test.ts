import { describe, expect, it } from 'vitest'
import type { SuggestionItem } from '../src/api/types'
import {
  adminEntityQuery,
  entityHrefTo,
  parseApproveIdent,
  promptSuggestionSource,
  suggestionEntityHref,
  tagSuggestionEntityQuery,
  tagsForWrite,
} from '../src/utils/entityDeepLink'

function item(partial: Partial<SuggestionItem> & Pick<SuggestionItem, 'kind'>): SuggestionItem {
  return {
    id: 1,
    payload: {},
    source: 'user',
    status: 'pending',
    created_at: '2026-01-01T00:00:00Z',
    ...partial,
  }
}

describe('entityDeepLink', () => {
  it('new_task 批准后从 result 解析 slug 深链实体详情', () => {
    const href = suggestionEntityHref(
      item({
        kind: 'new_task',
        payload: { title: '新题' },
        result: '题目 hello-task 挂 1 个作品',
        status: 'approved',
      }),
    )
    expect(href).toEqual({ kind: 'admin', query: adminEntityQuery('task', 'hello-task') })
    expect(entityHrefTo(href!)).toEqual({ path: '/admin', query: { tab: 'entities', type: 'task', id: 'hello-task' } })
  })

  it('task_match 用 payload.task_id 深链题目，不在队列改字段', () => {
    const href = suggestionEntityHref(item({ kind: 'task_match', payload: { task_id: 12, demo_id: 3 }, ref_id: 12 }))
    expect(href).toEqual({ kind: 'admin', query: adminEntityQuery('task', '12') })
  })

  it('new_model 用 result 箭头后的 slug', () => {
    expect(parseApproveIdent('模型 Foo → dsv4-flash', 'model')).toBe('dsv4-flash')
    const href = suggestionEntityHref(item({ kind: 'new_model', payload: { name: 'Foo' }, result: '模型 Foo → dsv4-flash' }))
    expect(href).toEqual({ kind: 'admin', query: adminEntityQuery('model', 'dsv4-flash') })
  })

  it('retag_demo 深链作品页（类型改的是 demo，不是知识实体字段）', () => {
    const href = suggestionEntityHref(item({ kind: 'retag_demo', payload: { demo_slug: 'pvz-demo', add: 'game' } }))
    expect(href).toEqual({ kind: 'demo', slug: 'pvz-demo' })
    expect(entityHrefTo(href!)).toEqual({ path: '/demo/pvz-demo' })
  })

  it('TagValueSuggestion 批准后用 tag id + key 深链（不并表）', () => {
    expect(tagSuggestionEntityQuery({ key: 'model', value: 'dsv4-flash' }, 88)).toEqual(
      adminEntityQuery('tag', '88', 'model'),
    )
    expect(tagSuggestionEntityQuery({ key: 'model', value: 'x' })).toBeNull()
  })

  it('pending new_task 只有 title 时不深链（实体尚未创建）', () => {
    expect(suggestionEntityHref(item({ kind: 'new_task', payload: { title: '新题' } }))).toBeNull()
  })

  it('new_prompt 批准后用服务端回填的 prompt_id 打开固定正文', () => {
    const href = suggestionEntityHref(item({
      kind: 'new_prompt',
      status: 'approved',
      payload: { title: '五阶魔方', prompt_id: 42 },
    }))
    expect(href).toEqual({ kind: 'prompt', id: 42 })
    expect(entityHrefTo(href!)).toEqual({ path: '/prompts/42' })
  })

  it.each(['pending', 'rejected'] as const)('new_prompt %s 时不公开正文入口', (status) => {
    expect(suggestionEntityHref(item({ kind: 'new_prompt', status, payload: { prompt_id: 42 } }))).toBeNull()
  })

  it.each([null, undefined, 0, -1, 1.5, '42/../admin', '0x2a', Number.MAX_SAFE_INTEGER + 1])(
    'new_prompt 不把无效 id %s 拼进路由', (promptId) => {
      expect(suggestionEntityHref(item({ kind: 'new_prompt', status: 'approved', payload: { prompt_id: promptId } }))).toBeNull()
    },
  )

  it('来源接受 HTTP(S) 绝对地址，保留路径和查询参数', () => {
    expect(promptSuggestionSource(item({ kind: 'new_prompt', payload: { source_url: ' https://example.org/prompts?id=42#text ' } })))
      .toBe('https://example.org/prompts?id=42#text')
    expect(promptSuggestionSource(item({ kind: 'new_prompt', payload: { source_url: 'http://example.org/prompt' } })))
      .toBe('http://example.org/prompt')
  })

  it.each(['javascript:alert(1)', 'data:text/html,test', 'file:///test.txt', '//example.org/prompt', '/prompts/42', 'not a url', '', null])(
    '来源拒绝非 HTTP(S) 绝对地址 %s', (sourceUrl) => {
      expect(promptSuggestionSource(item({ kind: 'new_prompt', payload: { source_url: sourceUrl } }))).toBeNull()
    },
  )

  it('来源拒绝内嵌账号密码（历史/异常 payload 防御）', () => {
    expect(promptSuggestionSource(item({ kind: 'new_prompt', payload: { source_url: 'https://user:pass@example.org/prompt' } }))).toBeNull()
    expect(promptSuggestionSource(item({ kind: 'new_prompt', payload: { source_url: 'http://user@example.org/prompt' } }))).toBeNull()
  })

  it('tagsForWrite 丢掉系统保留键', () => {
    expect(tagsForWrite([
      { key: 'model', value: 'dsv4-flash' },
      { key: 'author', value: 'alice' },
      { key: 'type', value: 'game' },
    ])).toEqual(['model:dsv4-flash', 'type:game'])
  })
})
