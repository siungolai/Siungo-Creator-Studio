import { useState } from 'react'
import { ApiError } from '../api/client'
import { creatorApi, GENERATE_STYLES, PLATFORMS, Work } from '../api/creator'

// AI 生成面板（T8）：主题 + 风格 + 平台 → 生成新版本。
// 生成中禁用 + 进度提示；失败明确错误 + 一键重试；成功后支持"再生成一版"。
interface Props {
  work: Work
  hasVersions: boolean
  onGenerated: (work: Work) => void // 生成成功（后端已激活新版本），父组件刷新表单
  onVersionCreated: () => void // 通知父组件版本历史需刷新
}

export default function GeneratePanel({ work, hasVersions, onGenerated, onVersionCreated }: Props) {
  // G9：下拉默认选干货型；作品已有四型风格则沿用
  const [platform, setPlatform] = useState('')
  const [style, setStyle] = useState(() => (work.style !== 'default' ? work.style : 'ganhuo'))
  const [topic, setTopic] = useState(work.topic)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState(false)

  async function generate() {
    if (!topic.trim() || generating) return
    setGenerating(true)
    setError('')
    setSuccess(false)
    try {
      const res = await creatorApi.generateVersion(work.id, {
        platform,
        style,
        topic: topic.trim(),
      })
      setSuccess(true)
      onVersionCreated()
      onGenerated(res.work)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '生成失败，请重试')
    } finally {
      setGenerating(false)
    }
  }

  return (
    <section className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
      <h3 className="text-xs font-medium text-muted dark:text-muted-dark">AI 生成</h3>
      <div className="mt-3 grid gap-3 sm:grid-cols-3">
        <div className="sm:col-span-1">
          <label htmlFor="gen-topic" className="block text-xs text-muted dark:text-muted-dark">
            主题/要点（必填）
          </label>
          <input
            id="gen-topic"
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            maxLength={200}
            placeholder="例如：AI 短视频制作技巧"
            className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
          />
        </div>
        <div>
          <label htmlFor="gen-style" className="block text-xs text-muted dark:text-muted-dark">
            风格模板
          </label>
          <select
            id="gen-style"
            value={style}
            onChange={(e) => setStyle(e.target.value)}
            className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
          >
            {GENERATE_STYLES.map((s) => (
              <option key={s.key} value={s.key}>
                {s.label}
              </option>
            ))}
          </select>
        </div>
        <div>
          <span className="block text-xs text-muted dark:text-muted-dark">目标平台</span>
          <div className="mt-1 flex gap-1.5">
            {PLATFORMS.map((p) => (
              <button
                key={p.key || 'general'}
                type="button"
                onClick={() => setPlatform(p.key)}
                aria-pressed={platform === p.key}
                className={`min-h-9 flex-1 rounded-control px-2 text-xs ${
                  platform === p.key
                    ? 'bg-accent text-accent-ink dark:bg-accent-dark dark:text-accent-ink-dark'
                    : 'border border-line-strong text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark'
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={() => void generate()}
          disabled={generating || !topic.trim()}
          className="min-h-11 rounded-control bg-accent px-4 py-2 text-sm text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
        >
          {generating ? 'AI 生成中…（约 1–2 分钟）' : hasVersions ? '再生成一版' : '生成'}
        </button>
        {generating && (
          <span role="status" className="text-xs text-muted dark:text-muted-dark">
            生成期间按钮已锁定，请稍候
          </span>
        )}
        {success && !generating && (
          <span role="status" className="text-xs text-emerald-600 dark:text-emerald-400">
            已生成新版本并设为当前
          </span>
        )}
        {error && (
          <span role="alert" className="text-xs text-danger">
            {error}
          </span>
        )}
      </div>
    </section>
  )
}
