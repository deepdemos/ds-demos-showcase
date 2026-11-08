#!/usr/bin/env node
// 文档引用门禁：文档里点名的仓库文件与契约端点，必须真实存在。
//
// 为什么需要它：这一层以前只靠人读。结果是反复出现「文档指向已经不存在的
// 东西」——`api.gen.go` 早拆成了两份、`detailOf` 搬家后松点还记着旧文件、
// 契约头「共 18 个端点」漂过三次。这些都不是笔误，是文档与代码分叉。
//
// 它只检查一件事：文档里用反引号引用的仓库内文件路径，文件必须存在。
//
// 曾经想连端点一起查（「文档提到的端点必须在 api/surface.json 里」），试了一版发现
// 113 处命中里绝大多数是**设计文档在写还没实现的端点**——那是正当的前瞻，不是漂移。
// 这类误报会让门禁被当成噪音绕过，所以端点这条线交给 `check-api-doc`（契约描述与实际
// 端点数一致）与 `check-negative-cases`（每个端点都要有反例）两道去管。
//
// 它检查不了：文档描述的「行为」对不对。文本门禁到不了语义（见松点 46）——
// 所以这里宁可漏也不滥报。
//
// 一类正当例外有确定判据，不靠人记：别的仓库里的路径（`owner/repo` 后面跟的那段）
// 不是我们的文件，跳过。
import fs from 'node:fs'
import path from 'node:path'

const ROOT = path.resolve(import.meta.dirname, '..')
const DOCS = path.join(ROOT, 'docs')

// 只认这几个顶层目录开头的引用：其余（`index.html`、`shapes.go` 这类裸文件名）
// 是叙述里的简称，不是「这个仓库里的某个路径」，硬查会全是误报。
const TOP_DIRS = ['backend/', 'web/', 'db/', 'api/', 'scripts/', 'deploy/', '.github/']

const FILE_TOKEN = /`([A-Za-z0-9_.\-/]+\.(?:go|ts|tsx|mjs|cjs|js|sql|yaml|yml|json|md|css|html|conf|service))`/g

/**
 * @param {{docs: {file: string, text: string}[], exists: (p: string) => boolean}} input
 * @returns {{ok: boolean, problems: string[]}}
 */
export function checkDocsRefs({ docs, exists }) {
  const problems = []

  for (const { file, text } of docs) {
    for (const m of text.matchAll(FILE_TOKEN)) {
      const ref = m[1]
      if (!TOP_DIRS.some((dir) => ref.startsWith(dir))) continue
      // 前文紧挨着 `owner/repo` 时，这是人家仓库里的路径。
      const before = text.slice(Math.max(0, m.index - 60), m.index)
      if (/`[\w.-]+\/[\w.-]+`\s*$/.test(before)) continue
      if (!exists(ref)) problems.push(`${file}：引用了不存在的文件 ${ref}`)
    }

  }

  return { ok: problems.length === 0, problems }
}

function main() {
  const files = fs
    .readdirSync(DOCS)
    .filter((n) => n.endsWith('.md'))
    .map((n) => path.join('docs', n))

  const docs = files.map((file) => ({
    file,
    text: fs.readFileSync(path.join(ROOT, file), 'utf8'),
  }))

  const { ok, problems } = checkDocsRefs({
    docs,
    exists: (p) => fs.existsSync(path.join(ROOT, p)),
  })

  if (!ok) {
    console.error(`文档引用门禁失败（${problems.length} 处）：`)
    for (const p of problems) console.error('  - ' + p)
    process.exit(1)
  }
  console.log(`文档引用门禁通过（${files.length} 篇；引用的仓库文件都存在）`)
}

if (process.argv[1] && process.argv[1].endsWith('check-docs-refs.mjs')) main()
