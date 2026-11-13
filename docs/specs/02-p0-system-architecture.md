# SPEC-02：P0 系统架构设计

> 版本：V1.0｜状态：研发评审稿｜架构形态：模块化单体 + 独立 Worker

## 1. 架构目标

P0 以最小复杂度交付可信 GEO Audit，同时保证抓取、模型调用和评估可异步扩展。首版不采用微服务拆分；通过清晰模块边界、队列和 Outbox 保留未来拆分能力。

## 2. 逻辑架构

```text
React Web
   │ HTTPS
API/BFF（Go）
   ├── Identity & RBAC
   ├── Project/Brand
   ├── Audit Orchestrator
   ├── Reporting
   └── Integration Admin
        │
        ├── MySQL（事务、索引、状态）
        ├── Redis（队列、缓存、限流、锁）
        ├── S3（HTML、响应、报告、快照）
        └── Outbox/Event Dispatcher
                 │
       ┌─────────┼──────────┐
       │         │          │
 Crawler Worker  Model Worker  Judge Worker
 Playwright       Gateway       Python/Go
       │         │          │
  Public Web   Model APIs    Metrics Builder
```

## 3. 部署单元

| 单元 | 技术 | 职责 | 扩展方式 |
|---|---|---|---|
| `web` | React + TypeScript | 管理后台和报告 | CDN/静态部署 |
| `api` | Go | 同步 API、鉴权、业务事务 | 无状态水平扩展 |
| `scheduler` | Go | 定时任务和审计展开 | 单活租约或分片 |
| `crawler-worker` | Go + Playwright | URL 获取、渲染、解析 | 按域名限流水平扩展 |
| `model-worker` | Go/Python | 模型调用、预算、响应持久化 | 按供应商限流扩展 |
| `judge-worker` | Python/Go | 判定、重评估、指标聚合 | 按评估版本扩展 |
| `report-worker` | Go | PDF/CSV 和快照 | 队列扩展 |

## 4. 模块边界

- Identity：用户、组织、成员和会话。
- Project：项目、品牌、竞品、站点和协议配置。
- Discovery：抓取运行、页面、内容和技术规则。
- Knowledge：实体、关系、Schema 和人工确认。
- Question：问题集、版本、聚类和快照。
- Audit：审计、采样计划、状态与成本。
- Model Gateway：供应商适配、限流、调用和原始响应。
- Judge：有效性、提及、推荐、情感和引用。
- Metrics：指标事实表、区间和切片。
- Reporting：报告版本、导出和证据下钻。
- Task：优化任务，不含 P0 自动发布。
- Governance：审计日志、配额、保留和删除。

模块只能通过公开应用服务或事件交互，不允许直接修改其他模块拥有的表。

## 5. 关键数据流

### 5.1 抓取

API 创建 `crawl_run` 与 Outbox 事件 → Scheduler 展开 URL → Crawler 获取并把大对象写 S3 → 事务写页面元数据与解析状态 → 规则引擎产生发现项 → API 查询进度。

### 5.2 模型审计

API 冻结协议和问题快照 → Scheduler 生成采样单元 → Model Worker 预算预留、调用、保存原始响应 → Judge Worker 评估 → Metrics Builder 在完整性门槛满足后聚合 → Report Worker 发布版本化报告。

### 5.3 重评估

新 Judge 版本读取已有原始响应 → 写入新的 `evaluation_run` 和结果 → 生成新指标版本；不得覆盖旧评估。

## 6. 一致性与事件

- 业务事务和 Outbox 在同一 MySQL 事务中提交。
- Dispatcher 至少一次投递；消费者通过 `event_id` 幂等。
- Redis 不作为事实源；任务状态以 MySQL 为准。
- 聚合采用最终一致性，报告发布前检查完整性门槛。
- 分布式锁只用于减少重复工作，正确性依靠唯一约束和状态条件更新。

## 7. 缓存策略

- 项目配置和权限短时缓存，权限变更主动失效。
- 抓取结果按规范 URL、内容协商和授权范围缓存。
- 模型响应默认不跨租户复用；相同租户相同协议可选显式复用并记录来源。
- 指标查询缓存键包含租户、项目、协议、评估和筛选哈希。

## 8. 可用性与降级

- 模型供应商异常：熔断该供应商，其他供应商继续，审计为 `partial_success`。
- Playwright 异常：保留静态解析结果并标记渲染失败。
- Judge 不可用：原始响应先落库，稍后补评估。
- 对象存储短暂异常：任务重试，不把未保存正文标记为成功。
- 报告生成失败：不影响已完成的审计数据，可单独重试。

## 9. 网络与安全边界

- API 可访问数据库、Redis、对象存储和密钥管理服务。
- Crawler 位于隔离网络，只允许出站公网和受控对象存储写入，禁止访问内网和云元数据。
- Worker 使用独立服务身份和最小权限。
- 所有内部调用使用服务身份认证；生产环境不信任仅靠网络位置的请求。
- 对象存储默认私有，通过短时签名 URL 访问。

## 10. 可观测性

统一 OpenTelemetry Trace；结构化日志包含 `trace_id`、`tenant_id`、`project_id`、`job_id`、`attempt_id`，正文和密钥不得写日志。

核心指标：API P95、队列深度、任务年龄、抓取成功率、域名阻断率、供应商错误率、调用延迟、Token/成本、Judge 低置信率、报告完整率、租户配额使用率。

## 11. 容量基线

- 单项目 10,000 页面、1,000 问题、20,000 响应/审计。
- API P95 ≤ 500 ms，报告普通查询 P95 ≤ 2 秒。
- Worker 通过队列积压自动扩容，但必须服从全局和租户预算。
- 容量测试以 10 个并发审计和 100 个并发用户为 P0 最低场景，正式目标由试点校准。

## 12. 技术决策

| 决策 | P0 选择 | 原因 |
|---|---|---|
| 服务形态 | 模块化单体 | 降低部署和事务复杂度 |
| 主数据库 | MySQL 8 | 事务、成熟运维、关系模型足够 |
| 队列 | Redis Streams/可靠队列封装 | 与缓存复用基础设施，必须以 MySQL 状态兜底 |
| 图谱 | MySQL 邻接表 | P0 关系规模有限 |
| 大对象 | S3 兼容存储 | 避免数据库膨胀 |
| 部署 | Docker | 环境一致、便于隔离 Worker |

## 13. 架构验收

- 任一 Worker 重启不会丢失已确认任务或产生重复业务结果。
- 单供应商、Judge 或报告组件故障不会破坏已保存原始数据。
- Crawler 无法访问 RFC1918、loopback、链路本地和元数据地址。
- 通过 Trace 可从报告结论追踪到原始响应和执行尝试。

