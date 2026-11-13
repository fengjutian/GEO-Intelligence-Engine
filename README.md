# GEO Intelligence Engine

GEO Intelligence Engine 的 P0 工程基线。实现与 V1.0 文档基线对齐，采用 Go API、React Web、TypeScript/Playwright 抓取器、Python Judge、MySQL、Redis、MinIO 与 Keycloak。

## 目录

- `cmd/api`、`internal`：Go API 与基础设施代码
- `apps/web`：React + Vite + Tailwind CSS 管理端（未使用 Ant Design）
- `apps/crawler-worker`：TypeScript 抓取与内容解析 Worker
- `apps/judge-worker`：Python Answer Judge 服务
- `db/migrations`：MySQL 版本化迁移
- `api/openapi.yaml`：HTTP API 契约
- `docs`：V1.0 产品、架构、功能与验收文档
- `tests/golden`：内容质量黄金测试集

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

分别打开四个终端启动服务：

```powershell
go run ./cmd/api
pnpm dev:web
pnpm dev:crawler
Set-Location apps/judge-worker; uv run uvicorn geo_judge.app:app --reload --port 8081
```

启动后：Web `http://localhost:5173`，API `http://localhost:8080`，Judge `http://localhost:8081`，Crawler 健康检查 `http://localhost:8082/healthz`。

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

该方式下 Web 地址为 `http://localhost:3000`，其余服务端口与原生运行一致。
