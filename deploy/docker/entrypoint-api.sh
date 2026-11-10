#!/bin/sh
# 容器启动顺序：等库 → 跑迁移 → 起服务。
#
# 为什么迁移放在这里而不是 compose 的独立一次性任务：全新部署时 `up -d`
# 一次就可用，不需要人去记「先跑 migrate 再起 api」。代价是每次重启都会跑一遍
# `migrate up`——它是幂等的（schema_migrations 里有记录就跳过），代价可以接受。
set -eu

: "${DATABASE_URL:?DATABASE_URL 必须设置}"

echo "[entrypoint] 等待数据库…"
i=0
until /app/backend/migrate status >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 60 ]; then
    echo "[entrypoint] 数据库 60 秒内没就绪，放弃" >&2
    /app/backend/migrate status >&2 || true
    exit 1
  fi
  sleep 1
done

echo "[entrypoint] 应用迁移…"
/app/backend/migrate up

echo "[entrypoint] 启动 API…"
exec /app/backend/api
