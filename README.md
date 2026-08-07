# 多 Agent 智能招聘系统（Go + Python）

面向招聘侧的 AI 流程辅助：HR 上传简历与 JD → 解析初筛 → 飞书约面发信 → 候选人点链接回复 → **确认后再出题**。

## 架构

```
HR 管理台 /admin
  → Go API（鉴权）→ Python Agent（parse/screen；confirm 后 generate_questions）
                  → 飞书日历 hold → 邮件队列（含 48h 单链接）
候选人 /r/{token}
  → 公开 API（HMAC token）→ accept / decline / reschedule / pick_slot
```

- 出题时机：候选人 **确认面试时间之后**（RAG 题库 + LLM）
- 候选人回复：邮件 **一个回调链接**（48 小时有效），页面上选择；无需 IMAP
- 观测：LangSmith（可选）+ MySQL `audit_events`

## 目录

```
services/          # Go：API / mailer / ingress + 嵌入式 web
python/hr_agent/   # LangGraph Agent
deploy/            # MySQL init + docker-compose
samples/           # 示例简历
scripts/           # 联调测试
```

## 快速开始

### 1. 配置

```bash
cp .env.example .env
```

关键项：

| 变量 | 说明 |
|------|------|
| `REPLY_TIMEOUT_HOURS` | 默认 `48` |
| `PUBLIC_BASE_URL` | 邮件链接前缀，如 `http://127.0.0.1:8080` |
| `REPLY_TOKEN_SECRET` | 候选人链接 HMAC 密钥 |
| `HR_API_TOKEN` | HR 管理台 / 受保护 API |
| `INTERNAL_API_TOKEN` | ingress → API 内部调用 |

### 2. Python Agent

```bash
cd python
.\.venv\Scripts\python.exe -m hr_agent.api.stdlib_server
# 或: uvicorn hr_agent.api.server:app --host 0.0.0.0 --port 8000
```

### 3. Go 服务

```bash
cd services
go build -o bin/api.exe ./cmd/api
go build -o bin/mailer.exe ./cmd/mailer
.\bin\api.exe
```

Go 启动时会**自动**向上查找并加载仓库根目录的 `.env`（`config.Load` + `godotenv`），一般**不必**再跑 `scripts/load_env.ps1`。  
`load_env.ps1` 仅在你用裸 `python scripts/*.py`、或要在当前 shell 里临时覆盖变量时才有用。

### 4. 使用入口

- **HR 后台**：<http://127.0.0.1:8080/admin/>
  - 默认 **飞书授权登录**（HttpOnly 会话 Cookie）
  - 开发备用：折叠区仍可用 `HR_API_TOKEN`
  - 统计、JD、上传简历、申请详情、人工闸门
- **候选人页**：邮件中的 `/r/{token}`（接受 / 拒绝 / 改期选时段）

### 飞书登录配置（开放平台）

1. 打开 [飞书开发者后台](https://open.feishu.cn/app) → 你的应用  
2. **安全设置 → 重定向 URL** 增加：  
   `http://127.0.0.1:8080/v1/auth/feishu/callback`（生产改为你的 HTTPS 域名）  
3. **权限管理 → 用户身份权限 (user_access_token)**（不是应用身份）开通并发布：  
   `auth:user_access_token:read`（获取 user_access_token 基本信息）  
   若拉用户姓名失败，再在「通讯录」下补 `contact:user.base:readonly`  

4. **应用可用性** 设为 HR 所在部门/人员可见  
5. `.env` 中 `FEISHU_LOGIN_ENABLED=true`，`FEISHU_APP_ID` / `FEISHU_APP_SECRET` 已有即可  
6. 可选白名单：`FEISHU_HR_ALLOW_OPEN_IDS` / `FEISHU_HR_ALLOW_EMAILS`

### 5. API 示例

创建申请（需 HR Token）：

```bash
curl -X POST http://127.0.0.1:8080/v1/admin/applications ^
  -H "Authorization: Bearer %HR_API_TOKEN%" ^
  -H "Content-Type: application/json" ^
  -d "{\"jd_id\":\"jd-backend-001\",\"candidate_email\":\"a@b.com\",\"candidate_name\":\"张三\",\"resume_text\":\"...\"}"
```

候选人结构化回复（公开）：

```bash
curl -X POST http://127.0.0.1:8080/v1/public/reply/TOKEN ^
  -H "Content-Type: application/json" ^
  -d "{\"action\":\"accept\"}"
```

## 状态机

`uploaded → parsing → screened → awaiting_reply → confirmed|declined|needs_human`  

- 筛选失败 → `rejected`
- 确认后异步写 `questions_json`（失败不回滚 confirmed）
- 超时 / 改期超限 / 低置信文本回复 → `needs_human`

## 联调测试

```bash
# 需已设置 HR_API_TOKEN 等（scripts/load_env.ps1）
python scripts/full_integration_test.py
python scripts/remaining_integration_test.py
```

## 说明

- 日历：`CALENDAR_PROVIDER=feishu|memory`
- `MAIL_DRY_RUN=true` 时邮件只打日志
- 旧文本 webhook（`/v1/applications/{id}/replies`）仍可用，需 HR 或 Internal Token
