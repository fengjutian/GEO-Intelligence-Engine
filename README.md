# GEO Intelligence Engine

GEO Intelligence Engine 的 P0 工程基线。实现与 V1.0 文档基线对齐，采用 Go API、React Web、TypeScript/Playwright 抓取器、Python Judge、MySQL、Redis、MinIO 与 Keycloak。

## 目录

- `cmd/api`、`internal`：Go API 与基础设施代码
- `cmd/model-worker`：审计采样执行 Worker，调用 Model Gateway 并持久化请求/响应
- `apps/web`：React + Vite + Tailwind CSS 管理端（未使用 Ant Design）
- `apps/crawler-worker`：TypeScript 抓取与内容解析 Worker
- `apps/judge-worker`：Python Answer Judge 服务
- `apps/report-renderer`：Playwright Chromium 报告 PDF 渲染器
- `db/migrations`：MySQL 版本化迁移
- `api/openapi.yaml`：HTTP API 契约
- `docs`：V1.0 产品、架构、功能与验收文档
- 功能优先级：[docs/FEATURE-PLAN.md](docs/FEATURE-PLAN.md)，区分核心 P0-C、支撑 P0-S 与冻结的非核心能力
- `tests/golden`：内容质量黄金测试集

当前已实现项目与品牌档案的创建、列表、详情、乐观锁编辑、归档和不可变配置发布。创建项目会在同一数据库事务中写入项目、主品牌和官方网站；编辑与归档要求 `If-Match` 版本，防止覆盖并发修改。配置发布包含 SHA-256 内容寻址，相同内容重复发布保持幂等。

品牌档案支持 Unicode 规范化别名、等价别名冲突检测、逐项启停，以及人工确认产品维护；这些内容会自动进入后续配置快照。

竞品档案同样使用规范化名称约束；主品牌不能被静默添加为竞品，等价竞品名称会返回冲突并要求消歧。

网站抓取已实现受控多页 P0 纵切：Go API 创建并调度 Crawl Run，TypeScript Worker 通过受限内部接口认领任务，逐跳执行 DNS/IP SSRF 检查，获取和解析 HTML，并将页面内容版本、哈希和抓取结果写回 MySQL。发现链接按同站范围、最大页数和最大深度进入队列，单页失败不终止整个 Run；Worker 崩溃后过期租约会自动重新入队。抓取前按目标 User-Agent 获取并缓存 `robots.txt`，被禁止的 URL 标记为 blocked 且不计入失败；每轮首次抓取还会解析 robots 声明的 Sitemap 或 `/sitemap.xml`，支持 Sitemap Index、嵌套与 gzip，证据写入 `robots_snapshots` 和 `crawl_sitemaps`，管理端抓取证据弹窗可查看 robots 分组规则、原始内容与 Sitemap 文档状态。抓取完成时还会运行确定性内容质量规则（`content-rules-v1`），报告标题结构、Answer First、统计无证据、FAQ 缺答案/重复、作者缺失、发布日期缺失、内容陈旧、重复术语无解释与绝对化表述等问题，并进入 Findings/报告/优化任务链路；`GET /projects/{id}/content-quality` 提供按严重度与规则的汇总，报告摘要同步冻结内容质量计数；`POST /crawl-runs/{id}/content-analysis-runs` 可基于页面快照独立重跑内容规则，旧 open Finding 标记 superseded，结果写入 `content_signals`。`GET /projects/{id}/findings` 与 `PATCH /findings/{id}` 支持人工确认/忽略，内容质量汇总只统计 open 问题。抓取器同时解析页面 JSON-LD；Go API 抽取 Organization、Brand、Product、SoftwareApplication 实体，保存别名、来源证据与 `brand`/`manufacturer` 关系，并提供实体列表、详情、人工确认、合并与撤销接口；管理端“实体知识”页面支持项目切换、证据查看、确认/拒绝与合并撤销。`POST /projects/{id}/knowledge-snapshots` 可发布包含实体与关系的知识快照。

