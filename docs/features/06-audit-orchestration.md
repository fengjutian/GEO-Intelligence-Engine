# FEAT-06：测量协议与审计编排

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：已批准  
> 层级：P0-C｜判定与排期见 [功能优先级规划](../FEATURE-PLAN.md)  
> 需求：MOD-002～MOD-005｜对齐：SPEC-01、03、07、09 V1.0

## 1. 功能目标

冻结问题、品牌知识和模型配置，展开确定性的采样计划，可靠执行多模型审计，并在取消、部分失败或 Worker 重启后保持可恢复、可追踪和可复算。

## 2. 审计前置条件

- 项目处于 active。
- 存在已发布品牌配置和问题集版本。
- 至少一个 active Engine Config。
- 协议包含渠道、模型、语言、地区、参数和采样次数。
- 成本估算未超过硬预算。
- 用户具有 run 权限。

## 3. 协议内容

协议冻结：问题集版本、知识快照、引擎/模型选择、渠道、语言、地区、时区、Prompt 模板、模型参数、采样次数、有效性规则、Judge 版本、指标版本、预算和停止规则。

发布后的协议只读；任何变化创建新语义版本。

## 4. 审计状态机

```text
draft → estimating → ready → queued → running
                                  ├→ succeeded
                                  ├→ partial_success
                                  ├→ failed
                                  └→ cancelled
```

用户可从 queued/running 请求取消。运行状态根据必需阶段和完整性门槛汇总，不能仅由最后一个 Worker 覆盖。

## 5. 采样展开

采样单元：`question_version × engine × model × channel × region × language × parameters × sample_no`。生成确定性 `sample_key` 并建立唯一约束。

Scheduler 分批展开，避免一次事务创建过多记录。调用顺序在协议窗口内随机化，随机种子保存以降低时间偏差并支持复现。

## 6. 数据模型

- `measurement_protocols`、`protocol_engines`。
- `audits`：快照引用、计划/完成/有效数、预算和状态。
- `audit_samples`：维度、sample_key、状态和重试信息。
- `jobs`、`job_attempts`、`outbox_events`、`inbox_events`。
- 后续关联 `ai_requests`、`ai_responses`、`evaluation_runs` 和 `metric_snapshots`。

## 7. API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/projects/{id}/measurement-protocols` | 创建草稿 |
| POST | `/api/v1/measurement-protocols/{id}:publish` | 校验并发布 |
| POST | `/api/v1/audits:estimate` | 返回采样量、费用和预计时间 |
| POST | `/api/v1/audits` | 创建并排队审计 |
| GET | `/api/v1/audits/{id}` | 状态、阶段和完整性 |
| POST | `/api/v1/audits/{id}:cancel` | 请求取消 |
| POST | `/api/v1/audits/{id}:resume` | 从可恢复状态继续 |
| GET | `/api/v1/audits/{id}/samples` | 采样明细 |

创建必须使用 Idempotency-Key；相同键不同请求哈希返回冲突。

## 8. 实现细节

1. API 在单一事务中保存 Audit、协议引用和 Outbox 事件。
2. Scheduler 使用游标分批创建 Sample，并由唯一 sample_key 去重。
3. 每个阶段使用 Workflow/Stage/Job/Attempt 层级；MySQL 为事实源。
4. Model Worker 完成后触发 Judge；Judge 达到门槛后触发指标；聚合完成后触发报告。
5. Gateway 提供 `transport_status`，Judge 提供 `content_validity` 和 `answer_outcome`，资格规则产生 `eligible_for_core_metrics`；四者分开保存。
6. 取消是持久状态；已发出的外部调用可保存结果，但不进入取消审计的正式指标。
7. 断点恢复扫描未完成 Sample 和过期租约，不重建成功 Sample。
8. 部分失败按引擎、问题和切片计算完整性；低于协议门槛不得发布正式报告。
9. 协议、问题、知识和引擎配置都保存快照哈希，开始后不读取可变当前配置。

## 9. UI

- 协议编辑器按问题、引擎、地域、参数、采样和预算分步配置。
- 启动确认页显示采样矩阵、预计费用、预计时长和渠道警告。
- 审计进度显示各阶段、成功/有效/失败数量和供应商状态。
- 样本页支持按问题、引擎、状态和错误分类筛选。
- 取消操作说明已发生费用不会退回，并要求确认。

## 10. 异常与恢复

- 重复创建请求：返回同一 Audit。
- Scheduler 中断：重新启动后继续未展开范围。
- Worker 租约过期：回收前检查外部副作用状态。
- 某供应商不可用：其他引擎继续，最终可能 partial_success。
- Judge 延迟：原始回答已保存，审计保持 running/evaluating。
- 预算耗尽：停止新调用，已完成样本保留，状态按完整性判断。

## 11. 验收标准

- AC-01：协议准确保存模型、参数、渠道、地区、语言、采样和所有算法版本。
- AC-02：相同输入产生确定且唯一的 Sample 集合，重复提交不产生重复样本。
- AC-03：队列、限流、超时、退避、取消和断点恢复均可通过故障注入验证。
- AC-04：模型 API、搜索 API 和消费者应用渠道在协议、样本、UI 和报告中始终分离。
- AC-05：传输状态、内容有效性、回答结果和指标资格正交保存，分母按 SPEC-01 资格规则计算。
- AC-06：任一当前配置变更不影响运行中或历史审计。
- AC-07：单供应商故障不破坏其他供应商结果，并正确生成 partial_success 或 failed。
- AC-08：取消后不再创建新外部调用，已发生调用结果保留但不生成正式趋势点。

## 12. 需求追踪

| 需求 | 验收覆盖 |
|---|---|
| MOD-002 | AC-01、06 |
| MOD-003 | AC-04 |
| MOD-004 | AC-02、03、07、08 |
| MOD-005 | AC-05 |

