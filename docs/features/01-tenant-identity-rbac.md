# FEAT-01：组织、租户与权限

> 功能版本：V1.0｜产品版本：P0 V1.0｜状态：研发评审稿  
> 对齐：PRD V1.0 第 4、9.3 节；SPEC-03 V1.0；SPEC-08 V1.0；技术栈 V1.0

## 1. 功能目标

为所有业务资源提供组织级租户隔离、用户身份、成员角色和资源授权，确保任何同步 API、异步任务、缓存、对象和导出都不能跨租户访问。

## 2. 用户故事

- 组织所有者可以邀请、停用成员并分配角色。
- 项目管理员只能管理其被授权的项目。
- 分析师可以运行审计，但不能管理密钥或组织成员。
- 只读用户只能查看，不可修改、运行或导出被限制的数据。
- 安全审计人员可以追踪登录、权限、导出和敏感数据访问。

## 3. 功能范围

### 3.1 P0 包含

- OIDC 登录和会话建立。
- 组织创建、切换和当前租户上下文。
- 成员邀请、接受、停用和角色修改。
- Owner、ProjectAdmin、Analyst、Editor、Approver、Viewer、ServiceAccount 角色。
- 项目级成员授权。
- API Token 创建、作用域、过期和撤销。
- 安全审计日志。

### 3.2 P0 不包含

- 自研密码、MFA 或 SSO 协议。
- 自定义角色编辑器和 ABAC 策略界面。
- 跨组织资源共享。

## 4. 权限模型

授权输入为 `principal + tenant + action + resource + context`。资源操作至少包含 read、create、update、delete、run、review、export、manage_members、manage_credentials。

权限检查顺序：身份有效 → 成员有效 → 租户匹配 → 角色允许动作 → 项目范围允许 → 资源状态允许。拒绝默认返回 404 或统一的 403 策略，避免泄漏其他租户资源存在性。

## 5. 数据模型

- `tenants`：组织、套餐、数据区域和状态。
- `users`：OIDC subject、规范邮箱和状态。
- `memberships`：租户、用户、角色和状态，组合唯一。
- `project_memberships`：可选项目范围。
- `service_accounts`、`api_tokens`：机器身份和 Token 哈希。
- `audit_logs`：不可变安全事件。

业务 Token 只保存哈希；OIDC Provider Secret 只存 Secret Manager 引用。

## 6. API

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/api/v1/me` | 已登录 |
| GET/POST | `/api/v1/tenants` | 用户/允许创建 |
| GET | `/api/v1/tenants/{id}/members` | manage_members 或只读管理权限 |
| POST | `/api/v1/tenants/{id}/invitations` | manage_members |
| PATCH | `/api/v1/tenants/{id}/members/{memberId}` | manage_members |
| POST | `/api/v1/service-accounts` | Owner |
| POST/DELETE | `/api/v1/api-tokens` | Token 所有者或 Owner |
| GET | `/api/v1/audit-logs` | Owner/安全审计权限 |

所有 ID 从 URL 读取后仍按当前 TenantContext 查询，不接受 Body 覆盖 `tenant_id`。

## 7. 实现细节

1. API 中间件验证 OIDC Token，映射本地用户并建立 TenantContext。
2. Repository 方法强制接收 TenantContext，SQL 使用 `(tenant_id,id)`。
3. 权限策略集中在 `internal/identity/authorization`，页面和 Worker 不复制规则。
4. 权限缓存 TTL 不超过 60 秒，成员停用和角色变更主动失效。
5. 异步消息携带 tenant/project ID，Worker 重新从数据库确认归属。
6. 对象签名 URL 由 API 在授权后签发，最长 5 分钟。
7. 邀请 Token 使用一次性随机值，只保存哈希并设过期时间。
8. Owner 不能移除租户最后一个有效 Owner。
9. 安全事件与业务事务使用 Outbox，避免操作成功但日志缺失。

## 8. UI

- 组织切换器。
- 成员列表、邀请 Dialog、角色选择和停用确认。
- Service Account/Token 创建页，只显示一次 Token。
- 审计日志筛选页，支持主体、动作、资源、结果和时间。
- 对无权限操作隐藏入口，同时服务端仍强制检查。

## 9. 异常与边界

- 邀请邮箱已是成员：返回现有状态，不创建重复成员。
- 邀请过期或已使用：返回稳定错误码。
- OIDC 邮箱变化：以稳定 subject 识别，不自动合并其他账号。
- 成员在任务执行中被停用：后续敏感步骤重新授权并停止越权执行。
- Token 泄漏：撤销后缓存和服务身份授权立即失效。

## 10. 可观测性

记录登录成功/失败、邀请、角色变化、Token 创建/撤销、跨租户拒绝、导出和原始证据访问。Metric 包括授权拒绝率、邀请失败率、Token 使用和跨租户探测告警。

## 11. 验收标准

- AC-01：A 租户用户使用 B 租户的项目、页面、回答、报告或对象 ID 时全部被拒绝，且不泄漏资源内容。
- AC-02：Viewer 不能创建项目、运行审计、修改实体或导出被限制报告。
- AC-03：成员角色变更在 60 秒内及主动失效后立即生效。
- AC-04：最后一个 Owner 无法被停用、降级或自行退出。
- AC-05：API Token 只在创建响应显示一次，数据库和日志中不存在明文。
- AC-06：异步 Worker 对伪造 tenant/project 组合拒绝执行并记录安全事件。
- AC-07：所有成员、权限、Token 和敏感查看操作可以在审计日志中查询。
- AC-08：缓存键、对象路径、队列消息和导出均通过跨租户自动化测试。

## 12. 测试重点

OIDC Token 过期、会话撤销、IDOR、批量接口、分页越权、缓存串租、对象签名 URL、任务消息篡改、并发 Owner 变更和审计日志完整性。

