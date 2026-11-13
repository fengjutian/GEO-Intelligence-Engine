# FEAT-04：实体知识与一致性

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：已批准  
> 需求：ENT-001～ENT-005｜对齐：SPEC-03、06、08 V1.0

## 1. 功能目标

从固定页面版本中提取品牌、组织、产品、软件、能力和场景实体，建立带来源证据的关系；通过消歧、人工确认和一致性检查形成可供问题生成和 Answer Judge 使用的品牌知识快照。

## 2. 实体与关系范围

P0 实体类型：Organization、Brand、Product、SoftwareApplication、Feature、Technology、Industry、Audience、UseCase。核心关系：owns、offers、has_feature、uses_technology、serves_industry、targets_audience、supports_use_case、same_as。

每个实体和关系必须具备来源、提取器版本、置信度和确认状态。

## 3. 流程

```text
页面版本
 → JSON-LD/Microdata/正文候选抽取
 → 名称规范化
 → 实体匹配与消歧
 → 关系候选
 → 冲突与一致性检查
 → 人工确认/合并/拆分
 → 发布知识快照
```

机器候选不能覆盖人工确认结论；低置信候选进入复核队列。

## 4. 数据模型

- `entities`、`entity_aliases`、`entity_relations`。
- `entity_evidence`：source、quote、DOM locator、提取器版本。
- `entity_candidates`：未确认候选和匹配分数。
- `entity_merge_events`：合并、拆分和撤销信息。
- `structured_data_items`：原始结构化数据和校验结果。
- `knowledge_snapshots`：审计使用的不可变实体/关系版本。
- `findings`：一致性冲突和 Schema 问题。

## 5. API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/projects/{id}/entity-extractions` | 对指定 Crawl Run 发起抽取 |
| GET | `/api/v1/projects/{id}/entities` | 实体列表和筛选 |
| GET/PATCH | `/api/v1/entities/{id}` | 查看证据和人工确认 |
| POST | `/api/v1/entities:merge` | 合并候选实体 |
| POST | `/api/v1/entity-merges/{id}:undo` | 撤销可逆合并 |
| GET/POST | `/api/v1/entities/{id}/relations` | 关系查看和确认 |
| POST | `/api/v1/projects/{id}/knowledge-snapshots` | 发布知识快照 |

## 6. 实现细节

1. 优先解析结构化数据，再从可见正文补充候选；二者冲突时不自动选择赢家。
2. 名称规范化保留原文，使用 Unicode、大小写、空白、标点和已确认别名建立匹配键。
3. 消歧综合名称、官方域名、所属组织、共现、类型、页面位置和项目品牌档案。
4. 自动合并仅限高置信且无类型/域名冲突的候选；阈值由版本化配置定义。
5. 合并通过别名和 canonical ID 实现，不立即物理删除旧实体；撤销恢复引用。
6. 关系以三元组保存，但必须关联至少一条证据；无证据人工关系标记 `manual_assertion`。
7. Schema 校验区分语法错误、缺少字段、无效 URL、正文冲突和实体引用错误。
8. 一致性检查比较规范名、描述、功能、价格、版本、部署方式等事实及其时间。
9. 发布快照前阻止未解决的主品牌歧义，普通低置信候选可排除后发布。

## 7. UI

- 实体列表：类型、规范名、置信度、状态、来源数和冲突数。
- 实体详情：属性、别名、关系、证据和历史。
- 合并复核：并排对比候选、来源和冲突，显示合并影响。
- Schema 查看器：原始 JSON-LD、解析树、错误和页面位置。
- 知识关系图仅作为辅助视图；P0 必须同时提供可访问的表格视图。

## 8. 异常与边界

- 同名品牌属于不同公司：域名/组织不足时保持独立并要求复核。
- 产品名唯一指向品牌但页面未写品牌：可建立候选关系，不自动算品牌提及。
- JSON-LD 与正文冲突：生成 Finding，不删除任何一方证据。
- 合并后已用于历史审计：历史快照保持原 ID 映射，不随当前图变化。
- 页面删除或过期：证据保留来源状态，不静默删除关系。

## 9. 验收标准

- AC-01：支持抽取规定实体类型，每个实体有名称、类型、来源片段、置信度和版本。
- AC-02：低置信或有冲突实体不自动合并，人工合并/拆分可撤销并有审计记录。
- AC-03：核心关系具有 subject、predicate、object 和至少一条来源证据。
- AC-04：JSON-LD 原文永久保留，语法、字段和正文冲突分类展示。
- AC-05：官网不同页面的名称、描述、价格、功能冲突可生成带双侧证据的 Finding。
- AC-06：人工确认结论在重新抽取后保持，不被机器结果覆盖。
- AC-07：发布知识快照后，后续当前实体编辑不影响已启动审计。

## 10. 需求追踪

| 需求 | 验收覆盖 |
|---|---|
| ENT-001 | AC-01 |
| ENT-002 | AC-02、06 |
| ENT-003 | AC-03 |
| ENT-004 | AC-04 |
| ENT-005 | AC-05 |

