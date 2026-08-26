#!/usr/bin/env bash
# 全量冒烟：构建验证 + 启动服务 + api-test 断言（T2 起步，随里程碑扩展）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-$ROOT/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$ROOT/.gomod}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

echo "==> verify"
bash scripts/verify.sh

echo "==> start server (smoke)"
export STUDIO_PASSWORD="${STUDIO_PASSWORD:-smoke-pass}"
export PORT="${SMOKE_PORT:-18081}"
# 独立临时数据库：测试数据与开发库隔离，跑完即删（避免残留脏数据影响断言）
export DB_PATH="$ROOT/.tmp-go/smoke-$$.db"
mkdir -p "$ROOT/.tmp-go"
(cd server && go run . >/dev/null 2>&1) &

# 清理：Git Bash 的 kill 杀不掉 Windows 子进程（go run 派生的 server.exe），
# 改为按端口定位并 taskkill 进程树，避免测试残留占用端口；同时删除临时数据库。
cleanup() {
  pid=$(netstat -ano 2>/dev/null | grep ":$PORT " | grep LISTENING | awk '{print $NF}' | head -1)
  if [ -n "$pid" ]; then
    taskkill //F //T //PID "$pid" >/dev/null 2>&1 || true
  fi
  rm -f "$DB_PATH"
}
trap cleanup EXIT

READY=0
for _ in $(seq 1 60); do
  if curl -s -o /dev/null "http://127.0.0.1:$PORT/api/v1/health"; then
    READY=1
    break
  fi
  sleep 1
done
if [ "$READY" != "1" ]; then
  echo "✗ 服务 60 秒内未就绪（端口 $PORT）"
  exit 1
fi

echo "==> api-test"
BASE="http://127.0.0.1:$PORT" PASS="$STUDIO_PASSWORD" bash scripts/api-test.sh
