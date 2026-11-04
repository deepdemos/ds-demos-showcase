// 单一出处门禁：同一个判定只许有一个出处。
//
// # 为什么要有这道门禁
//
// 这个仓库出过的 bug 里，占比最大的一类不是「逻辑写错」，而是**同一个判定被写了第二遍，
// 两遍各自漏了一点**。实证：`2026-11-04` 的第四次 review 报了 16 条，其中 6 条同根——
// 「这个作品现在对外可见吗」在 SQL 里出现 5 次（3 个 `visibility IN (…)` + 2 个
// `visibility = 'listed'`）、在 Go 里 6 次（`LifecycleState == "published"` /
// `ModerationState != "approved"` 的各种排列）、在前端 1 次。列表漏了 `hidden`、
// 详情漏了渠道、预览漏了渠道——同一个洞在三个入口各开了一次。
//
// 之前这类问题只能靠人读 diff 发现，也就是「全靠 review 的人是否刚好想到」。
// 这道门禁把「是不是又写了一遍」变成可机检的：**出现第二处就红**。
//
// # 它不是「禁止出现」，是「禁止悄悄出现」
//
// `allowed` 是终局唯一出处（通常是刚收敛完的那个文件）；`baseline` 是历史上已经存在的
// 副本清单（存量债）。这构成一个棘轮：
//   - 新增副本（新文件或计数变大）→ 红
//   - 清理了副本但没收紧 baseline → 红（逼你把棘轮往下拨，否则下次又能加回来）
//   - `--update` 按当前真实情况重写 baseline
//
// 只有往下拨，不能往上拨：`--update` 之后如果总量变多了，门禁当场拒绝（见 applyUpdate）。
//
// # 它证明什么、不证明什么
//
// 证明：某一类判定在某类文件里出现的**位置与次数**没有在基线之上增长。
// 不证明：出现在那个唯一出处里的判定本身是对的。判定正确性靠测试与反向验证。
// 也不证明：所有该收敛的判定都被本文件登记了——漏登记的规则只能靠人补。
import { readdirSync, readFileSync, statSync, writeFileSync, existsSync } from 'node:fs';
import { join, sep, posix } from 'node:path';
import { pathToFileURL } from 'node:url';

const ROOT = process.cwd();
const BASELINE = 'scripts/single-source.baseline.json';

// 扫描范围：只看手写代码。生成物（store / api / gen）与依赖不参与——
// 它们的内容由生成器决定，改了也会被「生成物一致」那道门禁打回。
const SCAN_ROOTS = ['backend', 'web/src', 'db'];
const SKIP_DIRS = new Set([
  'node_modules',
  '.git',
  'dist',
  '.e2e',
  'gen',
  'store',
  join('internal', 'api'),
]);

/**
 * 规则表。每条规则声明「这个判定在哪儿写算唯一出处」，其余位置一律算副本。
 *
 * `include` 决定扫哪些扩展名；`allowed` 是终局出处；`hint` 是红了之后该怎么改。
 */
