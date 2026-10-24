import type { DemoSummary, Paginated, PromptListItem, PromptListParams, PromptProposalIn, PromptProposalOut, PromptSummary, SuggestionItem } from './types'

type Entry = Pick<PromptSummary, 'id' | 'title' | 'content' | 'category' | 'source_url' | 'created_at'> & { contributed: boolean }
/** 内部全量行：列表字段 + 完整正文（PromptSummary 去掉摘要两字段；列表出口再降级为 content_excerpt） */
type FullRow = Omit<PromptListItem, 'content_excerpt' | 'content_truncated'> & { content: string }
const normalize = (text: string) => text.replace(/\r\n/g, '\n').trim()

/** 列表正文摘要上限：与后端契约一致（content_excerpt 最长 480 字符 + content_truncated 标记） */
const EXCERPT_MAX = 480

function excerptOf(content: string): { content_excerpt: string; content_truncated: boolean } {
  return content.length > EXCERPT_MAX
    ? { content_excerpt: content.slice(0, EXCERPT_MAX), content_truncated: true }
    : { content_excerpt: content, content_truncated: false }
}

/** 离线演示与真实接口共用可见性、固定正文和待审语义。 */
export function createMockPromptStore(deps: {
  demos: () => DemoSummary[]
  userId: () => number | null
  suggestions: SuggestionItem[]
  nextSuggestionId: () => number
}) {
  const entries = new Map<string, Entry>()
  let seq = 0

  function ensure(content: string, title: string, createdAt: string): Entry {
    const key = normalize(content)
    let entry = entries.get(key)
    if (!entry) {
      entry = { id: ++seq, title, content: key, category: null, source_url: null, created_at: createdAt, contributed: false }
      entries.set(key, entry)
    }
    return entry
  }

  /** 全量行（含完整正文）：内部搜索/去重/approve 都用它；列表出口另做摘要 */
  function rows(): FullRow[] {
    const visible = deps.demos().filter((d) => d.status === 'approved' && normalize(d.prompt || ''))
    for (const d of [...visible].sort((a, b) => a.created_at.localeCompare(b.created_at))) {
      ensure(d.prompt || '', d.title, d.created_at)
    }
    return [...entries.values()].flatMap((e) => {
      const works = visible.filter((d) => normalize(d.prompt || '') === e.content)
      if (!e.contributed && !works.length) return []
      // 与真实后端口径一致：分类取最早作品（后端按 min(Demo.id)，demos 数组序≈id 序）
      // 的 category 标签（多标签按字典序取首）；已收录条目的分类优先；模型数排除 deprecated。
      const earliest = works[0]
      const derivedCategory = earliest?.tags
        ?.filter((t) => t.key === 'category')
        .map((t) => t.value)
        .sort((a, b) => a.localeCompare(b))[0] ?? null
      const models = new Set(works.flatMap((d) => (d.models || []).filter((m) => m.status !== 'deprecated').map((m) => m.id)))
      return [{ id: e.id, title: e.title, content: e.content, category: e.category ?? derivedCategory,
        source_url: e.source_url, created_at: e.created_at,
        source: e.contributed ? 'contributed' as const : 'demo' as const,
        demos_total: works.length, models_total: models.size }]
    })
  }

  function paginate<T>(items: T[], page = 1, pageSize = 12): Paginated<T> {
    const p = Math.max(1, page)
    const size = Math.max(1, Math.min(100, pageSize))
    return { items: items.slice((p - 1) * size, p * size), total: items.length, page: p, page_size: size }
  }

  function list(params: PromptListParams = {}): Paginated<PromptListItem> {
    const q = (params.q || '').trim().toLowerCase()
    // 搜索仍走全量正文（rows()），列表出口才降级成摘要 —— 与后端契约一致
    const items = rows().filter((p) => (!q || `${p.title}\n${p.content}`.toLowerCase().includes(q))
      && (!params.category || p.category === params.category)
      && (params.has_results === undefined || (p.demos_total > 0) === params.has_results))
    items.sort((a, b) => (params.sort === 'most_tested' ? b.demos_total - a.demos_total : 0)
      || b.created_at.localeCompare(a.created_at) || b.id - a.id)
    // 出口不带全量 content：只给 480 字摘要 + 截断标记，全量正文走 get()
    return paginate(items.map((p): PromptListItem => ({
      id: p.id, title: p.title, category: p.category, source_url: p.source_url, created_at: p.created_at,
      source: p.source, demos_total: p.demos_total, models_total: p.models_total,
      ...excerptOf(p.content),
    })), params.page, params.page_size)
  }

  function get(id: number): PromptSummary {
    // 详情出口：摘要字段 + 完整正文（与后端契约的详情响应同形）
    const entry = rows().find((e) => e.id === id)
    if (!entry) throw new Error('提示词不存在或未公开')
    return { ...entry, ...excerptOf(entry.content) }
  }

  function demos(id: number, params: { page?: number; page_size?: number } = {}): Paginated<DemoSummary> {
    const entry = get(id)
    const works = deps.demos().filter((d) => d.status === 'approved' && normalize(d.prompt || '') === entry.content)
      .sort((a, b) => b.created_at.localeCompare(a.created_at) || a.slug.localeCompare(b.slug))
    return paginate(works, params.page, params.page_size)
  }

  function validate(input: PromptProposalIn): PromptProposalIn {
    const title = input.title.trim()
    const content = normalize(input.content)
    const category = input.category?.trim() || null
    const source = input.source_url?.trim() || null
    if (!title || title.length > 200 || !content || content.length > 20000 || (category?.length || 0) > 64) {
      throw new Error('请检查标题、正文和分类长度')
    }
    if (source) {
      let url: URL
      try { url = new URL(source) } catch { throw new Error('来源需为完整的 HTTP(S) 链接') }
      const authority = source.match(/^https?:\/\/([^/?#]*)/i)?.[1]
      if (!authority || authority.includes('@') || !['http:', 'https:'].includes(url.protocol) || source.length > 2048 || url.username || url.password
        || Array.from(source).some((char) => /\s/u.test(char) || char.charCodeAt(0) < 32 || char === '\u0085')) throw new Error('来源需为完整的 HTTP(S) 链接')
    }
    return { title, content, category, source_url: source }
  }

  function propose(input: PromptProposalIn): PromptProposalOut {
    const userId = deps.userId()
    if (userId === null) throw new Error('请先登录')
    const body = validate(input)
    const existing = rows().find((p) => p.content === body.content)
    if (existing) return { status: 'existing', prompt_id: existing.id }
    const pending = deps.suggestions.find((s) => s.kind === 'new_prompt' && s.status === 'pending'
      && s.payload.created_by === userId && s.payload.content === body.content)
    if (pending) return { status: 'pending', suggestion_id: pending.id }
    const id = deps.nextSuggestionId()
    deps.suggestions.push({ id, kind: 'new_prompt', source: 'user', status: 'pending', confidence: 0.98,
      payload: { ...body, created_by: userId, scope: 'deep' }, created_at: new Date().toISOString() })
    return { status: 'pending', suggestion_id: id }
  }

  function approve(s: SuggestionItem): number {
    const body = validate({ title: String(s.payload.title || ''), content: String(s.payload.content || ''),
      category: s.payload.category ? String(s.payload.category) : null, source_url: s.payload.source_url ? String(s.payload.source_url) : null })
    rows()
    const entry = ensure(body.content, body.title, new Date().toISOString())
    if (!entry.contributed) {
      entry.title = body.title
      entry.category = body.category || null
      entry.source_url = body.source_url || null
      entry.contributed = true
    }
    s.payload.prompt_id = entry.id
    s.result = `/prompts/${entry.id}`
    return entry.id
  }

  return { list, get, demos, propose, approve }
}
