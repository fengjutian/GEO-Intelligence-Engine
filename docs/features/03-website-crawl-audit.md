# FEAT-03：网站抓取与技术审计

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：研发评审稿  
> 需求：DIS-001～DIS-006｜对齐：SPEC-03、04、07、08 V1.0

## 1. 功能目标

在授权范围内发现、获取和解析网站页面，保存静态与渲染证据，识别影响搜索引擎和 AI 系统发现、访问与理解的技术问题。

## 2. 用户故事

- 分析师配置站点范围并启动抓取。
- 用户查看 robots、sitemap、页面和抓取错误。
- 用户可以暂停、继续或取消长时间抓取。
- 用户从技术问题下钻到页面和原始证据。
- 后续实体和内容分析使用固定页面版本，不受再次抓取影响。

## 3. 抓取配置

配置包括授权模式、起始 URL、允许/拒绝 host/path、最大深度、最大页面数、单页/总字节、每 host 速率、并发、User-Agent、是否允许动态渲染和超时。启动时生成不可变配置快照。

## 4. 流程

```text
创建 Crawl Run
 → 获取 robots.txt
 → 发现 Sitemap 与 URL
 → 安全/范围/robots 校验
 → 静态 HTTP 获取
 → 按需 Playwright 渲染
 → HTML/Schema/链接解析
 → 内容和哈希持久化
 → 技术规则检查
 → 汇总报告
```

每个 URL 独立状态和重试，单页失败不终止整个 Crawl Run。

## 5. 数据模型

使用 `websites`、`crawl_runs`、`crawl_urls`、`fetch_attempts`、`website_pages`、`page_contents`、`page_links`、`structured_data_items` 和 `findings`。大 HTML 写 S3，数据库保存哈希和引用。

## 6. API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/projects/{id}/crawl-runs` | 估算并启动抓取 |
| GET | `/api/v1/crawl-runs/{id}` | 进度和计数 |
| POST | `/api/v1/crawl-runs/{id}:pause` | 暂停新 URL 领取 |
| POST | `/api/v1/crawl-runs/{id}:resume` | 继续未完成任务 |
| POST | `/api/v1/crawl-runs/{id}:cancel` | 取消 |
| GET | `/api/v1/crawl-runs/{id}/pages` | 页面列表和筛选 |
| GET | `/api/v1/page-contents/{id}` | 页面版本与解析结果 |
| GET | `/api/v1/crawl-runs/{id}/findings` | 技术问题 |

## 7. 实现细节

1. Go Orchestrator 管理 URL 状态、租约和范围；TypeScript Worker 使用 undici、Playwright 和 Cheerio。
2. 每次调度前校验 scheme、DNS/IP、端口、范围、robots 和预算；重定向逐跳重复校验。
3. 默认先静态获取，仅正文不足或规则命中时动态渲染。
4. 保存原始字节哈希、DOM 哈希和正文哈希；内容不变可复用解析产物，但保留本次 Fetch。
5. Sitemap 支持 Index、嵌套、gzip、lastmod 和解析错误；设置递归和 URL 总量上限。
6. URL 规范化不随意删除业务参数；追踪参数使用明确规则表。
7. Pause 停止领取新任务，运行中短任务完成；Cancel 设置持久意图并在安全点停止。
8. Finding 规则版本化，至少覆盖 robots 阻断、noindex、非 2xx、canonical、重复 URL、断链、孤立页、JS 差异和 sitemap 缺失。
9. Crawler 无数据库和 Secret Manager 权限，只通过受限任务 API 与对象写权限工作。

## 8. UI

- 抓取配置 Drawer 和执行前范围/页数提示。
- 进度页显示 discovered、queued、fetched、rendered、parsed、blocked、failed。
- 页面表支持状态、类型、深度、canonical、内容变化和 Finding 筛选。
- robots 与 sitemap 独立查看器。
- 页面详情显示静态/渲染正文差异、链接、Schema 和证据位置。

## 9. 错误处理

- 429/503 遵守 Retry-After 并降低 host 速率。
- robots 阻断、4xx、超大响应和不支持 MIME 记为业务结果，不重复重试。
- 浏览器崩溃最多重试两次，仍失败则保留静态内容。
- 对象存储失败时不得把页面标记为成功。
- 租约过期可回收；唯一约束避免生成重复页面版本。

## 10. 验收标准

- AC-01：robots 内容、状态、抓取时间和针对目标 User-Agent 的规则可查看。
- AC-02：可解析嵌套/压缩 Sitemap，并对重复 URL 去重、对错误保留记录。
- AC-03：抓取严格执行域名、路径、深度、页数、速率、并发和字节限制。
- AC-04：暂停、继续和取消后状态正确，恢复不会重复生成业务记录。
- AC-05：JS 页面同时保存静态和渲染结果，页面详情可比较差异。
- AC-06：每个页面保存状态码、最终 URL、canonical、正文、元数据和内容哈希。
- AC-07：技术 Finding 覆盖 PRD 要求的六类规则，并能定位页面版本和证据。
- AC-08：所有内网、loopback、链路本地、元数据和恶意重定向测试被阻断。
- AC-09：单页失败不会导致整个 Crawl Run 失败，最终结果正确标记部分成功。

## 11. 需求追踪

| 需求 | 验收覆盖 |
|---|---|
| DIS-001 | AC-01 |
| DIS-002 | AC-02 |
| DIS-003 | AC-03、04、08、09 |
| DIS-004 | AC-05、06 |
| DIS-005 | AC-06、07 |
| DIS-006 | AC-07 |

