# GEO Intelligence Engine 产品发展与技术演进建议（现状校准版）

> 版本：V1.0  
> 日期：2026-11-20  
> 文档性质：对外部建议书的分析、取舍与本地化执行建议；不替代 PRD、Feature Spec 或 ADR  
> 适用基线：仓库当前工作树及 V1.103 变更记录

## 1. 结论

建议保留“从 GEO 测量工具演进为企业级 GEO Intelligence Platform”的产品方向，但不照搬原建议书的模块和排期。

当前系统已经不是单纯的检测工具，也不是从零开始建设 Knowledge、Experiment、Monitoring 或 Citation 能力。仓库已形成如下纵向能力：

```text
品牌/项目 → 抓取与实体知识 → 问题与测量协议 → 多渠道审计
         → Judge/指标/引用 → Finding/报告 → 优化任务/草稿
         → 审批发布/回滚 → 复测与证据链 → 监控与告警
```

因此，下一阶段的核心不应是继续横向增加模块或 Agent 数量，而应是：

1. 把现有纵切补成可验证、可复现、可运营的闭环；
2. 将“实体记录”升级为带断言、来源、冲突和人工裁决的品牌知识资产；
3. 将“指标与引用”升级为统一测量立方体和可解释的 Citation Intelligence；
4. 在稳定的证据链和变更工作流上引入受控智能，而不是先建设七个独立 Agent；
5. 用真实客户试点验证价值，再决定图数据库、复杂 Agent 框架或微服务拆分。

一句话优先级：**先可靠、再统一；先断言与证据、再智能优化；先闭环复测、再扩大自动化。**

## 2. 对原建议书的判断

### 2.1 应直接吸收

| 建议 | 判断 | 本地化方式 |
|---|---|---|
| 保留 Go、React、TypeScript/Playwright、Python、MySQL | 接受 | 继续沿用现有技术栈，不做重构式迁移 |
| 不急于引入 Kafka、Kubernetes、Neo4j | 接受 | 先证明吞吐、查询和运维瓶颈，再做 ADR |
| 关键结果必须有来源和证据链 | 接受 | 以现有 evidence chain、页面快照、回答快照为基础统一证据模型 |
| AI 推断与人工确认事实分开存储 | 接受 | 在 Knowledge Assertion 中区分机器候选、人工确认、驳回和冲突 |
| 测量需冻结问题集、模型、渠道、地区、语言、Judge 等条件 | 接受 | 扩展现有 measurement protocol 与 capture provenance，形成统一测量维度 |
| Agent 不得未经批准修改生产内容 | 接受 | 沿用 Draft → Review → Approve → Publish → Retest → Rollback |
| 优化效果必须通过实验或可比复测验证 | 接受 | 复用 experiments 与 finding evidence chain，不另起平行体系 |
| 长期壁垒来自知识资产、测量数据集和优化经验 | 接受 | 将三类资产设为数据治理和版本策略的核心对象 |

### 2.2 应吸收方向，但需修正

| 原建议 | 需要修正之处 | 修正后的决策 |
|---|---|---|
| “优先建设 Brand Knowledge Graph” | 当前已具备实体、关系、来源证据、人工确认、合并/撤销、知识快照与 Diff；缺口不是“有没有图”，而是断言治理 | 优先建设 Knowledge Assertion 与冲突裁决层；继续用 MySQL 表达图关系，暂不引入图数据库 |
| “建设 Citation Intelligence” | 当前已有 citations、官网/第三方指标和引用可见度字段；仍缺来源身份归一、权威性、真实引用与推断来源的严格区分 | 先统一 Citation Observation 与 Source Registry，再建设来源网络分析 |
| “建设 GEO Experiment Center” | 当前已有 experiments、功效分析、渠道约束、基线/处理审计和证据链复测 | 从“建模块”改为“补完整实验协议、分组、混杂因素控制和复现报告” |
| “建设持续监控” | 当前已有 schedule、runner、signal evaluation、alerts、SMTP/Webhook 投递和 UI | 从“建监控”改为“建立基线、告警解释、去重、送达审计与规模化运行” |
| “建设七类 Agent” | 过早按角色拆 Agent 会造成重复上下文、责任边界不清和难以验收 | 先建设一个可审计的 Optimization Copilot，按工具能力分层；有独立伸缩或权限需求后再拆 Agent |
| “Recognition / Understanding / Trustworthiness / Recommendation 四指标” | 方向正确，但 Understanding 与 Trustworthiness 若无金标和证据协议，很容易变成不可复算的模型主观分数 | 保留四维画像，不计算未经验证的总分；每个维度定义分子、分母、Judge 版本、证据要求和不确定性 |

