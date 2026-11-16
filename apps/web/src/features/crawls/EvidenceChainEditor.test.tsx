import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EvidenceChainEditor } from './EvidenceChainEditor'

const finding = { id: '019b1000-0000-7000-8000-000000000008', ruleCode: 'GEO_ANSWER_GAP', summary: 'Product answer lacks verifiable facts' }
const candidate = {
  questionId: '019b1000-0000-7000-8000-00000000000b', question: 'Which GEO product provides verifiable evidence?',
  responseId: '019b1000-0000-7000-8000-000000000012', auditId: '019b1000-0000-7000-8000-00000000000f',
  provider: 'deepseek', channel: 'model_api', model: 'deepseek-chat', content: 'Evidence answer baseline', score: 0.92,
}
const candidates = {
  findingId: finding.id, summary: finding.summary,
  page: { contentId: '019b1000-0000-7000-8000-000000000007', url: 'https://e2e.invalid/product', title: 'Evidence product page', contentHash: 'a'.repeat(64) },
  answers: [candidate],
}
const baseChain = {
  findingId: finding.id, status: 'evidence_ready', version: 1,
  question: { id: candidate.questionId, text: candidate.question },
  answer: { id: candidate.responseId, provider: 'deepseek', model: 'deepseek-chat', content: candidate.content },
  page: candidates.page, changeSummary: 'Add verifiable facts', expectedOutcome: 'Increase mention rate',
  verificationPlan: 'Repeat the same protocol', metricCode: 'mention_rate', targetDelta: 0.1,
  events: [{ id: 'event-1', eventType: 'created', fromStatus: '', toStatus: 'evidence_ready', reason: '', occurredAt: '2026-11-16T02:00:00Z' }],
}

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }))
}

afterEach(() => vi.unstubAllGlobals())

describe('EvidenceChainEditor end-to-end contract', () => {
  it('selects real audit evidence and saves the immutable baseline snapshot', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/evidence-candidates')) return jsonResponse(candidates)
      if (url.endsWith('/evidence-chain') && !init?.method) return jsonResponse({ title: 'not found' }, 404)
      if (url.endsWith('/evidence-chain') && init?.method === 'PUT') return jsonResponse(baseChain)
      return jsonResponse({ title: 'unexpected request' }, 500)
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<EvidenceChainEditor finding={finding} onClose={vi.fn()} />)

    expect(await screen.findByText('Evidence product page')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('修改说明'), { target: { value: 'Add verifiable facts' } })
    fireEvent.change(screen.getByLabelText('预期结果'), { target: { value: 'Increase mention rate' } })
    fireEvent.change(screen.getByLabelText('复测计划'), { target: { value: 'Repeat the same protocol' } })
    fireEvent.click(screen.getByRole('button', { name: '保存证据快照' }))

    expect(await screen.findByText('证据快照已保存')).toBeTruthy()
    const putCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(putCall).toBeTruthy()
    const payload = JSON.parse(String(putCall?.[1]?.body))
    expect(payload).toMatchObject({ questionId: candidate.questionId, responseId: candidate.responseId, pageContentId: candidates.page.contentId, metricCode: 'mention_rate', targetDelta: 0.1 })
    expect(screen.getByText(/事件历史（1）/)).toBeTruthy()
  })

  it('renders the automatic improved conclusion from a comparable retest', async () => {
    const evaluating = { ...baseChain, status: 'evaluating', retest: { auditId: '019b1000-0000-7000-8000-000000000015', status: 'succeeded', outcome: 'pending' } }
    const verified = {
      ...evaluating, status: 'verified', version: 6, retest: { ...evaluating.retest, outcome: 'improved' },
      comparison: { comparable: true, differences: [], delta: 0.4, decision: 'improved', reason: 'Retest confidence interval is above baseline' },
      events: [...baseChain.events, { id: 'event-2', eventType: 'evaluated', fromStatus: 'evaluating', toStatus: 'verified', reason: 'Retest confidence interval is above baseline', occurredAt: '2026-11-16T03:00:00Z' }],
    }
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/evidence-candidates')) return jsonResponse(candidates)
      if (url.endsWith('/evaluate') && init?.method === 'POST') return jsonResponse(verified)
      if (url.endsWith('/evidence-chain')) return jsonResponse(evaluating)
      return jsonResponse({ title: 'unexpected request' }, 500)
    }))
    render(<EvidenceChainEditor finding={finding} onClose={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: '自动计算结论' }))
    expect(await screen.findByText('复测结论：improved')).toBeTruthy()
    expect(screen.getByText('Retest confidence interval is above baseline')).toBeTruthy()
    expect(screen.getByText('Δ 0.4000')).toBeTruthy()
  })
})
