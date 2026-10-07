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
# 规范（docs/standards/conformance-gate.md §1）把 109 条条款分三档：① 机检 94 条 ·
# ② 半机械 8 条 · ③ 只能靠人 7 条。其中 ② 档「半机械，要跑起来才能判」共 8 条，
# 原本被指派给"各站的 smoke"。实做下来那个指派只对 3 条成立（A2.6 / C4.5 / A2.13）——
# 另外 5 条的判据宾语在**生产服务器**上（`/etc/`、systemd 定时器、真实数据目录、
# 历史部署动作），smoke 侧根本拿不到。所以拆成两边：
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

# ── 后半 5 条（A1.1 / A1.11 / A1.12 / A1.14 / A3.11）需要的站点声明 ────────
# A1.1：站点**真实对外**的端点清单（空格分隔）。缺省的退回顺序：
#       PROBE_LIST_ENDPOINTS + PROBE_ERROR_PATH；再缺就 SKIP 并说明。
PROBE_API_PATHS="${PROBE_API_PATHS:-}"
# A1.1：健康检查路径。规范 A1.15 允许它不带 /api 前缀，所以单独给变量。
PROBE_HEALTH_PATH="${PROBE_HEALTH_PATH:-/api/health}"
# A1.11：**用正确凭据**登录一次的端点与请求体（与 A1.13 的错误凭据是两回事）。
PROBE_SESSION_LOGIN_PATH="${PROBE_SESSION_LOGIN_PATH:-}"
PROBE_SESSION_LOGIN_BODY="${PROBE_SESSION_LOGIN_BODY:-}"
# A1.12：写端点（用来验"关掉浏览器的自动 CSRF 豁免之后拦不拦"）。
#        不声明时退回 PROBE_SESSION_LOGIN_PATH（登录本身也是非安全方法）。
PROBE_CSRF_ENDPOINT="${PROBE_CSRF_ENDPOINT:-}"
PROBE_CSRF_METHOD="${PROBE_CSRF_METHOD:-POST}"
PROBE_CSRF_BODY="${PROBE_CSRF_BODY:-}"
# A1.14：本站登录是否按 IP 限流。**只有显式写 1 才判**，不声明一律 SKIP ——
#        "猜一个限流键"会把按账号限流的站（orderflow 按邮箱）判成假红。
PROBE_IP_KEYED_RATELIMIT="${PROBE_IP_KEYED_RATELIMIT:-0}"
# A3.11：本站上传是否**做图片处理**（读 EXIF / 转码）。只有显式写 1 才判 ——
#        不做图片处理的站，"坏图片字节"测的只是字节存储，判了等于没判。
PROBE_IMAGE_UPLOAD="${PROBE_IMAGE_UPLOAD:-0}"
# A3.11：候选上传端点（`basic` 的取值顺序）。
PROBE_UPLOAD_ENDPOINT2="${PROBE_UPLOAD_ENDPOINT2:-}"
PROBE_UPLOAD_ENDPOINT3="${PROBE_UPLOAD_ENDPOINT3:-}"
PROBE_UPLOAD_FIELD3="${PROBE_UPLOAD_FIELD3:-file}"

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

