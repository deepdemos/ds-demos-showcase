// 前端调用 API 的统一入口。
//
// 为什么不直接从 `./gen` 拿：
//   orval 的 split 模式把模型拆到 `gen/model/`，`gen/index.ts` 只 import 它们、不 re-export，
//   所以 `DemoDetail` 这类类型在 `gen/index.ts` 里拿不到。而生成目录禁止手改，
//   于是这里做一个手写桶：类型与请求函数都从这里出。
//
// 换生成器（或 orval 改了导出策略）时，只改这一个文件，页面不用动。
export * from './gen';
export * from './gen/model';
