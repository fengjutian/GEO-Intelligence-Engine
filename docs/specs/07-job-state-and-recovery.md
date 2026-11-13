# SPEC-07：任务状态机与失败恢复设计

> 版本：V1.0｜状态：研发评审稿｜适用：抓取、模型采样、评估、聚合与报告任务

## 1. 设计目标

任务系统采用至少一次投递，业务处理必须幂等。MySQL 是状态事实源，Redis/队列只负责调度。任何失败都应回答：执行到哪一步、是否产生外部副作用、能否安全重试、由谁恢复。

## 2. 层级模型

```text
Workflow（一次审计/抓取）
  └─ Stage（发现、采集、评估、聚合、报告）
      └─ Job（一个可调度工作单元）
          └─ Attempt（一次执行尝试）
```

Workflow 汇总状态，不直接执行工作。Attempt 只追加写，Job 保存当前状态和租约。

## 3. 通用 Job 状态机

```text
draft → queued → leased → running → succeeded
                   │        ├→ retry_wait → queued
                   │        ├→ failed
                   │        └→ cancelled
                   └→ queued（租约过期回收）

queued/running/retry_wait → cancelling → cancelled
```

终态：`succeeded`、`failed`、`cancelled`。`dead_lettered` 是 failed 的运维分类，不是业务成功状态。

### 3.1 合法转换

状态转换使用条件更新：`WHERE id=? AND state=? AND version=?`。不合法转换返回冲突并记录事件。完成写入业务结果和状态转移必须位于同一事务，外部对象存储使用写后校验和补偿。

## 4. Workflow 状态

`draft → queued → running → succeeded | partial_success | failed | cancelled`

- `succeeded`：所有必需阶段满足完整性门槛。
- `partial_success`：存在不可恢复子任务失败，但足以生成带缺失披露的结果。
- `failed`：关键阶段未完成或数据完整性不足。
- `cancelled`：用户/系统终止，已完成证据保留但不进入正式趋势。

## 5. 领取与租约

Worker 原子领取 `queued` Job，写入 `lease_owner`、`lease_expires_at` 和 `attempt_id`。执行期间每 30 秒续租；租期默认 2 分钟。租约过期由 Reaper 回收为 `queued`，但外部副作用任务必须先进入对账流程。

Worker 时钟偏差不参与正确性判断；租约时间由数据库生成。

## 6. 幂等键

| 任务 | 幂等键示例 |
|---|---|
| URL 抓取 | `crawl_run + normalized_url_hash` |
| 模型采样 | `audit + protocol + question + engine + dimensions + sample_no` |
| Judge | `response + judge_version` |
| 指标聚合 | `audit + evaluation_run + metric_version` |
| 报告 | `audit + metric_snapshot + report_template_version` |
| Webhook | `event_id + endpoint_id` |

数据库唯一约束是最终防线。重复执行返回已有结果，不重复创建业务记录。

## 7. 重试分类

| 分类 | 示例 | 策略 |
|---|---|---|
| transient | 网络超时、429、临时 5xx | 指数退避 + 抖动，有限次数 |
| dependency_down | 数据库/对象存储不可用 | 熔断并延后，不快速消耗次数 |
| validation | 参数、权限、Schema 错误 | 不重试，直接失败 |
| policy | robots、预算、内容政策 | 不重试，记录业务结果 |
| unknown_outcome | 外部调用可能成功但未确认 | 对账，禁止盲重试 |
| bug | panic、不可预期异常 | 少量重试后死信并告警 |

退避默认 `min(5s × 2^n + jitter, 30m)`，各适配器可收紧，不得无限重试。

## 8. 外部副作用与对账

模型调用、Webhook 和未来 GitHub 操作可能在超时前已经成功。每次调用在发送前持久化 `attempt` 和客户端请求 ID，收到供应商 ID 后立即保存。结果未知时：

1. 查询供应商状态或按请求 ID检索。
2. 检查本地对象存储是否已有响应。
3. 可确认失败才允许重试。
4. 无法确认则标记 `outcome_unknown`，进入人工/定时对账，不自动重复计费操作。

## 9. Outbox/Inbox

业务事务同时写 Outbox。Dispatcher 投递后记录 attempt；消费者使用 Inbox `(consumer,event_id)` 唯一约束去重。事件至少包含版本、租户、资源 ID、发生时间和 trace，不携带大正文。

## 10. 取消与暂停

- `cancel` 是持久化意图，不仅是发送信号。
- 未开始 Job 直接取消；运行中 Worker 在安全检查点停止。
- 已发送的不可撤销模型调用允许完成并保存，但结果不再进入已取消 Workflow 指标。
- 暂停阻止新 Job 领取，不中断正在进行的短事务。
- 恢复从未完成阶段继续，不重新创建已成功 Job。

## 11. 各阶段恢复点

- 抓取：每个 URL 独立提交；浏览器崩溃只重跑当前 URL。
- 模型：每个 sample 独立；预算预留与结算可对账。
- Judge：原始响应落库后可任意重跑指定版本。
- 聚合：写临时 snapshot，全部校验后原子发布。
- 报告：对象先写临时 key，校验成功后更新报告引用。

## 12. 死信与人工处置

超过最大尝试、不可恢复 Bug 或未知结果进入死信视图。处置动作：查看脱敏上下文、重新排队、标记不可恢复、跳过并允许部分成功、关联缺陷。每个动作要求原因和审计记录。

## 13. 数据修复

禁止直接改生产表解决普通任务问题。修复使用版本化 Repair Job，包含目标范围、Dry Run、前置条件、变更摘要和可逆策略。大规模修复先在影子数据验证。

## 14. 监控与告警

监控队列深度、最老任务年龄、租约过期、各错误分类、重试放大率、死信数、取消延迟和 Workflow 完成时间。未知结果、同一 Job 重复外部收费、租约连续过期立即告警。

## 15. 验收

- 在任意持久化点杀死 Worker，恢复后不丢数据、不重复业务结果。
- 重复投递同一事件 100 次只产生一个业务结果。
- 模型调用成功但数据库确认失败的故障注入不会盲目二次调用。
- 取消、暂停、租约过期、死信重放和部分成功均有端到端测试。