# ────────────────────────────────────────────────────────────────────────────
# A1.1 端点必须挂在 /api 下且不带版本段
#      本机判得了：① 声明的端点真可达 ② /api→/api/v1 的同一尾段必须 404
#      判不了：nginx location / Vite proxy / 前端 API_BASE 那三处是否对齐
#      —— 判据的宾语是**部署配置**，那是 server-audit.sh 的活（写进 detail）。
# ────────────────────────────────────────────────────────────────────────────
probe_A11() {
	local list="${PROBE_API_PATHS}"
	[ -n "$list" ] || list="${PROBE_LIST_ENDPOINTS} ${PROBE_ERROR_PATH}"
	if [ -z "$(printf '%s' "$list" | tr -d ' \t')" ]; then
		record A1.1 SKIP "hooks 没声明 PROBE_API_PATHS（也没给 PROBE_LIST_ENDPOINTS/PROBE_ERROR_PATH）"
		return
	fi
	local p n=0 code bad=""
	# 探通全部端点（$WORK 是本次运行的临时目录，退出即删）
	for p in $list; do
		n=$((n + 1))
		req "a11_${n}" "${URL}${p}" "${PROBE_EXTRA_HEADERS[@]}"
		code="$(code_of "a11_${n}")"
		case "$code" in
			000|404) bad="${bad} ${p}→${code}（声明为真端点却不通）;" ;;
		esac
	done
	# 版本别名：把 **第一个** /api 换成 /api/v1 —— 站内 200（存在版本别名），站外 404（不存在）
	local p1 alias ac
	for p1 in $list; do
		alias="$(printf '%s' "$p1" | sed 's#/api#/api/v1#')"
		[ "$alias" != "$p1" ] || continue
		req a11_alias "${URL}${alias}" "${PROBE_EXTRA_HEADERS[@]}"
		ac="$(code_of a11_alias)"
		if [ "$ac" = "200" ]; then
			bad="${bad} ${p1} 同时存在版本别名 ${alias}（200）;"
		fi
		break
	done
	# 健康检查必须活着（A1.15 规定它自己最简，可豁免 /api 前缀）
	local hc
	if [ -n "${PROBE_HEALTH_PATH}" ]; then
		req a11_h "${URL}${PROBE_HEALTH_PATH}"
		hc="$(code_of a11_h)"
		case "$hc" in
			2*) ;;
			*) bad="${bad} ${PROBE_HEALTH_PATH}→${hc}（健康检查必须 2xx）;" ;;
		esac
	fi
	if [ -n "$bad" ]; then
		record A1.1 FAIL "路由形状不对：${bad# }"
	else
		record A1.1 PASS "声明的端点全部可达、没有 /api/v1 别名、${PROBE_HEALTH_PATH} 2xx（探了 ${n} 条）"
	fi
	echo "          ⚠️ 判不了的那半：nginx \`location\`、Vite dev proxy、前端 API_BASE 三处是否按同一前缀分流"
	echo "             —— 少一层会落 SPA 回退，浏览器拿到 HTML（Unexpected token 尖括号）。本机只证了服务端路由形状，"
	echo "             部署侧请看 Siungo-Workspace/scripts/server-audit.sh。"
}

# ────────────────────────────────────────────────────────────────────────────
# A1.11 浏览器会话凭证必须是 HttpOnly Cookie，不得交给 JS
#      本机判得了：登录响应到底下不下发 Cookie、下发的 cookie 属性写全没有
#      判不了：登录响应体里的 token 被前端拿去放哪儿（= 机检扫 localStorage 的活）、
#              TTL 续期、以及"服务端是否只存 sha256"
# ⚠️ 本函数把会话 Cookie 落成纯文本 ${WORK}/ph2_cookie.txt，供 probe_A112 复用。
# ────────────────────────────────────────────────────────────────────────────
# PH2-EPOCH: 把 HTTP 日期转成 epoch 秒。优先 GNU date -d（Linux CI），
# 退回 BSD date -j -f（macOS），都没有就打印空串（调用方只把结果当"解析得出上限"用）。
ph2_epoch() { # $1 = HTTP 日期字符串（如 Fri, 01 Jan 2027 00:00:00 GMT）
	local d="$1" e
	e="$(date -u -d "$d" +%s 2>/dev/null)" || e=""
	[ -n "$e" ] && { printf '%s' "$e"; return 0; }
	e="$(date -j -u -f '%a, %d %b %Y %T GMT' "$d" +%s 2>/dev/null)" || e=""
	printf '%s' "$e"
	return 0
}

