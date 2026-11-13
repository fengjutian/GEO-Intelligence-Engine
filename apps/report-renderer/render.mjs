import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const TEMPLATE_VERSION = 'report-html-v1'

const [, , inputPath, outputPath] = process.argv
if (!inputPath || !outputPath) {
  console.error('usage: node render.mjs <report.json> <output.pdf>')
  process.exit(2)
}

function findChromium() {
  const candidates = [
    process.env.GEO_CHROMIUM_PATH,
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
    '/usr/bin/google-chrome',
  ].filter(Boolean)
  return candidates.find((candidate) => existsSync(candidate))
}

const report = JSON.parse(readFileSync(inputPath, 'utf8').replace(/^\uFEFF/, ''))
const html = renderReport(report)

const browser = await chromium.launch({ headless: true, executablePath: findChromium(), args: ['--no-sandbox'] })
try {
  const version = browser.version()
  const page = await browser.newPage()
  await page.route('**/*', (route) => route.abort())
  await page.setContent(html, { waitUntil: 'load' })
  await page.pdf({
    path: outputPath,
    format: 'A4',
    printBackground: true,
    displayHeaderFooter: true,
    headerTemplate: '<div></div>',
    footerTemplate: `<div style="width:100%;padding:0 14mm;font-size:8px;color:#66756d;display:flex;justify-content:space-between"><span>GEO Intelligence Engine · ${TEMPLATE_VERSION}</span><span><span class="pageNumber"></span> / <span class="totalPages"></span></span></div>`,
    margin: { top: '16mm', bottom: '16mm', left: '14mm', right: '14mm' },
  })
  writeFileSync(`${outputPath}.meta.json`, JSON.stringify({ templateVersion: TEMPLATE_VERSION, chromiumVersion: version, renderer: 'playwright-chromium' }))
  process.stdout.write(JSON.stringify({ ok: true, chromiumVersion: version, templateVersion: TEMPLATE_VERSION }))
} finally {
  await browser.close()
}

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

function percent(value) {
  if (value === null || value === undefined) return '—'
  return `${(Number(value) * 100).toFixed(1)}%`
}

function interval(metric) {
  if (metric.ciLow === null || metric.ciHigh === null) return '—'
  return `${(Number(metric.ciLow) * 100).toFixed(1)}% – ${(Number(metric.ciHigh) * 100).toFixed(1)}%`
}

function sectionMap(report) {
  return Object.fromEntries((report.sections ?? []).map((section) => [section.code, section.content ?? {}]))
}

function renderMetrics(values) {
  if (!Array.isArray(values) || values.length === 0) return '<p class="muted">没有可用指标。</p>'
  const rows = values
    .map((metric) => {
      const dimensions = metric.dimensions ?? {}
      const scope = dimensions.engineConfigId
        ? `${dimensions.provider ?? ''} / ${dimensions.modelSelector ?? ''} · ${dimensions.channel ?? ''}`
        : `全部 · ${dimensions.channel ?? ''}`
      return `<tr><td>${escapeHtml(metric.metricCode)}</td><td>${escapeHtml(scope)}</td><td>${escapeHtml(metric.numerator ?? '—')} / ${escapeHtml(metric.denominator ?? '—')}</td><td>${percent(metric.value)}</td><td>${interval(metric)}</td></tr>`
    })
    .join('')
  return `<table><thead><tr><th>Metric</th><th>Scope</th><th>Numerator / Denominator</th><th>Value</th><th>95% CI</th></tr></thead><tbody>${rows}</tbody></table>`
}

function renderFindings(items) {
  if (!Array.isArray(items) || items.length === 0) return '<p class="muted">没有技术发现。</p>'
  return `<table><thead><tr><th>Rule</th><th>Severity</th><th>Summary</th></tr></thead><tbody>${items
    .map((finding) => `<tr><td>${escapeHtml(finding.ruleCode)}</td><td>${escapeHtml(finding.severity)}</td><td>${escapeHtml(finding.summary)}</td></tr>`)
    .join('')}</tbody></table>`
}

