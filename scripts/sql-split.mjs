// 把一段 SQL 切成可以逐条 exec 的语句。
//
// 为什么不是简单 split(';')：`db/schema/*.sql` 与 `db/migrations/*.sql` 里有几种
// 情况会切错——
//   1. 行尾注释（`-- ...`）里出现分号；
//   2. 语句块前后的大段说明注释；
//   3. **`$$ ... $$`（以及 `$tag$ ... $tag$`）里的分号**——plpgsql 函数体几乎每行
//      都以分号结尾，按行切会把函数劈成碎片（2026-11-03 加触发器时真踩到）。
//
// 所以这里不再用正则切，改为扫一遍字符流，只在「不在字符串、不在美元引用里」的
// 分号处断开。注释在扫描时整段丢掉。

/** 一条语句去掉注释后是否还有内容。 */
export function hasSql(sql) {
  return stripComments(sql).trim().length > 0;
}

/** 去掉 `--` 行注释（不处理字符串里的 `--`，这里只用于快速判定）。 */
export function stripComments(sql) {
  return sql
    .split('\n')
    .map((line) => line.replace(/--.*$/, ''))
    .join('\n');
}

const DOLLAR_TAG = /^\$([A-Za-z_][A-Za-z0-9_]*)?\$/;

/**
 * 切成可执行语句数组；纯注释块被丢弃。
 *
 * 认得三种「分号不算切点」的上下文：单引号字符串、美元引用（`$$` / `$tag$`）、
 * 行注释。行注释直接丢掉，不进结果。
 */
export function statements(sql) {
  const out = [];
  let buf = '';
  let i = 0;
  const n = sql.length;

  while (i < n) {
    const ch = sql[i];

    // 行注释：整段丢到行尾（含换行，避免把两条语句粘在一起）
    if (ch === '-' && sql[i + 1] === '-') {
      while (i < n && sql[i] !== '\n') i++;
      if (i < n) i++;
      buf += '\n';
      continue;
    }

    // 单引号字符串：'' 是转义的单引号
    if (ch === "'") {
      buf += ch;
      i++;
      while (i < n) {
        if (sql[i] === "'" && sql[i + 1] === "'") {
          buf += "''";
          i += 2;
          continue;
        }
        buf += sql[i];
        if (sql[i] === "'") {
          i++;
          break;
        }
        i++;
      }
      continue;
    }

    // 美元引用：$$ 或 $tag$，一直到同名收尾
    if (ch === '$') {
      const m = DOLLAR_TAG.exec(sql.slice(i));
      if (m) {
        const tag = m[0];
        buf += tag;
        i += tag.length;
        const end = sql.indexOf(tag, i);
        if (end === -1) {
          // 没有收尾：整段吞掉，交给 PostgreSQL 报语法错（比这里猜着切好）
          buf += sql.slice(i);
          i = n;
          break;
        }
        buf += sql.slice(i, end + tag.length);
        i = end + tag.length;
        continue;
      }
    }

    if (ch === ';') {
      const s = buf.trim();
      if (s) out.push(s + ';');
      buf = '';
      i++;
      continue;
    }

    buf += ch;
    i++;
  }

  const last = buf.trim();
  if (last) out.push(last + ';');
  return out;
}
