import { FormEvent, useEffect, useState } from 'react'

type Candidate = { questionId: string; question: string; responseId: string; auditId: string; provider: string; channel: string; model: string; content: string; score: number }
type Candidates = { findingId: string; summary: string; page: { contentId: string; url: string; title: string; contentHash: string }; answers: Candidate[] }
type Comparison = { comparable: boolean; differences: string[]; delta?: number; decision: string; reason: string }
type Chain = { findingId: string; status: string; version: number; question: { id: string; text: string }; answer: { id: string; provider: string; model: string; content: string }; page: { contentId: string; url: string; title: string }; task?: { id: string; status: string }; changeSummary: string; expectedOutcome: string; verificationPlan: string; metricCode: string; targetDelta: number; retest?: { auditId: string; status: string; outcome: string }; comparison?: Comparison; events: Array<{ id: string; eventType: string; fromStatus: string; toStatus: string; reason: string; occurredAt: string }> }

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({ title: `请求失败（${response.status}）` })) as { title?: string; detail?: string }
    throw new Error(problem.detail ?? problem.title ?? `请求失败（${response.status}）`)
  }
  return response.json() as Promise<T>
}

export function EvidenceChainEditor({ finding, onClose }: { finding: { id: string; ruleCode: string; summary: string }; onClose: () => void }) {
  const [candidates, setCandidates] = useState<Candidates | null>(null)
  const [chain, setChain] = useState<Chain | null>(null)
  const [selected, setSelected] = useState('')
  const [retestAuditId, setRetestAuditId] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    Promise.all([
      request<Candidates>(`/api/v1/findings/${finding.id}/evidence-candidates`),
      request<Chain>(`/api/v1/findings/${finding.id}/evidence-chain`).catch(() => null),
    ]).then(([nextCandidates, nextChain]) => {
      if (cancelled) return
      setCandidates(nextCandidates)
      setChain(nextChain)
      setSelected(nextChain?.answer.id ?? nextCandidates.answers[0]?.responseId ?? '')
      setRetestAuditId(nextChain?.retest?.auditId ?? '')
    }).catch((error: Error) => setMessage(error.message))
    return () => { cancelled = true }
  }, [finding.id])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const candidate = candidates?.answers.find((item) => item.responseId === selected)
    if (!candidate || !candidates) return
    const data = new FormData(event.currentTarget)
    setBusy(true)
    try {
      const saved = await request<Chain>(`/api/v1/findings/${finding.id}/evidence-chain`, {
        method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify({
          questionId: candidate.questionId, responseId: candidate.responseId, pageContentId: candidates.page.contentId,
          taskId: chain?.task?.id ?? '', retestAuditId,
          changeSummary: String(data.get('changeSummary')), expectedOutcome: String(data.get('expectedOutcome')),
          verificationPlan: String(data.get('verificationPlan')), metricCode: String(data.get('metricCode')),
          targetDelta: Number(data.get('targetDelta')),
        }),
      })
      setChain(saved); setMessage('证据快照已保存')
    } catch (error) { setMessage(error instanceof Error ? error.message : '保存失败') } finally { setBusy(false) }
  }

  async function transition(targetStatus: string) {
    setBusy(true)
    try {
      const updated = await request<Chain>(`/api/v1/findings/${finding.id}/evidence-chain/transition`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ targetStatus, reason: '管理端推进', retestAuditId }) })
      setChain(updated); setMessage(`状态已更新为 ${updated.status}`)
    } catch (error) { setMessage(error instanceof Error ? error.message : '状态更新失败') } finally { setBusy(false) }
  }

  async function createTask() {
    setBusy(true)
    try {
      await request(`/api/v1/findings/${finding.id}/optimization-tasks`, { method: 'POST', headers: { 'content-type': 'application/json', 'Idempotency-Key': `evidence-${finding.id}` }, body: JSON.stringify({ title: `修复 ${finding.ruleCode}`, description: chain?.changeSummary || finding.summary, acceptanceItems: ['按证据链完成页面修改', '执行同协议复测并自动判定'] }) })
      const updated = await request<Chain>(`/api/v1/findings/${finding.id}/evidence-chain`)
      setChain(updated); setMessage('优化任务已创建并关联')
    } catch (error) { setMessage(error instanceof Error ? error.message : '任务创建失败') } finally { setBusy(false) }
  }

  async function evaluate() {
    setBusy(true)
    try {
      const updated = await request<Chain>(`/api/v1/findings/${finding.id}/evidence-chain/evaluate`, { method: 'POST' })
      setChain(updated); setMessage(`复测结论：${updated.comparison?.decision ?? 'inconclusive'}`)
    } catch (error) { setMessage(error instanceof Error ? error.message : '复测判定失败') } finally { setBusy(false) }
  }

  const nextStatus: Record<string, string> = { evidence_ready: 'planned', planned: 'implementing', implementing: 'awaiting_retest', awaiting_retest: 'evaluating' }
  return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}><section className="evidence-chain-dialog" aria-labelledby="evidence-chain-title" onMouseDown={(event) => event.stopPropagation()}>
    <div className="profile-header"><div><p className="eyebrow">EVIDENCE CHAIN</p><h2 id="evidence-chain-title">{finding.ruleCode} · 问题到复测</h2><p>{finding.summary}</p></div><button onClick={onClose} aria-label="关闭">×</button></div>
    {!candidates && !message && <p>正在加载候选证据…</p>}
    {candidates && <form className="evidence-chain-form" onSubmit={save}>
      <label>候选回答<select value={selected} onChange={(event) => setSelected(event.target.value)} required>{candidates.answers.map((item) => <option key={item.responseId} value={item.responseId}>{item.question.slice(0, 55)} · {item.model} · {Math.round(item.score * 100)}%</option>)}</select></label>
      <div className="evidence-chain-page"><strong>{candidates.page.title || '关联页面'}</strong><small>{candidates.page.url}</small><code>{candidates.page.contentHash}</code></div>
      <label>修改说明<textarea name="changeSummary" required defaultValue={chain?.changeSummary} /></label>
      <label>预期结果<textarea name="expectedOutcome" required defaultValue={chain?.expectedOutcome} /></label>
      <label>复测计划<textarea name="verificationPlan" required defaultValue={chain?.verificationPlan} /></label>
      <div className="evidence-chain-fields"><label>指标<select name="metricCode" defaultValue={chain?.metricCode ?? 'mention_rate'} disabled={chain !== null && chain.status !== 'evidence_ready'}><option value="mention_rate">mention_rate</option><option value="recommendation_rate">recommendation_rate</option><option value="positive_mention_rate">positive_mention_rate</option><option value="official_citation_rate">official_citation_rate</option><option value="third_party_citation_rate">third_party_citation_rate</option><option value="eligible_response_rate">eligible_response_rate</option></select></label><label>目标提升<input name="targetDelta" type="number" min="0" max="1" step="0.001" defaultValue={chain?.targetDelta ?? 0.01} disabled={chain !== null && chain.status !== 'evidence_ready'} /></label><label>复测 Audit ID<input name="retestAuditId" value={retestAuditId} onChange={(event) => setRetestAuditId(event.target.value)} placeholder="进入评估前填写" /></label></div>
      <div className="evidence-chain-actions">{(!chain || chain.status === 'evidence_ready') && <button className="primary-button" disabled={busy || candidates.answers.length === 0}>保存证据快照</button>}{chain && !chain.task && <button type="button" onClick={() => void createTask()} disabled={busy}>创建任务</button>}{chain && nextStatus[chain.status] && <button type="button" onClick={() => void transition(nextStatus[chain.status])} disabled={busy || (nextStatus[chain.status] === 'evaluating' && !retestAuditId.trim())}>推进到 {nextStatus[chain.status]}</button>}{chain?.status === 'evaluating' && <button type="button" onClick={() => void evaluate()} disabled={busy}>自动计算结论</button>}</div>
    </form>}
    {chain?.comparison && <div className="evidence-comparison" data-comparable={chain.comparison.comparable}><strong>{chain.comparison.decision}</strong><span>{chain.comparison.reason}</span>{chain.comparison.delta !== undefined && <em>Δ {chain.comparison.delta.toFixed(4)}</em>}{chain.comparison.differences.length > 0 && <ul>{chain.comparison.differences.map((item) => <li key={item}>{item}</li>)}</ul>}</div>}
    {chain && <details className="evidence-events"><summary>事件历史（{chain.events.length}） · 当前 {chain.status}</summary>{chain.events.map((event) => <div key={event.id}><time>{new Date(event.occurredAt).toLocaleString()}</time><span>{event.eventType}: {event.fromStatus || '—'} → {event.toStatus}</span><small>{event.reason}</small></div>)}</details>}
    {message && <p className="evidence-chain-message" role="status">{message}</p>}
  </section></div>
}
