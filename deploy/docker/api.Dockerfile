# 后端镜像：多阶段构建。
#
# 为什么用 alpine 而不是 scratch / distroless：`cmd/migrate` 要在容器里跑迁移，
# 运维还要能 `docker compose exec api sh` 进去看文件（存储卷里是用户上传的作品），
# 一个没有 shell 的镜像会让「进去看一眼」变成重新起一个 debug 容器。
FROM golang:1.27-alpine AS build

WORKDIR /src/backend

# 先只拷依赖清单：改业务代码时这一层仍然命中缓存，不用重新拉模块。
COPY backend/go.mod backend/go.sum ./

# Go 模块代理：默认的 proxy.golang.org 在国内构建机上不通（实测 31 秒超时）。
# 用构建参数而不是写死，是为了让海外构建机也能覆盖（--build-arg GOPROXY=direct）。
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
RUN go mod download

COPY backend/ ./
# CGO_ENABLED=0：运行镜像里没有 libc 之外的依赖，纯静态二进制最省事。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin-grant ./cmd/admin-grant

FROM alpine:3.22

# ca-certificates：拉 models.dev 目录、发 SMTP 都要验证书。
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S app && adduser -S -G app app

WORKDIR /app/backend

COPY --from=build /out/api /out/migrate /out/admin-grant /app/backend/
COPY db/ /app/db/
COPY deploy/docker/entrypoint-api.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && mkdir -p /app/var/storage && chown -R app:app /app

# 存储卷挂在这里：容器可换，作品文件不能随容器消失。
VOLUME ["/app/var/storage"]

USER app
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