审计编排已实现引擎配置、问题集不可变版本发布、测量协议创建与冻结、审计成本估算、幂等创建、确定性采样展开、预算预留与结算、样本执行、Answer Judge 和取消。`cmd/model-worker` 以 2 分钟租约领取样本，调用 Model Gateway，并把 `ai_requests`、`ai_responses`、Usage、成本、延迟和哈希写回 MySQL；失败按一次重试后标记失败，审计按样本结果收敛为 succeeded/partial_success/failed。审计创建会在审计级预算账户中原子预留估算成本，超过硬上限返回 422；样本成功后按实际成本结算并写 `cost_ledger`，取消或失败会释放未使用预留。协议发布会冻结问题集版本、引擎配置和品牌知识快照，后续配置变更不影响已创建审计。Python Judge 对回答做确定性判定并写入 `evaluation_runs` / `answer_evaluations`：内容有效性、回答结果、品牌提及、推荐标签、情感与资格；`eligible_count` 随合格回答累加。审计收敛时自动生成 `metrics-v1` 指标快照，按渠道和引擎切片计算提及率、推荐率、拒答率、正向提及率、有效回答率与提及频次，比例附带 95% Wilson 区间并通过 `GET /api/v1/audits/{id}/metrics` 查询。管理端审计弹窗会记住项目最近的审计，轮询状态并展示指标卡片、分子分母、置信区间与小样本提示；点击指标卡片可下钻到构成回答的 Judge 标签。审计成功后可从弹窗生成版本化报告，报告冻结协议、Judge、指标与模板版本，包含摘要、范围、指标、技术发现、优先级建议和限制声明；优先级按 `priority-v1` 的五个可解释分项计算为 P0–P3。报告支持发布与 CSV/PDF 导出（CSV 带 UTF-8 BOM、公式注入转义；PDF 由 `apps/report-renderer` 的 Playwright Chromium 按 A4 模板渲染）。指标已覆盖提及、推荐、拒答、正向提及、官网引用、第三方引用与有效回答率；第三方引用暂按非品牌域名统计，尚未配置可信域名白名单。报告中的 P0–P3 优先级建议可直接创建优化任务，任务使用严格状态机、验收项勾选、评论、字段编辑（优先级/负责人/截止时间）、版本乐观锁和不可变事件历史；管理端任务看板支持流转与讨论。Judge 判定的不合格回答与拒答会自动进入复核队列；`GET /review-queue`、`POST /review-queue`、`PATCH /review-queue/{id}`、标注与仲裁接口支持人工入队、分派、一致性判定和最终仲裁，`GET /review-queue/stats` 提供状态、优先级与一致率统计。

身份与权限默认使用 `GEO_AUTH_MODE=dev`（开发租户 + `GEO_DEV_ROLE`）。`jwt` 模式校验内部 HS256 Token；`oidc` 模式校验 RS256 Token 的 issuer/audience/exp 并拉取 JWKS。两种 Token 模式都会查询 `memberships`，以数据库角色覆盖 Token 角色，非成员返回 403，401/403 自动写 `audit_logs`。`GET /api/v1/me` 返回当前主体；Owner 可查询成员、创建邀请并调整成员角色/状态，最后一名 Owner 不可被降级或停用；被邀请人可通过 `POST /api/v1/invitations/{token}:accept` 凭邮箱匹配加入租户。项目级授权支持 `project_memberships`：存在项目成员记录的用户仅能访问被授权项目，Owner/ProjectAdmin 不受限，项目列表同样按此规则过滤；报告查看、导出与下载写入 `audit_logs`。Web 端通过 `/api/v1/auth-config` 读取认证模式，OIDC 模式使用 Keycloak 授权码 + PKCE 登录，支持静默刷新、401 重试与登出；真实 Realm 尚未提供，当前以本地模拟 Issuer 验证。

Crawl Run 支持暂停、继续和取消。状态变更使用数据库锁串行化，迟到的 Worker 完成或失败回调不会覆盖取消状态；暂停期间已领取的短任务可在安全点结束，继续时从持久状态恢复。

抓取结果支持页面级下钻：页面列表和不可变内容版本包含 HTTP 状态、最终 URL、canonical、语言、标题、SHA-256、解析器版本及最多 100 条出站链接证据。

技术审计首批规则自动生成版本化 Finding，覆盖非 2xx、缺失 canonical、robots noindex、缺失 HTML 语言和正文过薄；每条 Finding 关联不可变页面版本并保存结构化证据。