ph2_session_cookie_name() { # $1=WORK 目录 → 打印会话 Cookie 的名字（没有则空）
	sed -n 's/^[Ss]et-[Cc]ookie:[[:space:]]*\([^=;]*\)=.*/\1/p' "${1}/a111.hdr" 2>/dev/null \
		| tr -d '\r' | grep -v '^$' | tail -1
}

probe_A111() {
	if [ -z "${PROBE_SESSION_LOGIN_PATH}" ]; then
		record A1.11 SKIP "hooks 没声明 PROBE_SESSION_LOGIN_PATH（正确凭据的登录端点）"
		return
	fi
	req a111 -X POST "${URL}${PROBE_SESSION_LOGIN_PATH}" \
		-H 'Content-Type: application/json' "${PROBE_EXTRA_HEADERS[@]}" \
		-d "${PROBE_SESSION_LOGIN_BODY}"
	local code; code="$(code_of a111)"
	if [ -z "$code" ] || [ "$code" = "000" ]; then
		record A1.11 SKIP "登录端点 ${PROBE_SESSION_LOGIN_PATH} 连不上（curl 000）—— 先修 hooks 的凭据/端点"
		return
	fi
	local body; body="$(flat_of a111)"
	local line; line="$(tr -d '\r' < "${WORK}/a111.hdr" | grep -i '^set-cookie:' | head -1)"; line="${line#*: }"
	if [ -z "$line" ]; then
		# 没有 Cookie：body 里出现会话 token/JWT 就是把凭证交给了 JS
		if printf '%s' "$body" | grep -q '"token"[[:space:]]*:[[:space:]]*"'; then
			record A1.11 FAIL "登录回 ${code} 且一个 Set-Cookie 都没有，body 里却带 token 字段 —— 会话凭证只能进 Web Storage（同源任何脚本可读，一次 XSS 全带走），且服务端无法察觉"
		elif printf '%s' "$body" | grep -qE '[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}'; then
			record A1.11 FAIL "登录回 ${code}、零 Set-Cookie，body 里能看到三段点分 JWT —— 同上，必须改成 HttpOnly Cookie"
		elif [ "$code" = "200" ]; then
			record A1.11 SKIP "登录 ${code} 但既无 Set-Cookie 也看不出 token —— 多半是 hooks 里的 PROBE_SESSION_LOGIN_BODY 不是真能登录的凭据"
		else
			record A1.11 N/A "登录回 ${code} 且没有下发会话 Cookie —— 本站浏览器端会话不用 Cookie；凭据写法判不了（条款对本形态不适用）"
		fi
		return
	fi
	# 有 Cookie：逐项对条款的属性表
	local rest name=http
	rest="${line#*;}"
	name="$(printf '%s' "$line" | sed -n 's/^\([^=]*\)=.*/\1/p')"
	case "$line" in *"; HttpOnly"*|*";HttpOnly"*|*"HttpOnly"*) http=1 ;; *) http=0 ;; esac
	case "$rest" in *"Path=/"*) path=1 ;; *) path=0 ;; esac
	local ss="" m
	for m in Strict Lax None; do case "$rest" in *"SameSite=${m}"*) ss="$m" ;; esac; done
	local bad="" det=""
	[ "$http" = "1" ] || bad="${bad} 缺 HttpOnly;"
	[ "$path" = "1" ] || bad="${bad} 缺 Path=/;"
	case "$ss" in
		Lax) ;;
		"")  bad="${bad} 缺 SameSite（条款要求 Lax）;" ;;
		*)   bad="${bad} SameSite=${ss}（条款要求 Lax；Strict 会让外部链接进来的首屏变未登录态，None 等于不设防）;" ;;
	esac
	# TTL：Max-Age 秒数 / Expires 绝对时间，取到哪个算哪个。上限 14 天。
	local maxage expi secs now
	maxage="$(printf '%s' "$rest" | sed -n 's/.*Max-Age=\([0-9]*\).*/\1/p')"
	expi="$(printf '%s' "$rest" | sed -n 's/.*[Ee]xpires=\([^;]*\).*/\1/p')"
	if [ -n "$maxage" ] && [ "$maxage" -gt 1209600 ] 2>/dev/null; then
		bad="${bad} Max-Age=${maxage}s 超过 14 天;"
	elif [ -z "$maxage" ] && [ -n "$expi" ]; then
		secs="$(ph2_epoch "$expi")"; now="$(date -u +%s 2>/dev/null)"
		if [ -n "$secs" ] && [ -n "$now" ] && [ "$secs" -gt "$((now + 1209600))" ] 2>/dev/null; then
			bad="${bad} Expires=${expi} 超过 14 天;"
		fi
	fi
	if [ -n "$bad" ]; then
		record A1.11 FAIL "会话 Cookie 属性不合条款：${bad# }（实测：${line}）"
	else
		record A1.11 PASS "会话 Cookie 属性齐：HttpOnly + Path=/ + SameSite=Lax（实测：${line}）"
	fi
	[ -n "$maxage" ] || det="${det} TTL：Max-Age 缺失，Expires 解析不出上限;"
	det="${det} 7 天续期阈值的宽限期日志（条款要求）判不了;"
	det="${det} 服务端是否只存 token 的 sha256 判不了（那是服务端存储，不在 HTTP 面上）;"
	[ "$name" = "session" ] || det="${det} ⚠️ cookie 名是 ${name} 而不是条款写的 session（只记不下判：orderflow 的 sid 是标杆）;"
	case "${URL}" in
		https://*) case "$line" in *"Secure"*) ;; *) bad="Secure 缺失"; det="${det} ⚠️ https 下缺 Secure;" ;; esac ;;
		*) det="${det} Secure 未判（本机 http，浏览器本来就不回传它）；生产必须 true，需 https 才判得了;" ;;
	esac
	printf '%s' "$line" > "${WORK}/ph2_cookie.txt"
	[ -n "$det" ] && echo "          ℹ️${det}"
	return 0
}

