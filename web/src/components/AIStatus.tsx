import { useEffect, useState } from 'react'
import { aiApi } from '../api/ai'

// AI 配置状态徽标：未配置（红）/已配置（绿）/探测失败（灰，不阻塞页面）。
// 仅展示状态与模型名，不涉及任何 key 内容。
export function AIStatus() {
  const [state, setState] = useState<'loading' | 'ok' | 'missing' | 'error'>('loading')
  const [model, setModel] = useState('')

  useEffect(() => {
    let cancelled = false
    aiApi
      .status()
      .then((s) => {
        if (cancelled) return
        setModel(s.model)
        setState(s.has_api_key ? 'ok' : 'missing')
      })
      .catch(() => {
        if (!cancelled) setState('error')
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (state === 'loading') return <span className="text-[10px] text-muted dark:text-muted-dark">AI…</span>

  if (state === 'ok') {
    return (
      <span className="text-[10px] text-muted dark:text-muted-dark">
        <span className="text-green-600 dark:text-green-400">●</span> AI 已配置
        {model ? ` · ${model}` : ''}
      </span>
    )
  }

  if (state === 'missing') {
    return <span className="text-[10px] text-amber-600 dark:text-amber-400">● AI 未配置</span>
  }

  return <span className="text-[10px] text-muted dark:text-muted-dark">AI 状态未知</span>
}
