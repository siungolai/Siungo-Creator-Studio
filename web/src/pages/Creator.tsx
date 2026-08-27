import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { creatorApi, STATUSES, Work, WorkStatus } from '../api/creator'
import CreateWorkDialog from '../components/CreateWorkDialog'
import Pagination from '../components/Pagination'
import StatusBadge from '../components/StatusBadge'
import { APIKeyConfig } from '../components/APIKeyConfig'
import { AIStatus } from '../components/AIStatus'
import { PromptConfig } from '../components/PromptConfig'
import { formatTime } from '../lib/format'
import { useDebouncedValue } from '../lib/useDebouncedValue'

// 作品列表页（T3 基础 + T5：状态筛选/关键词搜索/页码分页）。
// 筛选状态与页码同步 URL（?status=&q=&page=），刷新保持；搜索防抖 300ms。
const PAGE_SIZE = 20

const STATUS_TABS: { key: WorkStatus | ''; label: string }[] = [
  { key: '', label: '全部' },
  ...STATUSES.map((s) => ({ key: s.key as WorkStatus | '', label: s.label })),
]

export default function Creator() {
  const nav = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // URL 即状态源（刷新保持）
  const status = (searchParams.get('status') ?? '') as WorkStatus | ''
  const page = Math.max(1, Number(searchParams.get('page') ?? '1') || 1)
  const qInput = searchParams.get('q') ?? ''
  const q = useDebouncedValue(qInput, 300)

  const [pageData, setPageData] = useState<Work[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [showAISettings, setShowAISettings] = useState(false)
  // AI 配置变更计数：配置/清除成功后 +1，通过 key 强制 AIStatus 重挂载刷新徽标
  const [aiRefresh, setAiRefresh] = useState(0)

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // URL 参数更新（函数式 setSearchParams，引用稳定）
  const updateParams = useCallback(
    (patch: { status?: string; q?: string; page?: number }) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        if (patch.status !== undefined) {
          if (patch.status) next.set('status', patch.status)
          else next.delete('status')
          next.delete('page') // 切换筛选回到第 1 页
        }
        if (patch.q !== undefined) {
          if (patch.q) next.set('q', patch.q)
          else next.delete('q')
          next.delete('page')
        }
        if (patch.page !== undefined) {
          if (patch.page <= 1) next.delete('page')
          else next.set('page', String(patch.page))
        }
        return next
      })
    },
    [setSearchParams],
  )

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true)
      setError('')
      try {
        const res = await creatorApi.listWorks(
          {
            status: status || undefined,
            q: q || undefined,
            limit: PAGE_SIZE,
            offset: (page - 1) * PAGE_SIZE,
          },
          signal,
        )
        setPageData(res.items)
        setTotal(res.total)
        // 分页越界钳制（P1）：URL 页码超出总页数且存在数据 → 回退到最后一页
        const pages = Math.max(1, Math.ceil(res.total / PAGE_SIZE))
        if (res.items.length === 0 && res.total > 0 && page > 1) {
          updateParams({ page: pages })
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return // 卸载/参数变化取消，忽略
        setError(err instanceof Error ? err.message : '加载失败')
      } finally {
        setLoading(false)
      }
    },
    [status, q, page, updateParams],
  )

  useEffect(() => {
    const ctrl = new AbortController()
    void load(ctrl.signal)
    return () => ctrl.abort() // 副作用清理：卸载或参数变化时取消在途请求
  }, [load])

  function goPage(p: number) {
    const clamped = Math.min(Math.max(1, p), totalPages)
    if (clamped === page) return
    updateParams({ page: clamped })
  }

  async function removeWork(e: React.MouseEvent, w: Work) {
    e.stopPropagation()
    // 删除二次确认（验收项）
    if (!window.confirm(`删除作品「${w.title || w.topic}」？其版本与发布记录将一并删除。`)) return
    try {
      await creatorApi.deleteWork(w.id)
      void load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  const hasFilter = status !== '' || q !== ''
  const empty = !loading && pageData.length === 0

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-medium">作品列表</h2>
        <div className="flex items-center gap-3">
          <AIStatus key={aiRefresh} />
          <button
            onClick={() => setShowAISettings((v) => !v)}
            aria-expanded={showAISettings}
            className="min-h-9 rounded-control border border-line-strong px-3 py-1.5 text-xs text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark"
          >
            AI 设置
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="min-h-10 rounded-control bg-accent px-3 py-1.5 text-xs text-accent-ink dark:bg-accent-dark dark:text-accent-ink-dark"
          >
            + 新建作品
          </button>
        </div>
      </div>

      {/* AI 设置折叠区：key 手动输入（后端内存，重启失效）+ 提示词自定义 */}
      {showAISettings && (
        <div className="space-y-3">
          <APIKeyConfig onChanged={() => setAiRefresh((v) => v + 1)} />
          <PromptConfig />
        </div>
      )}

      {/* 筛选区：状态 tab + 关键词搜索 */}
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <div role="tablist" aria-label="状态筛选" className="flex flex-wrap gap-1.5">
          {STATUS_TABS.map((t) => (
            <button
              key={t.key || 'all'}
              role="tab"
              aria-selected={status === t.key}
              onClick={() => updateParams({ status: t.key })}
              className={`min-h-9 rounded-control px-3 py-1.5 text-xs ${
                status === t.key
                  ? 'bg-accent text-accent-ink dark:bg-accent-dark dark:text-accent-ink-dark'
                  : 'border border-line-strong text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        <label className="relative block">
          <span className="sr-only">搜索作品（标题/主题）</span>
          <input
            value={qInput}
            onChange={(e) => updateParams({ q: e.target.value })}
            placeholder="搜索标题/主题…"
            className="w-52 rounded-control border border-line-strong bg-card px-3 py-2 text-sm text-ink outline-none focus:border-ink dark:border-line-strong-dark dark:bg-card dark:text-ink-dark dark:focus:border-ink-dark"
          />
        </label>
      </div>

      {error && (
        <p role="alert" className="mt-3 text-xs text-danger">
          {error}
          <button onClick={() => void load()} className="ml-2 underline">
            重试
          </button>
        </p>
      )}

      <p className="mt-3 text-[10px] text-muted dark:text-muted-dark">共 {total} 条</p>

      {loading ? (
        <p className="mt-6 text-xs text-muted dark:text-muted-dark">加载中…</p>
      ) : empty ? (
        <div className="mt-8 rounded-card border border-dashed border-line p-8 text-center dark:border-line-dark">
          <p className="text-sm text-muted dark:text-muted-dark">
            {hasFilter ? '没有匹配的作品' : '还没有作品'}
          </p>
          <p className="mt-1 text-xs text-muted dark:text-muted-dark">
            {hasFilter ? '换个关键词或筛选条件试试' : '点击「新建作品」从主题开始，或等待 AI 选题功能（M4）'}
          </p>
        </div>
      ) : (
        <>
          <ul className="mt-4 space-y-2">
            {pageData.map((w) => (
              <li
                key={w.id}
                onClick={() => nav(`/creator/${w.id}`)}
                className="flex cursor-pointer items-center justify-between rounded-card border border-line bg-card px-4 py-3 hover:bg-hover dark:border-line-dark dark:bg-card-dark dark:hover:bg-hover-dark"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{w.title || w.topic}</p>
                  <p className="mt-0.5 truncate text-xs text-muted dark:text-muted-dark">
                    {w.title ? w.topic : '主题：' + w.topic}
                  </p>
                  <p className="mt-1 text-[10px] text-muted dark:text-muted-dark">
                    更新于 {formatTime(w.updatedAt)}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge status={w.status} />
                  <button
                    onClick={(e) => void removeWork(e, w)}
                    className="rounded-control px-2 py-1 text-xs text-danger hover:bg-hover dark:hover:bg-hover-dark"
                  >
                    删除
                  </button>
                </div>
              </li>
            ))}
          </ul>

          <Pagination page={page} totalPages={totalPages} onChange={goPage} />
        </>
      )}

      {showCreate && (
        <CreateWorkDialog
          onClose={() => setShowCreate(false)}
          onCreated={() => {
            setShowCreate(false)
            void load()
          }}
        />
      )}
    </div>
  )
}
