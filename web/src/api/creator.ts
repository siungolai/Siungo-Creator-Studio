// 自媒体模块 API（类型与后端 schema 同步，见 server/internal/creator）。
import { request } from './client'

export type WorkStatus = 'draft' | 'making' | 'published' | 'archived'

export interface Work {
  id: number
  title: string
  topic: string
  style: string
  script?: string
  activeVersionId?: number | null
  status: WorkStatus
  tags: string[]
  createdAt: string
  updatedAt: string
}

export interface Platform {
  id: string
  name: string
  sort: number
  enabled: boolean
}

export interface CreateWorkBody {
  topic: string // 必填
  title?: string // 可空
}

export interface UpdateWorkBody {
  title: string
  topic: string
  style: string
  status: WorkStatus
  tags: string[]
  script: string
}

// 分页响应结构（PRD §4.4：items/total/limit/offset）
export interface WorkPage {
  items: Work[]
  total: number
  limit: number
  offset: number
}

export interface WorkListParams {
  status?: WorkStatus | ''
  q?: string
  limit?: number
  offset?: number
}

// 查询串拼接（零依赖；过滤空值与 undefined）
function qs<T extends object>(params: T): string {
  const entries = Object.entries(params as Record<string, unknown>)
  const parts = entries
    .filter(([, v]) => v !== undefined && v !== '')
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  return parts.length ? '?' + parts.join('&') : ''
}

// 风格模板（CONTEXT.md：default=通用缺省；详情页可显式选择）
export const STYLES = [
  { key: 'default', label: '通用' },
  { key: 'ganhuo', label: '干货型' },
  { key: 'juqing', label: '剧情型' },
  { key: 'zhongcao', label: '种草型' },
  { key: 'tucao', label: '吐槽型' },
] as const

export const STATUSES: { key: WorkStatus; label: string }[] = [
  { key: 'draft', label: '草稿' },
  { key: 'making', label: '制作中' },
  { key: 'published', label: '已发布' },
  { key: 'archived', label: '归档' },
]

export const creatorApi = {
  listWorks: (params?: WorkListParams, signal?: AbortSignal) =>
    request<WorkPage>(`/api/v1/creator/works${qs(params ?? {})}`, { signal }),
  getWork: (id: number, signal?: AbortSignal) =>
    request<Work>(`/api/v1/creator/works/${id}`, { signal }),
  createWork: (body: CreateWorkBody) =>
    request<Work>('/api/v1/creator/works', { method: 'POST', body: JSON.stringify(body) }),
  updateWork: (id: number, body: UpdateWorkBody) =>
    request<Work>(`/api/v1/creator/works/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteWork: (id: number) => request<void>(`/api/v1/creator/works/${id}`, { method: 'DELETE' }),
  listPlatforms: () => request<Platform[]>('/api/v1/creator/platforms'),
}
