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

// 生成面板风格选项（G9：default 不出现在下拉，前端默认干货型）
export const GENERATE_STYLES = STYLES.filter((s) => s.key !== 'default')

// 平台维度（'' 通用 | douyin | bilibili）
export const PLATFORMS: { key: string; label: string }[] = [
  { key: '', label: '通用' },
  { key: 'douyin', label: '抖音' },
  { key: 'bilibili', label: 'B站' },
]

// AI 生成输出契约（与后端 ai.GenerateOutput 同步）
export interface GenerateOutput {
  titles: string[]
  script: string
  voiceover: string
  tags: string[]
  cover_copy: string
}

// 作品版本（与后端 creator.Version 同步）
export interface Version {
  id: number
  workId: number
  platform: string
  content: GenerateOutput
  model: string
  createdAt: string
}

export interface GenerateVersionBody {
  platform?: string
  topic?: string
  style?: string
}

export interface GenerateResult {
  version: Version
  work: Work
}

// 模块设置（与后端 settings.settingsResponse 同步；提示词空串 = 未设置，用内置默认）
export interface CreatorSettings {
  aiTimeoutSeconds: number
  generateSystemPrompt: string
  generateUserPromptTemplate: string
}

// 部分更新：只传要改的字段；提示词传空串 = 恢复默认
export interface UpdateCreatorSettingsBody {
  aiTimeoutSeconds?: number
  generateSystemPrompt?: string
  generateUserPromptTemplate?: string
}

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
  // 生成与版本（T7 后端已就绪）：AI 生成耗时可达数分钟，超时对齐后端上限 600s
  generateVersion: (workId: number, body: GenerateVersionBody) =>
    request<GenerateResult>(
      `/api/v1/creator/works/${workId}/versions`,
      { method: 'POST', body: JSON.stringify(body) },
      { timeoutMs: 600_000 },
    ),
  listVersions: (workId: number, signal?: AbortSignal) =>
    request<Version[]>(`/api/v1/creator/works/${workId}/versions`, { signal }),
  activateVersion: (workId: number, versionId: number) =>
    request<Work>(`/api/v1/creator/works/${workId}/versions/${versionId}/activate`, { method: 'PUT' }),
  // 模块设置（Issue 18：提示词可自定义）
  getSettings: () => request<CreatorSettings>('/api/v1/creator/settings'),
  updateSettings: (body: UpdateCreatorSettingsBody) =>
    request<CreatorSettings>('/api/v1/creator/settings', { method: 'PUT', body: JSON.stringify(body) }),
}
