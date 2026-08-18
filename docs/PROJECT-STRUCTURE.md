# HR-Agent 项目结构说明

> 解决「目录有点乱」的速查：每个顶层目录只干一件事；LangGraph 只在 Python Agent 里用。

## 顶层目录（约定）

| 路径 | 职责 | 不该出现的东西 |
|------|------|----------------|
| **`services/`** | Go：HTTP API、邮件、流水线、嵌入式静态页 | Python、`.venv` |
| **`python/`** | Python Agent（LangGraph 流水线、RAG、HTTP :8000） | Go 源码 |
| **`deploy/`** | MySQL init + migrations + docker-compose | 业务逻辑 |
| **`docs/`** | 方案与结构文档 | 可执行代码 |
| **`scripts/`** | 联调/运维脚本 | 核心服务 |
| **`samples/`** | 示例简历 | — |
| **`.env`** | 本地密钥（勿提交） | — |

**容易混淆的点**

- **`services/bin/`**：README 推荐的 `api.exe` 输出位置（`.gitignore` 已忽略）。
- **`bin/`（仓库根）**：历史/手工编译产物，**与 `services/bin` 重复**；以后统一只在 `services` 下 `go build`，根目录 `bin/` 可删。
- **`services/web/admin/index.html`**：当前生产管理台（单文件，逐步废弃）。
- **`services/web/admin-ui/`**：Vue 3 + Naive UI 新管理台源码；`npm run build` → `admin-ui/dist/`。
- **`python/.venv/`**：本地虚拟环境，勿提交。

## LangGraph 用在哪里？

**有。** 主路径在 Python，Go 只 HTTP 调 Agent，不跑图。

```
Go pipeline.Start
  → POST Python /v1/... (agentclient)
    → hr_agent.graph.pipeline.run_parse_screen
         try: pipeline_langgraph.py  (LangGraph StateGraph)
         except ImportError: pipeline_fallback.py  (顺序调用，无 LangGraph)
```

| 图 | 文件 | 节点（概要） |
|----|------|----------------|
| **parse → screen** | `python/hr_agent/graph/pipeline_langgraph.py` | `parse` → `verify` → `screen` → `tier` |
| **ReAct 解析** | `python/hr_agent/agents/parse_react_langgraph.py` | 带 tool 的解析纠错（`parse_react.py` 优先走此路） |
| **出题** | 同上 pipeline | `generate_questions` 子图/节点 |

依赖：`python/requirements.txt` / `pyproject.toml` 中 `langgraph>=0.2`。  
观测：可选 LangSmith（`.env` 里 `LANGCHAIN_*`）。

## Python `hr_agent/` 内部分层

```
hr_agent/
  api/           # stdlib_server / uvicorn 入口
  config/        # Settings、多厂商 LLM
  graph/         # LangGraph + fallback 入口
  agents/        # 解析 ReAct、parse_verify、启发式
  nodes/         # screen、screen_tier、questions
  tools/         # RAG、Chroma
  state/         # Pydantic 模型
```

## Go `services/` 内部分层

```
cmd/api/         # HTTP 主进程
cmd/mailer/      # 邮件 worker
cmd/ingress/     # 邮件/webhook  ingress（可选）
internal/
  api/           # 路由、鉴权
  pipeline/      # 招聘状态机、DB
  agentclient/   # 调 Python
  calendar/      # 飞书日历
  web/           # embed 静态资源 + admin_index 磁盘优先
web/
  admin/         # 旧管理台 index.html
  admin-ui/      # Vue 3 工程
  candidate/     # 候选人回复页
```

## 前端迁移（Vue 3 + Naive UI）

见 `docs/V2-ROADMAP.md` §9。开发：

```bash
cd services/web/admin-ui
npm install
npm run dev    # http://localhost:5173/admin/ ，代理 /v1 → :8080
```

生产切换（迁移完成后）：设置 `HR_ADMIN_VUE=1` 且 `npm run build`，Go 从 `admin-ui/dist` 提供 SPA + `/admin/assets/*`。

## 后续可做的「整理」（非必须）

1. 删除仓库根 `bin/`，文档只保留 `services/bin`。
2. Vue 全量替换后，旧 `admin/index.html` → `admin/index.legacy.html` 或删除。
3. 迁移脚本统一放 `deploy/mysql/migrations/`，init.sql 只负责空库 bootstrap。