# ────────────────────────────────────────────────────────────────────────────
# A1.12 用了 Cookie 就必须有 CSRF 来源校验（所有非安全方法）
#      本机判得了：不带 Origin/Referer 会不会被拒、外站 Origin 会不会被拒、
#                  带本站 Origin 会不会**因为 CSRF 本身**被拒
#      判不了：SameSite=Lax 之外浏览器侧的兜底行为、以及将来写操作变 GET 的回归
# ⚠️ 依赖 probe_A111 落下的 ${WORK}/ph2_cookie.txt（A1.11 在本清单里排在它前面）。
# ────────────────────────────────────────────────────────────────────────────
probe_A112() {
	local cookie=""
	[ -f "${WORK}/ph2_cookie.txt" ] && cookie="$(cat "${WORK}/ph2_cookie.txt")"
	if [ -z "$cookie" ]; then
		record A1.12 N/A "A1.11 那一步没看到会话 Cookie（本站会话不用 Cookie）—— 本条不适用；一旦按 A1.11 改造立刻适用"
		return
	fi
	local ep="${PROBE_CSRF_ENDPOINT}"
	[ -n "$ep" ] || ep="${PROBE_SESSION_LOGIN_PATH}"
	if [ -z "$ep" ]; then
		record A1.12 SKIP "hooks 没声明 PROBE_CSRF_ENDPOINT（也没给 PROBE_SESSION_LOGIN_PATH）"
		return
	fi
	# cookie 名 + 值：Set-Cookie 原文取第一个分号之前那一段（PH2-A112-1）
	local nv; nv="$(sed -n '1s/[[:space:]]*$//p' "${WORK}/ph2_cookie.txt" | cut -d';' -f1)"
	local origin="https://evil.example"
	local base="${URL#*://}"; base="${base%%/*}"
	local me="http://${base}"; me="${me%/}"
	local args=(-X "${PROBE_CSRF_METHOD}" -H 'Content-Type: application/json' -H "Cookie: ${nv}")
	# ⚠️ PH2-A112-2：三个请求**各用一份写坏的 body**（`__I__` → 固定串），别共用。
	# 共用过：A1.12 判据是"这一条能不能穿过 CSRF"，但共用同一份合法 body 时，
	# 第一条**真的会打到 handler**（CSRF 坏掉的那种站就是这么被发现的）并建出一个资源，
	# 第二条起拿到的是业务层的 duplicate_name/409 —— 探针就会把 409 读成"跨站写请求没被拦"，
	# 判出来的 FAIL 是被判对象之外的噪声（2026-10-07 在 orderflow 上实测踩到）。
	# 用固定串而不是真实唯一值：判据只关心"拦没拦"，不关心资源建没建成；
	# `__I__` 是引擎既有的占位符约定（probe_A113 也用），站点可以在 PROBE_CSRF_BODY 里用它。
	local csrf_body
	csrf_body="$(printf '%s' "${PROBE_CSRF_BODY}" | sed 's/__I__/spec-probe-csrf/g')"
	# ⚠️ PH2-A112-3（2026-10-07 在 orderflow 上实测踩到，报 code=403000）：
	# 把站点预置的 `Origin:` 摘掉 —— 但**不能只摘值**。`-H` 与它的值是数组里相邻的两个元素，
	# 只删掉值会留下一个**没有值的 `-H`**；curl 会把紧随其后的参数当成这个头的值
	# （我们这里正好是 `-d` 与 JSON），于是报
	#   `curl: (3) URL rejected: Port number was not a decimal number between 0 and 65535`
	# 而 `-w '%{http_code}'` 已经把状态码落盘、`req()` 里的 `|| true` 把非零退出吞掉，
	# 残留的 `000` 与上一次的 `403` 粘成 **`403000`**，被探针读成"非 403" ⟹ 假红。
	# 所以按**成对**摘：滤的时候记住"上一个进来的元素是选项"，遇到要摘的头就把那个选项一起退掉。
	# 两个细节：① `${#arr[@]}` 是空数组的正确守卫（`${arr[@]+…}` 在空/未设时不可靠）；
	#           ② `local -a x=()` 之后再 `x+=(…)` 在某些上下文里会把值吃掉，用索引赋值最稳。
	local -a filt=(); local h n=0 last_opt=""
	if [ "${#PROBE_EXTRA_HEADERS[@]}" -gt 0 ]; then
		for h in "${PROBE_EXTRA_HEADERS[@]}"; do
			case "$h" in
				[Oo][Rr][Ii][Gg][Ii][Nn]:*)
					if [ "$last_opt" = "1" ] && [ "${#filt[@]}" -gt 0 ]; then
						unset "filt[$((${#filt[@]} - 1))]"
					fi
					last_opt=""
					continue
					;;
			esac
			case "$h" in -*) last_opt="1" ;; *) last_opt="0" ;; esac
			filt[${#filt[@]}]="$h"
		done
	fi
	local -a extra=()
	n=0
	while [ "$n" -lt "${#filt[@]}" ]; do
		extra[${#extra[@]}]="${filt[$n]}"
		n=$((n + 1))
	done
	# 自检：数组长度必须是偶数（`-H`/`--header` 都成对），奇数说明配对被破坏 ⟹ 宁可不带头。
	if [ $(( ${#extra[@]} % 2 )) -ne 0 ]; then
		extra=()
	fi
	req csrf_a "${URL}${ep}" "${args[@]}" "${extra[@]}" -d "${csrf_body}"
	req csrf_b "${URL}${ep}" "${args[@]}" "${extra[@]}" -H "Origin: ${origin}" -d "${csrf_body}"
	req csrf_c "${URL}${ep}" "${args[@]}" "${extra[@]}" -H "Origin: ${me}" -H "Referer: ${origin}/" -d "${csrf_body}"
	local ca cb cc fa fb fc bad=""
	ca="$(code_of csrf_a)"; cb="$(code_of csrf_b)"; cc="$(code_of csrf_c)"
	fa="$(flat_of csrf_a | head -c 160)"; fb="$(flat_of csrf_b | head -c 160)"; fc="$(flat_of csrf_c | head -c 160)"
	# ⚠️ PH2-A112-4：判据要按"谁先拦"分级，不能只认 403。
	# 本条真判的是"**关掉浏览器自动豁免之后**来源校验还在不在场"：
	#   · 403 + csrf_failed ⟹ 来源校验拦住了（条款原样）；
	#   · 401（或 403/其它非 CSRF 码）⟹ 请求**穿过了 CSRF 这一层**才被鉴权/业务层拦下
	#     ——"这一关"是过了的，但拿不到直接证据，所以记 PASS 并在 detail 里说明；
	#   · 2xx / 业务码（201/409 …）⟹ 请求带着 cookie 一路打到 handler ⟹ 来源校验不在场 ⟹ FAIL。
	local soft=""
	case "$ca" in
		403) printf '%s' "$fa" | grep -q 'csrf_failed' || bad="${bad} 不带 Origin/Referer 回了 403 但 body 里没有 csrf_failed（分不清是 CSRF 拦的还是权限拦的）；" ;;
		401) soft="${soft} (a) 401（鉴权先拦，CSRF 层的直接证据没拿到）；" ;;
		000) bad="${bad} 不带 Origin/Referer 请求根本没发出去（curl 失败）；" ;;
		*) bad="${bad} 不带 Origin/Referer 回 ${ca}（条款要求 403 csrf_failed）；" ;;
	esac
	case "$cb" in
		403) printf '%s' "$fb" | grep -q 'csrf_failed' || bad="${bad} Origin=${origin} 回 403 但 body 不是 csrf_failed（分不清谁拦的）；" ;;
		401) soft="${soft} (b) 401（鉴权先拦）；" ;;
		000) bad="${bad} Origin=${origin} 请求根本没发出去（curl 失败）；" ;;
		*) bad="${bad} Origin=${origin} 回 ${cb}（跨站写请求没被拦）；" ;;
	esac
	# (c) 本站 Origin（Referer 故意写成外站，验"Origin 优先"）→ 不得因 CSRF 被判 403
	if [ "$cc" = "403" ] && printf '%s' "$fc" | grep -q 'csrf_failed'; then
		bad="${bad} 本站 Origin 也被 CSRF 拒了（Origin 优先没生效）；"
	fi
	if [ -n "$bad" ]; then
		record A1.12 FAIL "${bad# }（cookie=${nv%%=*}；a=${ca} b=${cb} c=${cc}；a.body=${fa}；b.body=${fb}；c.body=${fc}）"
	else
		local note=""
		[ "$cc" = "403" ] && note="；⚠️ 本站 Origin 也回 403 但 body 里不是 csrf_failed（需人工看一眼是不是中间件顺序问题）"
		record A1.12 PASS "非安全方法校验来源：缺头 ${ca}、外站 Origin ${cb}、本站 Origin ${cc}（后两问没被 CSRF 拦）${soft}${note}"
	fi
	echo "          ℹ️ (c) 回 ${cc} 只说明「过了 CSRF 这关」（401/400/其它业务码都算过）；SameSite=Lax 仍只是浏览器侧兜底，不等于服务端保证。"
}