function renderPriorities(items) {
  if (!Array.isArray(items) || items.length === 0) return '<p class="muted">没有开放的技术发现，无需排序。</p>'
  return `<table><thead><tr><th>Priority</th><th>Rule</th><th>Severity</th><th>Score</th></tr></thead><tbody>${items
    .map((item) => `<tr><td><strong>${escapeHtml(item.priority)}</strong></td><td>${escapeHtml(item.ruleCode)}</td><td>${escapeHtml(item.severity)}</td><td>${Number(item.score ?? 0).toFixed(3)}</td></tr>`)
    .join('')}</tbody></table>`
}

function renderReport(report) {
  const sections = sectionMap(report)
  const summary = sections.summary ?? {}
  const scope = sections.scope ?? {}
  const metrics = sections.metrics ?? {}
  const findings = sections.findings ?? {}
  const priorities = sections.priorities ?? {}
  const limitations = sections.limitations ?? {}
  const limitationsHtml = Array.isArray(limitations.items) && limitations.items.length > 0
    ? `<ul>${limitations.items.map((item) => `<li>${escapeHtml(item)}</li>`).join('')}</ul>`
    : '<p class="muted">无额外限制声明。</p>'
  const scopeRows = Object.entries({ ...scope, protocol: undefined })
    .filter(([, value]) => value !== undefined && value !== null && typeof value !== 'object')
    .map(([key, value]) => `<tr><th>${escapeHtml(key)}</th><td>${escapeHtml(value)}</td></tr>`)
    .join('')
  return `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>GEO Audit Report</title>
<style>
@page { size: A4; margin: 16mm 14mm; }
body { font-family: "Microsoft YaHei", "Noto Sans CJK SC", "PingFang SC", Arial, sans-serif; color:#1c2b23; font-size:11px; line-height:1.5; }
h1 { margin:0; font-size:24px; letter-spacing:-0.02em; }
h2 { margin:20px 0 8px; font-size:15px; border-bottom:1px solid #d8e2dc; padding-bottom:4px; }
.eyebrow { color:#4c7a62; font-size:10px; letter-spacing:.12em; text-transform:uppercase; margin:0 0 4px; }
.meta { color:#5c6f65; margin:4px 0 0; }
.cards { display:flex; gap:8px; margin:14px 0; }
.card { flex:1; padding:10px; border:1px solid #d8e2dc; border-radius:6px; }
.card span { display:block; color:#5c6f65; font-size:9px; }
.card strong { display:block; font-size:18px; margin-top:2px; }
table { width:100%; border-collapse:collapse; margin-top:6px; }
th, td { border-bottom:1px solid #e6ece8; padding:5px 6px; text-align:left; vertical-align:top; }
th { color:#5c6f65; font-weight:600; font-size:10px; }
.muted { color:#7b8b82; }
ul { margin:6px 0; padding-left:18px; }
.footer-note { margin-top:18px; padding-top:8px; border-top:1px solid #d8e2dc; color:#7b8b82; font-size:9px; }
</style></head>
<body>
<p class="eyebrow">GEO Audit Report · ${TEMPLATE_VERSION}</p>
<h1>${escapeHtml(report.projectId ?? 'GEO Audit')}</h1>
<p class="meta">Audit ${escapeHtml(report.auditId)} · 报告 v${escapeHtml(report.version)} · ${escapeHtml(summary.generatedAt ?? '')}</p>
<div class="cards">
  <div class="card"><span>计划样本</span><strong>${escapeHtml(summary.plannedCount ?? '—')}</strong></div>
  <div class="card"><span>完成样本</span><strong>${escapeHtml(summary.completedCount ?? '—')}</strong></div>
  <div class="card"><span>有效回答</span><strong>${escapeHtml(summary.eligibleCount ?? '—')}</strong></div>
  <div class="card"><span>审计状态</span><strong>${escapeHtml(summary.status ?? '—')}</strong></div>
</div>

<h2>数据范围与版本</h2>
<table>${scopeRows}</table>

<h2>核心指标</h2>
${renderMetrics(metrics.values)}

<h2>技术发现</h2>
${renderFindings(findings.items)}

<h2>优先级建议</h2>
${renderPriorities(priorities.items)}

<h2>限制声明</h2>
${limitationsHtml}

<p class="footer-note">本报告由 ${TEMPLATE_VERSION} 生成，指标口径 ${escapeHtml(metrics.metricVersion ?? '')}，Judge ${escapeHtml(scope.judgeVersion ?? '')}。部分样本缺失时不得作为完整审计或连续趋势使用。</p>
</body></html>`
}
