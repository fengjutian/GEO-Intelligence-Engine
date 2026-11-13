# GEO Intelligence 技术栈与工程选型

> 版本：V1.0  
> 状态：技术评审稿  
> 日期：2026-11-13  
> 适用范围：P0 GEO Audit 及后续演进  
> 关联文档：[PRD](./product-requirements.md)｜[技术原理](./technical-principles.md)｜[P0 设计规范集](./specs/README.md)

## 1. 选型结论

项目采用“Go 核心平台 + TypeScript 前端与爬虫 + Python 算法评估”的三语言架构。每种语言具有明确职责，不允许形成多套重复的核心业务后端。

```text
React Web
    │ REST/OpenAPI
Go Core Platform
    ├── MySQL：事务事实源
    ├── Redis Streams：缓存、限流和任务通知
    ├── S3：原始网页、模型响应和报告
    ├── TypeScript Crawler：HTTP/Playwright 页面采集
    └── Python Judge：评估、统计和离线算法
```

### 1.1 技术栈总览

| 层级 | 选型 | 用途 |
|---|---|---|
| Web 框架 | React 19 + TypeScript + Vite | 管理后台和报告应用 |
| UI 基础 | Tailwind CSS + Radix UI + shadcn/ui | 可访问、可定制的产品组件体系 |
| 数据表格 | TanStack Table | 审计、页面、回答和任务数据表 |
| 前端状态 | TanStack Query | 服务端状态、缓存和请求生命周期 |
| 前端路由 | TanStack Router | 类型安全路由和查询参数 |
| 表单校验 | React Hook Form + Zod | 表单状态和前后端边界校验 |
| 可视化 | Apache ECharts | 趋势、对比、置信区间和分布图 |
| 核心后端 | Go 1.26 + `net/http`/chi | API、权限、项目、审计、指标和报告 |
| 数据访问 | `database/sql` + sqlc | 显式 SQL 和强类型查询代码 |
| 数据库 | MySQL 8.4 LTS | 事务数据、状态、关系和指标事实 |
| 缓存/队列 | Redis Streams | 缓存、限流、短期任务流和消费者组 |
| 网页抓取 | Node.js LTS + TypeScript + Playwright | JS 页面渲染和浏览器自动化 |
| HTML 解析 | undici + Cheerio + 自研正文抽取 | HTTP 获取、DOM 和内容结构解析 |
| 算法服务 | Python 3.13+ + FastAPI/Pydantic | Judge、实体消歧和评估服务 |
| 数据分析 | Polars + SciPy + statsmodels + scikit-learn | Benchmark、统计与质量评估 |
| 对象存储 | S3 兼容存储；本地 MinIO | 大对象和不可变原始证据 |
| API 契约 | REST + OpenAPI 3.1 | 对外接口和客户端生成 |
| 可观测性 | OpenTelemetry + Prometheus + Grafana + Loki + Tempo | Trace、Metric 和 Log |
| 身份认证 | Keycloak 26.8.2 / OIDC | 登录、MFA 和企业 SSO |
| 密钥管理 | AWS Secrets Manager + KMS | 模型和第三方凭据 |
| 本地开发 | Docker Compose | 一键启动完整依赖 |
| 生产部署 | Docker + 托管容器平台 | P0 生产运行环境 |
| CI/CD | GitHub Actions | 测试、扫描、构建和部署 |

## 2. 技术选型原则

1. 可信测量优先于自动化数量。
2. MySQL 是业务状态事实源，Redis 不是永久事实存储。
3. 原始证据不可变，派生结果必须版本化和可重算。
4. 优先使用官方维护的 SDK 和运行时。
5. 核心业务事务集中在 Go，不在多语言间重复实现。
6. P0 选择低运维复杂度的模块化架构，不提前建设微服务平台。
7. 专用数据库和复杂基础设施必须由真实容量或查询需求触发。
8. 安全、租户隔离、成本和可观测性必须从首版设计。

## 3. 前端技术方案

### 3.1 架构形态

