#!/usr/bin/env bash
# 一键验证（T1 起随里程碑扩展；T2 起追加 api-test 冒烟断言）：
#   后端 go build/vet/test + 前端 tsc/oxlint/vite build
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

echo "✅ ALL GREEN"
