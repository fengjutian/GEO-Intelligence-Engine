# GEO Intelligence P0 功能规格集

> 文档集版本：V1.0  
> 对齐产品版本：P0 / V1.0  
> 状态：已批准  
> 日期：2026-11-13

本目录将 [PRD V1.0](../product-requirements.md) 拆分为可独立设计、开发、测试和验收的功能文档。技术约束来自 [技术栈 V1.0](../technology-stack.md) 和 [P0 设计规范集 V1.0](../specs/README.md)。

## 1. 版本对齐规则

| 文档层级 | 当前版本 | 变更规则 |
|---|---:|---|
| 产品 PRD | V1.0 | 产品范围或需求语义变化时升级 |
| 技术原理 | V1.0 | 核心测量或技术原理变化时升级 |
| 技术栈 | V1.0 | 主要语言、框架或基础设施变化时升级 |
| 设计规范 SPEC | V1.0 | 跨功能技术契约变化时升级 |
| 功能规格 FEAT | V1.0 | 功能行为、接口、数据或验收变化时升级 |

功能文档必须在头部声明所对齐的 PRD、SPEC 和技术栈版本。仅修正错别字可增加修订日期而不升级版本；影响实现或验收的变化至少升级次版本，并更新本索引的兼容矩阵。

## 2. 功能清单

| 编号 | 功能文档 | PRD 需求 | 主要交付物 |
|---|---|---|---|
| FEAT-01 | [组织、租户与权限](./01-tenant-identity-rbac.md) | P0 横切要求 | 登录上下文、成员、RBAC、审计日志 |
| FEAT-02 | [项目、品牌与竞品档案](./02-project-brand-profile.md) | PRJ-001～005 | 项目向导、品牌/产品/竞品配置 |
| FEAT-03 | [网站抓取与技术审计](./03-website-crawl-audit.md) | DIS-001～006 | Crawl Run、页面内容、技术 Findings |
| FEAT-04 | [实体知识与一致性](./04-entity-knowledge.md) | ENT-001～005 | 实体、关系、Schema、冲突 |
| FEAT-05 | [买家问题与问题空间](./05-buyer-question-space.md) | QRY-001～003 | 问题生成、导入、版本、聚类 |
| FEAT-06 | [测量协议与审计编排](./06-audit-orchestration.md) | MOD-002～005 | 协议冻结、采样计划、审计状态 |
| FEAT-07 | [Model Gateway 与预算](./07-model-gateway-budget.md) | MOD-001、002、003、004、006 | 供应商适配、成本、限流、幂等 |
| FEAT-08 | [Answer Judge 与人工复核](./08-answer-judge-review.md) | MOD-005、JDG-001～007 | 有效性、提及、推荐、情感、引用 |
| FEAT-09 | [可见度指标与分析](./09-visibility-metrics.md) | MET-001～006 | 指标、区间、切片和证据集合 |
| FEAT-10 | [内容结构与证据质量](./10-content-quality.md) | CON-001～002 | Answer First、结构和证据 Findings |
| FEAT-11 | [GEO Audit 报告与导出](./11-audit-reporting.md) | RPT-001～004 | 报告、证据下钻、PDF/CSV |
| FEAT-12 | [优化任务管理](./12-optimization-task.md) | TSK-001～002 | 从 Finding 建任务、状态机和审计 |

## 3. 需求覆盖

P0 功能需求共 46 条，全部由上述 Feature Spec 覆盖。MOD-002～005 同时涉及审计编排、Gateway 和 Judge，分别按“业务编排”“供应商调用”“回答判定”职责拆分；验收时由 FEAT-06 的端到端流程统一串联。

非功能需求不重复复制到每个 Feature Spec，统一继承：

- [SPEC-07 任务状态与失败恢复](../specs/07-job-state-and-recovery.md)
- [SPEC-08 多租户安全与数据权限](../specs/08-multitenancy-security.md)
- [SPEC-10 P0 测试计划与发布清单](../specs/10-p0-test-release.md)

## 4. 功能完成定义

每个功能只有同时满足以下条件才可标记 Done：

1. 对应 PRD Must 需求全部实现。
2. 数据迁移、API 契约和权限策略完成评审。
3. 实现细节中列出的失败路径得到处理。
4. 单元、集成、权限和端到端测试通过。
5. 验收标准具有可复现证据。
6. Trace、Metric、结构化日志和审计事件可用。
7. 用户文案、空状态、错误状态和可访问性完成。
8. 文档版本、代码版本和迁移版本可追踪。

## 5. 推荐开发顺序

```text
FEAT-01 → FEAT-02 → FEAT-03 → FEAT-04 → FEAT-05
                         ↓
FEAT-07 → FEAT-06 → FEAT-08 → FEAT-09 → FEAT-11
                                      ↘ FEAT-10
                                            ↓
                                         FEAT-12
```

FEAT-01、02 和公共任务框架是所有功能的基础；FEAT-07 可与抓取功能并行开发；报告必须在指标和内容分析稳定后验收。