前端采用 React SPA。产品主要是登录后的数据密集型管理系统，不依赖公开页面 SEO，也没有必须使用服务端渲染的首屏内容，因此 P0 不引入 Next.js。

```text
React Application
├── App Shell
├── Project Workspace
├── Crawl & Content Audit
├── Entity Knowledge
├── Question Space
├── AI Search Results
├── Metrics & Reports
├── Optimization Tasks
└── Organization Settings
```

### 3.2 UI 组件体系

项目明确不使用 Ant Design。推荐组合：

- Tailwind CSS：设计 Token 和布局样式。
- Radix UI：无样式、可访问的交互原语。
- shadcn/ui：以源码形式纳入项目的基础组件实现。
- Lucide：图标。
- TanStack Table：复杂表格内核。
- ECharts：指标和趋势可视化。

shadcn/ui 不是运行时黑盒依赖；组件源码进入仓库后由项目维护，可按照 GEO Intelligence 的视觉和交互要求修改。

### 3.3 设计系统

设计 Token 使用 CSS Variables 定义：

```text
颜色：background、surface、foreground、muted、primary、warning、danger
排版：font family、size、weight、line-height
空间：4px 基础间距体系
形状：radius、border、shadow
状态：hover、focus、active、disabled、loading
图表：品牌、竞品、区间和告警色板
```

Tailwind 配置只引用 Token，不在业务组件中散布任意色值。支持浅色、深色和高对比主题，但 P0 可先交付浅色主题。

### 3.4 核心组件

基础组件至少包括：

- Button、Input、Textarea、Select、Checkbox、Radio。
- Dialog、Drawer、Popover、Dropdown、Tooltip。
- Tabs、Breadcrumb、Pagination、Command Menu。
- Toast、Alert、Skeleton、Empty State、Error State。
- DataTable、FilterBar、ColumnPicker、BulkActions。
- MetricCard、TrendChart、ConfidenceInterval、EvidencePanel。
- StatusBadge、SeverityBadge、JobProgress、AuditTimeline。
- RawResponseViewer、CodeDiff、SourceCitation、EntityRelationView。

业务页面不得直接拼接未经统一封装的 Radix Primitive；先形成项目级组件，再在页面中复用。

### 3.5 表格策略

TanStack Table 只提供表格逻辑，项目负责视觉实现。统一支持：

- 服务端分页、排序和筛选。
- 列显示和顺序配置。
- 行选择与批量操作。
- URL 状态同步。
- 大数据量虚拟滚动。
- 导出当前筛选条件。
- 键盘导航和无障碍标签。

不得一次加载全部网页或回答记录到浏览器。

### 3.6 前端状态边界

- TanStack Query：服务端数据及缓存。
- Router Search Params：可分享的筛选、分页和视图状态。
- React Local State：短期交互状态。
- React Hook Form：表单状态。
- 只有出现明确跨页面客户端状态时才考虑 Zustand。

禁止把服务端数据复制到全局 Store 后自行维护缓存一致性。

### 3.7 可访问性

目标为 WCAG 2.1 AA：键盘可完成关键流程，焦点可见，表单错误可被辅助技术读取，图表提供数据表替代，颜色不是唯一状态信号。

## 4. Go 核心平台

### 4.1 职责

Go 负责：

- HTTP API 和 OpenAPI 契约。
- 用户上下文、RBAC 和租户隔离。
- 项目、品牌、竞品和问题集。
- 审计协议冻结和任务编排。
- Model Gateway。
- 预算、配额、限流和成本归集。
- 任务状态机、Outbox 和幂等。
- 指标聚合、报告元数据和优化任务。
- 审计日志、保留和删除流程。

Python 和 TypeScript Worker 不得自行实现用户权限、计费或核心状态机。

### 4.2 推荐组件

```text
Go 1.26
├── net/http + chi
├── database/sql
├── sqlc
├── go-redis
├── OpenTelemetry Go SDK
├── OpenAPI code generation
├── structured logging
└── golang-migrate 4.20.1
```

HTTP 框架保持轻量。业务代码不能直接依赖路由框架上下文，应通过明确的 Application Service 接口调用。

### 4.3 数据访问

