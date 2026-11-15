# SPEC-11 渠道采集协议（模型 API 之外的答案来源）

> 版本：V1.0｜状态：草案（待评审）｜日期：2026-11-15
> 对齐：[PRD](../product-requirements.md)、[SPEC-05 Model Gateway](./05-model-gateway.md)、[SPEC-06 Answer Judge](./06-answer-judge.md)、[SPEC-08 多租户安全](./08-multitenancy-security.md)
> 范围：**仅定义协议与契约，不涉及实现**。本文通过前，不新增代码、不建表、不改 OpenAPI。

## 1. 背景与目标

现有审计只覆盖 `model_api` 渠道（DeepSeek、MiniMax）。真正决定买家看到什么的 ChatGPT、Perplexity、Google AI Overviews、Copilot 以及消费端 App，均无采集能力。

这些渠道**没有稳定的官方问答 API**，因此协议采取"**观测上报**"而非"服务端拉取"：由人工、浏览器扩展或合作方客户端把看到的答案按规定格式提交，系统负责校验、去重、落库、参与评估与指标计算。

目标：

1. 让 `search_api` / `consumer_app_capture` 从枚举值变成可写入的数据源；
2. 每条答案都能回答"谁在何时、用什么方式、在哪看到"；
3. 不破坏既有指标口径：不同渠道分开切片，不混合汇总。

## 2. 术语

| 术语 | 含义 |
|---|---|
| Capture | 一次观测到的答案（一条回答 = 一条 capture） |
| Capture Agent | 产生 capture 的客户端：浏览器扩展、人工控制台、合作方服务 |
| Surface | 观测面：provider + product + model + locale + region + 账号档位 |
| Trust Tier | 采集可信度等级 A/B/C/D，见 §6 |
| Attestation | 客户端对 capture 内容的签名，用于证明"未被篡改" |

## 3. 渠道与观测面

`channel` 沿用现有枚举，不新增：

| channel | 典型 surface | 采集方式 | 备注 |
|---|---|---|---|
| `model_api` | deepseek / minimax | 服务端调用（现状） | 不适用本协议 |
| `search_api` | Google AI Overviews、Bing Copilot 搜索页、Perplexity 搜索页 | 截图 OCR 或合作方数据 | 结果可能无独立 URL |
| `consumer_app_capture` | ChatGPT、Perplexity App、豆包、Kimi 等 | 浏览器扩展 / 人工控制台 | 需用户授权，禁止自动抓取 |

`surface.product` 为自由文本但**必须**在服务端登记白名单，未登记的产品返回 `SURFACE_UNSUPPORTED`。

## 4. 传输协议

### 4.1 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/capture-batches` | 批量提交 capture（≤100 条/批） |
| GET | `/api/v1/capture-batches/{batchId}` | 查询批次与逐条结果 |
| GET | `/api/v1/audits/{auditId}/captures` | 按审计列出 capture（含 trust tier） |
| POST | `/api/v1/captures/{captureId}/revoke` | 撤回错误采样（软删 + 审计留痕） |

统一请求头：

```
X-Capture-Protocol: 1
X-Actor-ID / Authorization: 现有鉴权
Idempotency-Key: <batchId>            # 必填，与 body.batchId 一致
Content-Type: application/json
```

### 4.2 提交体

```json
{
  "batchId": "018f3f9e-7c1a-7b3e-9a51-2c1a0f4b6d21",
  "agent": {
    "id": "geo-ext-chrome",
    "kind": "browser_extension",
    "version": "1.4.0",
    "publicKeyId": "ext-2026-11",
    "signature": "base64(ed25519(canonical(captures)))"
  },
  "captures": [
    {
      "captureId": "018f3f9e-8a10-7c22-b001-6f1c2d3e4a55",
      "auditId": "01a1de7d-b707-7921-b185-ef29bce5ed70",
      "questionId": "01a1de7d-b69b-7b44-8629-d2fa3830398a",
      "channel": "consumer_app_capture",
      "surface": {
        "provider": "openai",
        "product": "chatgpt",
        "model": "gpt-5",
        "locale": "zh-CN",
        "region": "CN",
        "accountTier": "plus"
      },
      "observedAt": "2026-11-15T09:00:00Z",
      "answer": {
        "text": "如果只选一个，知识型团队优先选飞书……",
        "finishReason": "stop",
        "truncated": false
      },
      "citations": [
        { "url": "https://www.feishu.cn/product/messenger", "title": "飞书即时消息", "position": 1 }
      ],
      "provenance": {
        "method": "browser_extension",
        "sessionHash": "sha256:9f2c…",
        "screenshotSha256": "sha256:41ab…",
        "collectorVersion": "ext-1.4.0",
        "domSelectorVersion": "v3"
      },
      "consent": {
        "basis": "customer_authorized",
        "reference": "PO-2026-118"
      }
    }
  ]
}
```

