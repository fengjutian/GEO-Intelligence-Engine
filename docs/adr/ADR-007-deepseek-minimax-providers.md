# ADR-007：首发模型供应商使用 DeepSeek 与 MiniMax 国内站

> 状态：已批准｜决策日期：2026-11-13｜文档版本：V1.2

## 决策

P0 Model Gateway 首发支持：

1. DeepSeek OpenAI-compatible API，默认基地址 `https://api.deepseek.com`。
2. MiniMax 国内 OpenAI-compatible API，默认基地址 `https://api.minimaxi.com/v1`。
3. 两者统一使用服务端 Bearer Credential，密钥不得发送到浏览器、写入日志或保存在普通业务表。
4. DeepSeek Chat Completions 的输出上限字段使用 `max_tokens`；MiniMax 使用 `max_completion_tokens`。
5. MiniMax 除 HTTP 状态外还必须检查 `base_resp.status_code`；非零值按供应商错误处理。
6. 模型名称通过环境和模型目录配置，不将营销型号永久写死在业务逻辑中。

## 结果

本 ADR 取代 ADR-003。OpenAI 与 Google Gemini 不属于首发实现，可在后续 ADR 中作为新增适配器引入，不改变 Gateway 的统一业务契约。

## 官方依据

- [DeepSeek API 文档](https://api-docs.deepseek.com/api/create-chat-completion/)
- [MiniMax 国内开放平台](https://platform.minimaxi.com/)
