#!/usr/bin/env bash
# ============================================================================
# spec-probe.sh —— 规范「② 档」探针引擎
#
# ⚠️ 这个文件是**同源副本**：7 个站里各有一份，内容必须逐字相同。
#    母本在 Siungo-Workspace/scripts/spec-probe.sh，用
#        node scripts/sync-spec-probe.mjs
#    分发；`--check` 查漂移。**不要在本仓直接改它** —— 会被下一次同步覆盖，
#    而且改了别的站也不会跟着改。
#
# 站点专属的那一半在同目录的 spec-probe.hooks.sh（那个文件不同源，各站自己维护）。
#
# 用法：
#   bash scripts/spec-probe.sh
#
# 也可由已有的 smoke.sh 起好服务后直接调：
#   PROBE_BASE_URL=http://127.0.0.1:18080 bash scripts/spec-probe.sh
# （hooks 里只要能读到 PROBE_BASE_URL，就不会再起第二个服务）
#
# 退出码：0 = 没有 FAIL（SKIP / 豁免不算）；1 = 有 FAIL；2 = 环境或配置错误
#
# ── 为什么要有它 ──
# 规范（docs/standards/conformance-gate.md §1）把 88 条条款分三档，其中 ② 档
# 「半机械，要跑起来才能判」共 14 条，原本被指派给"各站的 smoke"。实做下来那个
# 指派只对 6 条成立 —— 另外 8 条的判据宾语在**生产服务器**上（`/etc/`、systemd
# 定时器、真实数据目录、历史部署动作），smoke 侧根本拿不到。所以拆成两边：
#
#   · 本文件：本机判得了的那一半（HTTP 形状、本地进程绑定、空库跑迁移…）
#   · Siungo-Workspace/scripts/server-audit.sh：要 ssh 上生产的那一半
#
# 两边都会显式打印自己**没**判什么。这是刻意的：**假绿比红更贵**。
# 见 conformance-gate.md §9.14。
# ============================================================================
set -uo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SELF_DIR}/.." && pwd)"
HOOKS="${SELF_DIR}/spec-probe.hooks.sh"
WORK="$(mktemp -d)"

if [ ! -f "${HOOKS}" ]; then
	echo "错误：缺 ${HOOKS}（站点专属配置，母本不提供）" >&2
	exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
	echo "错误：没有 curl" >&2
	exit 2
fi

# ── 站点声明的默认值（hooks 里覆盖）────────────────────────────────────────
PROBE_BASE_URL="${PROBE_BASE_URL:-}"
PROBE_SKIP="${PROBE_SKIP:-}"             # **不适用**于这个站的条款 → SKIP
PROBE_ALLOW_FAIL="${PROBE_ALLOW_FAIL:-}" # 已登记豁免的条款 → ALLOW，不判失败
PROBE_LIST_ENDPOINTS="${PROBE_LIST_ENDPOINTS:-}"
PROBE_LIST_QUERY="${PROBE_LIST_QUERY:-}"
PROBE_404_PATH="${PROBE_404_PATH:-/api/__spec-probe-no-such-route__}"
PROBE_ERROR_PATH="${PROBE_ERROR_PATH:-}"   # A1.5 用它：一个**必然失败**的应用端点
PROBE_405_PATH="${PROBE_405_PATH:-}"
PROBE_405_METHOD="${PROBE_405_METHOD:-PATCH}"
PROBE_LISTEN_PORT="${PROBE_LISTEN_PORT:-}"
PROBE_DATA_DIR="${PROBE_DATA_DIR:-}"
PROBE_MIGRATE_CMD="${PROBE_MIGRATE_CMD:-}"
PROBE_MIGRATE_NA="${PROBE_MIGRATE_NA:-}"   # 非空 = 本站没有迁移机制，显式判 N/A
PROBE_BODY_ENDPOINT="${PROBE_BODY_ENDPOINT:-}"
PROBE_BODY_LIMIT="${PROBE_BODY_LIMIT:-}"
# A1.9 的超限体形状：默认造一个合法 JSON（{"pad":"aaa…"}）。
# 纯 'a' 会被「MaxBytesReader + 流式 json.Decoder」的站在第一个字节拒成 400，
# 那样判出来的是探针的错。形状不合用的站用这两个变量覆盖。
PROBE_BODY_PREFIX="${PROBE_BODY_PREFIX:-{\"pad\":\"}"
PROBE_BODY_SUFFIX="${PROBE_BODY_SUFFIX:-\"}}"
PROBE_UPLOAD_ENDPOINT="${PROBE_UPLOAD_ENDPOINT:-}"
PROBE_UPLOAD_FIELD="${PROBE_UPLOAD_FIELD:-file}"
# 第二份上传文件（同内容、同伪装）。上传要求多字段的站必须在 hooks 里补上，
# 否则 A1.10 会因为"缺字段 400"而假红（select 的 preview + thumb 就是这样）。
PROBE_UPLOAD_FIELD2="${PROBE_UPLOAD_FIELD2:-}"
PROBE_CREATE_ENDPOINT="${PROBE_CREATE_ENDPOINT:-}"
PROBE_CREATE_BODY="${PROBE_CREATE_BODY:-}"
PROBE_DELETE_TMPL="${PROBE_DELETE_TMPL:-}"
# A1.4 删不存在的资源时替换 {id} 的占位值。路由参数是**整型**的站必须改，
# 否则非数字占位串会先撞参数解析回 400，永远测不到"删不存在的资源"。
PROBE_DELETE_GHOST="${PROBE_DELETE_GHOST:-}"
PROBE_LOGIN_PATH="${PROBE_LOGIN_PATH:-}"
PROBE_LOGIN_BODY="${PROBE_LOGIN_BODY:-}"
PROBE_EXTRA_HEADERS=()   # 如 -H "Origin: …"（CSRF 站必须给）
PROBE_AUTH_HEADERS=()    # 如 -H "Authorization: Bearer …"

