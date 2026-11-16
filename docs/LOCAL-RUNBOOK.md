# GEO Intelligence Engine 本地运行手册

本文说明如何在 Windows PowerShell 中启动 GEO Intelligence Engine，以及如何运行 GEO 证据链端到端测试。

## 1. 环境要求

- MySQL 8.x（项目基线为 MySQL 8.4）
- Go 1.26+
- Node.js 24+
- pnpm 11+
- Python 3.13
- `uv`

当前可执行基线只强依赖 MySQL。Redis、MinIO 和 Keycloak 不是本地开发的必需依赖。

## 2. 配置环境变量

在项目根目录复制示例配置：

```powershell
Copy-Item .env.example .env
```

打开 `.env`，确认数据库名称、用户和密码与本地 MySQL 一致：

```dotenv
GEO_DATABASE_DSN=geo:geo_local_only@tcp(localhost:3306)/geo?parseTime=true&charset=utf8mb4
GEO_MIGRATION_DATABASE_URL=mysql://geo:geo_local_only@tcp(localhost:3306)/geo
GEO_MIGRATIONS_PATH=db/migrations
```

使用示例配置前，需要在 MySQL 中创建 `geo` 数据库及对应账号，并授予该账号读写和修改 schema 的权限。

如需执行真实模型审计，至少配置一个供应商密钥：

```dotenv
DEEPSEEK_API_KEY=你的密钥
# 或
MINIMAX_API_KEY=你的密钥
```

未配置模型密钥时，管理端和基础 API 仍可启动，但 Model Worker 不会执行需要调用模型的审计样本。

## 3. 一键启动（推荐）

首次启动：

```powershell
python scripts/dev.py start --install
```

该命令会安装依赖、执行数据库迁移并启动本地服务。运行状态和日志保存在 `.run/`。

查看状态：

```powershell
python scripts/dev.py status
```

后续重启：

```powershell
python scripts/dev.py restart --skip-migrate
```

需要执行新增迁移时，不要使用 `--skip-migrate`：

```powershell
python scripts/dev.py restart
```

停止全部服务：

```powershell
python scripts/dev.py stop
```

## 4. 服务地址

| 服务 | 地址 |
| --- | --- |
| Web 管理端 | <http://localhost:5174> |
| Go API | <http://localhost:8080> |
| Judge Worker | <http://localhost:8081> |
| Crawler 健康检查 | <http://localhost:8082/healthz> |
| Model Worker 健康检查 | <http://localhost:8083/healthz> |

API 就绪检查：

```powershell
Invoke-RestMethod http://localhost:8080/readyz
```

预期返回：

```json
{"status":"ready"}
```

## 5. 手动启动

先安装依赖并升级数据库：

```powershell
pnpm install
Push-Location apps/judge-worker
uv sync
Pop-Location
go run ./cmd/migrate up
```

然后分别打开五个 PowerShell 终端。

API：

```powershell
go run ./cmd/api
```

Model Worker：

```powershell
go run ./cmd/model-worker
```

Web：

```powershell
pnpm dev:web
```

Crawler Worker：

```powershell
pnpm dev:crawler
```

Judge Worker：

```powershell
Set-Location apps/judge-worker
uv run uvicorn geo_judge.app:app --reload --port 8081
```

手动启动时，需要先将 `.env` 中的变量加载到当前终端，或者逐项设置必要的环境变量。`scripts/dev.py` 会自动读取根目录 `.env`。

## 6. GEO 证据链端到端测试

真实审计 E2E 会在 MySQL 中临时写入：

- 项目、官网页面与 Finding；
- 问题集、问题和测量协议；
- 基线审计及 `mention_rate=0.2` 指标；
- 同协议复测审计及 `mention_rate=0.6` 指标；
- 证据快照、状态事件和自动复测结论。

测试会验证候选证据、不可变快照、状态推进、复测可比性和 `improved` 结论，并在结束后清理临时数据。

如果当前终端已设置 `GEO_DATABASE_DSN`：

```powershell
$env:GEO_TEST_DATABASE_DSN=$env:GEO_DATABASE_DSN
go test ./internal/httpapi -run TestEvidenceChainRealAuditE2E -v
pnpm --dir apps/web test -- EvidenceChainEditor.test.tsx
```

如果尚未加载 `.env`，可以先载入其中的变量：

```powershell
Get-Content .env | ForEach-Object {
  if ($_ -match '^[A-Za-z_][A-Za-z0-9_]*=') {
    $pair = $_ -split '=', 2
    [Environment]::SetEnvironmentVariable($pair[0], $pair[1].Trim('"'), 'Process')
  }
}
$env:GEO_TEST_DATABASE_DSN=$env:GEO_DATABASE_DSN
```

未设置 `GEO_TEST_DATABASE_DSN` 时，真实数据库测试会跳过，普通单元测试不依赖本地 MySQL。

## 7. 全量质量检查

```powershell
go test ./...
go vet ./...
pnpm typecheck
pnpm test
pnpm build
Push-Location apps/judge-worker
uv run ruff check .
uv run pytest
Pop-Location
```

## 8. 常见问题

### API 报数据库连接失败

检查 MySQL 是否启动，并确认 `.env` 中的 `GEO_DATABASE_DSN` 使用的是 Go MySQL DSN 格式；迁移命令使用的 `GEO_MIGRATION_DATABASE_URL` 则是 `mysql://` URL 格式。

### 数据库版本不是最新

```powershell
go run ./cmd/migrate version
go run ./cmd/migrate up
```

当前 GEO 证据链版本要求 schema 版本至少为 38。

### Web 能打开但无法执行真实审计

检查：

1. 至少配置了一个模型供应商 API Key；
2. API、Model Worker 和 Judge Worker 均处于运行状态；
3. `/api/v1/model-providers` 返回对应供应商为已配置；
4. Model Worker 和 Judge Worker 日志中没有连接或鉴权错误。

### 端口被占用

先运行：

```powershell
python scripts/dev.py status
python scripts/dev.py stop
```

确认旧进程停止后再重新启动。
