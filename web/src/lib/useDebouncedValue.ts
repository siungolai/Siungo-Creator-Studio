import { useEffect, useState } from 'react'

// 防抖值（web-frontend 规范模板）：延迟触发，卸载/值变化时清理定时器。
export function useDebouncedValue<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])
  return debounced
}
