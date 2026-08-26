import { useState } from 'react'
import { api, ApiError } from './api/client'

// 登录页（T2）：单密码登录，错误/冷却提示；语义 token 类名（见 index.css @theme）。
export default function Login({ onSuccess }: { onSuccess: () => void }) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!password || busy) return
    setBusy(true)
    setError('')
    try {
      await api.login(password)
      onSuccess()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) setError('密码错误')
      else if (err instanceof ApiError && err.status === 429) setError('尝试过于频繁，请稍后再试')
      else setError(err instanceof Error ? err.message : '登录失败，请重试')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-page px-4 dark:bg-page-dark">
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-card border border-line bg-card p-6 shadow-xl dark:border-line-dark dark:bg-card-dark"
      >
        <h1 className="text-base font-medium text-ink dark:text-ink-dark">Siungo Creator Studio</h1>
        <p className="mt-1 text-xs text-muted dark:text-muted-dark">请输入访问密码</p>
        <label htmlFor="password" className="sr-only">
          访问密码
        </label>
        <input
          id="password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="密码"
          autoFocus
          aria-describedby={error ? 'login-error' : undefined}
          aria-invalid={error ? true : undefined}
          className="mt-4 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
        />
        {error && (
          <p id="login-error" role="alert" className="mt-2 text-xs text-danger">
            {error}
          </p>
        )}
        <button
          type="submit"
          disabled={busy || !password}
          className="mt-4 min-h-11 w-full rounded-control bg-accent px-3 py-2 text-sm text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
        >
          {busy ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  )
}
