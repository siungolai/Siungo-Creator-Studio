// 页码导航（从 Creator 拆出；G16 页码式：当前页 ±2 窗口，两端省略号）。
interface Props {
  page: number
  totalPages: number
  onChange: (page: number) => void
}

function pageList(current: number, total: number): (number | '…')[] {
  const pages: (number | '…')[] = []
  const start = Math.max(1, current - 2)
  const end = Math.min(total, current + 2)
  if (start > 1) pages.push(1, '…')
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < total) pages.push('…', total)
  return pages
}

export default function Pagination({ page, totalPages, onChange }: Props) {
  if (totalPages <= 1) return null
  return (
    <nav aria-label="分页" className="mt-5 flex items-center justify-center gap-1.5">
      <button
        onClick={() => onChange(page - 1)}
        disabled={page <= 1}
        className="min-h-9 rounded-control border border-line-strong px-2.5 text-xs text-muted disabled:opacity-40 dark:border-line-strong-dark dark:text-muted-dark"
      >
        ‹
      </button>
      {pageList(page, totalPages).map((p, i) =>
        p === '…' ? (
          <span key={`e${i}`} className="px-1 text-xs text-muted dark:text-muted-dark">
            …
          </span>
        ) : (
          <button
            key={p}
            onClick={() => onChange(p)}
            aria-current={p === page ? 'page' : undefined}
            className={`min-h-9 min-w-9 rounded-control px-2 text-xs ${
              p === page
                ? 'bg-accent text-accent-ink dark:bg-accent-dark dark:text-accent-ink-dark'
                : 'border border-line-strong text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark'
            }`}
          >
            {p}
          </button>
        ),
      )}
      <button
        onClick={() => onChange(page + 1)}
        disabled={page >= totalPages}
        className="min-h-9 rounded-control border border-line-strong px-2.5 text-xs text-muted disabled:opacity-40 dark:border-line-strong-dark dark:text-muted-dark"
      >
        ›
      </button>
    </nav>
  )
}
