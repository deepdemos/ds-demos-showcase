/**
 * 松点台账的结构门禁。
 *
 * # 为什么需要这个
 *
 * `docs/已知松点.md` 的「变更记录」被插进**第 1 条的元数据表格**里，这件事
 * 在三次不同的 PR 里各发生过一次（`df58626` / `32fbc4d` / `158d93a`）。
 * 每次都是同一个手法：用「找第一个表格的末尾」这类锚点插行，锚点命中到了
 * 别的小节的表格上。三次都是靠人眼在 review 里看出来的。
 *
 * 这是这个项目最反复出现的一类错：**手写文档的增长点没有判据**。
 * 所以这里给台账三个能机器判的结构约束。
 *
 * # 判据
 *
 * 1. **编号连续**：`## N.` 必须从 1 连续到 N，不许跳号、不许重号。
 *    跳号意味着「删了一条却没重新编号」，引用 `loose: N` 的地方就指错了。
 * 2. **变更记录只出现在「变更记录」小节里**：任何 `| YYYY-MM-DD | … |` 行
 *    出现在别的小节里即失败。这正是上面那三次事故的形状。
 * 3. **变更记录按日期不倒序**：新日期在上。倒序说明有人插到了中间。
 * 4. **「在哪」一栏提到的文件真的存在**：路径是松点唯一可核的部分。
 *    文件被改名/合并之后，这条松点就变成了「指向空气的警告」——它不会报错，
 *    只会让下一个人点开一个不存在的路径，然后开始怀疑整份台账。
 *    这条是在第五轮 review 里补的：前三条只判结构与顺序，判不了「还指得准」。
 *
 * # 它不能证明什么
 *
 * 它证明不了「这条松点写得对」——内容质量只有 review 能管。
 * 判据 4 也只核「文件在不在」，不核「这个文件里还有没有那段判据」。
 * 它也不检查「每条都写了能保证 / 不能保证」，因为历史条目用的是多种措辞，
 * 硬套关键词只会让人把措辞改成能被匹配的样子，而不是把话说清楚。
 */
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const DEFAULT_DOC = join(ROOT, 'docs', '已知松点.md');

const SECTION = /^##\s+(.*)$/;
const NUMBERED = /^(\d+)\.\s/;
const DATE_ROW = /^\|\s*(\d{4}-\d{2}-\d{2})\s*\|/;
const CHANGE_SECTION = '变更记录';

/**
 * 校验台账正文。
 *
 * @param {string} text 台账全文
 * @returns {{ok: boolean, problems: string[], entries: number, changes: number}}
 */
export function checkLoosePoints(text) {
  const problems = [];
  const lines = String(text ?? '').split('\n');

  // 逐行记住「这一行属于哪个小节」。
  const current = [];
  let heading = null;
  for (const line of lines) {
    const m = SECTION.exec(line);
    if (m) {
      heading = m[1].trim();
    }
    current.push(heading);
  }

  // 1) 编号连续
  const numbers = [];
  for (const line of lines) {
    const m = SECTION.exec(line);
    if (!m) continue;
    const n = NUMBERED.exec(m[1]);
    if (n) numbers.push(Number(n[1]));
  }
  for (let i = 0; i < numbers.length; i += 1) {
    if (numbers[i] !== i + 1) {
      problems.push(
        `第 ${i + 1} 个小节的编号是 ${numbers[i]}：编号必须从 1 连续到 ${numbers.length}，` +
          '跳号或重号会让 `loose: N` 的引用指错地方。',
      );
      break;
    }
  }

  // 2) 日期行只允许出现在「变更记录」小节里
  let changes = 0;
  lines.forEach((line, i) => {
    const m = DATE_ROW.exec(line);
    if (!m) return;
    if (current[i] !== CHANGE_SECTION) {
      problems.push(
        `第 ${i + 1} 行的变更记录（${m[1]}）出现在「${current[i] ?? '（文件开头）'}」小节里：` +
          `日期行只能写在「${CHANGE_SECTION}」那张表里。`,
      );
      return;
    }
    changes += 1;
  });

  // 4) 每条松点的「在哪」栏里提到的文件必须存在
  //
  // 只认反引号里**带目录**的路径：`grant.go` 这种裸文件名指不清是哪个包，
  // 硬核只会逼人把路径写成猜的样子；松点里也会写 `now()`、`GREATEST(…)`
  // 这类代码片段，一律当路径去核会把台账本身变成噪音源。
  const PATH_TOKEN = /`([\w.-]+\/[\w./-]*\.(?:go|sql|mjs|ts|tsx|js|json|yaml|yml|md))`/g;
  lines.forEach((line, i) => {
    if (!line.startsWith('| 在哪 |')) return;
    let m;
    PATH_TOKEN.lastIndex = 0;
    while ((m = PATH_TOKEN.exec(line)) !== null) {
      const rel = m[1].replace(/#L\d+(-L\d+)?$/, '');
      if (!existsSync(join(ROOT, rel))) {
        problems.push(
          `第 ${i + 1} 行（${current[i] ?? '（文件开头）'}）的「在哪」指向 ${rel}，` +
            '但仓库里没有这个文件：松点指向空气等于没有松点。',
        );
      }
    }
  });

  // 3) 变更记录按日期不倒序
  const changeDates = [];
  lines.forEach((line, i) => {
    const m = DATE_ROW.exec(line);
    if (m && current[i] === CHANGE_SECTION) changeDates.push(m[1]);
  });
  for (let i = 1; i < changeDates.length; i += 1) {
    if (changeDates[i] > changeDates[i - 1]) {
      problems.push(
        `变更记录第 ${i + 1} 条（${changeDates[i]}）比上一条（${changeDates[i - 1]}）新：` +
          '新的在上面，插错位置会让人以为这条已经过期。',
      );
      break;
    }
  }

  return { ok: problems.length === 0, problems, entries: numbers.length, changes };
}

function main() {
  if (!existsSync(DEFAULT_DOC)) {
    console.error(`松点门禁失败：找不到 ${DEFAULT_DOC}`);
    process.exit(1);
  }
  const result = checkLoosePoints(readFileSync(DEFAULT_DOC, 'utf8'));
  if (!result.ok) {
    console.error('松点门禁失败：');
    for (const p of result.problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log(
    `松点门禁通过（${result.entries} 条松点，变更记录 ${result.changes} 行，编号、落点与指路都对）`,
  );
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))) {
  main();
}