probe_setup() { echo "hooks 没有提供 probe_setup（也没给 PROBE_BASE_URL）" >&2; return 1; }
probe_teardown() { :; }

# shellcheck source=/dev/null
. "${HOOKS}"

trap 'probe_teardown 2>/dev/null || true; rm -rf "${WORK}"' EXIT

if [ -z "${PROBE_BASE_URL}" ]; then
	probe_setup || { echo "错误：probe_setup 失败 —— 起不来服务就什么都判不了" >&2; exit 2; }
fi
[ -n "${PROBE_BASE_URL}" ] || { echo "错误：probe_setup 没有 export PROBE_BASE_URL" >&2; exit 2; }
URL="${PROBE_BASE_URL%/}"
# hooks 没单独挑"必然失败的应用端点"时，退回用那个不存在的路径（判据会弱一档，见 probe_A15 注释）
PROBE_ERROR_PATH="${PROBE_ERROR_PATH:-${PROBE_404_PATH}}"

# ── 计分 ───────────────────────────────────────────────────────────────────
PASS_N=0; FAIL_N=0; SKIP_N=0; ALLOW_N=0; NA_N=0
FAILED_CLAUSES=""

contains_word() { case " $1 " in *" $2 "*) return 0 ;; *) return 1 ;; esac; }
skipped() { contains_word "${PROBE_SKIP}" "$1"; }

record() { # $1=条款 $2=PASS|FAIL|SKIP|N/A|ALLOW $3=说明
	printf '  [%-5s] %-6s %s\n' "$2" "$1" "$3"
	case "$2" in
		PASS)  PASS_N=$((PASS_N + 1)) ;;
		SKIP)  SKIP_N=$((SKIP_N + 1)) ;;
		ALLOW) ALLOW_N=$((ALLOW_N + 1)) ;;
		N/A)   NA_N=$((NA_N + 1)) ;;
		FAIL)
			if contains_word "${PROBE_ALLOW_FAIL}" "$1"; then
				ALLOW_N=$((ALLOW_N + 1))
				echo "          ↳ 已登记豁免，不判失败（见 conformance-exemptions.json）"
			else
				FAIL_N=$((FAIL_N + 1)); FAILED_CLAUSES="${FAILED_CLAUSES} $1"
			fi
			;;
	esac
}

