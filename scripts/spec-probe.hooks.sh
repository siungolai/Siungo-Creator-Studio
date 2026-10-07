#!/usr/bin/env bash
# ============================================================================
# spec-probe.hooks.sh —— Siungo-Creator-Studio（自媒体 AI 制作工作台）的站点钩子
#
# 判据全在同目录的 spec-probe.sh —— 那是**同源副本**（7 个站逐字相同），母本在
# Siungo-Workspace/scripts/spec-probe.sh。**不要在本仓改那个文件**：会被下一次同步
# 覆盖，而且别的站不会跟着改。本文件不同源，只回答一件事：这一站怎么起、端点长什么样。
#
# 布局：站点根就是仓库根；后端 server/（Go module .../Siungo-Creator-Studio/server），
#       前端 web/。统一检查入口是 scripts/verify.sh（CI 跑的就是它）。
# 后端是**纯 Go**（modernc.org/sqlite），没有 cgo ⟹ 没有 C 工具链的机器上也能起服务。
#
# 用法：
#   bash scripts/spec-probe.sh
# 已经有服务在跑时（比如出问题时人工复核），可以复用，不用再起一个：
#   PROBE_BASE_URL=http://127.0.0.1:18081 PROBE_STUDIO_PASSWORD=... bash scripts/spec-probe.sh
#
# ⚠️ 本站 deployed: false（Siungo-Workspace/scripts/stations.json：「已写代码未部署，
#    违反 🔴 不计违规；C4.x（部署）整章不适用」）⟹ C4.5 判 SKIP，理由见文件末尾。
# ============================================================================

# ── 工具链缓存：与 scripts/verify.sh 用同一套，免得同一个 CI job 里重复下载/编译 ──
export GOCACHE="${GOCACHE:-${ROOT}/.gocache}"
export GOMODCACHE="${GOMODCACHE:-${ROOT}/.gomod}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

# 探针用的回环端口：默认 18081（避开本站开发用的 8081 和 pet 站探针的 18080），
# 被占就在 probe_setup 里往上顺延找空的。
PROBE_PORT=18081
PROBE_TMP="$(mktemp -d)"
_PROBE_PID=""

# 探针自己造的单口令：只活在这个临时实例里，进程一退就没了；
# 存在的理由只是让探针带得上凭据（见 probe_login）。
# config.go:31 只从 STUDIO_PASSWORD 读，main.go:42-44 空值拒绝启动。
PROBE_STUDIO_PASSWORD="${PROBE_STUDIO_PASSWORD:-spec-probe-only-pw-9f3c1a}"

# ── 鉴权：A1.4 / A1.9 打的都是受保护端点，不带凭据只会回 401 被引擎判成 SKIP。
#    本站用 **cookie 会话**不是 Bearer（auth_http.go:61 http.SetCookie，
#    auth.go:21 SessionCookie = "session"），所以要从响应头里把 cookie 抠出来。
probe_login() {
	local hdr="${PROBE_TMP}/login.hdr" body="${PROBE_TMP}/login.json" code token
	code="$(curl -sS -o "${body}" -D "${hdr}" -w '%{http_code}' --max-time 20 \
		-X POST "${PROBE_BASE_URL}/api/v1/login" -H 'Content-Type: application/json' \
		--data "{\"password\":\"${PROBE_STUDIO_PASSWORD}\"}" 2>/dev/null || true)"
	if [ "${code}" != "200" ]; then
		echo "警告：登录失败（HTTP ${code:-?}）⟹ A1.4 / A1.9 会因 401 判 SKIP" >&2
		return 1
	fi
	# Set-Cookie: session=<token>; Path=/; HttpOnly; SameSite=Strict
	# 不用 jq（Git Bash 没有），也不整行抓 —— 只取 session= 到第一个 ';' 之间那段。
	token="$(tr -d '\r' < "${hdr}" | grep -i '^set-cookie:' | head -1 \
		| sed -n 's/.*[[:space:]]session=\([^;]*\).*/\1/p')"
	if [ -z "${token}" ]; then
		echo "警告：登录回 200 但没有 session cookie：$(head -c 200 "${hdr}")" >&2
		return 1
	fi
	PROBE_AUTH_HEADERS=(-H "Cookie: session=${token}")
	return 0
}

# ── 端口占用探测：Git Bash 的 bash 支持 /dev/tcp，连得上就是有人在听。
#    （这台机器上没有 ss / lsof，引擎自己的 C4.5 就因此判不了。）
_probe_port_free() {
	( exec 3<>"/dev/tcp/127.0.0.1/$1" ) 2>/dev/null && return 1
	return 0
}

