// AI 配置领域 API（key 手动输入功能，M-2026-08）。
// 统一走 client.ts 的 request：JSON + 错误归一 + 超时；业务模块禁止散落裸 fetch。
import { request } from './client'

export interface AIStatus {
  has_api_key: boolean
  model: string
}

export const aiApi = {
  // 当前 AI 配置状态（是否已配置 key、模型名）；401 由调用方处理，不触发整页跳转
  status: () => request<AIStatus>('/api/v1/ai/status', undefined, { authRedirect: false }),

  // 写入（api_key 非空）/清除（api_key 为空字符串）内存中的 API Key
  configure: (apiKey: string) =>
    request<{ message: string }>(
      '/api/v1/ai/configure',
      { method: 'POST', body: JSON.stringify({ api_key: apiKey }) },
      { authRedirect: false },
    ),
}