# ── HTTP 小工具 ────────────────────────────────────────────────────────────
req() { # req <name> [curl args...] —— 结果留在 $WORK/<name>.{body,hdr,code}，不打印
	local name="$1"; shift
	curl -sS -o "${WORK}/${name}.body" -D "${WORK}/${name}.hdr" \
		-w '%{http_code}' --max-time 20 "$@" > "${WORK}/${name}.code" 2> "${WORK}/${name}.err" || true
}
code_of() { cat "${WORK}/$1.code" 2>/dev/null; }
flat_of() { tr -d ' \t\r\n' < "${WORK}/$1.body" 2>/dev/null; }
ct_of()   { tr -d '\r' < "${WORK}/$1.hdr" 2>/dev/null | grep -i '^content-type:' | head -1 | cut -d' ' -f2-; }
hdr_field() { tr -d '\r' < "${WORK}/$1.hdr" 2>/dev/null | grep -i "^$2:" | head -1 | cut -d' ' -f2-; }
is_json() {
	if command -v node >/dev/null 2>&1; then
		node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{JSON.parse(s);process.exit(0)}catch{process.exit(1)}})' < "$1" 2>/dev/null
	else
		grep -qE '^[[:space:]]*[\[{]' "$1" 2>/dev/null
	fi
}

echo "══ 规范 ② 档探针 ══  ${URL}"
echo ""

# ────────────────────────────────────────────────────────────────────────────
# A1.7 错误响应必须是 JSON，且 Content-Type 带 charset=utf-8
#      本机判得了全部：404 / 405 正是标准库最容易绕过写出器的地方
# ────────────────────────────────────────────────────────────────────────────
probe_A17() {
	local bad=""
	req nf "${URL}${PROBE_404_PATH}"
	local ct; ct="$(ct_of nf)"
	[ -n "$ct" ] || bad="${bad} 404 没有 Content-Type;"
	case "$ct" in *application/json*) ;; *) bad="${bad} 404 Content-Type=${ct};" ;; esac
	case "$ct" in *charset=utf-8*) ;; *) bad="${bad} 404 缺 charset=utf-8;" ;; esac
	is_json "${WORK}/nf.body" || bad="${bad} 404 body 不是 JSON（$(head -c 60 "${WORK}/nf.body")）;"

	if [ -n "${PROBE_405_PATH}" ]; then
		req bm -X "${PROBE_405_METHOD}" "${URL}${PROBE_405_PATH}" "${PROBE_EXTRA_HEADERS[@]}"
		if [ "$(code_of bm)" != "405" ]; then
			bad="${bad} ${PROBE_405_METHOD} ${PROBE_405_PATH} 期待 405 得到 $(code_of bm);"
		else
			ct="$(ct_of bm)"
			case "$ct" in *application/json*charset=utf-8*) ;; *) bad="${bad} 405 Content-Type=${ct};" ;; esac
			is_json "${WORK}/bm.body" || bad="${bad} 405 body 不是 JSON;"
		fi
	fi

	if [ -n "$bad" ]; then record A1.7 FAIL "${bad# }"
	else record A1.7 PASS "404（+405）都是 application/json; charset=utf-8"; fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.5 失败必须给出机器可读的 code：{"error":{"code","message"}}
