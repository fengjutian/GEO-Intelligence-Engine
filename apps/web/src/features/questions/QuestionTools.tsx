import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type ImportRow = { line: number; text: string; status: string; message?: string }
type ImportResult = { committed: boolean; total: number; valid: number; invalid: number; rows: ImportRow[] }
type ClusterMember = { questionId: string; text: string; similarity: number; manuallyLocked: boolean }
type Cluster = { id: string; name: string; algorithmVersion: string; representativeQuestionId: string; locked: boolean; members: ClusterMember[] }
type Coverage = { total: number; branded: number; nonBranded: number; clusters: number; byType: Record<string, number>; missingTypes: string[]; missingStages: string[] }

const TEMPLATE = 'text,language,questionType,intent,topic,funnelStage,weight\n适合中国团队的 GEO 工具有哪些？,zh-CN,category,discovery,GEO,awareness,1'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

export function QuestionTools({ projectId, locale, onChanged }: { projectId: string; locale: string; onChanged: () => void }) {
  const queryClient = useQueryClient()
  const [csv, setCsv] = useState(TEMPLATE)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [selected, setSelected] = useState<Record<string, string[]>>({})
  const [mergeTargets, setMergeTargets] = useState<Record<string, string>>({})
  const [algorithm, setAlgorithm] = useState('semantic-v2')
  const [threshold, setThreshold] = useState('0.75')
  const [reviewNote, setReviewNote] = useState('')
  const clusters = useQuery({ queryKey: ['question-clusters', projectId], queryFn: () => request<{ items: Cluster[] }>(`/api/v1/projects/${projectId}/question-clusters`) })
  const coverage = useQuery({ queryKey: ['question-coverage', projectId], queryFn: () => request<Coverage>(`/api/v1/projects/${projectId}/question-coverage`) })
  const generate = useMutation({
    mutationFn: (mode: 'template' | 'llm') => request<{ run: { generated: number; skipped: number }; mode: string; warning?: string }>(`/api/v1/projects/${projectId}/question-generation-runs`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ language: locale, mode, provider: mode === 'llm' ? 'deepseek' : undefined }),
    }),
    onSuccess: async () => { onChanged(); await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] }) },
  })
  const importCSV = useMutation({
    mutationFn: (commit: boolean) => request<ImportResult>(`/api/v1/projects/${projectId}/questions:import`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ csv, commit }),
    }),
    onSuccess: async (value) => { setResult(value); if (value.committed) onChanged() },
  })
  const rebuild = useMutation({
    mutationFn: () => request<{ items: Cluster[]; algorithmVersion: string; threshold: number; reviewRequired?: boolean; reviewNote?: string }>(`/api/v1/projects/${projectId}/question-clusters:rebuild?algorithm=${algorithm}${algorithm === 'embedding-v1' ? `&threshold=${threshold}` : ''}`, { method: 'POST' }),
    onSuccess: async (value) => {
      setReviewNote(value.reviewRequired ? (value.reviewNote ?? '低阈值聚类，需人工复核') : '')
      await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] })
    },
  })
  const lock = useMutation({
    mutationFn: ({ id, locked }: { id: string; locked: boolean }) => request(`/api/v1/projects/${projectId}/question-clusters/${id}`, {
      method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ locked }),
    }),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] }) },
  })
  const split = useMutation({
    mutationFn: ({ id, questionIds }: { id: string; questionIds: string[] }) => request(`/api/v1/projects/${projectId}/question-clusters/${id}/split`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ questionIds, name: '人工拆分' }),
    }),
    onSuccess: async () => {
      setSelected({})
      await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] })
    },
  })
  const merge = useMutation({
    mutationFn: ({ id, target }: { id: string; target: string }) => request(`/api/v1/projects/${projectId}/question-clusters/${id}/merge`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ targetClusterId: target }),
    }),
    onSuccess: async () => {
      setMergeTargets({})
      await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] })
    },
  })
  return <div className="question-tools">
    <div className="question-toolbar">
      <button type="button" onClick={() => generate.mutate('template')} disabled={generate.isPending}>{generate.isPending ? '生成中…' : '生成建议问题'}</button>
      <button type="button" onClick={() => generate.mutate('llm')} disabled={generate.isPending}>LLM 生成</button>
      <select value={algorithm} onChange={(event) => setAlgorithm(event.target.value)} aria-label="聚类算法"><option value="semantic-v2">语义 v2</option><option value="embedding-v1">向量 v1</option><option value="jaccard-v1">基础 v1</option></select>
      {algorithm === 'embedding-v1' && <input type="number" min="0.5" max="0.95" step="0.01" value={threshold} onChange={(event) => setThreshold(event.target.value)} aria-label="向量阈值" />}
      <button type="button" onClick={() => rebuild.mutate()} disabled={rebuild.isPending}>{rebuild.isPending ? '聚类中…' : '重建聚类'}</button>
      {generate.data && <span>{generate.data.mode === 'llm' ? 'LLM' : '模板'}生成 {generate.data.run.generated} 条，跳过 {generate.data.run.skipped} 条{generate.data.warning ? `（${generate.data.warning}）` : ''}</span>}
    </div>
    {(generate.error || rebuild.error) && <p className="error" role="alert">{(generate.error ?? rebuild.error)?.message}</p>}
    {reviewNote && <p className="review-warning" role="status">{reviewNote}</p>}
    {(lock.error || split.error || merge.error) && <p className="error" role="alert">{(lock.error ?? split.error ?? merge.error)?.message}</p>}
    <details className="question-import">
      <summary>CSV 批量导入（两阶段）</summary>
      <textarea value={csv} onChange={(event) => setCsv(event.target.value)} spellCheck={false} />
      <div className="question-toolbar">
        <button type="button" onClick={() => importCSV.mutate(false)} disabled={importCSV.isPending}>预览校验</button>
        <button type="button" onClick={() => importCSV.mutate(true)} disabled={importCSV.isPending || !result || result.valid === 0}>确认提交</button>
      </div>
      {importCSV.error && <p className="error" role="alert">{importCSV.error.message}</p>}
      {result && <div className="import-result">
        <strong>{result.committed ? '已提交' : '预览'}：有效 {result.valid} / 无效 {result.invalid}</strong>
        <ul>{result.rows.map((row) => <li key={row.line} data-status={row.status}><span>第 {row.line} 行</span>{row.status === 'valid' ? '可导入' : row.message}</li>)}</ul>
      </div>}
    </details>
    <div className="cluster-list">
      {coverage.data && <div className="coverage-strip">
        <span>问题 {coverage.data.total}</span>
        <span>品牌型 {coverage.data.branded}</span>
        <span>非品牌 {coverage.data.nonBranded}</span>
        <span>聚类 {coverage.data.clusters}</span>
        {coverage.data.missingTypes.length > 0 && <em>缺类型：{coverage.data.missingTypes.join(' / ')}</em>}
        {coverage.data.missingStages.length > 0 && <em>缺阶段：{coverage.data.missingStages.join(' / ')}</em>}
      </div>}
      <h4>问题聚类 · {clusters.data?.items.length ?? 0}</h4>
      {clusters.data?.items.map((cluster) => <details key={cluster.id}>
        <summary>{cluster.name}<small>{cluster.members.length} 条 · {cluster.locked ? '已锁定' : cluster.algorithmVersion}<button type="button" className="cluster-lock" onClick={(event) => { event.preventDefault(); lock.mutate({ id: cluster.id, locked: !cluster.locked }) }}>{cluster.locked ? '解锁' : '锁定'}</button></small></summary>
        <ul>{cluster.members.map((member) => <li key={member.questionId}>
          <input type="checkbox" checked={(selected[cluster.id] ?? []).includes(member.questionId)} onChange={(event) => {
            const current = selected[cluster.id] ?? []
            setSelected({ ...selected, [cluster.id]: event.target.checked ? [...current, member.questionId] : current.filter((id) => id !== member.questionId) })
          }} />
          <span>{member.text}{member.manuallyLocked ? ' · 人工' : ''}</span><em>{Math.round(member.similarity * 100)}%</em>
        </li>)}</ul>
        <div className="cluster-actions">
          <button type="button" disabled={!(selected[cluster.id]?.length) || split.isPending} onClick={() => split.mutate({ id: cluster.id, questionIds: selected[cluster.id] ?? [] })}>拆分选中</button>
          <select value={mergeTargets[cluster.id] ?? ''} onChange={(event) => setMergeTargets({ ...mergeTargets, [cluster.id]: event.target.value })}><option value="">合并到…</option>{clusters.data?.items.filter((item) => item.id !== cluster.id).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select>
          <button type="button" disabled={!mergeTargets[cluster.id] || merge.isPending} onClick={() => merge.mutate({ id: cluster.id, target: mergeTargets[cluster.id] })}>合并</button>
        </div>
      </details>)}
    </div>
  </div>
}
