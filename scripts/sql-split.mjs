// 把一段 SQL 切成可以逐条 exec 的语句。
//
// 为什么不是简单 split(';')：`db/schema/*.sql` 里有几种情况会切错——
//   1. 行尾注释（`-- ...`）里出现分号；
//   2. 语句块前后的大段说明注释；
//   3. `$tag$ ... $tag$` 里的分号（目前没有，但 CHECK 里出现字面量就会踩）。
//
// 切法：按 `;\s*\n`（分号 + 换行）切，这是本仓库 SQL 的书写约定；
// 切完把「纯注释块」（去掉注释后为空）丢掉。约定变了这里会先炸，
// 比悄悄漏语句好——漏语句的验证等于没验证。

/** 一条语句去掉注释后是否还有内容。 */
export function hasSql(sql) {
  return stripComments(sql).trim().length > 0;
}

/** 去掉 `--` 行注释（不处理字符串里的 `--`，本仓库没有这种写法）。 */
export function stripComments(sql) {
  return sql
    .split('\n')
    .map((line) => line.replace(/--.*$/, ''))
    .join('\n');
}

/** 切成可执行语句数组；纯注释块被丢弃。 */
export function statements(sql) {
  return (
    stripComments(sql)
      // 先剥注释再切，否则一行以分号结尾的注释会变成一个假的切点
      .split(/;\s*\n/)
      .map((s) => s.trim())
      .filter(Boolean)
      // 最后一条语句后面没有换行时不会被切开，去掉可能残留的分号再统一补一个
      .map((s) => s.replace(/;+$/, '') + ';')
  );
}
