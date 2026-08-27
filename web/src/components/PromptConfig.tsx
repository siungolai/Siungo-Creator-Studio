import { useEffect, useState } from 'react'
import { ApiError } from '../api/client'
import { creatorApi } from '../api/creator'

// 提示词自定义面板（Issue 18）：调整生成所用的系统提示词与用户提示模板。
// 空输入 = 未设置 → 使用内置默认；保存空串 = 恢复默认（后端 DELETE key 语义）。
// 用户提示模板支持占位符 {topic}/{style}/{platform}，生成时替换为实际值。

export function PromptConfig() {
  const [sysPrompt, setSysPrompt] = useState('')
  const [userTpl, setUserTpl] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let cancelled = false
    creatorApi
      .getSettings()
      .then((s) => {
        if (cancelled) return
        setSysPrompt(s.generateSystemPrompt)
        setUserTpl(s.generateUserPromptTemplate)
        setLoaded(true)
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiError ? err.message : '无法加载提示词设置')
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function save(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const res = await creatorApi.updateSettings({
        generateSystemPrompt: sysPrompt.trim() ? sysPrompt : '',
        generateUserPromptTemplate: userTpl.trim() ? userTpl : '',
      })
      setSysPrompt(res.generateSystemPrompt)
      setUserTpl(res.generateUserPromptTemplate)
      setNotice('提示词已保存，下次生成生效')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function reset() {
    if (busy) return
    if (!window.confirm('恢复默认提示词？当前自定义内容将被清除。')) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const res = await creatorApi.updateSettings({ generateSystemPrompt: '', generateUserPromptTemplate: '' })
      setSysPrompt(res.generateSystemPrompt)
      setUserTpl(res.generateUserPromptTemplate)
      setNotice('已恢复默认提示词')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '恢复失败')
    } finally {
      setBusy(false)
    }
  }

  const inputCls =
    'min-w-0 flex-1 rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark'

  return (
    <div className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">生成提示词</h3>
        {loaded && (
          <button
            onClick={() => void reset()}
            disabled={busy}
            className="min-h-9 rounded-control border border-line-strong px-3 py-1.5 text-xs text-muted hover:bg-hover disabled:opacity-50 dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark"
          >
            恢复默认
          </button>
        )}
      </div>
      <p className="mt-1 text-[10px] text-muted dark:text-muted-dark">
        留空 = 使用内置默认。系统提示词只影响内容与风格，输出格式（JSON 结构）由系统固定；
        用户提示模板支持占位符{' '}
        <code className="rounded bg-hover px-1 dark:bg-hover-dark">{'{topic}'}</code>{' '}
        <code className="rounded bg-hover px-1 dark:bg-hover-dark">{'{style}'}</code>{' '}
        <code className="rounded bg-hover px-1 dark:bg-hover-dark">{'{platform}'}</code>
        ，生成时替换为实际值。
      </p>

      <form onSubmit={save} className="mt-3 space-y-3">
        <div>
          <label htmlFor="prompt-sys" className="block text-xs text-muted dark:text-muted-dark">
            系统提示词（生成主模板）
          </label>
          <textarea
            id="prompt-sys"
            value={sysPrompt}
            onChange={(e) => setSysPrompt(e.target.value)}
            rows={6}
            spellCheck={false}
            placeholder="留空使用内置默认模板"
            className={`${inputCls} mt-1 w-full resize-y font-mono text-xs leading-relaxed`}
          />
        </div>
        <div>
          <label htmlFor="prompt-user-tpl" className="block text-xs text-muted dark:text-muted-dark">
            用户提示模板
          </label>
          <input
            id="prompt-user-tpl"
            type="text"
            value={userTpl}
            onChange={(e) => setUserTpl(e.target.value)}
            placeholder="留空使用内置默认：主题：{topic}"
            spellCheck={false}
            maxLength={5000}
            className={`${inputCls} mt-1 w-full`}
          />
        </div>

        {error && (
          <p role="alert" className="text-xs text-danger">
            {error}
          </p>
        )}
        {notice && (
          <p role="status" className="text-xs text-muted dark:text-muted-dark">
            {notice}
          </p>
        )}

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={busy}
            className="min-h-10 rounded-control bg-accent px-3 py-1.5 text-xs text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
          >
            {busy ? '保存中…' : '保存提示词'}
          </button>
        </div>
      </form>
    </div>
  )
}