# ── 起服务：现场构建 server/，临时 SQLite + 回环端口 ─────────────────────────
probe_setup() {
	local bin="${PROBE_TMP}/probe-server"
	case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) bin="${bin}.exe" ;; esac

	if ! ( cd "${ROOT}/server" && go build -o "${bin}" . ) > "${PROBE_TMP}/build.log" 2>&1; then
		echo "错误：server 构建失败 ⟹ 服务起不来，十条款一条都判不了。" >&2
		sed 's/^/      | /' "${PROBE_TMP}/build.log" | head -25 >&2
		return 1
	fi

	# config.go:30 DBPath 默认是相对路径 "data.db"（就是 CWD），db.go:14-34 的 Open()
	# 只在**父目录已存在**时才能建库（sql.Open 是惰性的，报错会出在第一条 PRAGMA 上，
	# 表现为 main.go:48 `db: ...` 直接 Fatal）。所以 DB_PATH 落在 mktemp 目录里，
	# 父目录一定存在；同时不碰仓库里的 data.db。
	# 端口：默认 18081，被占就从 18082 往上找第一个空的。
	# 为什么值得这么麻烦：本机同时跑着好几个站的探针（别的 agent / 别的终端），
	# 8081 是本站的开发端口、18080 是 pet 站的探针端口，撞上就是 probe_setup 失败、
	# 整个探针以 2 退出、一条都判不了。宁可自己找个空位，也不要让人以为"探针坏了"。
	local port="${PROBE_PORT}" try
	if ! _probe_port_free "${port}"; then
		for try in $(seq $((PROBE_PORT + 1)) $((PROBE_PORT + 20))); do
			if _probe_port_free "${try}"; then port="${try}"; break; fi
		done
	fi
	if ! _probe_port_free "${port}"; then
		echo "错误：${PROBE_PORT} 起的 21 个端口全被占了 ⟹ 起不了服务。" >&2
		return 1
	fi

	HOST=127.0.0.1 PORT="${port}" \
	DB_PATH="${PROBE_TMP}/data.db" \
	STUDIO_PASSWORD="${PROBE_STUDIO_PASSWORD}" \
	"${bin}" > "${PROBE_TMP}/server.log" 2>&1 &
	_PROBE_PID=$!

	export PROBE_BASE_URL="http://127.0.0.1:${port}"
	PROBE_LISTEN_PORT="${port}"
	local i
	for i in $(seq 1 60); do
		curl -fsS "${PROBE_BASE_URL}/api/v1/health" >/dev/null 2>&1 && break
		sleep 0.25
	done
	if ! curl -fsS "${PROBE_BASE_URL}/api/v1/health" >/dev/null 2>&1; then
		echo "错误：服务没起来（${PROBE_BASE_URL}/api/v1/health 不通）" >&2
		sed 's/^/      | /' "${PROBE_TMP}/server.log" | tail -20 >&2
		return 1
	fi

	# 本站没有独立数据目录：SQLite 文件就是全部状态（DB_PATH，生产上用环境变量指到部署目录）。
	# 这里指给探针交给它的运行目录 —— mktemp 的 0700 让 A2.13 近乎恒真，
	# 这一条本来就只判得了"属主之外的位"，别把它当强证据（引擎自己也会打这句 ⚠️）。
	PROBE_DATA_DIR="${PROBE_TMP}"
	probe_login || true
	return 0
}

probe_teardown() {
	if [ -n "${_PROBE_PID}" ]; then
		kill "${_PROBE_PID}" 2>/dev/null || true
		wait "${_PROBE_PID}" 2>/dev/null || true
		_PROBE_PID=""
	fi
	if [ -n "${PROBE_TMP}" ] && [ -d "${PROBE_TMP}" ]; then rm -rf "${PROBE_TMP}"; fi
}

# ── 端点映射（每条都对着 server/ 里的实际路由/写出点，行号见注释）────────────

# A1.7 的 404 那一半。**注意路径不在 /api/v1/ 下**：main.go:86 把 web.SPAHandler 挂在
# "/"，于是任何 mux 没匹配上的路径都落到 web.go:28-35 的 SPA 回退，回 **200 + text/html**
# 的 index.html。web.go:11 的注释自己写着「/api/* 不应到达这里（由上层路由先匹配）」——
# 实际会到。这条判的就是这个。
PROBE_404_PATH="/api/__spec-probe-no-such-route__"

# A1.7 的 405 那一半：/api/v1/health 只收 GET（health.go:17-20），非 GET 走
# httpx.WriteError ⟹ 405 + application/json —— 单独暴露 httpx.go:15
# 「WriteJSON 只设 application/json，没有 charset=utf-8」这个全局毛病。
PROBE_405_PATH="/api/v1/health"
PROBE_405_METHOD="POST"

# A1.5 必须打在**应用自己写出**的错误上（框架兜底的 404 是 A1.7 的账）。
# GET /api/v1/creator/works/<不存在的数字 id>：creator_http.go:124-142 →
# service 回 ErrNotFound → :133 httpx.WriteError ⟹ {"error":"work not found"}，
# 没有机器可读的 code（httpx.go:22-25）。要带 cookie，否则中间件先回 401。
# id 取 999999：works 是 AUTOINCREMENT，探针的临时空库上不可能存在。
PROBE_ERROR_PATH="/api/v1/creator/works/999999"

