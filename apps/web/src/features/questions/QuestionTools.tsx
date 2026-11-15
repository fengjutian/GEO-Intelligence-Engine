import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type ImportRow = { line: number; text: string; status: string; message?: string }
type ImportResult = { committed: boolean; total: number; valid: number; invalid: number; rows: ImportRow[] }
type ClusterMember = { questionId: string; text: string; similarity: number }
type Cluster = { id: string; name: string; algorithmVersion: string; representativeQuestionId: string; members: ClusterMember[] }

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
  const clusters = useQuery({ queryKey: ['question-clusters', projectId], queryFn: () => request<{ items: Cluster[] }>(`/api/v1/projects/${projectId}/question-clusters`) })
  const generate = useMutation({
    mutationFn: () => request<{ run: { generated: number; skipped: number } }>(`/api/v1/projects/${projectId}/question-generation-runs`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ language: locale }),
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
    mutationFn: () => request<{ items: Cluster[] }>(`/api/v1/projects/${projectId}/question-clusters:rebuild`, { method: 'POST' }),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['question-clusters', projectId] }) },
  })
  return <div className="question-tools">
    <div className="question-toolbar">
      <button type="button" onClick={() => generate.mutate()} disabled={generate.isPending}>{generate.isPending ? '生成中…' : '生成建议问题'}</button>
      <button type="button" onClick={() => rebuild.mutate()} disabled={rebuild.isPending}>{rebuild.isPending ? '聚类中…' : '重建聚类'}</button>
      {generate.data && <span>生成 {generate.data.run.generated} 条，跳过 {generate.data.run.skipped} 条</span>}
    </div>
    {(generate.error || rebuild.error) && <p className="error" role="alert">{(generate.error ?? rebuild.error)?.message}</p>}
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
      <h4>问题聚类 · {clusters.data?.items.length ?? 0}</h4>
      {clusters.data?.items.map((cluster) => <details key={cluster.id}>
        <summary>{cluster.name}<small>{cluster.members.length} 条 · 相似度算法 {cluster.algorithmVersion}</small></summary>
        <ul>{cluster.members.map((member) => <li key={member.questionId}><span>{member.text}</span><em>{Math.round(member.similarity * 100)}%</em></li>)}</ul>
      </details>)}
    </div>
  </div>
}
