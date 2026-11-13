# SPEC-03：核心数据模型与数据库设计

> 版本：V1.0｜状态：已批准｜数据库：MySQL 8.4 LTS

## 1. 建模原则

- 多租户优先：所有业务数据直接或间接归属 `tenant_id`。
- 原始证据追加写，派生结果版本化。
- 配置快照与当前配置分离，历史审计不随项目编辑改变。
- 大对象进入 S3，MySQL 保存元数据、哈希和引用。
- 外键、唯一约束和条件更新共同保证正确性。
- 业务删除默认软删除，受控清除任务执行物理删除。

## 2. 标识与公共字段

主键使用 UUIDv7，数据库类型 `BINARY(16)`；API 使用规范字符串表示。公共字段：

```text
id, tenant_id, created_at, updated_at, created_by,
version, deleted_at（需要软删除时）
```

时间为 UTC `DATETIME(6)`。JSON 只用于供应商扩展或不可预测属性；可查询核心字段必须结构化。

## 3. 实体关系总览

```text
tenants ─< memberships >─ users
   │
   └─< projects ─< brands ─< brand_aliases
          ├─< websites ─< crawl_runs ─< website_pages ─< page_contents
          ├─< entities ─< entity_relations
          ├─< question_sets ─< question_set_versions ─< questions
          ├─< audits ─< audit_samples ─1 ai_responses
          │                         └─< answer_evaluations ─< mentions/citations
          ├─< metric_snapshots ─< metric_values
          ├─< findings ─< optimization_tasks
          └─< reports
```

## 4. 租户与权限表

| 表 | 关键字段/约束 |
|---|---|
| `tenants` | `name`, `status`, `plan_code`, `data_region`; 名称不要求全局唯一 |
| `users` | `email_normalized` 全局唯一、`status`, `mfa_state` |
| `memberships` | `(tenant_id,user_id)` 唯一，`role`, `status` |
| `project_memberships` | `(tenant_id,project_id,user_id)` 唯一，项目范围和可选权限覆盖 |
| `service_accounts` | `tenant_id`, `name`, `status`, `credential_ref` |
| `api_tokens` | `principal_type/id`, `token_hash`, `scopes`, `expires_at`, `last_used_at` |
| `invitations` | `tenant_id`, `email_normalized`, `role`, `token_hash`, `expires_at`, `status` |

禁止保存明文 Token。成员和服务账号权限变更写审计日志并主动失效缓存。

## 5. 项目与品牌表

| 表 | 关键字段/约束 |
|---|---|
| `projects` | `(tenant_id,name)` 条件唯一，`timezone`, `default_locale`, `status` |
| `project_config_versions` | `project_id`, `version`, `snapshot_json`, `content_hash`, `published_at`; 发布后只读 |
| `brands` | `project_id`, `canonical_name`, `official_domain`, `entity_id` |
| `brand_aliases` | `brand_id`, `alias_normalized`, `alias_type`, `enabled`; `(brand_id,alias_normalized)` 唯一 |
| `products` | `brand_id`, `name`, `status`, `source_entity_id` |
| `brand_context_items` | `brand_id`, `item_type`, `value`, `source_type/id`, `verification_status` |
| `competitors` | `project_id`, `name`, `domain`, `entity_id` |
| `competitor_aliases` | `competitor_id`, `alias`, `alias_normalized`, `enabled` |
| `competitor_products` | `competitor_id`, `name`, `mapped_product_id`, `status` |
| `websites` | `project_id`, `base_url`, `normalized_host`, `ownership_status`, `crawl_policy` |

## 6. 抓取与内容表

| 表 | 关键字段/约束 |
|---|---|
| `crawl_runs` | `website_id`, `config_snapshot`, `status`, 计数器、开始/结束时间 |
| `crawl_urls` | `crawl_run_id`, `normalized_url_hash`, `state`, `depth`, `priority`; 二者唯一 |
| `fetch_attempts` | `crawl_url_id`, `attempt_no`, 请求/响应元数据、错误码 |
| `website_pages` | `website_id`, `canonical_url`, `normalized_url_hash`, `latest_content_id` |
| `page_contents` | `page_id`, `crawl_run_id`, `content_hash`, S3 refs、标题、正文摘要、解析版本 |
| `page_links` | `source_content_id`, `target_url`, `target_page_id`, `rel`, `anchor_text` |
| `structured_data_items` | `content_id`, `schema_type`, `payload_ref`, `validation_status` |
| `findings` | `project_id`, `source_type/id`, `rule_code`, `severity`, `evidence`, `status` |
| `content_analysis_runs` | `crawl_run_id`, `analyzer_version`, `config_hash`, `status` |
| `content_blocks` | `page_content_id`, `block_type`, `text_hash`, `locator`, `position` |
| `content_signals` | `analysis_run_id`, `page_content_id`, `rule_code`, `value_json`, `confidence` |
| `evidence_references` | `finding_id`, `source_type/id`, `locator`, `quote`, `content_hash` |