# ────────────────────────────────────────────────────────────────────────────
# A1.14 真实 IP 只能取自 nginx 直设的头，且只在来源是回环时才信任转发头
#      本机判得了：**限流键到底听谁** —— 每个请求换 XFF（不带 X-Real-IP）会不会
#                  每次都拿到全新预算；固定 X-Real-IP 连打会不会被计到同一个键
#      判不了：生产 nginx 到底有没有覆写这两个头（判据宾语是 nginx 配置）
# ⚠️ 只声明 PROBE_IP_KEYED_RATELIMIT=1 才判。本探针会**真的把本机来源打满**
#    （诚实代价：A1.13 之后若同一次运行里还有别的登录探针，请单独跑），
#    所以站点用 PROBE_SKIP=A1.14 排除它时要写明理由。
# ────────────────────────────────────────────────────────────────────────────
probe_A114() {
	case "${PROBE_IP_KEYED_RATELIMIT}" in
		1) ;;
		*) record A1.14 SKIP "hooks 未声明 PROBE_IP_KEYED_RATELIMIT=1（本站登录不按 IP 限流，或不详）；判据是限流键，猜一个键会把按账号限流的站判成假红"; return ;;
	esac
	if [ -z "${PROBE_LOGIN_PATH}" ]; then
		record A1.14 SKIP "hooks 没声明 PROBE_LOGIN_PATH（按 IP 限流却没有登录端点？）"
		return
	fi
	local i code hitx=-1 hity=-1 xff
	for i in 1 2 3 4 5 6 7 8; do
		xff="203.0.113.${i}"
		req "ip_x${i}" -X POST "${URL}${PROBE_LOGIN_PATH}" -H 'Content-Type: application/json' \
			"${PROBE_EXTRA_HEADERS[@]}" -H "X-Forwarded-For: ${xff}" \
			-d "$(printf '%s' "${PROBE_LOGIN_BODY}" | sed "s/__I__/x${i}/g")"
		code="$(code_of "ip_x${i}")"
		if [ "$code" = "429" ]; then hitx=$i; break; fi
	done
	for i in 1 2 3 4 5 6 7 8; do
		req "ip_y${i}" -X POST "${URL}${PROBE_LOGIN_PATH}" -H 'Content-Type: application/json' \
			"${PROBE_EXTRA_HEADERS[@]}" -H "X-Real-IP: 203.0.113.7" \
			-d "$(printf '%s' "${PROBE_LOGIN_BODY}" | sed "s/__I__/y${i}/g")"
		code="$(code_of "ip_y${i}")"
		if [ "$code" = "429" ]; then hity=$i; break; fi
	done
	local det="  （键=X-Real-IP-分支第 ${hity} 次触发；键=X-Forwarded-For-分支第 ${hitx} 次触发，-1=没触发）"
	if [ "$hitx" = "-1" ]; then
		record A1.14 FAIL "不带 X-Real-IP、每个请求换一个 X-Forwarded-For(203.0.113.n) 连打 8 次一次限流都没触发 —— 限流键跟着**客户端可伪造的 XFF** 走，按 IP 限流等于没有${det}"
	elif [ "$hity" = "-1" ]; then
		record A1.14 FAIL "固定 X-Real-IP 连打 8 次也没触发限流 —— 限流键不在 X-Real-IP 上（条款顺序①要求优先取它）${det}"
	else
		record A1.14 PASS "限流键跟着 X-Real-IP 走；且只给 XFF（不带 X-Real-IP）时第 ${hitx} 次就被拦了（没退而信 XFF）${det}"
	fi
	echo "          ⚠️ 回环来源本身允许被信任转发头 —— 这条判的是没有 X-Real-IP 时会不会退而信 XFF，"
	echo "             以及限流键最终绑定在哪个头上。生产上 nginx 是否覆写这两个头、RemoteAddr 是否回环，"
	echo "             本机判不了（判据宾语是 nginx 配置），见 server-audit.sh。"
}

