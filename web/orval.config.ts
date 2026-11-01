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
      target: './src/api/gen/index.ts',
      client: 'fetch',
      mode: 'single',
      mock: false,
      // 生成物里不加 orval 的横幅注释，避免每次升级都产生无关 diff
      headers: false,
    },
  },
};