### 4.3 字段约束

| 字段 | 约束 |
|---|---|
| `batchId` / `captureId` | UUIDv7；批次内唯一；`captureId` 全局幂等 |
| `auditId` | 必须存在、属于当前租户、状态为 `succeeded`/`partial_success`/`running` |
| `questionId` | 必须在该审计的问题集版本内，否则 `QUESTION_NOT_IN_AUDIT` |
| `channel` | 只能是 `search_api` 或 `consumer_app_capture`；提交 `model_api` 返回 `CHANNEL_NOT_CAPTURABLE` |
| `observedAt` | 与服务器时间偏差 ≤ 7 天，否则 `TIMESTAMP_OUT_OF_RANGE` |
| `answer.text` | 1–20000 字符；空白串 → `ANSWER_EMPTY` |
| `citations[].url` | 必须 http(s)；无 URL 时允许 `title` 单填，但 `position` 必须给出 |
| `provenance.method` | `browser_extension` / `manual_console` / `partner_api` / `screenshot_ocr` |
| `consent.basis` | `customer_authorized` / `partner_agreement` / `public_web_snapshot` |
| `consent.reference` | 必填，用于合规追溯 |

## 5. 存储映射契约

一条 capture 落库为"**一个 sample + 一个 response + 一条溯源记录**"，复用现有审计链路，不新增并行评估路径。

| 目标表 | 写入规则 |
|---|---|
| `audit_samples` | 若 `(audit_id, question_id, engine_config_id, sample_no)` 不存在则创建；`channel` 取 capture；`status='succeeded'`；`dimensions_json` 记录 surface |
| `ai_responses` | `provider=surface.provider`、`resolved_model=surface.model`、`content_text=answer.text`、`transport_status='succeeded'`、`usage_json=NULL`、`cost_microunits=0`、`latency_ms=NULL`、`raw_hash=sha256(answer.text)` |
| `ai_requests` | **不创建**（没有服务端请求，不能伪造） |
| `capture_provenance`（新增） | `capture_id`、`batch_id`、`trust_tier`、`method`、`observed_at`、`session_hash`、`screenshot_sha256`、`consent_basis`、`consent_reference`、`agent_id`、`agent_version`、`signature_verified` |

> 注：`capture_provenance` 是本文通过后才创建的**新表**，本轮不实现。

### 5.1 幂等

幂等键：

```
sha256(auditId | questionId | channel | surface.provider | surface.model | floor(observedAt, 60s) | sha256(answer.text))
```

- 相同键重复提交：整批返回 200，该条 `status=duplicate`，不重复入库；
- `batchId` 相同但内容不同：409 `BATCH_CONFLICT`；
- 同一会话多次观测（用户刷新）应传相同 `sessionHash`，服务端只保留置信度最高的一条。

### 5.2 撤回

`POST /captures/{id}:revoke`：软删 `ai_response`、把 sample 置为 `cancelled`、给相关指标快照打 `stale` 标记（**不重算**）。撤回必须写审计日志，包含操作者与原因。

## 6. 可信度等级

| 等级 | 条件 | 指标标记 | 是否默认计入报告 |
|---|---|---|---|
| A | `partner_api` + 签名校验通过 | 无 | 是 |
| B | `browser_extension` + 签名校验通过 | 无 | 是 |
| C | `manual_console` + 截图哈希 | `capture_attested` | 是 |
| D | `manual_console` 无签名无截图 | `capture_unattested` | **否**（可配置） |

规则：

1. 签名验证失败直接拒绝（`ATTESTATION_INVALID`），不降级入库。
2. D 级数据进入复核队列（FEAT-08），人工确认后才计入。
3. 同一审计中 D 级占比 > 50% 时，报告在限制声明里显式标注。

## 7. 指标与评估影响

1. **分渠道切片**：capture 只与同 `channel` 的样本汇总，不与 `model_api` 混算；跨渠道对比只在报告层做"并列展示"，不做合并比率。
2. **Judge 复用**：capture 答案走现有 Answer Judge，`evaluation_runs` 增加 `channel` 维度不必要——按 sample 关联即可。
3. **截断答案**：`truncated=true` 时加 `answer_truncated` 标记；若截断且未检测到品牌，判 `unknown` 而非 `refusal`。
4. **引用率**：capture 的 citations 参与 `official_citation_rate` / `third_party_citation_rate`；无 URL 的 AI Overviews 记 `citationCoverage=null`（沿用 V1.86 语义）。
5. **成本**：capture 成本恒为 0，预算账户不扣减；报告的成本章节标注"该渠道无调用成本"。

