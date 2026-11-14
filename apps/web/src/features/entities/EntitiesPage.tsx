import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type ProjectOption = { id: string; name: string }
type Entity = { id: string; entityType: string; canonicalName: string; status: string }
type Evidence = { id: string; quote: string; locator: string; sourceType: string }
type Relation = { id: string; predicate: string; subjectName: string; objectName: string }
type Detail = Entity & { aliases: string[]; evidence: Evidence[]; relations: Relation[] }

type Props = { projects: ProjectOption[] }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

export function EntitiesPage({ projects }: Props) {
  const queryClient = useQueryClient()
  const [projectId, setProjectId] = useState(projects[0]?.id ?? '')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [mergeTargetId, setMergeTargetId] = useState('')
  const [lastMergeId, setLastMergeId] = useState<string | null>(null)
  const [message, setMessage] = useState('')
  const entities = useQuery({
    queryKey: ['entities', projectId],
    queryFn: () => request<{ items: Entity[] }>(`/api/v1/projects/${projectId}/entities`),
    enabled: Boolean(projectId),
  })
  const detail = useQuery({
    queryKey: ['entity', selectedId],
    queryFn: () => request<Detail>(`/api/v1/entities/${selectedId}`),
    enabled: Boolean(selectedId),
  })
  const quality = useQuery({
    queryKey: ['content-quality', projectId],
    queryFn: () => request<{ total: number; bySeverity: Record<string, number> }>(`/api/v1/projects/${projectId}/content-quality`),
    enabled: Boolean(projectId),
  })
  const findings = useQuery({
    queryKey: ['content-findings', projectId],
    queryFn: () => request<{ items: Array<{ id: string; ruleCode: string; severity: string; summary: string; status: string }> }>(`/api/v1/projects/${projectId}/findings?rulePrefix=CONTENT_&status=open&limit=20`),
    enabled: Boolean(projectId),
  })
  const reviewFinding = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) => request(`/api/v1/findings/${id}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ status, reason: '内容质量人工复核' }),
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['content-quality', projectId] })
      await queryClient.invalidateQueries({ queryKey: ['content-findings', projectId] })
      setMessage('内容问题状态已更新')
    },
    onError: (error) => setMessage(error.message),
  })
  const update = useMutation({
    mutationFn: (status: string) => request<Entity>(`/api/v1/entities/${selectedId}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ status }),
    }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['entity', selectedId] })
      await queryClient.invalidateQueries({ queryKey: ['entities', projectId] })
      setMessage('实体状态已更新')
    },
    onError: (error) => setMessage(error.message),
  })
  const merge = useMutation({
    mutationFn: () => request<{ id: string }>('/api/v1/entities:merge', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ sourceEntityId: selectedId, targetEntityId: mergeTargetId, reason: '实体页面人工合并' }),
    }),
    onSuccess: async (result) => {
      setLastMergeId(result.id)
      setSelectedId(null)
      setMergeTargetId('')
      await queryClient.invalidateQueries({ queryKey: ['entities', projectId] })
      setMessage('实体已合并，可撤销最近一次合并')
    },
    onError: (error) => setMessage(error.message),
  })
  const undo = useMutation({
    mutationFn: () => request(`/api/v1/entity-merges/${lastMergeId}:undo`, { method: 'POST' }),
    onSuccess: async () => {
      setLastMergeId(null)
      await queryClient.invalidateQueries({ queryKey: ['entities', projectId] })
      setMessage('合并已撤销')
    },
    onError: (error) => setMessage(error.message),
  })
  const mergeTargets = entities.data?.items.filter((item) => item.id !== selectedId && item.entityType === detail.data?.entityType) ?? []

  return <section className="workspace module-workspace">
    <div className="section-heading">
      <div><p className="eyebrow">ENTITY INTELLIGENCE</p><h2>实体知识</h2><p>来自页面 JSON-LD 的品牌、组织、产品与软件实体，人工确认后进入知识图谱。</p></div>
      <label className="entity-project">项目<select value={projectId} onChange={(event) => { setProjectId(event.target.value); setSelectedId(null) }}>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
    </div>
    {message && <p className="audit-message" role="status">{message}</p>}
    {quality.data && <div className="content-quality-card">
      <div><strong>{quality.data.total}</strong><span>内容质量问题</span></div>
      <span data-severity="high">高 {quality.data.bySeverity.high ?? 0}</span>
      <span data-severity="medium">中 {quality.data.bySeverity.medium ?? 0}</span>
      <span data-severity="low">低 {quality.data.bySeverity.low ?? 0}</span>
    </div>}
    {findings.data && findings.data.items.length > 0 && <div className="content-findings">
      {findings.data.items.map((finding) => <div key={finding.id} className="content-finding">
        <span data-severity={finding.severity}>{finding.severity}</span>
        <div><strong>{finding.ruleCode}</strong><small>{finding.summary}</small></div>
        <button type="button" disabled={reviewFinding.isPending} onClick={() => reviewFinding.mutate({ id: finding.id, status: 'confirmed' })}>确认</button>
        <button type="button" disabled={reviewFinding.isPending} onClick={() => reviewFinding.mutate({ id: finding.id, status: 'dismissed' })}>忽略</button>
      </div>)}
    </div>}
    <div className="entity-layout">
      <div className="entity-list">
        {entities.isLoading && <p>正在加载…</p>}
        {entities.error && <p className="error">{entities.error.message}</p>}
        {entities.data?.items.length === 0 && <p className="empty-state">该项目尚未抽取实体，先运行一次网站抓取。</p>}
        {entities.data?.items.map((entity) => <button key={entity.id} type="button" data-active={entity.id === selectedId} onClick={() => setSelectedId(entity.id)}>
          <span data-type={entity.entityType}>{entity.entityType}</span>
          <strong>{entity.canonicalName}</strong>
          <small>{entity.status}</small>
        </button>)}
      </div>
      <div className="entity-detail">
        {!selectedId && <p className="empty-state">选择左侧实体查看证据与关系。</p>}
        {detail.isLoading && <p>正在加载实体…</p>}
        {detail.data && <>
          <h3>{detail.data.canonicalName}</h3>
          <p className="entity-meta">{detail.data.entityType} · {detail.data.status}</p>
          <div className="entity-actions">
            <button type="button" disabled={update.isPending} onClick={() => update.mutate('confirmed')}>确认</button>
            <button type="button" className="danger" disabled={update.isPending} onClick={() => update.mutate('rejected')}>拒绝</button>
          </div>
          <div className="entity-merge">
            <select value={mergeTargetId} onChange={(event) => setMergeTargetId(event.target.value)}>
              <option value="">选择同类型目标实体…</option>
              {mergeTargets.map((item) => <option key={item.id} value={item.id}>{item.canonicalName}</option>)}
            </select>
            <button type="button" disabled={!mergeTargetId || merge.isPending} onClick={() => merge.mutate()}>合并</button>
            {lastMergeId && <button type="button" disabled={undo.isPending} onClick={() => undo.mutate()}>撤销最近合并</button>}
          </div>
          <h4>别名</h4>
          <p className="entity-aliases">{detail.data.aliases.length ? detail.data.aliases.join('、') : '无'}</p>
          <h4>来源证据</h4>
          <ul className="entity-evidence">{detail.data.evidence.length ? detail.data.evidence.map((item) => <li key={item.id}><strong>{item.quote}</strong><small>{item.sourceType} · {item.locator}</small></li>) : <li>无</li>}</ul>
          <h4>关系</h4>
          <ul className="entity-relations">{detail.data.relations.length ? detail.data.relations.map((relation) => <li key={relation.id}>{relation.subjectName} <em>{relation.predicate}</em> {relation.objectName}</li>) : <li>无</li>}</ul>
        </>}
      </div>
    </div>
  </section>
}
