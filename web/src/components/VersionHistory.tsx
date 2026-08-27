import { useCallback, useEffect, useMemo, useState } from 'react'
import { ApiError } from '../api/client'
import { creatorApi, Version, Work } from '../api/creator'
import { formatTime } from '../lib/format'

// 版本历史（T8）：按 通用/抖音/B站 分组，点击展开完整内容，可"选用当前版"。
// 版本不可变（CONTEXT.md）：无编辑/删除入口。
interface Props {
  workId: number
  activeVersionId?: number | null
  onActivate: (work: Work) => void // 选用成功后父组件刷新表单
}

const GROUP_DEFS = [
  { key: '', label: '通用' },
  { key: 'douyin', label: '抖音' },
  { key: 'bilibili', label: 'B站' },
]

// 版本内容展示（五个产出块）
function VersionContent({ content }: { content: Version['content'] }) {
  return (
    <div className="mt-2 space-y-2 border-t border-line pt-2 text-xs dark:border-line-dark">
      <div>
        <p className="text-muted dark:text-muted-dark">标题候选</p>
        <ul className="mt-0.5 list-inside list-disc text-ink dark:text-ink-dark">
          {content.titles.map((t, i) => (
            <li key={i}>{t}</li>
          ))}
        </ul>
      </div>
      <div>
        <p className="text-muted dark:text-muted-dark">脚本正文</p>
        <p className="mt-0.5 whitespace-pre-wrap text-ink dark:text-ink-dark">{content.script}</p>
      </div>
      {content.voiceover && (
        <div>
          <p className="text-muted dark:text-muted-dark">口播稿</p>
          <p className="mt-0.5 whitespace-pre-wrap text-ink dark:text-ink-dark">{content.voiceover}</p>
        </div>
      )}
      {content.tags.length > 0 && (
        <div>
          <p className="text-muted dark:text-muted-dark">标签建议</p>
          <p className="mt-0.5 text-ink dark:text-ink-dark">{content.tags.join('、')}</p>
        </div>
      )}
      {content.cover_copy && (
        <div>
          <p className="text-muted dark:text-muted-dark">封面文案</p>
          <p className="mt-0.5 text-ink dark:text-ink-dark">{content.cover_copy}</p>
        </div>
      )}
    </div>
  )
}

export default function VersionHistory({ workId, activeVersionId, onActivate }: Props) {
  const [versions, setVersions] = useState<Version[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<number | null>(null)
  const [activating, setActivating] = useState(false)

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true)
      setError('')
      try {
        setVersions(await creatorApi.listVersions(workId, signal))
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : '加载失败')
      } finally {
        setLoading(false)
      }
    },
    [workId],
  )

  useEffect(() => {
    const ctrl = new AbortController()
    void load(ctrl.signal)
    return () => ctrl.abort()
  }, [load])

  // 按平台分组（组内保持倒序）
  const groups = useMemo(() => {
    return GROUP_DEFS.map((g) => ({
      ...g,
      items: versions.filter((v) => v.platform === g.key),
    })).filter((g) => g.items.length > 0)
  }, [versions])

  async function activate(v: Version) {
    if (activating) return
    setActivating(true)
    setError('')
    try {
      const work = await creatorApi.activateVersion(workId, v.id)
      onActivate(work)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '选用失败')
    } finally {
      setActivating(false)
    }
  }

  return (
    <section className="rounded-card border border-line bg-card p-4 dark:border-line-dark dark:bg-card-dark">
      <h3 className="text-xs font-medium text-muted dark:text-muted-dark">版本历史（不可修改）</h3>

      {error && (
        <p role="alert" className="mt-2 text-xs text-danger">
          {error}
        </p>
      )}
      {loading ? (
        <p className="mt-3 text-xs text-muted dark:text-muted-dark">加载中…</p>
      ) : versions.length === 0 ? (
        <p className="mt-3 text-xs text-muted dark:text-muted-dark">
          暂无版本——使用上方「生成」创建第一个版本
        </p>
      ) : (
        <div className="mt-3 space-y-4">
          {groups.map((g) => (
            <div key={g.key || 'general'}>
              <p className="text-[10px] text-muted dark:text-muted-dark">{g.label}</p>
              <ul className="mt-1.5 space-y-1.5">
                {g.items.map((v) => {
                  const isActive = v.id === activeVersionId
                  const isOpen = expanded === v.id
                  return (
                    <li
                      key={v.id}
                      className={`rounded-control border px-3 py-2 ${
                        isActive
                          ? 'border-accent/60 bg-hover dark:border-accent-dark/60 dark:bg-hover-dark'
                          : 'border-line dark:border-line-dark'
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <button
                          type="button"
                          onClick={() => setExpanded(isOpen ? null : v.id)}
                          aria-expanded={isOpen}
                          className="min-h-9 text-left text-xs text-ink hover:underline dark:text-ink-dark"
                        >
                          {formatTime(v.createdAt)} · {v.model || '未知模型'}
                        </button>
                        <div className="flex shrink-0 items-center gap-2">
                          {isActive && (
                            <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                              当前
                            </span>
                          )}
                          {!isActive && (
                            <button
                              type="button"
                              onClick={() => void activate(v)}
                              disabled={activating}
                              className="min-h-9 rounded-control border border-line-strong px-2.5 text-xs text-muted hover:bg-hover disabled:opacity-50 dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark"
                            >
                              {activating ? '选用中…' : '选用当前版'}
                            </button>
                          )}
                        </div>
                      </div>
                      {isOpen && <VersionContent content={v.content} />}
                    </li>
                  )
                })}
              </ul>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
