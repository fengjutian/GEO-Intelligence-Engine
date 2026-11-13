# ADR-004：PDF 报告渲染

> 状态：已批准｜决策日期：2026-11-13

## 决策

使用独立 TypeScript `report-renderer`：React 静态打印模板生成 HTML，固定版本 Playwright Chromium 使用 `page.pdf()` 输出 PDF。图表在服务端渲染为 SVG；字体随镜像固定并包含中英文字符集。

渲染工件记录模板版本、数据快照 ID、Chromium 版本、镜像 Digest、字体包版本和 SHA-256。禁用外部网络、用户脚本和远程字体；只读取经过校验的报告 JSON 和本地静态资源。

PDF 使用 A4、CSS `@page`、打印背景和 tagged PDF（运行时支持时启用）。相同输入要求内容与视觉快照一致，不要求 PDF 二进制逐字节一致。

## 参考

- [Playwright page.pdf](https://playwright.dev/docs/api/class-page#page-pdf)