# A1.2 的列表端点。
#   /api/v1/creator/works     → WorkPage{items,total,limit,offset}（creator_service.go:150-156）
#   /api/v1/creator/platforms → **[]Platform 裸数组**（creator_http.go:258 + service.go:235）
# 两个都列出来是照条款字面判：列表就该包一层 {items:[…]}。platforms 只是两张固定字典
# 记录，算不算"列表端点"你可以自己定 —— 定得下就登记豁免，别在 hooks 里悄悄放过。
PROBE_LIST_ENDPOINTS="/api/v1/creator/works /api/v1/creator/platforms"
PROBE_LIST_QUERY=""

# A1.4：POST /api/v1/creator/works（creator_http.go:209-228）→ 201 但**没有 Location**。
# 必填字段只有 topic（creator_service.go:55-58、:86-89）。
# 删除那一半打 /api/v1/creator/works/{id}：creator_http.go:230-248 对 ErrNotFound 回 404。
# ⚠️ 但 {id} 是**整型**路径参数：:232-236 先 ParseInt，非数字直接 400 "invalid id"。
#    所以必须给 PROBE_DELETE_GHOST 一个"语法合法但不存在"的数字，否则永远测不到
#    "删不存在的资源"这一半。（体外验证过：DELETE …/works/999999 → 404 work not found，
#    DELETE …/works/__spec-probe-no-such-id__ → 400 invalid id。）
PROBE_CREATE_ENDPOINT="/api/v1/creator/works"
PROBE_CREATE_BODY='{"topic":"spec probe temp"}'
PROBE_DELETE_TMPL="/api/v1/creator/works/{id}"
PROBE_DELETE_GHOST="999999"

# A1.9：全站最小的应用侧 body 上限是创建作品那条的 8192（creator_http.go:212），
# 取同一个数（不是随手编的阈值）。超限走 :213 httpx.WriteError 400 "invalid request"。
# 体外验证过（20033 字节的**合法 JSON** body → 400，同端点小 body → 201）⟹ 是真违规，
# 不是"探针塞了一串 'a' 被 JSON 语法先拒掉"那种假红。
PROBE_BODY_ENDPOINT="/api/v1/creator/works"
PROBE_BODY_LIMIT=8192

# A1.10：**本站没有上传端点**（全仓 grep 无 multipart / FormFile / ParseMultipartForm），
# 所以刻意不声明 PROBE_UPLOAD_ENDPOINT —— 引擎会判 SKIP 并写明"hooks 没声明"。
# 这不是放水：没有文件上传，就没有"类型不支持回 415"这回事。

# A1.13 的映射。登录只认 {"password":…}（auth_http.go:15-17），body 里 __I__ 换成第几次。
PROBE_LOGIN_PATH="/api/v1/login"
PROBE_LOGIN_BODY='{"password":"spec-probe-wrong-__I__"}'

# C4.5 的端口（本轮在 PROBE_SKIP 里，见下）。
PROBE_LISTEN_PORT="${PROBE_PORT}"

# A2.6：本站没有迁移框架 —— db.go:29 的 Open() 每次启动都跑 migrate()，全是
# CREATE TABLE IF NOT EXISTS + INSERT OR IGNORE 种子数据。所以"空库跑通全部迁移"
# 的自动化验证就是那几个各自新建空库的测试包（每个测试都走 db.Open → migrate）。
# ⚠️ 不要图省事写成 ./internal/db/：那个包没有测试文件，go test 会以
#    "no test files" 退出 0，A2.6 就成了最危险的那种假绿。
PROBE_MIGRATE_CMD="cd server && go test ./internal/creator/ ./internal/auth/ ./internal/settings/ -count=1"

# ── 条款豁免 ───────────────────────────────────────────────────────────────
# PROBE_ALLOW_FAIL 只填**已经在 Siungo-Workspace/docs/standards/conformance-exemptions.json
# 里登记过**的条款（填了只是"翻红不算失败"，不是"不判"）。两处由
# scripts/sync-spec-probe.mjs 对账。
# 下面六条是 2026-10-07 本探针首次真跑翻出来的真违规（已逐条核实不是规则误报），
# 依据精确到 file:line，写在豁免表的 reason 里；当天登记，review_by 2027-01-07。
PROBE_ALLOW_FAIL="A1.7 A1.5 A1.2 A1.4 A1.9 A1.13"

# ── 显式排除 ───────────────────────────────────────────────────────────────
PROBE_SKIP="C4.5"
PROBE_REASON_C45="本站 deployed: false（Siungo-Workspace/scripts/stations.json：creator-studio 与 pet 都标了「已写代码未部署，违反 🔴 不计违规；C4.x（部署）整章不适用」）⟹ 没有生产进程可以判"默认绑在哪"。代码这一侧是对的：server/main.go:91 addr := cfg.Host + \":\" + cfg.Port、server/config.go:28 Host: getenv(\"HOST\", \"127.0.0.1\") 都默认回环。等它真部署了，这一条要改成真判。"

# 已经有人起好服务（PROBE_BASE_URL 预置）时顺手把 cookie 取上；取不到只警告不拦。
if [ -n "${PROBE_BASE_URL}" ]; then
	probe_login || true
fi
