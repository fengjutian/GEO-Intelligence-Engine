# ADR-002：数据库迁移使用 golang-migrate

> 状态：已批准｜决策日期：2026-11-13｜基线版本：golang-migrate 4.20.1

## 决策

使用 golang-migrate 4.20.1 管理 MySQL 8.4 LTS Schema。迁移目录为 `db/migrations`，文件使用六位递增编号和 `.up.sql/.down.sql`。生产迁移由独立受控 Job 执行。

已在生产执行的迁移不可修改。破坏性变更采用 expand/migrate/contract；Down Migration 只用于本地和测试，生产回滚优先使用应用回滚与前向修复。

## 门禁

CI 必须从空库执行全部 Up、运行结构测试，并从上一发布版本升级。迁移包含前置条件、数据校验和预估锁表影响。

## 参考

- [golang-migrate Releases](https://github.com/golang-migrate/migrate/releases)

