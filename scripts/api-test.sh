#!/usr/bin/env bash
# API 冒烟断言（T2 起步，断言数随里程碑增长，见 PRD §5）。
# 前置：服务已启动（BASE 默认 127.0.0.1:8081），密码与启动时一致（PASS 默认 test-pass）。
set -u
BASE="${BASE:-http://127.0.0.1:8081}"
PASS="${PASS:-test-pass}"
JAR="$(mktemp)"
PASS_COUNT=0

fail() { echo "✗ $1"; rm -f "$JAR"; exit 1; }
ok() { PASS_COUNT=$((PASS_COUNT + 1)); }

# 1. 未登录访问受保护接口 → 401
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/me")
[ "$code" = "401" ] && ok "未登录 /api/v1/me → 401" || fail "未登录 /api/v1/me 期望 401 实际 $code"

# 2. 错误密码 → 401（统一错误体）
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{"password":"wrong-pass"}' "$BASE/api/v1/login")
[ "$code" = "401" ] && ok "错误密码 → 401" || fail "错误密码期望 401 实际 $code"
body=$(curl -s -H 'Content-Type: application/json' -d '{"password":"wrong-pass"}' "$BASE/api/v1/login")
echo "$body" | grep -q '"error"' && ok "错误响应为统一 JSON 错误体" || fail "错误响应非 JSON 错误体: $body"

# 3. 正确密码 → 200 且下发 session cookie
code=$(curl -s -o /dev/null -w '%{http_code}' -c "$JAR" -H 'Content-Type: application/json' -d "{\"password\":\"$PASS\"}" "$BASE/api/v1/login")
[ "$code" = "200" ] && ok "正确密码 → 200" || fail "正确密码期望 200 实际 $code"
grep -q "session" "$JAR" && ok "响应包含 session cookie" || fail "未下发 session cookie"

# 4. 登录态访问 /api/v1/me → 200
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$BASE/api/v1/me")
[ "$code" = "200" ] && ok "登录态 /api/v1/me → 200" || fail "登录态期望 200 实际 $code"

# 5. 登出 → 200；旧 cookie 再访问 → 401
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST "$BASE/api/v1/logout")
[ "$code" = "200" ] && ok "登出 → 200" || fail "登出期望 200 实际 $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$BASE/api/v1/me")
[ "$code" = "401" ] && ok "登出后 /api/v1/me → 401" || fail "登出后期望 401 实际 $code"

# 6. 连续 5 次错误密码后第 6 次 → 429（冷却）
for _ in 1 2 3 4 5; do
  curl -s -o /dev/null -H 'Content-Type: application/json' -d '{"password":"wrong-pass"}' "$BASE/api/v1/login"
done
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{"password":"wrong-pass"}' "$BASE/api/v1/login")
[ "$code" = "429" ] && ok "冷却触发 → 429" || fail "冷却期望 429 实际 $code"

# 7. 未登录访问受保护占位 API → 401（认证中间件兜底）
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/anything")
[ "$code" = "401" ] && ok "未登录 /api/v1/* → 401" || fail "未登录 /api/v1/* 期望 401 实际 $code"

# 8. 健康检查 → 200 且带 trace ID 响应头
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/health")
[ "$code" = "200" ] && ok "健康检查 → 200" || fail "健康检查期望 200 实际 $code"
trace=$(curl -s -D - -o /dev/null "$BASE/api/v1/health" | grep -i '^X-Trace-Id:' | tr -d '\r')
[ -n "$trace" ] && ok "响应含 X-Trace-Id 头" || fail "缺少 X-Trace-Id 响应头"

rm -f "$JAR"
echo "✅ api-test: $PASS_COUNT 断言通过"
