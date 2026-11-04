import type { E2EConfig } from 'e2e';
import { web } from '@e2e-dev/web';

/**
 * 浏览器端到端测试的配置。
 *
 * # 为什么只写 app.url，不写 app.command
 *
 * runner 有两种模式：自己把应用拉起来（`app.command`），或者对着已经在跑的应用
 * 跑（只给 `app.url`）。我们这条链路要三件套同时在场——前端 vite、Go API、PostgreSQL——
 * 编排它属于 workflow 的活，不该塞进 runner。而且 `app.command` 配一个已经在跑的服务，
 * CI 下会直接报 `APP_ALREADY_RUNNING` 把整个 job 打断。
 *
 * 所以约定：**跑之前服务必须已经起好**，`APP_URL` 指到前端。
 *
 * # 为什么不用 agents
 *
 * agent 断言需要模型 key，而且它的判断是概率性的——门禁里最不该有的就是这种
 * 时灵时不灵的东西。这里全部用 locator 断言。
 */
const appUrl = process.env.APP_URL ?? 'http://127.0.0.1:7000';

export default {
  targets: [
    {
      engine: web(),
      app: { url: appUrl },
    },
  ],
  tests: ['e2e/**/*.e2e.ts'],
} satisfies E2EConfig;
