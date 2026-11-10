# 前端镜像：pnpm 构建 → nginx 静态托管 + 反代 API。
#
# 为什么 web 和 api 分成两个镜像：静态资源的扩缩容节奏与 API 完全不同，
# 合成一个镜像会让「改一行样式」也要重新构建 Go 二进制。
FROM node:24-alpine AS build

RUN corepack enable && corepack prepare pnpm@11.21.0 --activate

WORKDIR /src/web

# 依赖清单单独一层：只改页面代码时不必重装依赖。
#
# pnpm-workspace.yaml 必须一起拷：里面写着 allowBuilds（core-js 的构建脚本），
# 少了它容器里的 pnpm 会以 ERR_PNPM_IGNORED_BUILDS 直接失败——本地能装、
# 镜像装不了，就是这一行的差别。
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
# 构建前先做 typecheck（package.json 的 build 里已经带了），失败就不出镜像。
RUN pnpm build

FROM nginx:1.27-alpine

# 官方镜像会把这个目录里的 *.template 用 envsubst 渲染到 conf.d，
# 所以 API 上游地址是运行时变量（`${API_UPSTREAM}`），不是写死的。
COPY deploy/docker/nginx.conf.template /etc/nginx/templates/default.conf.template
COPY deploy/nginx/security-headers.conf /etc/nginx/snippets/security-headers.conf
COPY --from=build /src/web/dist /usr/share/nginx/html

ENV API_UPSTREAM=api:8080

EXPOSE 80
