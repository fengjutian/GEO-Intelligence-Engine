# ADR-005：P0 运行平台与基础服务

> 状态：已批准｜决策日期：2026-11-13

## 决策

本地开发使用 Docker Compose、MySQL 8.4、Redis、MinIO、Keycloak 和 OpenTelemetry Collector。

P0 默认生产基线为 AWS：ECS Fargate 运行容器，RDS MySQL 8.4、ElastiCache Redis、S3、AWS Secrets Manager/KMS、CloudFront/WAF 和托管日志。应用层保持 S3、OIDC 和 Secret Provider 接口，未来私有化可替换实现，但 P0 部署脚本只保证该基线。

生产资源版本、区域和镜像 Digest通过环境清单冻结，不写死在业务代码。Crawler 使用独立任务定义、子网和安全组，禁止访问内部数据面和元数据服务。

## 结果

不在 P0 引入 Kubernetes、Vault 或自建对象存储生产集群。MinIO 仅用于本地和测试。