采用手写 SQL + sqlc：

- 普通查询生成强类型 Go 方法。
- 所有租户查询显式包含 `tenant_id`。
- 状态转换使用带旧状态和版本的条件更新。
- 指标聚合 SQL 有独立测试数据集。
- 复杂事务在 Service 层明确组织。

不使用 Active Record 或允许隐式跨表加载的重量 ORM。

## 5. 网页抓取技术方案

### 5.1 职责划分

```text
Go Discovery Orchestrator
    ├── URL 状态、租约、限额和任务调度
    └── TypeScript Crawler Worker
            ├── undici：静态 HTTP
            ├── Playwright：动态渲染
            ├── Cheerio：DOM 解析
            └── S3 Client：原始证据上传
```

Playwright Worker 使用 TypeScript，是因为 Node.js 属于 Playwright 官方支持的主要运行时。Go 不承担浏览器控制，避免依赖非官方 Go 绑定。

### 5.2 两阶段获取

1. 默认使用 undici 执行轻量 HTTP 获取。
2. 根据正文密度、应用根节点、脚本特征或项目配置判断是否需要浏览器。
3. 只有动态页面进入 Playwright。
4. 原始 HTML、渲染 HTML 和抽取结果分别保存。

这样可以控制浏览器资源成本，并提高抓取吞吐量。

### 5.3 隔离

Crawler 独立容器和网络身份，无权访问主数据库和密钥系统；通过受限 API 获取任务并写入指定对象路径。必须实施 SSRF、重定向逐跳检查、响应大小限制和浏览器沙箱。

## 6. Python 算法与评估服务

### 6.1 职责

Python 仅负责：

- Answer Judge 的语义分类。
- 品牌和产品实体消歧。
- 问题相似度、去重和聚类。
- 置信度校准。
- 黄金测试集评估。
- Benchmark 和实验统计。
- 离线重评估任务。

Python 不负责项目 CRUD、用户权限、审计状态、预算和报告发布。

### 6.2 推荐组件

```text
Python 3.13+
├── FastAPI
├── Pydantic
├── Polars
├── NumPy/SciPy
├── statsmodels
├── scikit-learn
├── pytest
└── uv
```

Judge 输入和输出使用版本化 JSON Schema。Go 传入原始响应引用、品牌快照和 Judge 版本，Python 返回带证据和置信度的结构化结果。

### 6.3 模型调用边界

如果 Judge 需要调用大模型，仍通过 Model Gateway 完成，不能绕过预算、审计和原始响应保存机制直接使用供应商密钥。

## 7. 数据与存储

### 7.1 MySQL 8.4 LTS

用于：租户、项目、实体关系、问题集、任务状态、模型调用元数据、Judge 结果、指标事实和审计日志。

P0 暂不引入图数据库。知识图谱使用实体表、关系表和证据表实现。只有多跳遍历成为经过测量的性能瓶颈后再评估专用图数据库。

### 7.2 Redis

用于：

- Redis Streams 任务通知。
- 消费者组和短期重放。
- API 缓存。
- 限流计数。
- 短期租约与协调。

MySQL 保存 Job 当前状态和 Attempt。Redis 丢失后必须能够从 MySQL 重建待处理任务。

### 7.3 S3/MinIO

用于保存：

- 原始和渲染 HTML。
- AI 请求与原始响应。
- 页面快照。
- 大型结构化解析结果。
- PDF、CSV 和报告工件。

对象默认私有并使用服务端加密。数据库保存对象键、SHA-256、大小、MIME 和生成版本。

## 8. Model Gateway

Model Gateway 由 Go 实现，保持薄层设计：

- 供应商适配。
- 模型别名解析。
- API/Search/Consumer 渠道标记。
- 幂等和重试。
- RPM、TPM 和并发限制。
- 调用前预算预留与调用后结算。
- 原始响应持久化。
- Usage、成本和错误归一化。

业务 Prompt、品牌判断、Judge 和指标不得进入 Gateway。

供应商优先使用官方 SDK；官方 SDK 不适合时使用受控 HTTP Adapter，并为每个 Adapter 建立契约测试。