#      ⚠️ 判据必须打在**应用自己写出**的错误上。框架兜底的 404（`404 page not found`）
#         既没有 code 也没有 message，用它判 A1.5 会把"框架没走写出器"记成"应用缺 code"
#         —— 那是 A1.7 的账，不是 A1.5 的。所以单独声明 PROBE_ERROR_PATH。
# ────────────────────────────────────────────────────────────────────────────
probe_A15() {
	req ae "${URL}${PROBE_ERROR_PATH}" "${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}"
	local f; f="$(flat_of ae)"
	local st; st="$(code_of ae)"
	if [ "$st" = "200" ] || [ "$st" = "201" ]; then
		record A1.5 SKIP "PROBE_ERROR_PATH=${PROBE_ERROR_PATH} 返回 ${st}（没触发失败），hooks 要挑一个必然失败的端点"
		return
	fi
	if echo "$f" | grep -q '"error":{'; then
		local code; code="$(echo "$f" | sed -n 's/.*"error":{[^{}]*"code":"\([^"]*\)".*/\1/p')"
		if [ -z "$code" ]; then
			record A1.5 FAIL '有 error 对象，但没有 error.code'
		elif ! echo "$code" | grep -Eq '^[a-z][a-z0-9]*(_[a-z0-9]+)*$'; then
			record A1.5 FAIL "error.code 不是 snake_case：${code}"
		else
			record A1.5 PASS "error.code=${code}"
		fi
	elif echo "$f" | grep -q '"error":"'; then
		record A1.5 FAIL '{"error":"<一句文案>"} —— 没有机器可读的 code（规范要求 {"error":{"code","message"}}）'
	elif echo "$f" | grep -q '"error":'; then
		record A1.5 FAIL "error 不是对象：$(echo "$f" | head -c 80)"
	else
		record A1.5 FAIL "失败响应里没有 error 字段：$(echo "$f" | head -c 80)"
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.2 单对象不带信封；列表包一层固定键名 {items:[…]}；items 不得为 null
# ────────────────────────────────────────────────────────────────────────────
probe_A12() {
	# 兼容两种写法：把 PROBE_LIST_ENDPOINTS 写成空格分隔字符串（契约默认），
	# 或写成 bash 数组（钩子作者十有八九会这么写）。
	# ⚠️ 数组若无引号展开，只会取到第 0 个元素，其余**静默漏判** —— 这个坑真踩过：
	#    studio 首轮显示"1 个列表端点"，改成字符串后才判到 5 个。
	local eps=()
	if declare -p PROBE_LIST_ENDPOINTS 2>/dev/null | grep -q '^declare -a'; then
		eps=("${PROBE_LIST_ENDPOINTS[@]}")
	elif [ -n "${PROBE_LIST_ENDPOINTS}" ]; then
		# shellcheck disable=SC2206
		eps=(${PROBE_LIST_ENDPOINTS})
	fi
	if [ "${#eps[@]}" -eq 0 ]; then
		record A1.2 SKIP "hooks 没声明 PROBE_LIST_ENDPOINTS"
		return
	fi
	local bad="" ep i=0
	for ep in "${eps[@]}"; do
		i=$((i + 1))
		req "l${i}" "${URL}${ep}${PROBE_LIST_QUERY}" "${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}"
		local code; code="$(code_of "l${i}")"
		if [ "$code" != "200" ]; then bad="${bad} ${ep} 期待 200 得到 ${code};"; continue; fi
		local f; f="$(flat_of "l${i}")"
		case "$f" in
			'['*) bad="${bad} ${ep} 是裸数组（规范要求 {items:[…]}）;"; continue ;;
		esac
		if echo "$f" | grep -q '"items":null'; then
			bad="${bad} ${ep} 的 items 是 null（空列表必须序列化成 []，前端 list.map 会抛）;"; continue
		fi
		if ! echo "$f" | grep -q '"items":\['; then
			bad="${bad} ${ep} 没有 items 数组（顶层键：$(echo "$f" | head -c 70)）;"; continue
		fi
		# 「要么四个都给，要么只给 items」——给了 total 就必须真的接受分页入参
		if echo "$f" | grep -q '"total":' \
			&& ! echo "$f" | grep -q '"limit":' && ! echo "$f" | grep -q '"offset":'; then
			bad="${bad} ${ep} 给了 total 却既不接受 limit 也不接受 offset（规范要求四个键一起给，或只给 items）;"
		fi
	done
	if [ -n "$bad" ]; then record A1.2 FAIL "${bad# }"
	else record A1.2 PASS "${i} 个列表端点都是 {items:[…]}"; fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.4 创建 201+Location；删不存在的资源必须 404（不许 200 {"status":"deleted"}）
# ────────────────────────────────────────────────────────────────────────────
probe_A14() {
	if [ -z "${PROBE_CREATE_ENDPOINT}" ]; then
		record A1.4 SKIP "hooks 没声明可写创建端点（PROBE_CREATE_ENDPOINT）"
		return
	fi
	local bad=""
	req cr -X POST "${URL}${PROBE_CREATE_ENDPOINT}" -H 'Content-Type: application/json' \
		"${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}" -d "${PROBE_CREATE_BODY}"
	case "$(code_of cr)" in
		401|403) record A1.4 SKIP "可写端点要鉴权（$(code_of cr)），hooks 没给凭据"; return ;;
		201) ;;
		*) bad="${bad} POST 创建期待 201 得到 $(code_of cr);" ;;
	esac
	if [ -z "$bad" ] && [ -z "$(hdr_field cr location)" ]; then
		bad="${bad} 201 没有 Location;"
	fi
	# 删除那一半：只有本站确实有 DELETE 端点时才判
	if [ -z "${PROBE_DELETE_TMPL}" ]; then
		if [ -n "$bad" ]; then record A1.4 FAIL "${bad# }（本站没有 DELETE 端点，删除那一半未判）"
		else record A1.4 PASS "创建 201+Location（本站没有 DELETE 端点，删除那一半未判）"; fi
		return
	fi
	# ghost id 必须**语法合法但不存在**。路由参数是整型的站会把非数字串先判成 400，
	# 于是"删不存在的资源"永远测不到 —— 那是探针的错，不是应用的错。
	local ghostid="${PROBE_DELETE_GHOST:-__spec-probe-no-such-id__}"
	local ghost="${PROBE_DELETE_TMPL//\{id\}/$ghostid}"
	req gd -X DELETE "${URL}${ghost}" "${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}"
	case "$(code_of gd)" in
		404|410) ;;
		400) bad="${bad} 删不存在的资源期待 404 得到 400（若原因是 id 格式非法，请在 hooks 里把 PROBE_DELETE_GHOST 设成语法合法但不存在的 id 再判）;" ;;
		*) bad="${bad} 删不存在的资源期待 404 得到 $(code_of gd);" ;;
	esac
	if [ -n "$bad" ]; then record A1.4 FAIL "${bad# }"
	else record A1.4 PASS "创建 201+Location；删不存在 404"; fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.9 请求体超限必须由应用回 413
