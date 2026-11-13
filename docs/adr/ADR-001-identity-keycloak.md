# ADR-001：身份平台使用 Keycloak

> 状态：已批准｜决策日期：2026-11-13｜基线版本：Keycloak 26.8.2

## 背景

P0 需要 OIDC、MFA、会话撤销和未来企业身份联合，不应自行实现密码和认证协议。

## 决策

采用 Keycloak 26.8.2 作为 P0 身份提供方，使用 Authorization Code + PKCE。应用维护租户 Membership、项目范围和业务 RBAC；Keycloak 只负责身份认证、MFA 和会话。容器镜像必须固定 Digest，不使用 `latest`。

Realm：每个部署环境一个 Realm；租户不是 Realm。稳定 `sub` 是本地用户唯一外部标识，邮箱不作为合并依据。生产强制管理账号 MFA，禁用 Direct Access Grants。

## 结果

应用无需保存密码；需要维护 Keycloak 升级和备份。身份平台不可用时已有短会话可按策略继续，不能创建新会话。

## 参考

- [Keycloak 26.8.2 Downloads](https://www.keycloak.org/downloads)

