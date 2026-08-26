// 将 web/dist 构建产物同步到 server/static（go:embed 目标）。
// 只做合并复制、不清理目标目录，保证 server/static/PLACEHOLDER 占位文件始终存在（go:embed 需非空目录）。
import { cpSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const from = resolve(root, 'web', 'dist')
const to = resolve(root, 'server', 'static')

if (!existsSync(from)) {
  console.error('web/dist 不存在，请先执行 vite build')
  process.exit(1)
}
mkdirSync(to, { recursive: true })
cpSync(from, to, { recursive: true })
console.log(`static 已同步: ${to}`)