同一页面内容哈希未变化时可复用解析产物，但必须记录本次 fetch 和复用来源。

## 7. 实体与关系表

| 表 | 关键字段/约束 |
|---|---|
| `entities` | `project_id`, `entity_type`, `canonical_name`, `normalized_name`, `status` |
| `entity_aliases` | `entity_id`, `alias`, `locale`, `source_id`, `confidence` |
| `entity_relations` | `subject_id`, `predicate`, `object_id`, `status`, `confidence` |
| `entity_evidence` | `entity/relation_id`, `source_type/id`, `quote`, `locator`, `extractor_version` |
| `entity_merge_events` | 合并前后 ID、操作者、原因、可撤销状态 |
| `entity_candidates` | `extraction_run_id`, `candidate_json`, `match_entity_id`, `confidence`, `status` |
| `entity_extraction_runs` | `crawl_run_id`, `extractor_version`, `config_hash`, `status` |
| `knowledge_snapshots` | `project_id`, `version`, `content_hash`, `snapshot_ref`, `published_at` |

人工确认字段与机器推断分开保存；自动任务不得覆盖 `human_verified` 结论。

## 8. 问题与协议表

| 表 | 关键字段/约束 |
|---|---|
| `question_sets` | `project_id`, `name`, `status` |
| `question_set_versions` | `question_set_id`, `version`, `content_hash`, `published_at`; 版本唯一 |
| `questions` | `version_id`, `text`, `text_hash`, `intent`, `funnel_stage`, `weight`, `status` |
| `question_relations` | `parent_question_id`, `child_question_id`, `relation_type` |
| `query_clusters` | `question_version_id`, `name`, `representative_question_id`, `algorithm_version`, `locked` |
| `cluster_members` | `cluster_id`, `question_id`, `similarity`, `manually_locked` |
| `question_generation_runs` | `project_id`, `knowledge_snapshot_id`, `generator_version`, `status`, `config_hash` |
| `measurement_protocols` | `project_id`, `semantic_version`, `config_json`, `config_hash`, `status` |
| `protocol_engines` | `protocol_id`, `engine_config_id`, `model_selector`, `channel`, 参数 |

发布后的问题集版本和协议仅可读。

## 9. 审计、模型与评估表

| 表 | 关键字段/约束 |
|---|---|
| `audits` | `project_id`, `protocol_id`, `question_version_id`, `status`, 计划/完成/有效数、预算 |
| `audit_samples` | `audit_id`, 维度快照、`sample_no`, `sample_key`, `status`; `sample_key` 唯一 |
| `engine_configs` | `tenant_id`, `provider`, `channel`, `credential_ref`, `region`, `limits_json`, `status` |
| `model_catalog` | `provider`, `model_id`, `capabilities_json`, `pricing_version`, `status` |
| `model_alias_versions` | `alias`, `provider`, `resolved_model`, `version`, `effective_at` |
| `ai_requests` | `sample_id`, `provider`, `request_ref`, `request_hash`, 参数、发送时间 |
| `ai_responses` | `sample_id`, `raw_ref`, `raw_hash`, `provider_request_id`, 模型、usage、cost、finish_reason、`transport_status` |
| `provider_attempts` | `request_id`, `attempt_no`, `state`, `provider_request_id`, 错误和时间 |
| `evaluation_runs` | `audit_id`, `judge_version`, `validity_version`, `status`, `config_hash` |
| `answer_evaluations` | `response_id`, `evaluation_run_id`, `content_validity`, `answer_outcome`, `eligible_for_core_metrics`, `eligibility_rule_version`, 推荐、情感、置信度；组合唯一 |
| `brand_mentions` | `evaluation_id`, `entity_id`, offsets、类别、置信度、证据 |
| `citations` | `evaluation_id`, 原始/规范 URL、域名、来源类型、支持关系、置信度 |
| `review_queue_items` | `evaluation_id`, `reason_code`, `priority`, `status`, `assignee_id` |
| `human_annotations` | `review_item_id`, `annotator_id`, `labels_json`, `reason`, `created_at` |
| `adjudications` | `review_item_id`, `adjudicator_id`, `result_json`, `reason` |
| `budget_accounts` | 租户/项目/审计作用域、货币、软硬限额和周期 |
| `budget_reservations` | `budget_account_id`, `request_id`, 预留金额、状态和过期时间 |
| `cost_ledger` | `budget_account_id`, `request_id`, 预留/结算/释放流水和价格版本 |
| `provider_health_events` | `engine_config_id`, `event_type`, `observed_at`, `details_json` |

原始请求和响应不可变；供应商返回中可能含敏感内容，读取必须经过权限和脱敏层。

## 10. 指标、报告与任务表

