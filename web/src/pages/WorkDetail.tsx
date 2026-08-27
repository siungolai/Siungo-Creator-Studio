import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router'
import { ApiError } from '../api/client'
import { creatorApi, STATUSES, STYLES, UpdateWorkBody, Work } from '../api/creator'
import GeneratePanel from '../components/GeneratePanel'
import StatusBadge from '../components/StatusBadge'
import VersionHistory from '../components/VersionHistory'
import { formatTime } from '../lib/format'

// 作品详情/编辑页（T4 基本信息+工作副本；T8 接入 AI 生成面板与版本历史）。
// 发布管理为 T9 范围。
export default function WorkDetail() {
  const { id } = useParams()
  const workId = Number(id)

  const [work, setWork] = useState<Work | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [loadError, setLoadError] = useState('')

  const [title, setTitle] = useState('')
  const [topic, setTopic] = useState('')
  const [style, setStyle] = useState<string>('default')
  const [status, setStatus] = useState<Work['status']>('draft')
  const [tagsInput, setTagsInput] = useState('')
  const [script, setScript] = useState('')

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)

  // 版本历史刷新信号：生成新版本后自增（VersionHistory 以 key 重挂载）
  const [versionTick, setVersionTick] = useState(0)

  // 生成/选用成功后同步工作副本等表单字段（不打断用户编辑：style/topic 保持表单值）
  const applyWork = useCallback((w: Work) => {
    setWork(w)
    setScript(w.script ?? '')
    setTitle(w.title)
    setStatus(w.status)
  }, [])

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true)
      setLoadError('')
      try {
        const w = await creatorApi.getWork(workId, signal)
        setWork(w)
        setTitle(w.title)
        setTopic(w.topic)
        setStyle(w.style)
        setStatus(w.status)
        setTagsInput(w.tags.join('，'))
        setScript(w.script ?? '')
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return // 卸载取消，忽略
        if (err instanceof ApiError && err.status === 404) setNotFound(true)
        else setLoadError(err instanceof Error ? err.message : '加载失败')
      } finally {
        setLoading(false)
      }
    },
    [workId],
  )

  useEffect(() => {
    const ctrl = new AbortController()
    void load(ctrl.signal)
    return () => ctrl.abort() // 副作用清理：卸载或切换作品时取消在途请求
  }, [load])

  async function save(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (saving) return
    setSaving(true)
    setSaveError('')
    setSaved(false)
    // 标签：逗号/中文逗号分隔，trim 后过滤空项
    const tags = tagsInput
      .split(/[,，]/)
      .map((t) => t.trim())
      .filter(Boolean)
    const body: UpdateWorkBody = { title: title.trim(), topic: topic.trim(), style, status, tags, script }
    try {
      const updated = await creatorApi.updateWork(workId, body)
      setWork(updated)
      setSaved(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return <p className="text-xs text-muted dark:text-muted-dark">加载中…</p>
  }

  if (notFound) {
    return (
      <div>
        <p role="alert" className="text-sm text-danger">
          作品不存在或已被删除
        </p>
        <Link to="/creator" className="mt-3 inline-block text-xs underline">
          ← 返回作品列表
        </Link>
      </div>
    )
  }

  if (!work) {
    return (
      <div>
        <p role="alert" className="text-sm text-danger">
          {loadError || '加载失败'}
        </p>
        <button onClick={() => void load()} className="mt-3 text-xs underline">
          重试
        </button>
      </div>
    )
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Link to="/creator" className="text-xs text-muted hover:underline dark:text-muted-dark">
            ← 列表
          </Link>
          <h2 className="text-sm font-medium">作品详情</h2>
          <StatusBadge status={status} />
        </div>
      </div>

      <form onSubmit={save} className="mt-4 space-y-5">
        {/* 基本信息 */}
        <section className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
          <h3 className="text-xs font-medium text-muted dark:text-muted-dark">基本信息</h3>
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <div>
              <label htmlFor="w-title" className="block text-xs text-muted dark:text-muted-dark">
                标题（可空，AI 生成后自动回填）
              </label>
              <input
                id="w-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                maxLength={100}
                className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
              />
            </div>
            <div>
              <label htmlFor="w-topic" className="block text-xs text-muted dark:text-muted-dark">
                主题/要点（必填）
              </label>
              <input
                id="w-topic"
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                maxLength={200}
                className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
              />
            </div>
            <div>
              <label htmlFor="w-style" className="block text-xs text-muted dark:text-muted-dark">
                风格模板
              </label>
              <select
                id="w-style"
                value={style}
                onChange={(e) => setStyle(e.target.value)}
                className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
              >
                {STYLES.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor="w-tags" className="block text-xs text-muted dark:text-muted-dark">
                标签（逗号分隔，≤10 个）
              </label>
              <input
                id="w-tags"
                value={tagsInput}
                onChange={(e) => setTagsInput(e.target.value)}
                placeholder="干货，AI，效率"
                className="mt-1 w-full rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
              />
            </div>
          </div>
          {/* 四态切换（归档仅标记，不锁定操作） */}
          <div className="mt-3">
            <span className="block text-xs text-muted dark:text-muted-dark">状态</span>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {STATUSES.map((s) => (
                <button
                  key={s.key}
                  type="button"
                  onClick={() => setStatus(s.key)}
                  aria-pressed={status === s.key}
                  className={`min-h-9 rounded-control px-3 py-1.5 text-xs ${
                    status === s.key
                      ? 'bg-accent text-accent-ink dark:bg-accent-dark dark:text-accent-ink-dark'
                      : 'border border-line-strong text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark'
                  }`}
                >
                  {s.label}
                </button>
              ))}
            </div>
          </div>
        </section>

        {/* AI 生成面板（T8） */}
        <GeneratePanel
          work={work}
          hasVersions={versionTick > 0}
          onGenerated={applyWork}
          onVersionCreated={() => setVersionTick((t) => t + 1)}
        />

        {/* 工作副本（人工编辑区） */}
        <section className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-medium text-muted dark:text-muted-dark">工作副本（当前脚本）</h3>
            <p className="text-[10px] text-muted dark:text-muted-dark">
              来自被选用版本的内容快照，可人工编辑
            </p>
          </div>
          <textarea
            id="w-script"
            value={script}
            onChange={(e) => setScript(e.target.value)}
            rows={14}
            placeholder="脚本正文（可手动编辑，保存后成为作品当前内容）"
            className="mt-2 w-full rounded-control border border-line-strong bg-card px-3 py-2 font-mono text-sm leading-relaxed text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-page dark:text-ink-dark dark:focus:border-ink-dark"
          />
        </section>

        {/* 版本历史（T8） */}
        <VersionHistory
          key={versionTick}
          workId={workId}
          activeVersionId={work.activeVersionId}
          onActivate={applyWork}
        />

        {saveError && (
          <p role="alert" className="text-xs text-danger">
            {saveError}
          </p>
        )}
        {saved && (
          <p role="status" className="text-xs text-emerald-600 dark:text-emerald-400">
            已保存（{formatTime(new Date().toISOString())}）
          </p>
        )}

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={saving || !topic.trim()}
            className="min-h-11 rounded-control bg-accent px-4 py-2 text-sm text-accent-ink disabled:opacity-50 dark:bg-accent-dark dark:text-accent-ink-dark"
          >
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </form>
    </div>
  )
}
