// 前端 HTTP 客户端的生成配置。
//
// 契约是唯一事实源：api/openapi.yaml。产物在 web/src/api/gen/，**入库**——
// review 时能看见契约改动对前端类型的影响。CI 会重新生成并比对，不一致就红。
//
// 生成（仓库根目录）：npm run gen:api:web
//
// 为什么这里不 import { defineConfig } from 'orval'：
//   生成走的是 `npx --yes orval@8.46.0`，orval 并没有装在 web/ 下。配置里一旦 import，
//   从 web/ 解析不到该模块，直接 MODULE_NOT_FOUND。纯对象 export 没有这个问题。
//
// 为什么 client 选 fetch 而不是 axios：
//   前端目前没有 axios，选 fetch 可以不引入任何运行时依赖；代价是错误处理要自己包一层
//   （见 web/src/api/http.ts）。如果哪天确实需要拦截器/重试，再换成 axios 也不影响契约。
export default {
  deepdemos: {
    input: {
      target: '../api/openapi.yaml',
    },
    output: {
      client: 'fetch',
      mock: false,
      // 生成物里不加 orval 的横幅注释，避免每次升级都产生无关 diff
      headers: false,
      // 按类型拆成多个文件。单文件会随端点数线性增长：3 端点 303 行 → 7 端点 925 行，
      // 已越过 file-size 的 800 行硬线。拆开后每个文件只含自己的模型与操作，
      // 增长落在文件数而不是单个文件长度上。
      //
      // 注意：schemas 放在 `gen/model/`，操作放在 `gen/<tag>/`。调用方统一从
      // `src/api/index.ts`（手写桶）拿，那个文件 re-export 生成物。
      //
      // `tags-split` 而不是 `split`：后者把所有操作塞进一个 index.ts，随端点线性增长
      // （10 端点时 821 行，越过 file-size 的 800 行硬线）。按 tag 拆开后，
      // 增长落在目录里而不是单个文件长度上。
      target: './src/api/gen/index.ts',
      schemas: './src/api/gen/model',
      mode: 'tags-split',
    },
  },
};
