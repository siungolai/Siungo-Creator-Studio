# Siungo Creator Studio — 自媒体 AI 制作工作台

面向个人创作者的短视频全流程 AI 工作台：**选题 → AI 生成脚本 → 人工编辑 → 发布管理 → 历史复盘**。AI 既是生产工具（产出内容初稿），也是编排中枢（出选题、拆内容）。

## 功能规划

- **作品管理**：四态生命周期（草稿 / 制作中 / 已发布 / 归档）；卡片列表 + 手动新建 + 详情编辑（基本信息 / 工作副本 / 标签 / 风格）+ 状态筛选 + 关键词搜索 + 分页（20 条/页，页码保持 URL）
- **AI 生成**：输入主题 + 风格模板（干货 / 剧情 / 种草 / 吐槽）→ 产出标题候选、脚本正文、口播稿、标签建议、封面文案；支持通用 / 抖音 / B站 分平台版本，多版本对比与选用
- **AI 选题**：批量生成选题建议，勾选一键成为作品草稿
- **发布管理**：平台可配置（预置抖音 + B站），逐平台绑定版本、维护发布状态与链接
- **历史记录**：AI 生成版本历史 + 调用日志（耗时 / token / 错误）

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.27 · 标准库 `net/http` · SQLite（`modernc.org/sqlite`，纯 Go 无 CGO） |
| 前端 | React 19 · TypeScript · Tailwind CSS 4 · Vite · React Router 7 |
| AI | DeepSeek（`deepseek-go`，MIT）· 多 provider 架构预留，API key 仅环境变量 |

设计原则：前端零 UI 库、零状态管理库；后端除 AI 客户端外零第三方依赖；单二进制部署。

## 开发路线

| 里程碑 | 内容 | 状态 |
|--------|------|------|
| M0 骨架+认证 | 项目骨架、验证链路、单密码登录（持久会话） | ✅ 已完成 |
| M1 作品管理 | 数据表 + 作品 CRUD + 详情编辑 + 搜索/筛选/分页 | ✅ 已完成 |
| M2 AI 生成 | DeepSeek 接入 + 生成面板 + 版本历史 | ⏳ |
| M3 发布管理 | 平台配置 + 发布状态维护 | ⏳ |
| M4 选题 + 设置 | AI 选题建议 + 设置弹层 | ⏳ |
| M5 收尾 | 失败重试打磨 + 文档同步 | ⏳ |

## 本地开发

```bash
# 后端（默认 127.0.0.1:8081；STUDIO_PASSWORD 必设，缺失拒绝启动）
cd server && export STUDIO_PASSWORD=<你的密码> && go run .

# 前端（Vite 5173，/api 代理到后端）
cd web && npm install && npm run dev
```

- 浏览器访问 **http://127.0.0.1:8081**（后端已内嵌前端产物，单二进制形态）或 http://localhost:5173（dev 模式）
- 环境变量：`HOST`（默认 127.0.0.1）、`PORT`（默认 8081）、`DB_PATH`（默认 data.db）、`AUTH_FAIL_COOLDOWN`（登录冷却秒数）、`COOKIE_SECURE=true`（https 部署）、`TRUST_XFF=true`（nginx 反代）；AI 相关见下
- 验证：`bash scripts/verify.sh`（构建全绿）｜ `bash scripts/smoke.sh`（构建 + 起服务 + api-test 断言）

AI 功能需配置环境变量 `AI_API_KEY`（DeepSeek API key）等；缺失时 AI 功能返回明确错误提示，其余功能不受影响。

## 许可证

[Apache-2.0](./LICENSE)