export const RULES = [
  {
    id: 'visibility-sql',
    title: '渠道可见性判定（SQL）',
    // 两种写法都要抓：`visibility IN ('listed','unlisted')` 与 `visibility = 'listed'`。
    // 前者用于「对外可见」，后者用于「上列表」，两者是不同问题但同一个事实源。
    pattern: /visibility\s+(?:IN\s*\(|=\s*')/g,
    include: [/\.sql$/],
    // 库结构与触发器不算副本：那是约束的**定义处**，不是应用里的第二个判定。
    exclude: [/^db\/(schema|migrations)\//],
    allowed: ['db/queries/demos.sql'],
    hint: '把判定收进视图 demo_public（或唯一的可见性函数），调用处只 join/调用它',
  },
  {
    id: 'visibility-go',
    title: '渠道可见性判定（Go 布尔）',
    // `LifecycleState == "published"` / `ModerationState != "approved"` 及其组合。
    // 这类布尔散落在 handler 里，每加一个入口就重写一遍，且极易漏掉渠道那一项。
    pattern: /(?:LifecycleState|ModerationState)\s*[!=]=\s*"(?:published|approved)"/g,
    include: [/\.go$/],
    // 测试文件不算副本：断言「已发布的作品可见」是验证行为，不是第二份实现。
    exclude: [/_test\.go$/],
    allowed: [],
    hint: '改成调用 domain 层的 Visible(row) / IsPublic(row)，不要在各 handler 里重算',
  },
  {
    id: 'visibility-web',
    title: '渠道可见性判定（前端）',
    pattern: /lifecycleState\s*[!=]==?\s*'published'/g,
    include: [/\.tsx?$/],
    allowed: [],
    hint: '用服务端返回的 downloadable / 可见性字段，前端不自己推导',
  },
  {
    id: 'state-write',
    title: '状态跃迁写入（UPDATE … SET 状态列）',
    // 状态机只能有一个写入口。现在 publish 写 lifecycle、review 写 moderation，
    // 再接归档/移除/删除就会变成四份 WHERE——那正是这次要防的。
    pattern: /SET\s+(?:lifecycle_state|moderation_state)\s*=/g,
    include: [/\.sql$/],
    exclude: [/^db\/(schema|migrations)\//],
    allowed: [],
    hint: '收进 domain/demo/state.go 的单一过渡函数，SQL 只留一条由它调用的语句',
  },
  {
    id: 'audit-write',
    title: '审计写入（INSERT INTO audit_log）',
    // 审计出口必须唯一：actor_type / entity_type 这些口径写死在不同语句里，
    // 已经出现过「一处写死 user、另一处要 system」的不一致。
    pattern: /INSERT\s+INTO\s+audit_log/g,
    include: [/\.sql$/],
    allowed: ['db/queries/audit.sql'],
    hint: '所有审计写入走同一条 InsertAuditLog（带 actor 类型参数），不要各写各的',
  },
];

// 递归收集待扫描文件，统一成仓库相对路径（正斜杠）。
function collectFiles(dir, out) {
  const full = join(ROOT, dir);
  if (!existsSync(full)) return out;
  for (const entry of readdirSync(full)) {
    const rel = posix.join(dir.split(sep).join('/'), entry);
    const abs = join(ROOT, rel);
    const stat = statSync(abs);
    if (stat.isDirectory()) {
      if (SKIP_DIRS.has(entry)) continue;
      // 生成目录用完整路径片段匹配，避免误伤名字里带 store 的业务包。
      if (rel === 'backend/internal/store' || rel === 'backend/internal/api' || rel === 'web/src/api/gen') continue;
      collectFiles(rel, out);
      continue;
    }
    out.push(rel);
  }
  return out;
}

/** 按规则扫描，返回 { ruleId: { path: count } }，只含有命中的文件。 */
export function scan(files, rules = RULES) {
  const result = {};
  for (const rule of rules) {
    const hits = {};
    for (const file of files) {
      if (!rule.include.some((re) => re.test(file))) continue;
      if (rule.allowed.includes(file)) continue;
      if (rule.exclude?.some((re) => re.test(file))) continue;
      const text = readFileSync(join(ROOT, file), 'utf8');
      // 每行只算一次：一行里写两遍同一个判定仍然是同一个出处，不该被算成 2。
      let count = 0;
      for (const line of text.split('\n')) {
        rule.pattern.lastIndex = 0;
        if (rule.pattern.test(line)) count += 1;
      }
      if (count > 0) hits[file] = count;
    }
    if (Object.keys(hits).length > 0) result[rule.id] = hits;
  }
  return result;
}

/**
 * 比对当前扫描结果与基线。
 *
 * 返回 { errors, warnings }；errors 非空即门禁失败。
 *
 * 三类错误：
 *   growth     — 某文件副本数超过基线（新增了副本）
 *   emerged    — 基线里没有的文件出现了副本（新开一处）
 *   stale      — 基线里的副本已不存在或变少（清理了但没收紧基线）
 */
export function compare(current, baseline, rules = RULES) {
  const errors = [];
  const warnings = [];
  const known = new Set(rules.map((r) => r.id));

  for (const id of Object.keys(current)) {
    if (!known.has(id)) warnings.push(`基线里的规则 ${id} 已不在规则表中，--update 会把它清掉`);
  }

  for (const rule of rules) {
    const have = current[rule.id] ?? {};
    const want = baseline[rule.id] ?? {};
    for (const [file, count] of Object.entries(have)) {
      if (!(file in want)) {
        errors.push({ kind: 'emerged', rule: rule.id, file, count, hint: rule.hint });
      } else if (count > want[file]) {
        errors.push({ kind: 'growth', rule: rule.id, file, count, was: want[file], hint: rule.hint });
      } else if (count < want[file]) {
        errors.push({ kind: 'stale', rule: rule.id, file, count, was: want[file] });
      }
    }
    for (const [file, was] of Object.entries(want)) {
      if (!(file in have)) {
        errors.push({ kind: 'stale', rule: rule.id, file, count: 0, was });
      }
    }
  }
  return { errors, warnings };
}

/**
 * 应用 `--update`：按当前真实情况重写基线，但**拒绝变松**。
 *
 * 为什么拒绝变松：不然 `--update` 就成了「红了一键变绿」的后门，
 * 和 `check-api-compat --update` 的松点（第 7 条）是同一个病。
 * 只在两种情况下允许：总量下降，或某个文件变了但总量没涨。
 */
export function applyUpdate(current, previous) {
  const before = total(previous);
  const after = total(current);
  if (after > before) {
    const err = new Error(
      `拒绝更新：单一出处副本从 ${before} 处涨到 ${after} 处。` +
        `这道门禁只允许往下拨；先删掉新增的副本再 --update。`,
    );
    err.code = 'LOOSEN';
    throw err;
  }
  return current;
}

function total(surface) {
  let sum = 0;
  for (const files of Object.values(surface)) {
    for (const n of Object.values(files)) sum += n;
  }
  return sum;
}

function readBaseline() {
  if (!existsSync(join(ROOT, BASELINE))) return {};
  return JSON.parse(readFileSync(join(ROOT, BASELINE), 'utf8'));
}

function main() {
  const files = [];
  for (const root of SCAN_ROOTS) collectFiles(root, files);
  const current = scan(files);

  if (process.argv.includes('--update')) {
    const previous = readBaseline();
    const seeded = !existsSync(join(ROOT, BASELINE));
    // 首次写入是「立账」：本来就是从 0 涨到 N，不能按「只许往下拨」判。
    // 但必须说清：这一次记下的全是存量债，之后只能减少。
    const next = seeded ? current : applyUpdate(current, previous);
    writeFileSync(join(ROOT, BASELINE), JSON.stringify(next, null, 2) + '\n');
    if (seeded) {
      console.log(
        `首次写入 ${BASELINE}：记下 ${total(next)} 处存量副本。\n` +
          '从下一次起这道门禁只允许往下拨——新增副本会红，清理后也必须同步收紧基线。',
      );
    } else {
      console.log(`已更新 ${BASELINE}（待收敛副本 ${total(next)} 处）`);
    }
    return;
  }

  const { errors, warnings } = compare(current, readBaseline());
  for (const w of warnings) console.warn(`[warn] ${w}`);
  if (errors.length === 0) {
    console.log(`单一出处门禁通过（待收敛副本 ${total(current)} 处，与基线一致）`);
    return;
  }

  for (const e of errors) {
    if (e.kind === 'stale') {
      console.error(
        `[stale] ${e.rule} ${e.file}：基线记了 ${e.was} 处，实际 ${e.count} 处——` +
          `清理了副本就把棘轮往下拨：node scripts/check-single-source.mjs --update`,
      );
      continue;
    }
    const grew = e.kind === 'growth' ? `（基线 ${e.was} 处）` : '（基线里没有这个文件）';
    console.error(`[${e.kind}] ${e.rule} ${e.file}：${e.count} 处副本${grew}`);
    console.error(`        改法：${e.hint}`);
  }
  console.error(
    `\n单一出处门禁失败（${errors.length} 条）。规则见 scripts/check-single-source.mjs 顶部注释。`,
  );
  process.exitCode = 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