## 9. API 与服务通信

### 9.1 外部 API

采用 REST + OpenAPI 3.1：

- 路径前缀 `/api/v1`。
- 游标分页。
- 统一错误码和 `trace_id`。
- 创建操作支持 `Idempotency-Key`。
- 长任务返回 Job ID。
- 导出和报告异步生成。

由 OpenAPI 生成 TypeScript Client，前端禁止重复手写接口类型。

### 9.2 内部通信

P0 内部优先使用版本化 HTTP/JSON，减少 gRPC 和 Protobuf 工具链复杂度。大正文通过 S3 引用传递，不通过消息队列或内部 API 搬运。

当内部吞吐、流式或多语言契约证明 HTTP/JSON 不足时，再评估 ConnectRPC/gRPC。

## 10. 身份与密钥

认证使用 Keycloak 26.8.2 和 OIDC Authorization Code + PKCE。应用只管理组织、成员、角色和资源权限，不自行实现密码、MFA 或 SSO 协议。详见 ADR-001。

生产密钥进入 AWS Secrets Manager 并使用 KMS 加密；数据库只存密钥引用。各 Worker 使用独立服务身份和最小权限。详见 ADR-005。

## 11. 可观测性

从第一版接入 OpenTelemetry：

```text
Application SDKs
    ↓ OTLP
OpenTelemetry Collector
    ├── Prometheus：指标
    ├── Tempo：Trace
    └── Loki：日志
            ↓
         Grafana
```

链路字段：`trace_id`、`tenant_id`、`project_id`、`audit_id`、`job_id`、`attempt_id`、`sample_id`。日志禁止包含密钥、完整网页正文、Prompt 和 AI 回答。

## 12. 测试技术栈

| 层级 | 工具 |
|---|---|
| Go 单元/集成 | `go test`、testcontainers-go |
| 前端单元 | Vitest + Testing Library |
| 前端 E2E | Playwright Test |
| TypeScript Worker | Vitest + 录制网页 Fixture |
| Python | pytest |
| API 契约 | OpenAPI Schema Test |
| 数据库 | 真实 MySQL 容器和迁移测试 |
| 安全 | SAST、SCA、Secret Scan、Container/IaC Scan |
| 性能 | k6 |

CI 默认不调用真实付费模型 API。供应商行为通过脱敏 Fixture 和 Adapter 模拟器测试，真实调用放在受预算限制的 Staging Smoke Test。

## 13. 本地开发环境

Docker Compose 至少启动：

```text
mysql
redis
minio
api
scheduler
crawler-worker
model-worker
judge-worker
report-worker
otel-collector
prometheus
grafana
mailpit
```

前端可在宿主机使用 Vite 开发服务器运行，也可以选择容器运行。仓库提供 `.env.example`，但不提供真实密钥。

## 14. 生产部署

P0 采用 Docker 和托管容器平台，不自建 Kubernetes。部署单元按 API、Scheduler、Crawler、Model Worker、Judge 和 Report Worker 分开扩容。

推荐部署原则：

- MySQL、Redis 和对象存储优先使用托管服务。
- Crawler 使用独立网络和安全组。
- API 不直接暴露数据库。
- Worker 按队列深度扩容，同时受供应商和预算限制。
- 数据库迁移由独立受控 Job 执行。
- 发布采用 Feature Flag 和分阶段 Canary。

## 15. 仓库结构建议

推荐 Monorepo：

```text
/
├── apps/
│   ├── web/                  # React/Vite
│   ├── api/                  # Go API
│   ├── scheduler/            # Go 调度器
│   ├── crawler-worker/       # TypeScript/Playwright
│   ├── model-worker/         # Go Model Gateway Worker
│   ├── judge-worker/         # Python Judge
│   └── report-worker/        # Go 报告任务
├── internal/                 # Go 业务模块
│   ├── identity/
│   ├── project/
│   ├── discovery/
│   ├── knowledge/
│   ├── question/
│   ├── audit/
│   ├── gateway/
│   ├── judge/
│   ├── metrics/
│   └── reporting/
├── packages/
│   ├── ui/                   # 前端设计系统
│   ├── api-client/           # OpenAPI 生成客户端
│   ├── schemas/              # JSON Schema/OpenAPI
│   └── config/               # TS 共享配置
├── db/
│   ├── migrations/
│   ├── queries/
│   └── fixtures/
├── deploy/
│   ├── compose/
│   ├── containers/
│   └── environments/
├── docs/
└── tests/
    ├── contract/
    ├── e2e/
    ├── performance/
    └── security/
```

