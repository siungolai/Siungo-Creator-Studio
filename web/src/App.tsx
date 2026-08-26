import { lazy, Suspense, useEffect, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router'
import { api } from './api/client'
import Login from './Login'

// 路由级懒加载（web-frontend 规范：代码分割；各页面沿用此模式）
const Creator = lazy(() => import('./pages/Creator'))
const WorkDetail = lazy(() => import('./pages/WorkDetail'))

// 应用壳：认证守卫（T2）。
// 未登录 → 登录页；已登录 → 顶栏（登出）+ 路由区（/ 重定向 /creator）。
type AuthState = 'loading' | 'out' | 'in'

export default function App() {
  const [auth, setAuth] = useState<AuthState>('loading')

  useEffect(() => {
    api
      .me()
      .then(() => setAuth('in'))
      .catch(() => setAuth('out'))
  }, [])

  if (auth === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-page text-xs text-muted dark:bg-page-dark dark:text-muted-dark">
        加载中…
      </div>
    )
  }

  if (auth === 'out') {
    // 未登录：任意页面引导至登录（登录成功后原地进入）
    return <Login onSuccess={() => setAuth('in')} />
  }

  return (
    <div className="min-h-screen bg-page text-ink dark:bg-page-dark dark:text-ink-dark">
      <header className="border-b border-line dark:border-line-dark">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-4">
          <h1 className="text-base font-medium">Siungo Creator Studio — 自媒体 AI 制作工作台</h1>
          <button
            onClick={() => {
              void api.logout().finally(() => setAuth('out'))
            }}
            className="min-h-10 rounded-control border border-line-strong px-3 py-1.5 text-xs text-muted hover:bg-hover dark:border-line-strong-dark dark:text-muted-dark dark:hover:bg-hover-dark"
          >
            登出
          </button>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-8">
        <Suspense
          fallback={
            <p className="text-xs text-muted dark:text-muted-dark">页面加载中…</p>
          }
        >
          <Routes>
            <Route path="/" element={<Navigate to="/creator" replace />} />
            <Route path="/creator" element={<Creator />} />
            <Route path="/creator/:id" element={<WorkDetail />} />
            <Route path="*" element={<Navigate to="/creator" replace />} />
          </Routes>
        </Suspense>
      </main>
    </div>
  )
}
