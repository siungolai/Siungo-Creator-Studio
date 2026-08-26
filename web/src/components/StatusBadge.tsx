// 作品状态徽标（列表/详情共用）。
import { WorkStatus } from '../api/creator'

const STATUS_META: Record<WorkStatus, { label: string; className: string }> = {
  draft: { label: '草稿', className: 'bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300' },
  making: { label: '制作中', className: 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300' },
  published: { label: '已发布', className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300' },
  archived: { label: '归档', className: 'bg-neutral-200 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400' },
}

export default function StatusBadge({ status }: { status: WorkStatus }) {
  const meta = STATUS_META[status]
  return <span className={`rounded px-1.5 py-0.5 text-[10px] ${meta.className}`}>{meta.label}</span>
}
