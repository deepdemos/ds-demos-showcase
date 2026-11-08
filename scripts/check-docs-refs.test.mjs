import test from 'node:test'
import assert from 'node:assert/strict'
import { checkDocsRefs } from './check-docs-refs.mjs'

// 用一个假仓库跑判据：这些用例证明的是「门禁会拦什么」，不是「真仓库现在是干净的」。

function run(docs, files) {
  const exists = (p) => files.includes(p)
  return checkDocsRefs({ docs, exists })
}

test('引用存在的文件：通过', () => {
  const r = run([{ file: 'docs/a.md', text: '见 `backend/internal/x.go`' }], ['backend/internal/x.go'])
  assert.equal(r.ok, true, r.problems.join('; '))
})

test('引用不存在的文件：失败，且报出是哪篇文档', () => {
  const r = run([{ file: 'docs/a.md', text: '见 `backend/internal/gone.go`' }], [])
  assert.equal(r.ok, false)
  assert.match(r.problems[0], /docs\/a\.md/)
  assert.match(r.problems[0], /backend\/internal\/gone\.go/)
})

test('裸文件名不算仓库路径：不报', () => {
  // `index.html` / `shapes.go` 这类是叙述里的简称，硬查会全是误报。
  const r = run([{ file: 'docs/a.md', text: '入口通常是 `index.html`，形态表在 `shapes.go`' }], [])
  assert.equal(r.ok, true, r.problems.join('; '))
})

test('别的仓库里的路径：不报', () => {
  const r = run([{ file: 'docs/a.md', text: '对照 `discourse/discourse` `docs/INSTALL-cloud.md`' }], [])
  assert.equal(r.ok, true, r.problems.join('; '))
})

test('顶层目录开头但文件不在：失败（这条是当初漏掉 api.gen.go 的形状）', () => {
  const r = run([{ file: 'docs/a.md', text: '生成物在 `backend/internal/api/api.gen.go`' }], [])
  assert.equal(r.ok, false)
  assert.match(r.problems[0], /api\.gen\.go/)
})

test('一处都没引用：通过（空文档不该报错）', () => {
  const r = run([{ file: 'docs/a.md', text: '没有引用。' }], [])
  assert.equal(r.ok, true)
})
