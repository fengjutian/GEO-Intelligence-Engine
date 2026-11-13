# GEO Intelligence P0 设计规范集

> 版本：V1.0｜状态：研发评审稿｜日期：2026-11-13

本目录把 [PRD](../product-requirements.md) 和 [技术原理](../technical-principles.md) 落实为 P0 可执行规范。发生冲突时，安全与数据隔离规范优先；指标口径以测量协议为准；接口与表结构变更必须同步更新相关文档。

统一工程选型见 [技术栈与工程选型](../technology-stack.md)。

面向开发和验收的逐功能拆分见 [P0 功能规格集](../features/README.md)。

| 编号 | 文档 | 主要评审人 |
|---|---|---|
| SPEC-01 | [GEO 测量协议与指标口径](./01-measurement-protocol.md) | 产品、算法、数据 |
| SPEC-02 | [P0 系统架构设计](./02-p0-system-architecture.md) | 架构、后端、SRE |
| SPEC-03 | [核心数据模型与数据库设计](./03-core-data-model.md) | 后端、DBA、数据 |
| SPEC-04 | [网站抓取与内容解析规范](./04-crawling-and-parsing.md) | 爬虫、后端、安全 |
| SPEC-05 | [Model Gateway 接口规范](./05-model-gateway.md) | AI、后端、财务运营 |
| SPEC-06 | [Answer Judge 标注与评估规范](./06-answer-judge.md) | 算法、标注、QA |
| SPEC-07 | [任务状态机与失败恢复设计](./07-job-state-and-recovery.md) | 后端、SRE、QA |
| SPEC-08 | [多租户安全与数据权限设计](./08-multitenancy-security.md) | 安全、架构、合规 |
| SPEC-09 | [实验设计与统计分析规范](./09-experiment-statistics.md) | 数据、算法、产品 |
| SPEC-10 | [P0 测试计划与发布清单](./10-p0-test-release.md) | QA、研发、产品、SRE |

## 共同约定

- 时间统一存 UTC，展示时按项目时区转换。
- 主键采用 UUIDv7 或等价的时序全局唯一 ID。
- 原始网页、原始模型响应和审计事件追加写，不原地覆盖。
- 派生数据带算法、配置和输入版本，可重算、可追溯。
- 所有跨边界调用携带 `trace_id`、`tenant_id`、`project_id` 和幂等键。
- 机密只存密钥管理系统引用，严禁进入日志、Prompt 或业务表。
- P0 不允许 Agent 自动修改或发布生产内容。