# ────────────────────────────────────────────────────────────────────────────
# A3.11 图片处理的坏输入不得 5xx、不得把进程打死
#      本机判得了：无 EXIF 的合法 PNG / 坏 EXIF 段 / 畸形 TIFF 三份下去，
#                  ① 都不是 5xx ② 三次之后服务还活着
#      判不了：图片内容有没有被破坏（EXIF 失败路径必须 return "没有"、不得中断
#              上传）—— 那要在响应里回读照片记录，得站点自证
# ⚠️ 只在 PROBE_IMAGE_UPLOAD=1 的站判（不做图片处理的站，坏字节测的只是存储）。
# ────────────────────────────────────────────────────────────────────────────
# PH2-BADS: 三份坏输入。"字节必须真合法"说的是容器头/魔数，不是内容。
ph2_make_bad_images() { # $1 = WORK 目录
	printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==' | base64 -d > "$1/bad1.png" 2>/dev/null
	printf '\377\330\377\341\000\020Exif\000\000MM\000\052\000\000\000\010\000\001\001\032\000\005\000\000\000\001\000\000\000\000\000\000\000' > "$1/bad2.jpg"
	printf 'II\052\000\377\377\377\177\000\000\000\000\000\000\000\000\000\000' > "$1/bad3.tif"
}

