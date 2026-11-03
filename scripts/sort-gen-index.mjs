#!/usr/bin/env node
// 把 orval 生成的汇总出口 `web/src/api/gen/index.ts` 的导出行排序。
//
// 为什么需要：`mode: 'tags-split'` 下，orval 按**目录遍历顺序**写这些 `export *` 行，
// 而目录遍历顺序在不同文件系统上不一样（本机 Windows 是 身份/系统/作品，CI 的 Linux 是
// 作品/系统/身份）。结果就是「同一份契约、同一版 orval」在两台机器上生成出不同的字节，
// CI 的「重新生成后 diff 为空」永远红——但代码其实是等价的。
//
// 修法只能放在生成之后：生成目录不许手改，而这个排序是幂等的、与内容无关的。
import fs from 'node:fs';
import path from 'node:path';

export const GENERATED_INDEX = 'web/src/api/gen/index.ts';

/** 对汇总出口的 `export *` 行排序；其余内容原样保留。返回是否发生了改动。 */
export function sortGeneratedIndex(file, cwd = process.cwd()) {
  const full = path.join(cwd, file);
  if (!fs.existsSync(full)) return false;
  const original = fs.readFileSync(full, 'utf8');
  const lines = original.split('\n');
  const exportLines = lines.filter((line) => line.startsWith('export * from'));
  if (exportLines.length < 2) return false;
  const sorted = [...exportLines].sort();
  if (exportLines.every((line, i) => line === sorted[i])) return false;

  // 保持原有的行位置：把第一个 export 行的位置起，按排序后的顺序依次填回去。
  let cursor = 0;
  const rebuilt = lines.map((line) =>
    line.startsWith('export * from') ? sorted[cursor++] : line,
  );
  fs.writeFileSync(full, rebuilt.join('\n'), 'utf8');
  return true;
}

if (process.argv[1] && import.meta.url.endsWith(path.basename(process.argv[1]))) {
  const changed = sortGeneratedIndex(GENERATED_INDEX);
  console.log(changed ? `已排序 ${GENERATED_INDEX}` : `${GENERATED_INDEX} 无需排序`);
}
