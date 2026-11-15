import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { TaskBoard } from '../tasks/TaskBoard'

type EngineConfig = { id: string; provider: string; channel: string; status: string }
type QuestionSet = { id: string; draftVersionId: string; draftVersion: number; versions: Array<{ id: string; version: number; questionCount: number }> }
type Protocol = { id: string; semanticVersion: string; status: string }
type Audit = { id: string; status: string; plannedCount: number; completedCount: number; eligibleCount: number; budgetMicrounits: number; currency: string }
type Report = { id: string; version: number; status: string; sections: Array<{ code: string; content: Record<string, unknown> }> }
type MetricValue = {
  id: string
  metricCode: string
  aggregationMethod: string
  dimensions: Record<string, unknown>
  numerator: number | null
  denominator: number | null
  value: number | null
  ciLow: number | null
  ciHigh: number | null
  intervalMethod: string
}

type TrendValue = { value: number | null; numerator: number | null; denominator: number | null; ciLow: number | null; ciHigh: number | null }
type TrendPoint = { auditId: string; finishedAt: string | null; metricVersion: string; plannedCount: number; eligibleCount: number; metrics: Record<string, TrendValue>; delta: Record<string, number | null> }
type AuditTrend = { metrics: string[]; points: TrendPoint[] }
type MetricDelta = { metricCode: string; baseValue: number | null; targetValue: number | null; delta: number | null; baseNumerator: number | null; baseDenominator: number | null; targetNumerator: number | null; targetDenominator: number | null }
type AuditComparison = { baseAuditId: string; targetAuditId: string; baseCounts: { plannedCount: number; completedCount: number; eligibleCount: number }; targetCounts: { plannedCount: number; completedCount: number; eligibleCount: number }; metrics: MetricDelta[] }
type EngineSeries = { engineConfigId: string; provider: string; modelSelector: string; metrics: Record<string, TrendValue>; delta: Record<string, number | null> }
type EngineTrendPoint = { auditId: string; finishedAt: string | null; engines: EngineSeries[] }
type EngineTrend = { metrics: string[]; points: EngineTrendPoint[] }
type ShareEntity = { name: string; type: string; mentions: number; answers: number; share: number; mentionShare: number; positive: number; negative: number; neutral: number }
type ShareOfVoice = { totalAnswers: number; brandShare: number; entities: ShareEntity[] }
type StoredAnswer = { id: string; question: string; provider: string; model: string; content: string; contentValidity: string; answerOutcome: string; eligible: boolean }
type ShareTrendEntity = { entity: string; entityType: string; share: number | null; delta: number | null }
type ShareTrendPoint = { auditId: string; finishedAt: string | null; entities: ShareTrendEntity[] }
type ShareTrend = { points: ShareTrendPoint[] }

type Props = { projectId: string; projectName: string; onClose: () => void }

