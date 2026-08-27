import { useEffect, useState } from 'react'
import { ApiError } from '../api/client'
import { aiApi } from '../api/ai'

// AI 设置面板：DeepSeek API Key 手动输入（存后端内存，重启/清除即失效）。
// 集成在 Creator 页顶部折叠区；父组件控制展开状态，本组件只负责状态拉取与写入。
interface Props {
  onChanged?: () => void // 配置/清除成功后通知父组件（如刷新状态徽标）
}

export function APIKeyConfig({ onChanged }: Props) {
  const [apiKey, setApiKey] = useState('')
  const [configured, setConfigured] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let cancelled = false
    aiApi
      .status()
      .then((s) => {
        if (!cancelled) setConfigured(s.has_api_key)
      })
      .catch(() => {
        if (!cancelled) setError('无法获取 AI 状态')
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function save(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const key = apiKey.trim()
    if (!key || busy) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const res = await aiApi.configure(key)
      setNotice(res.message)
      setConfigured(true)
      setApiKey('')
      onChanged?.()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function clear() {
    if (busy) return
    if (!window.confirm('清除 API Key？本次运行内 AI 功能将不可用。')) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const res = await aiApi.configure('')
      setNotice(res.message)
      setConfigured(false)
      onChanged?.()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '清除失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
      <h3 className="text-sm font-medium">AI 设置</h3>
      <p className="mt-1 text-[10px] text-muted dark:text-muted-dark">
        DeepSeek API Key 仅保存在服务器内存中，重启服务或清除后需重新输入；不会写入数据库或日志。
      </p>

      {configured ? (
        <div className="mt-3 flex items-center justify-between gap-3">
          <span className="text-xs text-muted dark:text-muted-dark">已配置，AI 生成功能可用</span>
          <button
            onClick={() => void clear()}
            disabled={busy}
            className="min-h-9 rounded-control border border-danger/40 px-3 py-1.5 text-xs text-danger hover:bg-hover disabled:opacity-50 dark:hover:bg-hover-dark"
          >
            {busy ? '处理中…' : '清除 API Key'}
          </button>
        </div>
      ) : (
        <form onSubmit={save} className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-start">
          <label className="sr-only" htmlFor="ai-api-key">
            DeepSeek API Key
          </label>
          <input
            id="ai-api-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="sk-…"
            autoComplete="off"
            spellCheck={false}
            maxLength={200}
            className="min-w-0 flex-1 rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
          />
          <button
            type="submit"
            disabled={busy || !apiKey.trim()}
            className="min-h-10 rounded-control bg-accent px-3 py-1.5 text-xs text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
          >
            {busy ? '保存中…' : '保存'}
          </button>
        </form>
      )}

      {error && (
        <p role="alert" className="mt-2 text-xs text-danger">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="mt-2 text-xs text-muted dark:text-muted-dark">
          {notice}
        </p>
      )}
    </div>
  )
}