| 表 | 关键字段/约束 |
|---|---|
| `metric_snapshots` | `audit_id`, `evaluation_run_id`, `metric_version`, `status`, `generated_at` |
| `metric_values` | `snapshot_id`, `metric_code`, `aggregation_method`, `dimension_hash`, `dimensions_json`, numerator、denominator、value、CI、区间算法元数据 |
| `metric_evidence_sets` | `metric_value_id`, `query_hash`, `member_count`, `members_ref` |
| `metric_quality_flags` | `metric_value_id`, `flag_code`, `details_json` |
| `reports` | `project_id`, `audit_id`, `version`, 依赖版本、`status`, `published_at` |
| `report_sections` | `report_id`, `section_code`, `data_snapshot_ref`, `content_hash` |
| `report_artifacts` | `report_id`, `artifact_type`, `object_key`, `sha256`, `size_bytes`, 渲染版本 |
| `report_access_events` | `report_id`, `actor_id`, `action`, `occurred_at` |
| `finding_priorities` | `finding_id`, 分项、计算版本、人工调整和原因 |
| `optimization_tasks` | `project_id`, `finding_id`, `status`, `priority`, `assignee_id`, 验收条件 |
| `task_events` | `task_id`, `from_state`, `to_state`, `actor`, `reason`, `occurred_at` |
| `task_evidence_links` | `task_id`, `source_type/id`, `source_version`, `content_hash` |
| `task_acceptance_items` | `task_id`, `description`, `status`, `position` |
| `task_assignments` | `task_id`, `user_id`, `assignment_role` |
| `task_comments` | `task_id`, `author_id`, `body`, `created_at` |
| `task_attachments` | `task_id`, `object_key`, `sha256`, `scan_status` |
| `audit_logs` | 租户、主体、动作、资源、结果、IP、trace、不可变 payload 摘要 |

## 10.1 任务与事件表

| 表 | 关键字段/约束 |
|---|---|
| `workflows` | `tenant_id`, `workflow_type`, `resource_type/id`, `status`, 开始/结束时间 |
| `jobs` | `workflow_id`, `stage`, `job_type`, `idempotency_key`, `state`, 租约、重试和版本；幂等键唯一 |
| `job_attempts` | `job_id`, `attempt_no`, `worker_id`, `state`, 错误、开始/结束时间 |
| `outbox_events` | `tenant_id`, `event_type`, `aggregate_type/id`, `payload_json`, `published_at` |
| `inbox_events` | `consumer_name`, `event_id`, `processed_at`; 组合唯一 |

指标值必须保存分子和分母，不能只保存百分比。`dimension_hash` 由规范化维度 JSON 计算。

## 11. 对象存储布局

```text
tenant/{tenant_id}/project/{project_id}/
  crawls/{crawl_id}/fetch/{attempt_id}/{raw|rendered}.html.gz
  responses/{audit_id}/{sample_id}/response.json.gz
  reports/{report_id}/report.pdf
```

Bucket 默认私有，启用版本控制和服务端加密。数据库保存 `object_key`、SHA-256、字节数、MIME、压缩方式和加密上下文。

## 12. 索引建议

- 所有租户表首列索引 `tenant_id`，常用访问路径使用 `(tenant_id, project_id, ...)` 复合索引。
- `crawl_urls(crawl_run_id,state,priority)` 支持领取任务。
- `audit_samples(audit_id,status)` 与 `sample_key` 唯一索引。
- `answer_evaluations(evaluation_run_id,response_id)` 唯一。
- `metric_values(snapshot_id,metric_code,dimension_hash)` 唯一。
- URL、正文和问题使用 SHA-256 二进制哈希索引，避免长文本唯一索引。

## 13. 并发与乐观锁

可编辑资源使用 `version` 乐观锁：更新条件包含旧版本，冲突返回 409。Worker 领取任务使用状态条件更新和租约：只有 `queued` 或租约过期任务可被领取。

## 14. 保留、删除与备份

- P0 默认原始响应保留 365 天，可按合同配置。
- 项目删除先软删除并进入 30 天冷静期，随后执行分批物理删除。
- 删除顺序覆盖数据库、对象存储、缓存、搜索索引和派生报告。
- 审计日志按合规策略保留，不保存已删除正文，仅保留操作事实和不可逆摘要。
- 每日全量备份与增量日志；恢复演练验证 RPO ≤ 24h、RTO ≤ 4h。

## 15. 迁移规范

- 使用有序迁移工具，迁移文件一经生产执行不可修改。
- 大表变更采用 expand/migrate/contract，禁止高峰期阻塞式 DDL。
- Schema 变更必须说明回滚或前向修复方案、数据校验 SQL 和兼容窗口。

## 16. 验收标准

- 外键和租户约束可阻止孤立及跨项目关联。
- 原始响应重评估不修改任何原始表。
- 相同 `sample_key` 和事件 ID 无法产生重复业务记录。
- 项目删除演练可给出所有数据面的清除证明。