## 8. 错误码

| code | HTTP | 含义 |
|---|---|---|
| `CAPTURE_PROTOCOL_UNSUPPORTED` | 400 | 缺少/不支持的 `X-Capture-Protocol` |
| `BATCH_TOO_LARGE` | 422 | 超过 100 条 |
| `CHANNEL_NOT_CAPTURABLE` | 422 | 提交了 `model_api` |
| `QUESTION_NOT_IN_AUDIT` | 422 | 问题不属于该审计版本 |
| `SURFACE_UNSUPPORTED` | 422 | 产品未登记白名单 |
| `CONSENT_MISSING` | 422 | 缺少 consent basis/reference |
| `ATTESTATION_INVALID` | 422 | 签名校验失败 |
| `TIMESTAMP_OUT_OF_RANGE` | 422 | 时间偏差超限 |
| `ANSWER_EMPTY` | 422 | 答案为空 |
| `BATCH_CONFLICT` | 409 | 同 batchId 内容不一致 |
| `DUPLICATE` | 200（条目级） | 幂等命中 |

部分成功：整批返回 200，`results[]` 逐条给 `accepted|duplicate|rejected` 与原因；全部失败返回 422。

限流：每租户 60 批/分钟、6000 capture/小时；超限 429 并带 `Retry-After`。

## 9. 合规底线

1. **禁止自动抓取消费端界面**。只接受人工操作、经用户授权的扩展、或正式合作方接口。
2. **不存凭据**：不接收账号、密码、Cookie、会话 token；`sessionHash` 仅用于去重。
3. **截图存哈希不存原图**：原图若需留存，必须进受控对象存储并单独授权，业务库只存哈希。
4. **可删除**：支持按 capture 删除（GDPR/个保法），删除后指标快照标记 `stale`。
5. **渠道准入**：每个 provider/product 上线前必须由法务确认 ToS，登记到白名单后才可采集。
6. 采集数据的保留期默认 180 天，可按租户配置。

## 10. 分阶段落地

| 阶段 | 范围 | 交付 | 验收标准 |
|---|---|---|---|
| P1 | 人工控制台上报（D 级） | 提交端点 + 存储映射 + 复核队列接入 | 截图/文本可入库；D 级不进报告 |
| P2 | 浏览器扩展（C/B 级） | 扩展 + 签名校验 + 溯源表 | 签名失败 100% 拒绝；重复提交 0 重复入库 |
| P3 | 合作方 API（A 级） | 伙伴接入文档 + 批量回执 | 1000 条/批压测通过 |
| P4 | AI Overviews / SERP 截图 OCR（C 级） | OCR 管道 + 置信度门槛 | OCR 文本与人工校对一致率 ≥ 95% 才计入 |

每阶段都必须先有该渠道的法务结论与至少一个真实样本。

## 11. 一致性测试

1. 同一 capture 提交两次 → 首次 `accepted`，二次 `duplicate`，库中仅 1 条。
2. 同 `batchId` 不同内容 → 409。
3. 截断答案且未提品牌 → 判 `unknown`，不计入 refusal。
4. 签名篡改 → `ATTESTATION_INVALID`，且不落库。
5. 跨租户提交 → 404（不泄露存在性）。
6. 提交 `model_api` → `CHANNEL_NOT_CAPTURABLE`。
7. 撤回后指标快照状态为 `stale`，且审计日志有记录。
8. D 级 capture 默认不出现在报告指标中。

## 12. 待决问题

1. OCR 管道是否允许进入 P0 范围？准确率门槛 95% 是否足够？
2. 同一用户同一问题多次捕获（不同时间）是否算独立样本？本文默认"算"，但需产品确认。
3. AI Overviews 无独立 URL 时，citation 如何定位到具体来源卡片？
4. 合作方 API 是否需要单独的计费/配额模型？
5. 渠道白名单的维护归属（产品 or 法务）尚未确定。

## 13. 通过条件

- 产品、架构、安全、法务四方评审通过；
- 渠道白名单与合规章节逐条确认；
- 与 SPEC-05（Gateway）、SPEC-06（Judge）、SPEC-08（多租户）无冲突；
- 通过后按 §10 的 P1 先行，实现另立变更单。
