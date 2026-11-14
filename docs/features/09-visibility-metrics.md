# FEAT-09：可见度指标与分析

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：已批准  
> 层级：P0-C｜判定与排期见 [功能优先级规划](../FEATURE-PLAN.md)  
> 需求：MET-001～MET-006｜对齐：SPEC-01、03、09 V1.0

## 1. 功能目标

基于指定评估版本计算可重复的品牌提及、推荐、引用和正向提及指标，展示分子、分母、样本量及与聚合方式匹配的 95% 区间，并保证任一聚合结果可下钻到构成样本。

## 2. 核心指标

```text
Mention Rate = 含目标品牌提及的有效回答数 / 有效回答数
Recommendation Rate = 主动推荐目标品牌的有效回答数 / 有效回答数
Official Citation Rate = 引用自有域名的有效回答数 / 有效回答数
Trusted Third-party Citation Rate = 相关可信第三方引用回答数 / 有效回答数
Positive Mention Rate = 正向提及回答数 / 含品牌提及的有效回答数
Valid Response Rate = 有效回答数 / 计划采样数
```

分母为零返回 null；同一回答多次提及在回答率中只计一次，频次另存。

## 3. 维度

至少支持：引擎、实际模型、模型别名版本、问题、主题、意图、漏斗、问题类型、地区、语言、渠道、时间窗口、品牌/竞品、协议版本和 Judge 版本。

渠道是强制隔离维度，跨渠道查询默认分组而不合并。

## 4. 聚合方式

- 回答等权：默认反映实际采样。
- 问题等权：先按问题计算再平均。
- 业务加权：仅使用审计前冻结权重，并同时展示未加权结果。

少于 30 个合格回答显示小样本警告；少于 5 个不生成趋势结论。回答等权简单比例使用 95% Wilson；问题等权和业务加权分别使用问题级与加权 Cluster Bootstrap。算法、随机种子和重采样次数写入 Snapshot。

## 5. 数据模型

- `metric_snapshots`：Audit、evaluation_run、metric_version、状态和完整性。
- `metric_values`：metric_code、dimensions、dimension_hash、numerator、denominator、value、CI low/high。
- `metric_evidence_sets`：聚合值到 response/evaluation ID 的可重建引用或查询定义。
- `metric_quality_flags`：小样本、不完整、协议断点和异常。

Snapshot 在临时状态计算，校验完成后原子发布。

## 6. API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/audits/{id}/metrics` | 指标和维度查询 |
| GET | `/api/v1/metric-values/{id}/evidence` | 构成样本下钻 |
| GET | `/api/v1/projects/{id}/metric-trends` | 兼容批次趋势 |
| POST | `/internal/metrics/v1/snapshots` | 从指定评估版本聚合 |

查询参数使用白名单维度，防止动态 SQL 注入和无限基数查询。

## 7. 实现细节

1. 聚合只读取一个明确 evaluation_run，不读取“最新”可变结果。
2. 有效性和指标分子以版本化规则映射；SQL 和参考实现双重测试。
3. 先生成基础事实，再按允许的常用维度物化；任意组合可走受限在线查询。
4. dimension JSON 使用确定性排序计算哈希，唯一约束防止重复值。
5. 聚合前校验样本状态总和、有效分母和重复 response。
6. 结果保存分子、分母和区间，不只保存浮点百分比。
7. 趋势比较检查协议、问题、渠道和模型兼容性；不兼容时显示断点。
8. 缓存键包含租户、项目、snapshot、维度和筛选哈希。
9. 竞品指标使用相同口径，但不得与目标品牌的正向目标混写。

## 8. UI

- 四类核心 Metric Card 同时显示百分比、分子/分母和区间。
- FilterBar 支持规定维度并同步 URL。
- 趋势图标记协议或模型断点，图表有数据表替代。
- 点击指标打开 Evidence Drawer，列出问题、回答、Judge 标签和渠道。
- 小样本和数据不完整使用明确警告，不用绿色/红色制造确定性错觉。

## 9. 异常与边界

- 分母为零：显示“无有效样本”，不是 0%。
- Positive Mention 无任何提及：返回 null。
- 相同 response 被重复关联：完整性检查失败，不发布 Snapshot。
- Judge 更新：创建新 Snapshot；趋势需要两期使用相同 Judge 重算。
- 模型版本不透明：趋势显示潜在断点和限制说明。

## 10. 验收标准

- AC-01：Mention、Recommendation、Official Citation 和Positive Mention 结果与独立参考实现一致。
- AC-02：每个指标显示准确分子、分母、值和 95% 区间；回答等权使用 Wilson，问题等权/业务加权使用对应 Cluster Bootstrap。
- AC-03：支持规定的九类业务切片，渠道不能被无提示合并。
- AC-04：小样本和零分母按规则显示，不产生伪精确结论。
- AC-05：任一指标可下钻到完整构成回答，集合重算与指标一致。
- AC-06：相同原始响应和评估版本重复聚合得到相同 Snapshot 内容。
- AC-07：不兼容协议默认不绘制连续趋势，并显示断点原因。

## 11. 需求追踪

| 需求 | 验收覆盖 |
|---|---|
| MET-001 | AC-01、02、05 |
| MET-002 | AC-01、02 |
| MET-003 | AC-01、02 |
| MET-004 | AC-01、02、04 |
| MET-005 | AC-03 |
| MET-006 | AC-02、04、07 |

