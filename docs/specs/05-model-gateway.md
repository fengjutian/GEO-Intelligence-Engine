# SPEC-05：Model Gateway 接口规范

> 版本：V1.0｜状态：已批准｜协议前缀：`/internal/model-gateway/v1`

## 1. 职责边界

Model Gateway 统一模型供应商差异，负责认证、模型解析、限流、预算、重试、原始响应保存和成本归集。它不负责业务问题生成、品牌判定或指标计算。

## 2. 核心对象

### 2.1 EngineConfig

```json
{
  "id": "eng_...",
  "provider": "provider_code",
  "channel": "model_api",
  "credential_ref": "secret://...",
  "base_url_ref": "config://...",
  "region": "global",
  "status": "active",
  "limits": {"rpm": 60, "tpm": 100000, "max_concurrency": 5}
}
```

凭据永不通过普通 API 返回。

### 2.2 ModelRequest

```json
{
  "request_id": "req_...",
  "idempotency_key": "sample_key",
  "tenant_id": "...",
  "project_id": "...",
  "engine_config_id": "...",
  "model_selector": "provider/model-alias",
  "channel": "model_api",
  "messages": [{"role": "user", "content": [{"type": "text", "text": "..."}]}],
  "parameters": {"temperature": 0.2, "max_output_tokens": 2000},
  "locale": {"language": "zh-CN", "region": "CN"},
  "timeout_ms": 60000,
  "metadata": {"audit_id": "...", "sample_id": "..."}
}
```

禁止在 `metadata` 中放正文、密钥或个人敏感数据。

### 2.3 ModelResponse

```json
{
  "request_id": "req_...",
  "provider_request_id": "...",
  "resolved_model": "provider-model-version",
  "channel": "model_api",
  "status": "succeeded",
  "output": [{"type": "text", "text": "..."}],
  "citations": [{"url": "https://...", "title": "..."}],
  "usage": {"input_tokens": 100, "output_tokens": 500},
  "cost": {"amount_microunits": 12345, "currency": "USD", "pricing_version": "..."},
  "finish_reason": "stop",
  "latency_ms": 2300,
  "raw_response_ref": "s3://...",
  "raw_sha256": "..."
}
```

## 3. 接口

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/requests` | 提交同步或异步请求，必须带 Idempotency-Key |
| GET | `/requests/{id}` | 查询规范化结果与状态 |
| POST | `/requests/{id}:cancel` | 尽力取消未发送或供应商支持的请求 |
| GET | `/models` | 获取允许使用的模型目录与能力 |
| POST | `/estimates` | 按请求数和 token 估算成本，不承诺最终费用 |
| GET | `/health/providers` | 内部供应商健康，不向普通租户暴露敏感细节 |

批量审计由 Orchestrator 展开成单请求，Gateway 不接受无限大批次。

## 4. 幂等

作用域为 `(tenant_id, engine_config_id, idempotency_key)`。首次请求保存请求哈希；相同键且相同哈希返回原结果，相同键但不同哈希返回 `409 IDEMPOTENCY_CONFLICT`。

如果供应商成功但本地确认失败，重试必须先按本地请求和供应商请求 ID 恢复，禁止盲目再次计费调用。

## 5. 模型解析与版本

`model_selector` 可以是别名，但发送前必须解析为供应商接受值，并在响应保存实际返回的模型标识。别名映射有版本和生效时间。供应商不暴露精确版本时标记 `version_visibility=opaque`，趋势系统据此提示不可比风险。

## 6. 限流和公平性

四级限制：供应商全局、凭据、租户、项目。使用令牌桶控制 RPM/TPM/并发，并采用按租户加权公平队列，防止单租户占满容量。

限流返回预计可重试时间。内部 Worker 尊重 `retry_after_ms`，不得忙轮询。

## 7. 预算控制

调用前执行估算和预算预留，完成后以实际 usage 结算并释放差额。预算层级：组织月度、项目月度、单次审计、单任务。

超过软阈值告警；超过硬阈值拒绝新调用。未知价格模型默认禁止生产使用，除非配置保守上限。所有金额使用整数微单位和 ISO 货币代码。

## 8. 重试策略

| 情况 | 策略 |
|---|---|
| 连接失败、408、429、5xx | 带抖动指数退避，遵守 Retry-After |
| 400、认证、内容政策拒绝 | 不重试，返回稳定错误码 |
| 超时但供应商状态未知 | 先查询/对账；无能力时标记 `outcome_unknown` 人工处理 |
| 流式中断 | 保存部分内容并标记 truncated，默认不自动重新计费调用 |

默认最多 3 次技术尝试；每次 attempt 单独记录。业务 Orchestrator 决定是否补样。

## 9. 错误模型

```json
{
  "error": {
    "code": "PROVIDER_RATE_LIMITED",
    "message": "Provider temporarily rate limited",
    "retryable": true,
    "retry_after_ms": 5000,
    "provider_code": "rate_limit",
    "trace_id": "..."
  }
}
```

稳定错误码至少包括：认证失败、模型不可用、参数无效、限流、预算不足、政策阻断、超时、供应商故障、结果未知和取消。

## 10. 原始与规范化响应

供应商原始响应先写加密对象存储，再写数据库引用。规范化不得删除原始字段。若原始保存失败，请求不能标记为 `succeeded`。日志只记录长度、哈希和请求标识，不记录完整 Prompt/回答。

## 11. 搜索和引用

适配器必须明确声明 `supports_search`、`supports_citations`、`supports_region` 等能力。没有正式搜索能力时禁止伪装成 `search_api`。引用保留供应商原始结构，再映射为规范 URL、标题和片段。

## 12. 安全

- Gateway 只接受服务身份调用，不直接暴露公网。
- 租户自带密钥与平台密钥分离，按租户密钥加密。
- Prompt 视为敏感数据；供应商的数据使用/训练设置必须可配置和记录。
- 禁止将网页中的指令转换为 Gateway 管理操作。

## 13. 可观测性与 SLO

指标：按供应商/模型的请求量、成功率、P50/P95 延迟、429/5xx、重试、outcome_unknown、Token、费用和预算拒绝数。

P0 Gateway 自身（不含供应商）月可用性目标 99.9%；内部处理开销 P95 < 100ms。供应商健康和平台健康分别统计。

## 14. 契约测试

每个适配器必须通过：成功、流式、引用、429、5xx、超时、无效凭据、取消、未知字段、模型版本不透明、用量缺失和原始响应保存失败测试。使用录制脱敏 fixture，不在 CI 调用真实付费 API。