Python 和 Node 依赖限定在对应应用目录，避免全仓库环境污染。跨语言共享契约通过 OpenAPI/JSON Schema，不共享运行时代码。

## 16. 版本与依赖策略

- Go 使用当前稳定主版本并跟随最新安全补丁。
- React 使用 Latest 稳定通道，不使用 Canary/Experimental。
- MySQL 固定在 8.4 LTS 系列，按补丁版本升级。
- Node.js 选择当前 Active/Maintenance LTS，项目锁定精确版本。
- Python 固定次版本，依赖使用 lockfile。
- 前端、Go、Python 和容器镜像均使用自动依赖更新工具，但生产升级必须通过测试门禁。
- 锁文件和容器 Digest 提交仓库，禁止生产使用未固定的 `latest` 镜像。

## 17. P0 暂不引入

以下技术不进入 P0，除非出现经过测量的刚性需求：

- Kubernetes。
- 微服务治理平台和 Service Mesh。
- Kafka/Pulsar。
- Temporal。
- Elasticsearch/OpenSearch。
- ClickHouse。
- 向量数据库。
- 专用图数据库。
- Next.js 服务端渲染。
- 大型 Agent Framework。
- 自研身份认证系统。

Temporal 可在 P2/P3 出现跨天等待人工审批、复杂补偿和长时间 Agent 工作流时重新评估。ClickHouse、OpenSearch、向量库和图数据库必须由容量和查询基准证明需要。

## 18. 架构红线

1. 不允许 Python 或 Crawler 直接绕过 Go 核心平台修改审计状态。
2. 不允许任何 Worker 绕过 Model Gateway 使用生产模型密钥。
3. 不允许使用 Redis 作为任务和指标的唯一事实源。
4. 不允许跨渠道合并 API 与消费者应用的 GEO 指标。
5. 不允许把网页内容作为可信指令执行。
6. 不允许自动发布 Agent 自己生成的生产修改。
7. 不允许把完整模型响应、网页正文或密钥写入日志。
8. 不允许为了“知识图谱”或“AI”标签提前引入专用数据库。

## 19. 最终推荐基线

```text
Frontend
React + TypeScript + Vite
Tailwind CSS + Radix UI + shadcn/ui
TanStack Query/Router/Table + ECharts

Core Platform
Go + chi + sqlc + MySQL 8.4 LTS

Async
Redis Streams + MySQL Job State + Outbox

Crawler
TypeScript + undici + Playwright + Cheerio

AI & Data
Python + FastAPI + Pydantic + Polars + scikit-learn

Storage & Operations
MySQL + Redis + S3/MinIO
OpenTelemetry + Prometheus + Grafana + Loki + Tempo
Docker Compose → Managed Container Platform
```

这套技术栈能够支持 P0 的可信测量闭环，并为后续 GEO Intelligence、Optimization 和 Agent 阶段保留演进空间，同时避免首版承担微服务、Kubernetes和多种专用数据库的额外复杂度。

## 20. 官方参考

- [Go Release History](https://go.dev/doc/devel/release)
- [React Versions](https://react.dev/versions)
- [React Versioning Policy](https://react.dev/community/versioning-policy)
- [MySQL 8.4 LTS Release Model](https://dev.mysql.com/doc/refman/8.4/en/mysql-releases.html)
- [Redis Streams](https://redis.io/docs/latest/develop/data-types/streams/)
- [sqlc Documentation](https://docs.sqlc.dev/)
- [OpenTelemetry Language Support](https://opentelemetry.io/docs/languages/)
- [Temporal Durable Execution](https://docs.temporal.io/ai)

