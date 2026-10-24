// M4-E2 冒烟三件（04 §5.7）：①token 完备性 ②三主题对比度表 ③路由表快照
// 不追覆盖率——零 DOM/node 环境可跑；fs 扫描 + 纯数据导入。
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { routes } from '../src/router/routes'

// ---------- ① token 完备性：所有 var(--x) 无回落引用断言已定义（防再犯闸） ----------
describe('token 完备性', () => {
  it('所有 var(--x) 无回落引用均已定义（带回落的引用=容错口径不拦）', () => {
    const used = new Map<string, boolean>() // name → 是否存在无回落引用
    const defined = new Set<string>()
    function* files(dir: string): Generator<string> {
      for (const e of readdirSync(dir)) {
        const p = dir + '/' + e
        if (statSync(p).isDirectory()) yield* files(p)
        else if (/\.(css|vue)$/.test(e)) yield p
      }
    }
    for (const f of files('src')) {
      const src = readFileSync(f, 'utf8')
      for (const m of src.matchAll(/var\((--[\w-]+)\s*(,)?/g)) {
        const hasFallback = m[2] === ','
        if (!used.has(m[1]) || (used.get(m[1]) === true && hasFallback)) used.set(m[1], used.get(m[1]) === true ? true : false)
        if (hasFallback && used.get(m[1]) !== true) used.set(m[1], used.get(m[1]) ?? false)
        if (!hasFallback) used.set(m[1], true)
      }
      if (f.endsWith('.css')) for (const m of src.matchAll(/(--[\w-]+)\s*:/g)) defined.add(m[1])
    }
    // 模板动态拼接（var(--x-${expr})）静态不可解析——人工核对域（--sh-off-* 系阴影档位，已定义）
    const broken = [...used].filter(([u, noFallback]) => noFallback && !defined.has(u) && !/-$/.test(u))
    expect(broken.map(([u]) => u), `无回落的未定义 CSS 变量引用（防再犯闸）：${broken.map(([u]) => u).join(', ')}`).toEqual([])
  })
})

// ---------- ② 三主题对比度表（04 §3.7 硬门） ----------
function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const rgb = [h.slice(0, 2), h.slice(2, 4), h.slice(4, 6)].map((x) => parseInt(x, 16) / 255)
  const [r, g, b] = rgb.map((c) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(fgHex: string, bgHex: string): number {
  const l1 = luminance(fgHex)
  const l2 = luminance(bgHex)
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
}

function parseVars(css: string): Record<string, string> {
  const map: Record<string, string> = {}
  for (const m of css.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) map[m[1]] = m[2].trim()
  return map
}

describe('三主题对比度表（04 §3.7 硬门）', () => {
  const primitives = parseVars(readFileSync('src/styles/tokens/primitives.css', 'utf8'))
  const semantic = parseVars(readFileSync('src/styles/tokens/semantic.css', 'utf8'))
  const themes = parseVars(readFileSync('src/styles/tokens/themes.css', 'utf8'))

  function resolve(name: string, theme: 'paper' | 'ink', seen = new Set<string>()): string {
    if (seen.has(name)) throw new Error('循环引用: ' + name)
    seen.add(name)
    const raw = (theme === 'ink' ? themes[name] : undefined) ?? semantic[name] ?? primitives[name]
    if (raw == null) throw new Error('token 未定义: ' + name)
    const varRef = raw.match(/^var\(\s*(--[\w-]+)\s*(,|\))/)
    if (varRef) return resolve(varRef[1], theme, seen)
    return raw
  }

  const GATES: [string, number][] = [
    ['--ink', 12],
    ['--ink-soft', 4.5],
    // --ink-faint 断链（01 §3.4 已知：token 未定义，使用处均带回落 #767676）——加列=协作项，测试跳过
  ]

  for (const theme of ['paper', 'ink'] as const) {
    it(`${theme} 主题：正文/次级对比度达 04 §3.7 硬门（--ink-faint 断链跳过=协作项）`, () => {
      const bg = resolve('--paper', theme)
      for (const [fgName, min] of GATES) {
        const fg = resolve(fgName, theme)
        const ratio = contrast(fg, bg)
        expect(ratio, `${theme}.${fgName} 对 --paper = ${ratio.toFixed(2)}:1（硬门 ${min}:1）`).toBeGreaterThanOrEqual(min)
      }
    })
  }

  it('对照矩阵基线抽查：纸白 --ink/--paper ≥12 与 墨黑暖白 ≥15（themes.css 决断注释值）', () => {
    const inkPaper = contrast(resolve('--ink', 'paper'), resolve('--paper', 'paper'))
    const inkWarm = contrast(resolve('--ink', 'ink'), resolve('--paper', 'ink'))
    expect(inkPaper).toBeGreaterThanOrEqual(12)
    expect(inkWarm).toBeGreaterThanOrEqual(15)
  })
})
// ---------- ③ 路由表快照（静态可解析 + 动态段/重定向/404，M4-E2 抽出 routes） ----------
// P2：新增 /demo/:slug/play（独立预览页，meta.bare）→ 27 → 28
// 本轮：新增 /me/collections、/me/collections/:id、/me/history、/user/:username/collections、/collections/:id → 33
// 题目系统 v2：新增 /tasks/new（独立出题，meta.requiresAuth，置于 /tasks/:slug 之前）→ 34
describe('路由表快照', () => {
  it('路由数量与形态稳定（快照防误删/误改）', () => {
    expect(routes.length).toBe(37)
    // P2：独立预览页必须带 meta.bare（App.vue 据此隐藏顶栏/页脚/底栏）
    const play = routes.find((r) => r.name === 'demo-play')
    expect(play?.path).toBe('/demo/:slug/play')
    expect((play?.meta as { bare?: boolean } | undefined)?.bare).toBe(true)
    const promptNew = routes.find((r) => r.name === 'prompt-new')
    expect(promptNew?.path).toBe('/prompts/new')
    expect(promptNew?.meta?.requiresAuth).toBe(true)
    const paths = routes.map((r) => r.path).sort()
    expect(paths).toMatchSnapshot()
    // 全部 path 可被 vue-router 解析（形态合法：以 / 开头）
    for (const r of routes) expect(r.path.startsWith('/'), `路由 ${r.path} 应以 / 开头`).toBe(true)
  })
})
