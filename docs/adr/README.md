# GEO Intelligence 架构决策记录

> 基线：P0 V1.0｜状态：已批准｜批准日期：2026-11-13

| ADR | 决策 | 状态 |
|---|---|---|
| [ADR-001](./ADR-001-identity-keycloak.md) | 身份平台使用 Keycloak 26.8.2 | 已批准 |
| [ADR-002](./ADR-002-database-migrations.md) | 数据库迁移使用 golang-migrate 4.20.1 | 已批准 |
| [ADR-003](./ADR-003-model-providers.md) | 首发模型供应商为 OpenAI 与 Google Gemini | 已批准 |
| [ADR-004](./ADR-004-pdf-rendering.md) | PDF 使用 React 打印模板和 Playwright Chromium | 已批准 |
| [ADR-005](./ADR-005-runtime-platform.md) | AWS 托管生产基线；本地部分已被 ADR-006 取代 | 部分取代 |
| [ADR-006](./ADR-006-native-development.md) | 原生本地开发，Docker 仅作为可选方案 | 已批准 |

ADR 一经批准不得原地改写决策语义；变更必须新增 ADR 并标记旧 ADR 为 Superseded。

