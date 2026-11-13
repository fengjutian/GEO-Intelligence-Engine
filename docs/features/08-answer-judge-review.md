# FEAT-08：Answer Judge 与人工复核

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：研发评审稿  
> 需求：MOD-005、JDG-001～JDG-007｜对齐：SPEC-01、03、06 V1.0

## 1. 功能目标

将原始 AI 回答转换为带证据、置信度和版本的有效性、品牌提及、推荐、位置、情感和引用判定，并允许人工纠错而不覆盖机器结果。

## 2. Judge 流程

```text
原始响应 + 请求元数据 + 品牌知识快照
 → 技术状态校验
 → 回答有效性
 → 确定性实体和 URL 候选
 → 上下文消歧
 → 推荐/位置/情感判断
 → 引用规范化和来源分类
 → 置信度校准
 → 低置信复核队列
 → 版本化评估结果
```

## 3. 输出标签

- 有效性：valid、refusal、no_recommendation、empty、transport_error、truncated、wrong_language、policy_blocked。
- 提及：direct_brand、product_implied、ambiguous、competitor。
- 推荐：primary、recommended_option、conditional、neutral_listing、not_recommended、insufficient。
- 情感：positive、neutral、negative、mixed、unknown。
- 引用支持：supports、partially_supports、does_not_support、unknown。

每项语义判定带证据片段、locator、置信度和 Judge 版本。

## 4. 数据模型

- `evaluation_runs`：Judge、规则、Prompt、模型、别名和配置版本。
- `answer_evaluations`：有效性、推荐、情感和总置信度。
- `brand_mentions`：字符范围、实体、类别和证据。
- `citations`：原始/规范 URL、来源类型、支持性和证据。
- `review_queue_items`：复核原因、优先级和分配状态。
- `human_annotations`、`adjudications`：人工结果和仲裁。

机器和人工结果并存；报告选择哪个版本必须明确记录。

## 5. API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/internal/judge/v1/evaluations` | 创建指定版本评估 |
| GET | `/api/v1/responses/{id}/evaluations` | 历史评估版本 |
| GET | `/api/v1/review-queue` | 低置信/抽样复核 |
| POST | `/api/v1/review-queue/{id}/annotations` | 提交人工标注 |
| POST | `/api/v1/evaluation-runs` | 批量重评估 |
| GET | `/api/v1/judge-versions/{id}/quality` | 黄金集质量报告 |

内部接口使用 JSON Schema；解析失败返回明确错误，不接受非结构化自由文本作为最终结果。

## 6. 实现细节

1. 技术错误由 Gateway 状态提供，Judge 只补充内容有效性，避免 LLM 猜测网络结果。
2. 品牌候选先用启用别名、产品和域名规则检测，再用上下文模型消歧。
3. 推荐必须存在建议语义；普通列举、引用域名和免责声明不计推荐。
4. 推荐位置保存首次出现、列表位置、是否首选和同列品牌数。
5. URL 解析先保留原值，再规范化 host/path 并受控解析重定向；不得由 Judge 任意访问内网 URL。
6. LLM Judge 必须通过 Model Gateway，调用成本独立归集。
7. 置信度使用验证集校准，禁止直接信任 LLM 自报概率。
8. 低置信、规则/模型冲突、同名实体、mixed 情感和随机质量抽样进入复核。
9. 新 Judge 版本读取历史原始响应并写新评估，不覆盖旧结果或已发布报告。
10. 人工纠错需要原因；双人分歧进入仲裁。

## 7. UI

- 回答详情并排显示原文、提及高亮、推荐标签、情感和引用。
- 证据点击定位到回答字符或引用。
- 复核页支持键盘操作、标签指南和冲突提示。
- 版本切换显示不同 Judge 结果 Diff。
- 质量页展示 Precision、Recall、F1、混淆矩阵和分层结果。

## 8. 权限

Analyst 可查看和提交普通复核；被授权 Reviewer 可修改人工标注；Judge Admin 才能发布版本和触发全量重评估。原始回答访问写审计日志。

## 9. 异常与边界

- 品牌只出现在 URL：计 Citation，不自动计 Mention。
- 条件建议：“若需私有化可考虑 A”计 conditional recommendation。
- 同名普通词：上下文不足时标 ambiguous，不进入 Mention 分子。
- 引用不可访问：保留 URL 并标 unknown，不删除。
- 回答截断：按有效性版本决定是否排除，原文始终保存。

## 10. 验收标准

- AC-01：品牌/竞品提及输出字符范围、实体、类别、置信度和证据。
- AC-02：普通列举不计推荐，主动和条件推荐能正确区分。
- AC-03：推荐位置、首次出现、首选和同列品牌数量可查询。
- AC-04：情感输出五类之一并附支持片段，无法判断时为 unknown。
- AC-05：引用保存原始/规范 URL、域名和官方/竞品/第三方/未知分类。
- AC-06：重评估创建新版本，原始响应、旧评估和旧报告不被覆盖。
- AC-07：人工纠错与机器结果并存，包含操作者、原因和时间。
- AC-08：黄金集上 Mention F1 ≥ 0.95、Recommendation ≥ 0.85、Citation ≥ 0.95 或按发布门槛执行批准的降级。
- AC-09：拒答、空响应、错误、截断和语言不符按 SPEC-01 分类并可重算。

## 11. 需求追踪

| 需求 | 验收覆盖 |
|---|---|
| MOD-005 | AC-09 |
| JDG-001 | AC-01 |
| JDG-002 | AC-02 |
| JDG-003 | AC-03 |
| JDG-004 | AC-04 |
| JDG-005 | AC-05 |
| JDG-006 | AC-06 |
| JDG-007 | AC-07、08 |

