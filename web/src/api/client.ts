// 统一 fetch 封装：JSON + 错误处理 + 401 未登录跳转 + 默认超时。
// 项目约定：零第三方依赖，仅标准 fetch。
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

interface RequestOptions {
  /** 401 时是否跳转登录页（登录接口自身除外） */
  authRedirect?: boolean
  /** 超时毫秒数（默认 30s；长任务如 AI 生成可覆盖，上限与后端 600s 一致） */
  timeoutMs?: number
}

async function request<T>(path: string, init?: RequestInit, opts: RequestOptions = {}): Promise<T> {
  const timeoutMs = opts.timeoutMs ?? 30_000
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    // 默认 30s 超时；调用方传 signal 时与超时组合（任一触发即中止）
    signal: init?.signal
      ? AbortSignal.any([init.signal, AbortSignal.timeout(timeoutMs)])
      : AbortSignal.timeout(timeoutMs),
    ...init,
  })
  if (res.status === 401 && opts.authRedirect !== false) {
    window.location.href = '/login'
    throw new ApiError(401, '未登录')
  }
  if (res.status === 204) {
    return undefined as T
  }
  if (!res.ok) {
    throw new ApiError(res.status, await errorMessage(res))
  }
  return (await res.json()) as T
}

// 优先取服务端统一错误体 {"error": "..."}，非 JSON 时用状态码兜底
async function errorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (body && typeof body.error === 'string') return body.error
  } catch {
    // 非 JSON 响应，忽略
  }
  return `请求失败（${res.status}）`
}

// 供各领域 api 模块复用（如 api/creator.ts）；业务模块禁止散落裸 fetch。
export { request }

export const api = {
  // me 是登录态探测接口：401 是正常结果（未登录），由调用方渲染登录页，绝不触发整页跳转
  me: () => request<{ authenticated: boolean }>('/api/v1/me', undefined, { authRedirect: false }),
  login: (password: string) =>
    request<{ authenticated: boolean }>(
      '/api/v1/login',
      { method: 'POST', body: JSON.stringify({ password }) },
      { authRedirect: false },
    ),
  // logout 失败也应由调用方本地退出，不跳转
  logout: () =>
    request<{ authenticated: boolean }>('/api/v1/logout', { method: 'POST' }, { authRedirect: false }),
}
