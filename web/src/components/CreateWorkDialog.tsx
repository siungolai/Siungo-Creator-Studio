import { useState } from 'react'
import { ApiError } from '../api/client'
import { creatorApi } from '../api/creator'

// 新建作品弹层（从 Creator 拆出，单一职责：创建交互与校验反馈）。
interface Props {
  onClose: () => void
  onCreated: () => void // 创建成功后由父组件刷新列表
}

export default function CreateWorkDialog({ onClose, onCreated }: Props) {
  const [topic, setTopic] = useState('')
  const [title, setTitle] = useState('')
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!topic.trim() || creating) return
    setCreating(true)
    setError('')
    try {
      await creatorApi.createWork({ topic: topic.trim(), title: title.trim() || undefined })
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4" onClick={onClose}>
      <form
        onSubmit={submit}
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-sm rounded-card border border-line bg-card p-6 shadow-xl dark:border-line-dark dark:bg-card-dark"
      >
        <h3 className="text-sm font-medium">新建作品</h3>
        <label htmlFor="create-topic" className="mt-4 block text-xs text-muted dark:text-muted-dark">
          主题/要点（必填）
        </label>
        <input
          id="create-topic"
          value={topic}
          onChange={(e) => setTopic(e.target.value)}
          placeholder="例如：AI 短视频制作技巧"
          maxLength={200}
          autoFocus
          className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
        />
        <label htmlFor="create-title" className="mt-3 block text-xs text-muted dark:text-muted-dark">
          标题（可选，AI 生成后自动回填）
        </label>
        <input
          id="create-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="可留空"
          maxLength={100}
          className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
        />
        {error && (
          <p id="create-error" role="alert" className="mt-2 text-xs text-danger">
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="min-h-10 rounded-control border border-line-strong px-3 py-1.5 text-xs text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={creating || !topic.trim()}
            className="min-h-10 rounded-control bg-accent px-3 py-1.5 text-xs text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
          >
            {creating ? '创建中…' : '创建'}
          </button>
        </div>
      </form>
    </div>
  )
}