### 2.3 暂不采纳

- 不以“六个 Intelligence 模块全部出现于导航”作为产品完成标准。
- 不为每类 Agent 建独立微服务或独立运行时。
- 不立即引入 Neo4j；关系规模、查询模式和 MySQL 性能尚未证明存在必要性。
- 不直接新增原建议书列出的全部表；先复用现有 entities、citations、experiments、monitoring、content drafts、publish events 和 evidence chains，避免双模型并存。
- 不接受未经代码审计得出的固定工期。里程碑以验收证据和真实客户试点为退出条件。

## 3. 当前能力与真实缺口

| 领域 | 当前已有能力 | 主要缺口 | 建议优先级 |
|---|---|---|---|
| 工程可靠性 | 任务租约、预算预留/结算、重试、不可变快照、回归脚本、OIDC/PKCE 代码 | 真实 Keycloak Realm、MiniMax 配额下的双供应商验收、发布/恢复演练 | P0 |
| Knowledge Intelligence | JSON-LD/正文实体候选、别名、关系、来源证据、确认、合并/撤销、快照与 Diff | 断言级状态、来源等级、冲突聚合、时效性、人工裁决、知识版本语义 | P1-A |
| Search/Measurement Intelligence | 问题集、协议冻结、API 与捕获渠道、Judge、指标切片、Wilson 区间 | 统一 Brand × Query × Model × Channel × Time 观测模型；四维品牌画像的金标和复算协议 | P1-B |
| Citation Intelligence | 引用记录、官网/第三方引用率、位置与字数份额 | Source Registry、来源实体归一、权威性版本、引用出处真实性、竞品引用网络 | P1-B |
| Optimization Intelligence | Findings、优先级、任务、草稿、审批、发布、上一版本恢复 | 自动形成策略候选、Diff 质量门禁、风险分级、CMS/Git 连接器、复测触发 | P1-C |
| Experiment Intelligence | 实验记录、baseline/treatment audit、channel、功效分析、自动复测结论 | 对照组/处理组对象、协议冻结、样本分配、DiD/时间序列前提检查、可复现包 | P2 |
| Monitoring Intelligence | 定时审计、信号告警、份额下降/竞品超越/爬虫静默、通知投递 | 基线版本、告警解释、抑制/去重、项目级渠道、送达状态、容量与分页 | P1-C/P2 |

## 4. 建议的产品主线

不要把六类 Intelligence 作为六个平行项目。建议用一条证据闭环串联它们：

```text
Knowledge Assertions
        ↓
Measurement Observations ──→ Citation Observations
        ↓                         ↓
Explainable Findings ←──── Evidence Resolution
        ↓
Change Proposal → Human Approval → Publish/Rollback
        ↓
Comparable Retest / Experiment
        ↓
Verified Outcome → Strategy Memory → Monitoring Baseline
```

主线中的每个节点都必须回答四个问题：

1. 数据来自哪里？
2. 使用了什么版本的协议、模型、规则或 Judge？
3. 哪些是观测事实，哪些是系统推断，哪些经过人工确认？
4. 能否从最终结论回放到不可变原始证据？

## 5. 下一阶段实施建议

### M0：生产可信度收口（P0）

目标：证明现有测量闭环可供真实企业试用，而不是继续扩功能。

交付物：

- 真实 Keycloak Realm 的登录、刷新、登出、角色和租户隔离验收；
- DeepSeek 与 MiniMax 各完成至少一次受控真实 E2E，或对供应商不可用状态形成正式例外记录；
- 核心回归、数据库备份恢复、Worker 中断恢复、发布回滚演练；
- 指标独立复算、证据下钻和报告导出的一致性证明；
- 一份真实试点运行手册和已知限制清单。

退出条件：同一冻结协议能够重复执行，结果差异可解释；任何报告指标都能追溯到回答、Judge 和证据。

### M1：Knowledge Assertion（P1-A）

目标：在现有实体知识之上增加“可裁决的事实陈述”，而不是重写实体系统。