const TERMINAL = ['succeeded', 'partial_success', 'failed', 'cancelled']
const METRIC_LABELS: Record<string, string> = {
  mention_rate: '品牌提及率',
  recommendation_rate: '主动推荐率',
  refusal_rate: '拒答率',
  positive_mention_rate: '正向提及率',
  official_citation_rate: '官网引用率',
  third_party_citation_rate: '第三方引用率',
  eligible_response_rate: '有效回答率',
  mention_frequency: '平均提及次数',
  share_of_voice: '回答覆盖率',
  mention_share: '提及份额',
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

function formatPercent(value: number | null): string {
  if (value === null) return '—'
  return `${(value * 100).toFixed(1)}%`
}

function formatInterval(metric: MetricValue): string {
  if (metric.ciLow === null || metric.ciHigh === null) return ''
  return `95% CI ${(metric.ciLow * 100).toFixed(1)}–${(metric.ciHigh * 100).toFixed(1)}%`
}

export function AuditDialog({ projectId, projectName, onClose }: Props) {
  const queryClient = useQueryClient()
  const storageKey = `geo.audit.${projectId}`
  const questionSets = useQuery({ queryKey: ['question-sets', projectId], queryFn: () => request<{ items: QuestionSet[] }>(`/api/v1/projects/${projectId}/question-sets`) })
  const protocols = useQuery({ queryKey: ['protocols', projectId], queryFn: () => request<{ items: Protocol[] }>(`/api/v1/projects/${projectId}/measurement-protocols`) })
  const engines = useQuery({ queryKey: ['engine-configs'], queryFn: () => request<{ items: EngineConfig[] }>('/api/v1/engine-configs') })
  const [samples, setSamples] = useState(1)
  const [budget, setBudget] = useState(100000)
  const [costPerSample, setCostPerSample] = useState(1000)
  const [auditId, setAuditId] = useState<string | null>(() => window.localStorage.getItem(storageKey))
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [report, setReport] = useState<Report | null>(null)
  const [reportBusy, setReportBusy] = useState(false)
  const [evidence, setEvidence] = useState<{ members: Array<Record<string, unknown>>; memberCount: number; truncated?: boolean } | null>(null)
  const [evidenceLoading, setEvidenceLoading] = useState(false)
  const [taskStates, setTaskStates] = useState<Record<string, string>>({})
  const [taskBoardOpen, setTaskBoardOpen] = useState(false)
  const audit = useQuery({
    queryKey: ['audit', auditId],
    queryFn: () => request<Audit>(`/api/v1/audits/${auditId}`),
    enabled: auditId !== null,
    refetchInterval: (query) => TERMINAL.includes(query.state.data?.status ?? '') ? false : 2000,
  })
  const trend = useQuery({
    queryKey: ['audit-trends', projectId],
    queryFn: () => request<AuditTrend>(`/api/v1/projects/${projectId}/audit-trends?limit=12`),
  })
  const previousAuditId = trend.data && trend.data.points.length > 1 ? trend.data.points[trend.data.points.length - 2].auditId : null
  const comparison = useQuery({
    queryKey: ['audit-comparison', auditId, previousAuditId],
    queryFn: () => request<AuditComparison>(`/api/v1/audits/${auditId}/comparison?baseAuditId=${previousAuditId}`),
    enabled: Boolean(auditId && previousAuditId && auditId !== previousAuditId),
  })
  const engineTrend = useQuery({
    queryKey: ['audit-engine-trends', projectId],
    queryFn: () => request<EngineTrend>(`/api/v1/projects/${projectId}/audit-trends?limit=12&dimension=engine`),
  })
  const shareOfVoice = useQuery({
    queryKey: ['share-of-voice', auditId],
    queryFn: () => request<ShareOfVoice>(`/api/v1/audits/${auditId}/share-of-voice`),
    enabled: Boolean(auditId),
  })
  const storedAnswers = useQuery({
    queryKey: ['stored-answers', auditId],
    queryFn: () => request<{ items: StoredAnswer[] }>(`/api/v1/audits/${auditId}/answers?limit=20`),
    enabled: Boolean(auditId),
  })
  const shareTrend = useQuery({
    queryKey: ['share-trends', projectId],
    queryFn: () => request<ShareTrend>(`/api/v1/projects/${projectId}/audit-trends?limit=12&dimension=share`),
  })
  const metrics = useQuery({
    queryKey: ['audit-metrics', auditId],
    queryFn: () => request<MetricValue[]>(`/api/v1/audits/${auditId}/metrics`),
    enabled: auditId !== null && ['succeeded', 'partial_success'].includes(audit.data?.status ?? ''),
  })

  function rememberAudit(id: string | null) {
    setAuditId(id)
    if (id) window.localStorage.setItem(storageKey, id)
    else window.localStorage.removeItem(storageKey)
  }

  async function ensureEngines(): Promise<EngineConfig[]> {
    const existing = engines.data?.items.filter((engine) => engine.status === 'active') ?? []
    if (existing.length >= 1) return existing
    const created: EngineConfig[] = []
    for (const provider of ['deepseek', 'minimax']) {
      created.push(await request<EngineConfig>('/api/v1/engine-configs', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ provider, channel: 'model_api', credentialRef: `secret://env/${provider.toUpperCase()}_API_KEY`, region: 'CN' }),
      }))
    }
    await queryClient.invalidateQueries({ queryKey: ['engine-configs'] })
    return created
  }

  async function startAudit() {
    setBusy(true)
    setMessage('')
    try {
      const set = questionSets.data?.items[0]
      if (!set) throw new Error('请先在项目问题空间中添加问题')
      let questionVersionId = set.versions[0]?.id
      if (!questionVersionId) {
        const published = await request<{ id: string }>(`/api/v1/question-sets/${set.id}/versions`, { method: 'POST' })
        questionVersionId = published.id
        await queryClient.invalidateQueries({ queryKey: ['question-sets', projectId] })
      }
      const activeEngines = await ensureEngines()
      const protocol = await request<Protocol>(`/api/v1/projects/${projectId}/measurement-protocols`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          questionVersionId,
          engines: activeEngines.map((engine) => ({ engineConfigId: engine.id, modelSelector: engine.provider === 'deepseek' ? 'deepseek-flash' : 'MiniMax-M2.5', channel: engine.channel, parameters: { temperature: 0.2 } })),
          region: 'CN',
          language: 'zh-CN',
          samplesPerQuestion: samples,
          budgetMicrounits: budget,
          currency: 'CNY',
          judgeVersion: 'judge-v1',
          metricVersion: 'metrics-v1',
          promptTemplateVersion: 'prompt-v1',
          estimatedCostPerSample: costPerSample,
        }),
      })
      await request(`/api/v1/measurement-protocols/${protocol.id}:publish`, { method: 'POST' })
      const estimate = await request<{ plannedSamples: number }>('/api/v1/audits:estimate', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ projectId, protocolId: protocol.id }),
      })
      const created = await request<Audit>('/api/v1/audits', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'Idempotency-Key': `${projectId}-${Date.now()}` },
        body: JSON.stringify({ projectId, protocolId: protocol.id }),
      })
      rememberAudit(created.id)
      await queryClient.invalidateQueries({ queryKey: ['audit', created.id] })
      setMessage(`协议 ${protocol.semanticVersion} 已发布，预计 ${estimate.plannedSamples} 个样本`)
      await queryClient.invalidateQueries({ queryKey: ['protocols', projectId] })
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '发起审计失败')
    } finally {
      setBusy(false)
    }
  }

  async function cancelAudit() {
    if (!auditId) return
    setBusy(true)
    try {
      await request<Audit>(`/api/v1/audits/${auditId}:cancel`, { method: 'POST' })
      await queryClient.invalidateQueries({ queryKey: ['audit', auditId] })
      setMessage('审计已取消，已排队样本标记为 cancelled')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '取消失败')
    } finally {
      setBusy(false)
    }
  }

  async function generateReport() {
    if (!auditId) return
    setReportBusy(true)
    try {
      const created = await request<Report>(`/api/v1/audits/${auditId}/reports`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ templateVersion: 'report-v1' }),
      })
      setReport(created)
      setMessage(`报告 v${created.version} 已生成`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '报告生成失败')
    } finally {
      setReportBusy(false)
    }
  }

  async function downloadExport(format: 'csv' | 'pdf') {
    if (!report) return
    setReportBusy(true)
    try {
      const result = await request<{ downloadUrl: string }>(`/api/v1/reports/${report.id}/exports`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ format }),
      })
      window.location.assign(result.downloadUrl)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : `${format.toUpperCase()} 导出失败`)
    } finally {
      setReportBusy(false)
    }
  }

  async function loadEvidence(metric: MetricValue) {
    setEvidenceLoading(true)
    try {
      const result = await request<{ members: Array<Record<string, unknown>>; memberCount: number; truncated?: boolean }>(`/api/v1/metric-values/${metric.id}/evidence`)
      setEvidence(result)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '指标证据加载失败')
    } finally {
      setEvidenceLoading(false)
    }
  }

  async function publishReport() {
    if (!report) return
    setReportBusy(true)
    try {
      const updated = await request<Report>(`/api/v1/reports/${report.id}:publish`, { method: 'POST' })
      setReport(updated)
      setMessage('报告已发布')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '报告发布失败')
    } finally {
      setReportBusy(false)
    }
  }

  async function createTask(findingId: string, ruleCode: string) {
    try {
      const task = await request<{ id: string; status: string }>(`/api/v1/findings/${findingId}/optimization-tasks`, {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'Idempotency-Key': `task-${findingId}-${Date.now()}` },
        body: JSON.stringify({ title: `修复 ${ruleCode}`, description: '由审计报告优先级建议创建', acceptanceItems: ['确认问题已修复', '重新审计验证'] }),
      })
      setTaskStates((current) => ({ ...current, [findingId]: task.status }))
      setMessage(`任务 ${task.id} 已创建`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '任务创建失败')
    }
  }

  const current = audit.data
  const priorityItems = report?.sections.find((section) => section.code === 'priorities')?.content.items
  const priorityList = Array.isArray(priorityItems) ? priorityItems as Array<Record<string, unknown>> : []
  const priorityCount = priorityList.length
  const channelMetrics = (metrics.data ?? []).filter((metric) => !('engineConfigId' in metric.dimensions))
  const engineMetrics = (metrics.data ?? []).filter((metric) => 'engineConfigId' in metric.dimensions)

  return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
    <section className="audit-dialog" aria-labelledby="audit-title" onMouseDown={(event) => event.stopPropagation()}>
      <div className="profile-header">
        <div><p className="eyebrow">GEO AUDIT</p><h2 id="audit-title">{projectName} · 发起审计</h2><p>冻结问题集与品牌知识，展开确定性采样并排队。</p></div>
        <button type="button" aria-label="关闭" onClick={onClose}>×</button>
      </div>
      <div className="audit-summary">
        <div><small>已发布协议</small><strong>{protocols.data?.items.filter((protocol) => protocol.status === 'published').length ?? 0}</strong></div>
        <div><small>可用引擎</small><strong>{engines.data?.items.length ?? 0}</strong></div>
        <div><small>问题集版本</small><strong>{questionSets.data?.items[0]?.versions.length ?? 0}</strong></div>
      </div>
      <div className="audit-form">
        <label>每问题采样次数<input type="number" min="1" max="10" value={samples} onChange={(event) => setSamples(Number(event.target.value))} /></label>
        <label>预算（微单位）<input type="number" min="0" value={budget} onChange={(event) => setBudget(Number(event.target.value))} /></label>
        <label>单样本估算成本<input type="number" min="0" value={costPerSample} onChange={(event) => setCostPerSample(Number(event.target.value))} /></label>
      </div>
      <p className="audit-note">使用当前项目最新的已发布问题集；若只有草稿，会先发布为不可变版本。没有可用引擎配置时会按环境密钥引用自动创建 DeepSeek 与 MiniMax 配置。</p>
      <div className="dialog-actions">
        <button type="button" onClick={onClose}>关闭</button>
        <button className="primary-button" type="button" disabled={busy} onClick={() => void startAudit()}>{busy ? '处理中…' : '创建协议并发起审计'}</button>
      </div>
      {message && <p className="audit-message" role="status">{message}</p>}
      {audit.error && <p className="error" role="alert">{audit.error.message}</p>}
      {current && <article className="audit-result">
        <header><strong>{current.id}</strong><span data-status={current.status}>{current.status}</span></header>
        <div><small>计划样本</small><strong>{current.plannedCount}</strong></div>
        <div><small>完成 / 有效</small><strong>{current.completedCount} / {current.eligibleCount}</strong></div>
        <div><small>预算</small><strong>{current.budgetMicrounits} {current.currency}</strong></div>
        {['queued', 'running'].includes(current.status) && <button type="button" onClick={() => void cancelAudit()} disabled={busy}>取消审计</button>}
      </article>}
      {current && !TERMINAL.includes(current.status) && <p className="audit-note">Worker 正在处理，状态每 2 秒自动刷新。</p>}
      {current && TERMINAL.includes(current.status) && metrics.isLoading && <p className="audit-note">正在加载指标…</p>}
      {metrics.error && <p className="error" role="alert">{metrics.error.message}</p>}
      {channelMetrics.length > 0 && <section className="audit-metrics">
        <h3>核心指标</h3>
        <div className="metric-cards">
          {channelMetrics.map((metric) => <article key={`${metric.metricCode}-${String(metric.dimensions.channel)}`} role="button" tabIndex={0} title="查看构成证据" onClick={() => void loadEvidence(metric)} onKeyDown={(event) => { if (event.key === 'Enter') void loadEvidence(metric) }}>
            <small>{METRIC_LABELS[metric.metricCode] ?? metric.metricCode}</small>
            <strong>{formatPercent(metric.value)}</strong>
            <span>{metric.numerator ?? '—'} / {metric.denominator ?? '—'}</span>
            <em>{formatInterval(metric)}</em>
            {metric.denominator !== null && metric.denominator < 30 && <b>小样本</b>}
          </article>)}
        </div>
      </section>}
      {engineMetrics.length > 0 && <section className="audit-metrics">
        <h3>按引擎 / 渠道</h3>
        <div className="metric-table">
          <div className="metric-row metric-head"><span>指标</span><span>引擎</span><span>渠道</span><span>比例</span><span>95% CI</span></div>
          {engineMetrics.map((metric) => <div className="metric-row" key={`${metric.metricCode}-${String(metric.dimensions.engineConfigId)}`}>
            <span>{METRIC_LABELS[metric.metricCode] ?? metric.metricCode}</span>
            <span>{String(metric.dimensions.provider)} / {String(metric.dimensions.modelSelector)}</span>
            <span>{String(metric.dimensions.channel)}</span>
            <span>{formatPercent(metric.value)}（{metric.numerator ?? '—'}/{metric.denominator ?? '—'}）</span>
            <span>{formatInterval(metric) || '—'}</span>
          </div>)}
        </div>
      </section>}
      {trend.data && trend.data.points.length > 1 && <section className="audit-metrics audit-trend">
        <h3>跨审计趋势 · {trend.data.points.length} 次</h3>
        <div className="trend-grid">
          {trend.data.points.slice(-4).map((point) => <article key={point.auditId}>
            <small>{point.finishedAt ? new Date(point.finishedAt).toLocaleDateString() : '未完成'} · {point.metricVersion}</small>
            {['mention_rate', 'recommendation_rate', 'official_citation_rate'].map((code) => {
              const metric = point.metrics[code]
              const delta = point.delta[code]
              return <div key={code}>
                <span>{METRIC_LABELS[code]}</span>
                <strong>{formatPercent(metric?.value ?? null)}</strong>
                <em data-direction={delta == null ? 'flat' : delta > 0 ? 'up' : delta < 0 ? 'down' : 'flat'}>{delta == null ? '—' : `${delta >= 0 ? '+' : ''}${(delta * 100).toFixed(1)}pp`}</em>
              </div>
            })}
          </article>)}
        </div>
        <p className="audit-note">趋势按审计完成时间排序；口径取各审计最新可用指标快照（含人工回写版本）。</p>
      </section>}
      {engineTrend.data && engineTrend.data.points.length > 0 && <section className="audit-metrics audit-engine-trend">
        <h3>按引擎 · 最新审计</h3>
        <div className="metric-table">
          <div className="metric-row metric-head"><span>引擎</span><span>提及率</span><span>推荐率</span><span>官网引用率</span><span>变化</span></div>
          {engineTrend.data.points[engineTrend.data.points.length - 1].engines.map((engine) => <div className="metric-row" key={engine.engineConfigId}>
            <span>{engine.provider} / {engine.modelSelector}</span>
            <span>{formatPercent(engine.metrics.mention_rate?.value ?? null)}</span>
            <span>{formatPercent(engine.metrics.recommendation_rate?.value ?? null)}</span>
            <span>{formatPercent(engine.metrics.official_citation_rate?.value ?? null)}</span>
            <span data-direction={(engine.delta.mention_rate ?? 0) > 0 ? 'up' : (engine.delta.mention_rate ?? 0) < 0 ? 'down' : 'flat'}>{engine.delta.mention_rate == null ? '—' : `${engine.delta.mention_rate >= 0 ? '+' : ''}${(engine.delta.mention_rate * 100).toFixed(1)}pp`}</span>
          </div>)}
        </div>
      </section>}
      {shareOfVoice.data && shareOfVoice.data.totalAnswers > 0 && <section className="audit-metrics audit-share">
        <h3>Share of Voice · 品牌覆盖 {Math.round(shareOfVoice.data.brandShare * 100)}%</h3>
        <div className="metric-table">
          <div className="metric-row metric-head"><span>实体</span><span>类型</span><span>出现回答</span><span>提及次数</span><span>回答覆盖</span><span>提及份额</span><span>正/负/中</span></div>
          {shareOfVoice.data.entities.map((entity) => <div className="metric-row" key={`${entity.type}-${entity.name}`}>
            <span>{entity.name}</span>
            <span>{entity.type === 'brand' ? '品牌' : '竞品'}</span>
            <span>{entity.answers} / {shareOfVoice.data.totalAnswers}</span>
            <span>{entity.mentions}</span>
            <span>{Math.round(entity.share * 100)}%</span>
            <span>{Math.round(entity.mentionShare * 100)}%</span>
            <span>{entity.positive}/{entity.negative}/{entity.neutral}</span>
          </div>)}
        </div>
      </section>}
      {storedAnswers.data && storedAnswers.data.items.length > 0 && <section className="audit-metrics audit-answers">
        <h3>回答正文 · {storedAnswers.data.items.length} 条</h3>
        {storedAnswers.data.items.map((answer) => <details key={answer.id}>
          <summary>{answer.question}<small>{answer.answerOutcome} · {answer.contentValidity} · {answer.content.length} 字</small></summary>
          <p>{answer.content}</p>
        </details>)}
      </section>}
      {shareTrend.data && shareTrend.data.points.length > 0 && <section className="audit-metrics audit-share-trend">
        <h3>Share of Voice 趋势 · {shareTrend.data.points.length} 次</h3>
        <div className="metric-table">
          <div className="metric-row metric-head"><span>实体</span><span>类型</span><span>最新份额</span><span>变化</span></div>
          {shareTrend.data.points[shareTrend.data.points.length - 1].entities.map((entity) => <div className="metric-row" key={`${entity.entityType}-${entity.entity}`}>
            <span>{entity.entity}</span>
            <span>{entity.entityType === 'brand' ? '品牌' : '竞品'}</span>
            <span>{entity.share == null ? '—' : `${Math.round(entity.share * 100)}%`}</span>
            <span data-direction={entity.delta == null ? 'flat' : entity.delta > 0 ? 'up' : entity.delta < 0 ? 'down' : 'flat'}>{entity.delta == null ? '—' : `${entity.delta >= 0 ? '+' : ''}${(entity.delta * 100).toFixed(1)}pp`}</span>
          </div>)}
        </div>
      </section>}
      {comparison.data && <section className="audit-metrics audit-compare">
        <h3>与上次审计对比</h3>
        <div className="metric-table">
          <div className="metric-row metric-head"><span>指标</span><span>上次</span><span>本次</span><span>变化</span></div>
          {comparison.data.metrics.slice(0, 5).map((metric) => <div className="metric-row" key={metric.metricCode}>
            <span>{METRIC_LABELS[metric.metricCode] ?? metric.metricCode}</span>
            <span>{formatPercent(metric.baseValue)}</span>
            <span>{formatPercent(metric.targetValue)}</span>
            <span data-direction={metric.delta == null ? 'flat' : metric.delta > 0 ? 'up' : metric.delta < 0 ? 'down' : 'flat'}>{metric.delta == null ? '—' : `${metric.delta >= 0 ? '+' : ''}${(metric.delta * 100).toFixed(1)}pp`}</span>
          </div>)}
        </div>
        <p className="audit-note">样本：计划 {comparison.data.baseCounts.plannedCount} → {comparison.data.targetCounts.plannedCount}；有效 {comparison.data.baseCounts.eligibleCount} → {comparison.data.targetCounts.eligibleCount}。</p>
      </section>}
      {evidence && <section className="audit-metrics evidence-panel">
        <h3>指标证据 · {evidence.memberCount} 条{evidence.truncated ? '（已截断）' : ''} <button type="button" className="evidence-close" onClick={() => setEvidence(null)}>关闭</button></h3>
        <div className="metric-table">
          <div className="metric-row evidence-row metric-head"><span>问题</span><span>回答结果</span><span>推荐</span><span>情感</span><span>提及</span><span>官网引用</span><span>三方引用</span></div>
          {evidence.members.map((member, index) => <div className="metric-row evidence-row" key={index}>
            <span title={String(member.questionId)}>{String(member.questionId)}</span>
            <span>{String(member.answerOutcome)}</span>
            <span>{String(member.recommendationLabel)}</span>
            <span>{String(member.sentiment)}</span>
            <span>{String(member.mentionCount)}</span>
            <span>{String(member.officialCitationCount ?? 0)}</span>
            <span>{String(member.thirdPartyCitationCount ?? 0)}</span>
          </div>)}
        </div>
      </section>}
      {priorityList.length > 0 && <section className="audit-metrics">
        <h3>优先级建议 <button type="button" className="task-board-link" onClick={() => setTaskBoardOpen(true)}>任务看板</button></h3>
        <div className="metric-table">
          <div className="metric-row priority-row metric-head"><span>优先级</span><span>规则</span><span>分数</span><span>任务</span></div>
          {priorityList.map((item) => {
            const findingId = String(item.findingId)
            const taskStatus = taskStates[findingId]
            return <div className="metric-row priority-row" key={findingId}>
              <span><strong>{String(item.priority)}</strong> · {String(item.severity)}</span>
              <span title={findingId}>{String(item.ruleCode)}</span>
              <span>{Number(item.score ?? 0).toFixed(2)}</span>
              <span>{taskStatus ? taskStatus : <button type="button" onClick={() => void createTask(findingId, String(item.ruleCode))}>创建任务</button>}</span>
            </div>
          })}
        </div>
      </section>}
      {current && ['succeeded', 'partial_success'].includes(current.status) && <section className="audit-report">        <h3>报告</h3>
        {!report && <div className="report-create">
          <p>把指标、技术发现、范围与限制固化为版本化报告；可继续导出 CSV。</p>
          <button className="primary-button" type="button" disabled={reportBusy} onClick={() => void generateReport()}>{reportBusy ? '生成中…' : '生成报告'}</button>
        </div>}
        {report && <div className="report-card">
          <div><small>版本</small><strong>v{report.version}</strong></div>
          <div><small>状态</small><strong>{report.status}</strong></div>
          <div><small>章节 / 优先级</small><strong>{report.sections.length} / {priorityCount}</strong></div>
          {report.status === 'ready' && <button type="button" disabled={reportBusy} onClick={() => void publishReport()}>发布</button>}
          <button type="button" disabled={reportBusy} onClick={() => void downloadExport('csv')}>{reportBusy ? '导出中…' : '导出 CSV'}</button>
          <button type="button" disabled={reportBusy} onClick={() => void downloadExport('pdf')}>导出 PDF</button>
          <span>{report.status === 'published' ? '已发布' : 'PDF 由 Playwright Chromium 渲染'}</span>
        </div>}
        {report && <div className="report-sections">
          {report.sections.map((section) => <span key={section.code}>{reportSectionLabel(section.code)}</span>)}
        </div>}
        {report && (() => {
          const geo = report.sections.find((section) => section.code === 'geo_summary')?.content
          if (!geo) return null
          return <div className="report-geo-summary">
            <div><small>品牌份额</small><strong>{Math.round(Number(geo.brandShare ?? 0) * 100)}%</strong></div>
            <div><small>提及率</small><strong>{Math.round(Number(geo.mentionRate ?? 0) * 100)}%</strong></div>
            <div><small>推荐率</small><strong>{Math.round(Number(geo.recommendationRate ?? 0) * 100)}%</strong></div>
            <div><small>内容高风险</small><strong>{Number(geo.contentQualityHigh ?? 0)}</strong></div>
          </div>
        })()}
      </section>}
      {auditId && <button className="audit-clear" type="button" onClick={() => { rememberAudit(null); setMessage('已清除本项目的审计记录引用') }}>清除记录</button>}
      {taskBoardOpen && <TaskBoard projectId={projectId} projectName={projectName} onClose={() => setTaskBoardOpen(false)} />}
    </section>
  </div>
}

function reportSectionLabel(code: string): string {
  const labels: Record<string, string> = {
    geo_summary: 'GEO 总览',
    competitive_landscape: '竞品格局',
    content_trust: '内容可信度',
    summary: '摘要',
    scope: '范围',
    metrics: '指标',
    share_of_voice: '份额',
    fact_checks: '事实核验',
    findings: '发现',
    priorities: '优先级',
    limitations: '限制',
  }
  return labels[code] ?? code
}
