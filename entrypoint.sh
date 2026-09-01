#!/bin/sh
set -e

# 若传入了自定义命令参数（如 docker run ... eco-hub sh），直接透传执行
if [ "$#" -gt 0 ]; then
    exec "$@"
fi

ROLE=$(printf '%s' "${CLUSTER_ROLE:-master}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')

if [ "$ROLE" = "worker" ]; then
    echo "[Cluster] 当前节点以 Worker 角色运行: 仅启动 Go API 服务 (端口 8080)，跳过 Web 进程"
    exec /usr/bin/supervisord -c /etc/supervisord-worker.conf
else
    echo "[Cluster] 当前节点以 Master 角色运行: 同时启动 Go 后端与 Web 进程"
    exec /usr/bin/supervisord -c /etc/supervisord.conf
fi