最小模型：

```text
KnowledgeAssertion
  subject_entity_id
  predicate
  object_entity_id | object_value
  source_id
  evidence_snapshot_id
  extraction_method
  confidence              # 抽取/证据质量，不是事实为真的概率
  status                  # candidate/confirmed/rejected/conflicted/superseded
  valid_from / valid_to
  first_seen_at / last_seen_at
  asserted_by / reviewed_by
  version
```

必须同步建设：

- Source Registry：官方、监管、学术、行业媒体、社区等来源身份和等级；
- Assertion 去重与冲突键：同一 subject/predicate 在同一有效期内出现不兼容 object；
- 人工裁决：确认、驳回、合并、标记过期、选择主张；
- 快照语义：知识快照冻结的是哪些 assertion 版本，而不只是当前实体行；
- 迁移兼容：已有 entity relationship 可渐进映射，不做一次性替换。

退出条件：任一品牌核心事实能显示原文、来源、状态、冲突、人工决策和版本历史。

### M2：统一测量与 Citation Intelligence（P1-B）

目标：把当前审计、捕获、Judge、指标和 citations 统一为可比较的观测模型。

统一维度至少包括：

```text
Brand × Query × Model × Channel × Time
× ModelVersion × SearchMode × Region × Language
× QuestionSetVersion × PromptTemplateVersion × SamplingPolicy × JudgeVersion
```

实施顺序：

1. 定义 Measurement Observation 的稳定标识和必填版本字段；
2. 建立 Citation Source Registry 和 URL/域名/组织归一；
3. 严格区分 `observed_citation`、`search_result`、`inferred_source`；
4. 定义 Recognition、Understanding、Trustworthiness、Recommendation 的评估协议和金标集；
5. 分维度展示，不在未验证权重下合成总分；
6. 建立品牌—问题—模型—引用来源的竞争矩阵与证据下钻。

退出条件：任意趋势变化能明确说明比较条件是否相同、引用是否真实观察到、指标如何复算。

### M3：受控 Optimization Copilot（P1-C）

目标：让 AI 参与分析和方案生成，但不绕过现有任务、审批、发布和回滚体系。

第一阶段只做一个编排器，提供以下工具能力：

- 读取 Knowledge Assertion、测量结果、引用来源和竞品差距；
- 生成带来源的策略候选；
- 生成内容或技术修改草稿及结构化 Diff；
- 对变更做风险分类和验收项建议；
- 经人工批准后调用既有发布连接器；
- 自动创建可比复测并把结果写回 evidence chain。

每次运行必须记录输入快照、模型/提示词/工具版本、预算、输出、人工决策、发布事件和复测结果。

只有当 Research、Content、Technical 等角色出现独立权限、独立伸缩或独立评测需求时，才拆成多个 Agent。

退出条件：一次优化能完整回放 Draft → Diff → Review → Approve → Publish → Retest → Rollback，且未经批准不能产生外部写入。

### M4：实验与持续运营（P2）

目标：把现有 experiments 和 monitoring 从纵切能力升级为长期数据壁垒。

重点不是新增页面，而是：

- 冻结实验协议、样本、渠道、模型、Judge 和观察窗口；
- 支持处理组/对照组和分配依据；
- 在适用前提满足时使用 Difference-in-Differences，并记录平行趋势等前提检查；
- 对小样本、模型版本变化、搜索索引变化给出不确定性和不可比原因；
- 将验证后的策略、适用条件和失败案例写入 Strategy Memory；
- 监控基线绑定协议版本，告警展示“为什么触发”和“与哪条基线比较”；
- 增加告警抑制、去重、升级、送达审计和容量控制。

退出条件：系统能回答“哪种优化策略在什么条件下、对什么指标产生了多大且多可靠的影响”。

## 6. 数据设计约束

新增模型前必须先做现有表复用评审。建议遵守以下约束：

1. 观测、推断、人工决策分层保存，不互相覆盖；
2. 原始证据和已发布快照不可变，修正通过新版本或 supersede 表达；
3. 所有指标必须具备定义版本、分子、分母、切片和不确定性；
4. 所有 Agent/Copilot 运行必须有输入输出快照和成本记录；
5. 所有外部写入必须有审批、幂等键、发布事件和可验证回滚；
6. 来源权威性是带版本的业务判断，不是永久常量；
7. `confidence` 必须注明含义，禁止混用抽取置信度、证据质量和事实概率；
8. 跨时间比较必须先通过可比性检查；不可比时返回原因，不生成误导性趋势；
9. 知识、测量、策略三类资产都必须支持租户隔离、导出和删除策略；
10. 任何新表都要给出与现有实体、引用、实验、监控和证据链表的映射，避免双写失控。

