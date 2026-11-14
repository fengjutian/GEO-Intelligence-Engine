import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type Item = {
  id: string
  evaluationId: string
  auditId: string
  question: string
  reasonCode: string
  priority: string
  answerOutcome: string
  contentValidity: string
  recommendationLabel: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

export function ReviewPage() {
  const queryClient = useQueryClient()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [decision, setDecision] = useState('agree')
  const [reason, setReason] = useState('')
  const [message, setMessage] = useState('')
  const queue = useQuery({
    queryKey: ['review-queue'],
    queryFn: () => request<{ items: Item[] }>('/api/v1/review-queue?status=pending&limit=50'),
  })
  const stats = useQuery({
    queryKey: ['review-stats'],
    queryFn: () => request<{ byStatus: Record<string, number>; decisions: Record<string, number>; agreementRate: number | null }>('/api/v1/review-queue/stats'),
  })
  const annotate = useMutation({
    mutationFn: (id: string) => request(`/api/v1/review-queue/${id}/annotations`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ labels: { decision }, reason }),
    }),
    onSuccess: async () => {
      setSelectedId(null)
      setReason('')
      setMessage('复核意见已提交')
      await queryClient.invalidateQueries({ queryKey: ['review-queue'] })
      await queryClient.invalidateQueries({ queryKey: ['review-stats'] })
    },
    onError: (error) => setMessage(error.message),
  })
  const selected = queue.data?.items.find((item) => item.id === selectedId)

  return <section className="workspace module-workspace">
    <div className="module-heading"><div><p className="eyebrow">HUMAN REVIEW</p><h2>复核队列</h2><p>自动进入队列的拒答与不合格回答，人工判定后标记为已复核。</p></div><span>{queue.data?.items.length ?? 0} 待复核</span></div>
    {message && <p className="audit-message" role="status">{message}</p>}
    {stats.data && <div className="review-stats">
      <span>待复核 <strong>{stats.data.byStatus.pending ?? 0}</strong></span>
      <span>已复核 <strong>{stats.data.byStatus.reviewed ?? 0}</strong></span>
      <span>已仲裁 <strong>{stats.data.byStatus.adjudicated ?? 0}</strong></span>
      <span>一致率 <strong>{stats.data.agreementRate == null ? '—' : `${Math.round(stats.data.agreementRate * 100)}%`}</strong></span>
    </div>}
    <div className="review-layout">
      <div className="review-list">
        {queue.isLoading && <p>正在加载…</p>}
        {queue.error && <p className="error">{queue.error.message}</p>}
        {queue.data?.items.length === 0 && <p className="empty-state">当前没有待复核项。</p>}
        {queue.data?.items.map((item) => <button key={item.id} type="button" data-active={item.id === selectedId} onClick={() => { setSelectedId(item.id); setDecision('agree'); setReason('') }}>
          <span data-priority={item.priority}>{item.priority}</span>
          <strong>{item.question}</strong>
          <small>{item.reasonCode} · {item.answerOutcome} · {item.contentValidity}</small>
        </button>)}
      </div>
      <div className="review-detail">
        {!selected && <p className="empty-state">选择左侧复核项查看详情。</p>}
        {selected && <>
          <h3>{selected.question}</h3>
          <dl>
            <div><dt>原因</dt><dd>{selected.reasonCode}</dd></div>
            <div><dt>优先级</dt><dd>{selected.priority}</dd></div>
            <div><dt>回答结果</dt><dd>{selected.answerOutcome}</dd></div>
            <div><dt>内容有效性</dt><dd>{selected.contentValidity}</dd></div>
            <div><dt>推荐标签</dt><dd>{selected.recommendationLabel || '—'}</dd></div>
            <div><dt>审计</dt><dd className="hash">{selected.auditId}</dd></div>
          </dl>
          <h4>复核结论</h4>
          <form className="review-form" onSubmit={(event) => { event.preventDefault(); if (reason.trim()) annotate.mutate(selected.id) }}>
            <label>判定<select value={decision} onChange={(event) => setDecision(event.target.value)}><option value="agree">一致</option><option value="disagree">不一致</option><option value="uncertain">无法判断</option></select></label>
            <label>原因<textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="说明判断依据…" maxLength={2000} /></label>
            <button type="submit" disabled={annotate.isPending || !reason.trim()}>提交复核</button>
          </form>
        </>}
      </div>
    </div>
  </section>
}
