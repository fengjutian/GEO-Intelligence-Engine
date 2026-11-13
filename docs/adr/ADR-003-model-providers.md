# ADR-003：P0 首发模型供应商

> 状态：已批准｜决策日期：2026-11-13

## 决策

P0 首发两个 `model_api` 供应商：

1. OpenAI：使用 Responses API 和官方 Go SDK。
2. Google Gemini：使用 Gemini Interactions API 和官方 Go SDK；若特定能力尚未覆盖，可在适配器内部使用仍受支持的 generateContent，不改变平台统一契约。

模型具体名称不写死在业务代码中，通过版本化 Model Catalog allow-list 配置。生产启用模型必须有价格版本、上下文限制、数据使用配置和契约测试。

P0 不把普通模型 API 标记为 `search_api` 或 `consumer_app_capture`。只有供应商正式提供检索能力并完成独立合规评审后才能新增渠道配置。

## 结果

Model Gateway 必须实现 OpenAI、Gemini 两个 Adapter；CI 使用脱敏 Fixture，Staging Smoke Test 使用硬预算真实调用。

## 参考

- [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses)
- [Gemini Interactions API](https://ai.google.dev/gemini-api/docs/interactions-overview)