## 7. 导航建议

原建议书的信息架构可作为长期目标，但当前只建议渐进归类，不进行前端大改：

| 一级域 | 复用当前页面/能力 | 后续增加 |
|---|---|---|
| Workspace | Overview、Projects、Brands、Competitors | 多品牌工作区视图 |
| Intelligence | Entities、Competitive、Citations | Assertions、Source Registry、Knowledge Conflicts |
| Measurement | Questions、Protocols、Audits、Reviews、Metrics | 四维品牌画像、测量可比性 |
| Optimization | Findings、Tasks、Drafts、Publish | Copilot Runs、Strategy Memory |
| Experiment | Experiments、Evidence Chain Retest | Groups、Protocol Freeze、Reproduction |
| Monitoring | Schedules、Alerts、Trends | Baselines、Delivery Audit、Escalation |
| Reports/Admin | Reports、Members、Providers、Audit Logs | 资产导出与治理策略 |

导航名称应跟随已经通过验收的能力，不以空页面占位。

## 8. 决策门槛

以下技术升级只有满足门槛后才进入 ADR：

| 技术选择 | 触发门槛 |
|---|---|
| Neo4j/图数据库 | MySQL 无法在目标 SLA 内完成已确定的多跳查询，且查询负载由真实客户数据证明 |
| Kafka/事件总线 | 数据库任务队列无法满足可量化吞吐、重放或跨服务解耦需求 |
| Kubernetes | 单机/Compose/现有部署方式无法满足明确的可用性、弹性或租户隔离目标 |
| Agent 微服务拆分 | 出现独立权限边界、独立扩缩容、故障隔离或不同发布周期 |
| 向量数据库 | MySQL/现有方案无法满足经过基准测试的召回率、延迟或数据规模要求 |

## 9. 验收与经营指标

避免以功能数量衡量进展。建议每个里程碑至少跟踪：

- 可追溯率：报告结论中可回放到原始证据的比例；
- 可复算率：指标独立复算一致的比例；
- 可比率：跨期观测中通过协议可比性检查的比例；
- 裁决质量：Knowledge Assertion 冲突关闭时间与复核一致率；
- 优化闭环率：已批准变更中完成发布、复测和结论写回的比例；
- 有效率：通过可比复测或实验确认改善的优化比例；
- 回滚可靠性：发布失败或回滚演练成功率；
- 告警质量：有效告警率、重复率、送达率和平均处理时间；
- 单次审计成本、单次已验证优化成本和真实客户节省的人工时间。

## 10. 建议立即执行的三件事

1. **先完成 M0 验收清单**：真实身份、双供应商或正式例外、恢复演练、指标复算和试点手册。
2. **为 Knowledge Assertion 编写 Feature Spec 和迁移映射**：明确如何复用现有 entities、relationships、snapshots 和 evidence，不先写代码。
3. **建立统一观测字段盘点**：把 audits、samples、captures、evaluations、metrics、citations 中的维度列成数据字典，找出无法比较或版本缺失的字段。

在以上三项完成前，不建议启动新的 Agent 角色、图数据库或大规模导航重构。

## 11. 依据与证据边界

本建议基于以下本地材料校准：

- `README.md` 中当前能力说明；
- `docs/FEATURE-PLAN.md`、V1.99–V1.103 变更记录；
- `db/migrations/000027`–`000039` 的数据结构；
- `internal/entities`、`internal/experiments`、`internal/monitoring`、`internal/evidencechain`、`internal/audits/citations.go`；
- GitNexus 对 evidence chain、experiments、monitoring 的流程和符号查询。

证据限制：GitNexus 索引显示比当前 HEAD 落后 9 个提交。已两次尝试使用项目 runner 刷新，进程均长时间无输出且索引文件未更新，因此图谱结果只用于确认既有调用关系，不用于否定最近 9 个提交中的能力；最新状态以当前源码、迁移、README 和变更记录为准。

