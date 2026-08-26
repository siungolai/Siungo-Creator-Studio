#!/usr/bin/env bash
# API 冒烟断言（T3：+作品 CRUD/校验/平台预置，共 23 条；断言数随里程碑增长，见 PRD §5）。
# 前置：服务已启动（BASE 默认 127.0.0.1:8081），密码与启动时一致（PASS 默认 test-pass）。
set -u
BASE="${BASE:-http://127.0.0.1:8081}"
PASS="${PASS:-test-pass}"
JAR="$(mktemp)"
PASS_COUNT=0

fail() { echo "✗ $1"; rm -f "$JAR"; exit 1; }
ok() { PASS_COUNT=$((PASS_COUNT + 1)); }

# ---- 认证 ----

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

# ---- 作品 CRUD（T3）----

# 4a. 未登录访问作品接口 → 401
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/creator/works")
[ "$code" = "401" ] && ok "未登录 /api/v1/creator/works → 401" || fail "未登录作品列表期望 401 实际 $code"

# 注意：含中文的 body 一律写临时文件用 -d @file 发送。
# 原因：Git Bash 调用 Windows 原生 curl.exe 时，中文经命令行参数传递会被转成 GBK（乱码入库）。
TMPBODY="$(mktemp)"

# 4b. 空主题 → 400
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -d '{"topic":"  "}' "$BASE/api/v1/creator/works")
[ "$code" = "400" ] && ok "空主题 → 400" || fail "空主题期望 400 实际 $code"

# 4c. 主题超长（201 字符）→ 400
printf '%s' "{\"topic\":\"$(printf '长%.0s' $(seq 1 201))\"}" > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -d @"$TMPBODY" "$BASE/api/v1/creator/works")
[ "$code" = "400" ] && ok "主题超长 → 400" || fail "主题超长期望 400 实际 $code"

# 4d. 新建作品 A（标题可空）→ 201
printf '%s' '{"topic":"作品A-测试主题"}' > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -d @"$TMPBODY" "$BASE/api/v1/creator/works")
[ "$code" = "201" ] && ok "新建作品 A → 201" || fail "新建 A 期望 201 实际 $code"

# 4e. 列表包含 A
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
echo "$resp" | grep -q '作品A-测试主题' && ok "列表包含作品 A" || fail "列表缺少作品 A: $resp"

# 4f. 新建作品 B → 201
printf '%s' '{"topic":"作品B-测试主题","title":"作品B标题"}' > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -d @"$TMPBODY" "$BASE/api/v1/creator/works")
[ "$code" = "201" ] && ok "新建作品 B → 201" || fail "新建 B 期望 201 实际 $code"

# 4g. 列表按更新时间倒序（B 在 A 前）
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
case "${resp%%作品B-测试主题*}" in
  *作品A-测试主题*) fail "列表未按更新时间倒序: $resp" ;;
  *) ok "列表按更新时间倒序（B 在前）" ;;
esac

# 4h. 平台预置数据（抖音/B站）
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/platforms")
echo "$resp" | grep -q '"id":"douyin"' && echo "$resp" | grep -q '"id":"bilibili"' && ok "平台预置 douyin/bilibili" || fail "平台预置数据异常: $resp"

# 4i. 删除不存在的作品 → 404
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$BASE/api/v1/creator/works/999999")
[ "$code" = "404" ] && ok "删除不存在作品 → 404" || fail "删除不存在期望 404 实际 $code"

# 4j. 删除作品 A → 204
A_ID=$(echo "$resp" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
# 上面 resp 是 platforms 响应，重新取列表拿 A 的 id
list=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
A_ID=$(echo "$list" | grep -o '"id":[0-9]*,"title":"","topic":"作品A-测试主题"' | grep -o '[0-9]*' | head -1)
[ -n "$A_ID" ] || fail "未找到作品 A 的 id"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$BASE/api/v1/creator/works/$A_ID")
[ "$code" = "204" ] && ok "删除作品 A → 204" || fail "删除 A 期望 204 实际 $code"

# 4k. 删除后列表不含 A；删除 B 后列表为空数组
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
echo "$resp" | grep -q '作品A-测试主题' && fail "删除后列表仍含作品 A" || ok "删除后列表不含 A"
B_ID=$(echo "$resp" | grep -o '"id":[0-9]*,"title":"作品B标题"' | grep -o '[0-9]*' | head -1)
[ -n "$B_ID" ] || fail "未找到作品 B 的 id"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$BASE/api/v1/creator/works/$B_ID")
[ "$code" = "204" ] && ok "删除作品 B → 204" || fail "删除 B 期望 204 实际 $code"
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
echo "$resp" | grep -q '"total":0' && ok "清空后列表 total=0" || fail "清空后应 total=0 实际: $resp"

# 4l. 详情：不存在的作品 → 404
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$BASE/api/v1/creator/works/999999")
[ "$code" = "404" ] && ok "详情不存在 → 404" || fail "详情不存在期望 404 实际 $code"

# 4m. 新建作品 C → 201
printf '%s' '{"topic":"作品C-详情主题","title":"C标题"}' > "$TMPBODY"
C_RESP=$(curl -s -b "$JAR" -H 'Content-Type: application/json' -d @"$TMPBODY" "$BASE/api/v1/creator/works")
C_ID=$(echo "$C_RESP" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
[ -n "$C_ID" ] && ok "新建作品 C → 201" || fail "新建 C 失败: $C_RESP"

# 4n. 详情 → 200 且字段正确
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works/$C_ID")
echo "$resp" | grep -q '"topic":"作品C-详情主题"' && ok "详情字段正确" || fail "详情字段异常: $resp"

# 4o. PUT 更新（主题/风格/状态=归档/标签/工作副本）→ 200
printf '%s' '{"title":"C新标题","topic":"C改后主题","style":"tucao","status":"archived","tags":["标签1","标签2"],"script":"第一行\n第二行"}' > "$TMPBODY"
resp=$(curl -s -b "$JAR" -H 'Content-Type: application/json' -X PUT -d @"$TMPBODY" "$BASE/api/v1/creator/works/$C_ID")
echo "$resp" | grep -q '"status":"archived"' && echo "$resp" | grep -q '"style":"tucao"' && ok "PUT 更新生效（归档+风格+脚本）" || fail "PUT 更新异常: $resp"

# 4p. PUT 非法状态 → 400
printf '%s' '{"title":"","topic":"x","style":"default","status":"bad","tags":[],"script":""}' > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -X PUT -d @"$TMPBODY" "$BASE/api/v1/creator/works/$C_ID")
[ "$code" = "400" ] && ok "PUT 非法状态 → 400" || fail "PUT 非法状态期望 400 实际 $code"

# 4q. PUT 不存在的作品（合法 body）→ 404
printf '%s' '{"title":"","topic":"x","style":"default","status":"draft","tags":[],"script":""}' > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -X PUT -d @"$TMPBODY" "$BASE/api/v1/creator/works/999999")
[ "$code" = "404" ] && ok "PUT 不存在 → 404" || fail "PUT 不存在期望 404 实际 $code"