#      ⚠️ 只判得到"应用这一半"；"上限 < nginx client_max_body_size"在生产上。
#         这句话必须打出来，否则就是最危险的那种假绿。
# ────────────────────────────────────────────────────────────────────────────
probe_A19() {
	if [ -z "${PROBE_BODY_ENDPOINT}" ] || [ -z "${PROBE_BODY_LIMIT}" ]; then
		record A1.9 SKIP "hooks 没声明 PROBE_BODY_ENDPOINT / PROBE_BODY_LIMIT"
		return
	fi
	# 造一个**合法 JSON** 的超限体。
	# ⚠️ 曾经用纯 'a'（head -c /dev/zero | tr），而任何「MaxBytesReader + 流式
	#    json.Decoder」的站都会在第一个字节以语法错误拒成 400 `invalid_json`
	#    —— 于是这条永远测不到 413 分支，判出来的是"探针的错"。
	#    orderflow / studio / token-cost / pet 都因此假红过。
	#    形状可用 PROBE_BODY_PREFIX / PROBE_BODY_SUFFIX 覆盖（默认 {"pad":"…"}）。
	{
		printf '%s' "${PROBE_BODY_PREFIX}"
		head -c "$((PROBE_BODY_LIMIT + 65536))" /dev/zero | tr '\0' 'a'
		printf '%s' "${PROBE_BODY_SUFFIX}"
	} > "${WORK}/big.bin"
	req bg -X POST "${URL}${PROBE_BODY_ENDPOINT}" -H 'Content-Type: application/json' \
		"${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}" --data-binary "@${WORK}/big.bin"
	case "$(code_of bg)" in
		401|403) record A1.9 SKIP "端点要鉴权（$(code_of bg)），hooks 没给凭据"; return ;;
		400) record A1.9 FAIL "超限回 400 不是 413（应用把\"超出上限\"当成了\"解析失败\"）$(flat_of bg | head -c 80)"; return ;;
		413) ;;
		*) record A1.9 FAIL "超限期待 413 得到 $(code_of bg)"; return ;;
	esac
	if flat_of bg | grep -Eq '"code":"(too_large|payload_too_large)"'; then
		record A1.9 PASS "413 + too_large ⚠️ nginx 侧那一半未验证（server-audit.sh）"
	else
		record A1.9 FAIL "413 但 code 不是 too_large/payload_too_large：$(flat_of bg | head -c 80)"
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.10 类型不支持回 415/422，且判定依据是**文件魔数**不是 Content-Type
# ────────────────────────────────────────────────────────────────────────────
probe_A110() {
	if [ -z "${PROBE_UPLOAD_ENDPOINT}" ]; then
		record A1.10 SKIP "本站没有上传端点"
		return
	fi
	printf '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>' > "${WORK}/evil.svg"
	# 有些站的上传要求不止一个字段（select 要 preview + thumb），只发一个会被
	# 判成"缺字段 400"，与 A1.10 想测的类型校验完全无关 ⟹ 可选补第二份。
	local extra=()
	if [ -n "${PROBE_UPLOAD_FIELD2}" ]; then
		cp "${WORK}/evil.svg" "${WORK}/evil2.svg"
		extra=(-F "${PROBE_UPLOAD_FIELD2}=@${WORK}/evil2.svg;type=image/jpeg;filename=evil2.jpg")
	fi
	req sv -X POST "${URL}${PROBE_UPLOAD_ENDPOINT}" \
		-F "${PROBE_UPLOAD_FIELD}=@${WORK}/evil.svg;type=image/jpeg;filename=evil.jpg" \
		"${extra[@]}" \
		"${PROBE_EXTRA_HEADERS[@]}" "${PROBE_AUTH_HEADERS[@]}"
	# 401/403 必须与 415/422 分开，否则"没登录"会被记成"类型校验失败"
	case "$(code_of sv)" in
		401|403) record A1.10 SKIP "上传端点要鉴权（$(code_of sv)），hooks 没给凭据"; return ;;
		415|422) ;;
		400) record A1.10 FAIL "SVG 冒充 image/jpeg 期待 415/422 得到 400（若本站上传要求多个字段，请在 hooks 里补 PROBE_UPLOAD_FIELD2 再判）$(flat_of sv | head -c 80)"; return ;;
		*) record A1.10 FAIL "SVG 冒充 image/jpeg 期待 415/422 得到 $(code_of sv)（按 Content-Type 判就会放它进去）"; return ;;
	esac
	if flat_of sv | grep -q '"code":"'; then
		record A1.10 PASS "SVG 被魔数挡下（$(code_of sv)）"
	else
		record A1.10 FAIL "$(code_of sv) 但没有机器可读的 error.code"
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# A1.13 限流必须回 429 + Retry-After（不许用 403 表达限流）
#      ⚠️ 探测身份必须按**限流键**选。按 IP 限流的站（主站）在 CI 上会把自己打满，
#         所以那种站直接在 hooks 里 PROBE_SKIP=A1.13。
# ────────────────────────────────────────────────────────────────────────────
probe_A113() {
	if [ -z "${PROBE_LOGIN_PATH}" ]; then
		record A1.13 SKIP "hooks 没声明登录端点（或按 IP 限流，CI 里探测会自杀式污染）"
		return
	fi
	local i code hit=0
	for i in 1 2 3 4 5 6 7 8; do
		req "lg${i}" -X POST "${URL}${PROBE_LOGIN_PATH}" -H 'Content-Type: application/json' \
			"${PROBE_EXTRA_HEADERS[@]}" -d "$(printf '%s' "${PROBE_LOGIN_BODY}" | sed "s/__I__/${i}/")"
		code="$(code_of "lg${i}")"
		if [ "$code" = "403" ]; then
			record A1.13 FAIL '限流用了 403（规范明令禁止：403 的语义是「已认证但无权」）'; return
		fi
		[ "$code" = "429" ] && { hit=$i; break; }
	done
	if [ "$hit" = 0 ]; then
		record A1.13 FAIL "连打 8 次都没触发限流"; return
	fi
	local ra; ra="$(hdr_field "lg${hit}" retry-after)"
	if [ -z "$ra" ]; then
		record A1.13 FAIL "第 ${hit} 次触发了 429，但没有 Retry-After"
	elif ! echo "$ra" | grep -Eq '^[0-9]+$'; then
		record A1.13 FAIL "Retry-After 不是秒数：${ra}"
	else
		record A1.13 PASS "第 ${hit} 次 429，Retry-After=${ra}s"
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# A2.6 空库必须能跑通全部迁移（条款原文就要求"必须自动验证"）
# ────────────────────────────────────────────────────────────────────────────
probe_A26() {
	if [ -n "${PROBE_MIGRATE_NA}" ]; then
		record A2.6 N/A "${PROBE_MIGRATE_NA}"
		return
	fi
	if [ -z "${PROBE_MIGRATE_CMD}" ]; then
		record A2.6 SKIP "hooks 没声明 PROBE_MIGRATE_CMD / PROBE_MIGRATE_NA"
		return
	fi
	if ( cd "${ROOT}" && eval "${PROBE_MIGRATE_CMD}" ) > "${WORK}/mig.log" 2>&1; then
		record A2.6 PASS "空库跑通：${PROBE_MIGRATE_CMD}"
	else
		record A2.6 FAIL "空库跑不通：${PROBE_MIGRATE_CMD}"
		tail -5 "${WORK}/mig.log" | sed 's/^/          /'
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# C4.5 后端必须绑回环 —— 本机只能判"这个二进制默认绑哪"
#      ⚠️ 合规判据的宾语是**生产上那个正在跑的进程**，这里判不了。
# ────────────────────────────────────────────────────────────────────────────
probe_C45() {
	if [ -z "${PROBE_LISTEN_PORT}" ]; then
		record C4.5 SKIP "hooks 没声明 PROBE_LISTEN_PORT"
		return
	fi
	if ! command -v ss >/dev/null 2>&1; then
		record C4.5 SKIP "本机没有 ss（Windows/Git Bash）—— 这一条只在 Linux CI 上判得了"
		return
	fi
	local addrs a bad=""
	addrs="$(ss -tlnH 2>/dev/null | awk -v p=":${PROBE_LISTEN_PORT}" '$4 ~ p"$" {print $4}')"
	if [ -z "$addrs" ]; then
		record C4.5 FAIL "没有进程在听 :${PROBE_LISTEN_PORT}（探针自己起的服务呢？）"
		return
	fi
	for a in $addrs; do
		case "$a" in
			127.*:*|"[::1]:"*) ;;
			*) bad="${bad} 通配/对外监听 ${a};" ;;
		esac
	done
	if [ -n "$bad" ]; then
		record C4.5 FAIL "默认绑定不是回环：${bad# }"
	else
		record C4.5 PASS "默认只绑回环（$(echo "$addrs" | tr '\n' ' ' | sed 's/ $//')）"
	fi
}

