#!/usr/bin/env bash
# 一键验证（T1 起随里程碑扩展；T2 起追加 api-test 冒烟断言）：
#   后端 go build/vet/test + 前端 tsc/oxlint/vite build + 规范 ② 档探针
# 缓存一律指工作区（DSH 沙箱环境约定，见 .doc/handoff-2026-08-26.md §5）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-$ROOT/.gocache}"
export GOMODCACHE="${GOMODCACHE:-$ROOT/.gomod}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

echo "==> server: go build/vet/test"
(cd server && go build ./... && go vet ./... && go test ./...)

echo "==> web: typecheck/lint/build"
(cd web && npm run typecheck && npm run lint && npm run build)

# 规范 ② 档（半机械、要跑起来才能判）的 10 条由探针引擎判（母本在 Siungo-Workspace，
# 本仓的是逐字副本）；站点专属的起服务方式与端点映射在 scripts/spec-probe.hooks.sh。
# 有 FAIL 时探针以 1 退出，set -e 会让整个入口跟着红 —— 这是故意的。
echo "==> 规范 ② 档探针（scripts/spec-probe.sh）"
bash scripts/spec-probe.sh

echo "✅ ALL GREEN"