probe_A311() {
	case "${PROBE_IMAGE_UPLOAD}" in
		1) ;;
		*) record A3.11 SKIP "hooks 未声明 PROBE_IMAGE_UPLOAD=1（本站上传不做图片处理/无上传端点）；坏图片字节测不出条款要的东西"; return ;;
	esac
	local ep="${PROBE_UPLOAD_ENDPOINT}" field="${PROBE_UPLOAD_FIELD}"
	[ -n "$ep" ] || { ep="${PROBE_UPLOAD_ENDPOINT2}"; field="${PROBE_UPLOAD_FIELD2}"; }
	if [ -z "$ep" ]; then ep="${PROBE_UPLOAD_ENDPOINT3}"; field="${PROBE_UPLOAD_FIELD3}"; fi
	if [ -z "$ep" ]; then record A3.11 SKIP "hooks 没声明 PROBE_UPLOAD_ENDPOINT（上传端点）"; return; fi
	[ -n "$field" ] || field="file"
	ph2_make_bad_images "${WORK}"
	local names="bad1.png bad2.jpg bad3.tif" nm code bad=""
	for nm in $names; do
		req "a311_${nm}" -X POST "${URL}${ep}" "${PROBE_AUTH_HEADERS[@]}" "${PROBE_EXTRA_HEADERS[@]}" \
			-F "${field}=@${WORK}/${nm};type=application/octet-stream"
		code="$(code_of "a311_${nm}")"
		case "$code" in
			5*|000) bad="${bad} ${nm}→${code} $(flat_of "a311_${nm}" | head -c 120);" ;;
		esac
	done
	# 三次之后进程必须还活着
	local hp="${PROBE_HEALTH_PATH}"
	[ -n "$hp" ] || hp="${PROBE_404_PATH}"
	req a311_up "${URL}${hp}"
	local up; up="$(code_of a311_up)"
	case "$up" in
		000) bad="${bad} 三次坏输入之后 ${hp} 连不上（进程被打死了？）;" ;;
	esac
	if [ -n "$bad" ]; then
		record A3.11 FAIL "坏输入把上传打崩了：${bad# }"
	else
		record A3.11 PASS "三份坏输入都非 5xx（$(code_of a311_bad1.png)/$(code_of a311_bad2.jpg)/$(code_of a311_bad3.tif)），之后 ${hp} 仍回 ${up}"
	fi
	echo "          ℹ️ 判不了的那半：条款还要 EXIF 读失败必须返回「没有」、且不得中断上传 ——"
	echo "             本机只证了不 5xx + 进程不死；图片内容/照片记录有没有被破坏要站点自己回读（见 §6）。"
}

for c in A1.7 A1.5 A1.2 A1.4 A1.1 A1.11 A1.12 A1.9 A1.10 A1.13 A1.14 A3.11 A2.6 C4.5 A2.13; do
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
