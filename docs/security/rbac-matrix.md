# GEO Intelligence P0 RBAC 矩阵

> 版本：V1.0｜状态：已批准｜日期：2026-11-13  
> 对齐：FEAT-01 V1.0、SPEC-08 V1.0、ADR-001

## 1. 角色

- Owner：组织所有者。
- ProjectAdmin：被授权项目的管理员。
- Analyst：分析和运行审计。
- Editor：处理优化任务和内容草稿。
- Approver：审核任务和未来变更。
- Viewer：只读访问。
- ServiceAccount：显式 scopes 和项目范围，无继承型人类角色。

## 2. 权限矩阵

符号：A=全部允许，P=仅授权项目，O=仅本人/分配给本人，R=只读，—=禁止。

| 资源/动作 | Owner | ProjectAdmin | Analyst | Editor | Approver | Viewer |
|---|---:|---:|---:|---:|---:|---:|
| 组织设置 read/update | A/A | R/— | R/— | R/— | R/— | R/— |
| 成员 read/manage | A/A | P/— | —/— | —/— | —/— | —/— |
| 凭据 read-metadata/manage | A/A | P/P | —/— | —/— | —/— | —/— |
| 项目 read/create/update/archive | A/A/A/A | P/P/P/P | P/—/P/— | P/—/—/— | P/—/—/— | P/—/—/— |
| 品牌/竞品 read/update/publish | A/A/A | P/P/P | P/P/按策略 | P/—/— | P/—/— | P/—/— |
| 抓取 read/run/cancel | A/A/A | P/P/P | P/P/P | P/—/— | P/—/— | P/—/— |
| 问题 read/update/publish | A/A/A | P/P/P | P/P/按策略 | P/—/— | P/—/— | P/—/— |
| 协议 read/update/publish | A/A/A | P/P/P | P/P/按策略 | P/—/— | P/—/— | P/—/— |
| 审计 read/run/cancel | A/A/A | P/P/P | P/P/P | P/—/— | P/—/— | P/—/— |
| 原始回答 read | A | P | P | — | P | 按项目策略 |
| Judge 结果 read/review/publish-version | A/A/A | P/P/— | P/P/— | —/—/— | P/P/— | P/—/— |
| 指标/报告 read/generate/publish | A/A/A | P/P/P | P/P/按策略 | P/—/— | P/—/— | P/—/— |
| 报告 export | A | P | 按项目策略 | — | P | 按项目策略 |
| Finding read/triage | A/A | P/P | P/P | P/O | P/P | P/— |
| 优化任务 read/create/update | A/A/A | P/P/P | P/P/P | P/—/O | P/—/P | P/—/— |
| 任务 approve/reject | A/A | P/P | —/— | —/— | P/P | —/— |
| 审计日志 read | A | P（仅项目事件） | — | — | — | — |
| 项目物理删除 | A（双重确认） | — | — | — | — | — |

“按策略”默认关闭，由 Owner/ProjectAdmin 在项目权限配置中显式开启。

## 3. HTTP 语义

- 未认证或 Token 无效：401。
- 已认证但主体不是当前租户成员：404，避免泄漏资源存在性。
- 同租户但动作不允许：403，返回稳定 `FORBIDDEN`。
- 资源不存在或不属于当前租户：404，返回统一 `NOT_FOUND`。
- 批量操作先校验所有目标；P0 默认全有或全无，不返回混合越权结果。

## 4. Service Account Scopes

允许 scopes：`projects:read`、`crawls:run/read`、`audits:run/read`、`reports:read/export`、`webhooks:manage`。P0 不允许 `members:manage`、`credentials:manage`、`projects:delete`。Service Account 必须绑定租户，可选绑定项目列表。

## 5. 实现与测试

授权决策集中于 Go Policy Service。每个矩阵单元生成自动化测试；直接 ID、列表、搜索、导出、签名 URL、队列和缓存路径都执行同一权限语义。权限变更产生审计事件并主动清除缓存。