# ────────────────────────────────────────────────────────────────────────────
# A2.13 数据目录不得对 other 可写（本地只判得"应用自建目录"这一半）
# ────────────────────────────────────────────────────────────────────────────
probe_A213() {
	if [ -z "${PROBE_DATA_DIR}" ] || [ ! -d "${PROBE_DATA_DIR}" ]; then
		record A2.13 SKIP "hooks 没声明 PROBE_DATA_DIR（或服务没把它建出来）"
		return
	fi
	local bad; bad="$(find "${PROBE_DATA_DIR}" -perm -0002 2>/dev/null | head -5)"
	if [ -n "$bad" ]; then
		record A2.13 FAIL "对 other 可写：$(echo "$bad" | tr '\n' ' ')"
	else
		record A2.13 PASS "应用自建数据目录 other 不可写 ⚠️ 属主一维在 CI 里恒真，站点根见 server-audit.sh"
	fi
}

# ── 逐条跑 ─────────────────────────────────────────────────────────────────
# hooks 里可以写 PROBE_REASON_A12="……" 之类，给 SKIP 一条人话理由。
for c in A1.7 A1.5 A1.2 A1.4 A1.9 A1.10 A1.13 A2.6 C4.5 A2.13; do
	id="$(echo "$c" | tr -d '.')"
	if skipped "$c"; then
		rv="PROBE_REASON_${id}"
		record "$c" SKIP "${!rv:-hooks 的 PROBE_SKIP 里显式排除}"
		continue
	fi
	"probe_${id}"
done

echo ""
echo "── 探针小结：PASS ${PASS_N} · FAIL ${FAIL_N} · 豁免 ${ALLOW_N} · N/A ${NA_N} · SKIP ${SKIP_N} ──"
if [ "${SKIP_N}" -gt 0 ]; then
	echo "   ⚠️ SKIP 不是通过。每条 SKIP 都写了为什么判不了 ——"
	echo "      要么去 Siungo-Workspace/scripts/server-audit.sh 判，要么补本仓的 hooks。"
fi
if [ "${FAIL_N}" -gt 0 ]; then
	echo "   ❌ FAIL：${FAILED_CLAUSES# }"
	exit 1
fi
echo "   ✅ 探针无 FAIL"
exit 0