项目级抓取历史持久化展示最近 50 次 Crawl Run。页面刷新或服务重启后仍可恢复状态、计数，并重新进入任意历史 Run 的页面和 Finding 证据。

## 本地开发（默认方式，不使用 Docker）

前置环境：Go 1.26+、Node.js 24、pnpm 11、Python 3.13（可由 uv 自动安装）及 MySQL 8.4。当前可执行基线只强依赖 MySQL；Redis、MinIO 与 Keycloak 将在相应业务模块接入时启用。

先创建数据库和账号，并在 PowerShell 中设置环境变量：

```powershell
$env:GEO_DATABASE_DSN = 'geo:geo_local_only@tcp(localhost:3306)/geo?parseTime=true&charset=utf8mb4'
$env:GEO_MIGRATION_DATABASE_URL = 'mysql://geo:geo_local_only@tcp(localhost:3306)/geo'
```

安装依赖并升级数据库：

```powershell
pnpm install
Push-Location apps/judge-worker; uv sync; Pop-Location
go run ./cmd/migrate up
```

也可以使用零第三方依赖的 Python 管理脚本完成迁移并统一启停五个服务：

```powershell
python scripts/dev.py start --install
python scripts/dev.py status
python scripts/dev.py stop
```

脚本读取根目录 `.env`，运行状态和日志保存在 `.run/`。后续启动可省略 `--install`；如需保留当前数据库版本，可增加 `--skip-migrate`。

分别打开五个终端启动服务：

```powershell
go run ./cmd/api
go run ./cmd/model-worker
pnpm dev:web
pnpm dev:crawler
Set-Location apps/judge-worker; uv run uvicorn geo_judge.app:app --reload --port 8081
```

启动后：Web `http://localhost:5174`，API `http://localhost:8080`，Judge `http://localhost:8081`，Crawler 健康检查 `http://localhost:8082/healthz`，Model Worker 健康检查 `http://localhost:8083/healthz`。未配置任何供应商密钥时 Worker 保持健康但不会领取样本。

核心回归（需 Worker 可调用 Provider）：`python scripts/regression.py`，覆盖项目→问题→协议→审计→Judge→指标→报告/CSV，并独立复算指标与 Wilson 区间；结果写入 `.run/regression-verification.json`。

## 质量检查

```bash
go test ./...
go vet ./...
pnpm typecheck
pnpm test
pnpm build
cd apps/judge-worker && uv run ruff check . && uv run pytest
```

数据库结构默认由 `go run ./cmd/migrate up` 升级。开发接口返回 RFC 9457 风格的问题响应，并通过 `X-Trace-ID` 贯穿请求日志。

## Docker（可选）

Docker 不是开发或运行前提，只保留为隔离环境、CI 验证或快速演示选项：

```powershell
docker compose up --build
```

## AI 模型供应商

P0 首批支持 DeepSeek 与 MiniMax。MiniMax 默认使用国内站 API，模型密钥只配置在 API 服务端，不写入 Web 前端：

```dotenv
DEEPSEEK_API_KEY=
DEEPSEEK_BASE_URL=https://api.deepseek.com
DEEPSEEK_MODEL=deepseek-flash
MINIMAX_API_KEY=
MINIMAX_BASE_URL=https://api.minimaxi.com/v1
MINIMAX_MODEL=MiniMax-M2.5
GEO_MODEL_RPM=60
GEO_MODEL_MAX_CONCURRENCY=4
```

`GEO_MODEL_RPM` 控制每个供应商的每分钟调用预算（平滑令牌桶），`GEO_MODEL_MAX_CONCURRENCY` 控制单供应商并发；Worker 对 408/429/5xx 和网络错误最多重试 3 次，并遵守 `Retry-After`。

复制 `.env.example` 为 `.env` 后填写至少一个供应商密钥，再用 `python scripts/dev.py restart --skip-migrate` 重启。可通过 `GET /api/v1/model-providers` 查看配置状态，通过管理界面的“模型供应商”区域执行健康检查。

该方式下 Web 地址为 `http://localhost:3000`，其余服务端口与原生运行一致。