# 4r. 归档不锁操作：archived → draft → 200
printf '%s' '{"title":"C新标题","topic":"C改后主题","style":"tucao","status":"draft","tags":["标签1"],"script":"第一行\n第二行"}' > "$TMPBODY"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Content-Type: application/json' -X PUT -d @"$TMPBODY" "$BASE/api/v1/creator/works/$C_ID")
[ "$code" = "200" ] && ok "归档后改回草稿 → 200" || fail "归档后改回期望 200 实际 $code"

# 4s. 工作副本持久化 + 清理 C
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works/$C_ID")
echo "$resp" | grep -q '第一行' && ok "工作副本持久化" || fail "工作副本未持久化: $resp"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$BASE/api/v1/creator/works/$C_ID")
[ "$code" = "204" ] && ok "清理作品 C → 204" || fail "清理 C 期望 204 实际 $code"

# ---- 分页/筛选/搜索（T5）----

# 4t. 建 3 个作品 → limit=2 返回 items=2 且 total=3
for i in 1 2 3; do
  printf '{"topic":"分页主题%s"}' "$i" > "$TMPBODY"
  curl -s -o /dev/null -b "$JAR" -H 'Content-Type: application/json' -d @"$TMPBODY" "$BASE/api/v1/creator/works"
done
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?limit=2")
ITEM_COUNT=$(echo "$resp" | grep -o '"id":' | wc -l)
echo "$resp" | grep -q '"total":3' && [ "$ITEM_COUNT" = "2" ] && ok "分页 limit=2 → items=2 total=3" || fail "分页异常: $resp"

# 4u. offset=2&limit=2 → 剩余 1 条
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?limit=2&offset=2")
ITEM_COUNT=$(echo "$resp" | grep -o '"id":' | wc -l)
[ "$ITEM_COUNT" = "1" ] && ok "offset=2 → items=1" || fail "offset=2 items=$ITEM_COUNT: $resp"

# 4v. 状态筛选：最新作品改为 making，status=making 仅 1 条
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?limit=1")
TOP_ID=$(echo "$resp" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)
[ -n "$TOP_ID" ] || fail "未取到最新作品 id"
printf '%s' '{"title":"","topic":"分页主题3","style":"default","status":"making","tags":[],"script":""}' > "$TMPBODY"
curl -s -o /dev/null -b "$JAR" -H 'Content-Type: application/json' -X PUT -d @"$TMPBODY" "$BASE/api/v1/creator/works/$TOP_ID"
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?status=making")
ITEM_COUNT=$(echo "$resp" | grep -o '"id":' | wc -l)
[ "$ITEM_COUNT" = "1" ] && ok "状态筛选 making → 1 条" || fail "状态筛选异常: $resp"

# 4w. 关键词搜索（命中主题）
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?q=%E5%88%86%E9%A1%B5%E4%B8%BB%E9%A2%981")
ITEM_COUNT=$(echo "$resp" | grep -o '"id":' | wc -l)
[ "$ITEM_COUNT" = "1" ] && ok "关键词搜索 → 1 条" || fail "搜索异常: $resp"

# 4x. 搜索 + 状态组合
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?q=%E5%88%86%E9%A1%B5%E4%B8%BB%E9%A2%983&status=making")
ITEM_COUNT=$(echo "$resp" | grep -o '"id":' | wc -l)
[ "$ITEM_COUNT" = "1" ] && ok "搜索+状态组合 → 1 条" || fail "组合异常: $resp"

# 4y. 非法参数 → 400
for bad in "limit=-1" "limit=101" "limit=abc" "status=bad"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$BASE/api/v1/creator/works?$bad")
  [ "$code" = "400" ] || fail "非法参数 $bad 期望 400 实际 $code"
done
ok "非法参数（limit/status）→ 400"

# 4z. 清理分页测试作品
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works?limit=100")
for id in $(echo "$resp" | grep -o '"id":[0-9]*' | cut -d: -f2); do
  curl -s -o /dev/null -b "$JAR" -X DELETE "$BASE/api/v1/creator/works/$id"
done
resp=$(curl -s -b "$JAR" "$BASE/api/v1/creator/works")
echo "$resp" | grep -q '"total":0' && ok "清理完成 total=0" || fail "清理未完成: $resp"

# ---- 认证（续）----

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

rm -f "$JAR" "$TMPBODY"
echo "✅ api-test: $PASS_COUNT 断言通过"
