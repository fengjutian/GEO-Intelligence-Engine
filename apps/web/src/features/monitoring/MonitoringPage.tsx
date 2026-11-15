import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type Project = { id: string; name: string }
type Protocol = { id: string; semanticVersion: string; status: string }
type Schedule = { id: string; projectId: string; protocolId: string; intervalMinutes: number; nextRunAt: string; enabled: boolean; lastAuditId: string | null; consecutiveFailures: number; lastError: string | null }
type Alert = { id: string; scheduleId: string; auditId: string | null; kind: string; message: string; status: string; createdAt: string }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

export function MonitoringPage() {
  const queryClient = useQueryClient()
  const [projectId, setProjectId] = useState('')
  const [protocolId, setProtocolId] = useState('')
  const [intervalMinutes, setIntervalMinutes] = useState(1440)
  const [message, setMessage] = useState('')
  const projects = useQuery({ queryKey: ['monitor-projects'], queryFn: () => request<{ items: Project[] }>('/api/v1/projects?limit=100') })
  useEffect(() => {
    if (!projectId && projects.data?.items.length) setProjectId(projects.data.items[0].id)
  }, [projectId, projects.data])
  const protocols = useQuery({
    queryKey: ['monitor-protocols', projectId],
    queryFn: () => request<{ items: Protocol[] }>(`/api/v1/projects/${projectId}/measurement-protocols`),
    enabled: Boolean(projectId),
  })
  useEffect(() => {
    const published = protocols.data?.items.filter((item) => item.status === 'published') ?? []
    if (published.length && !published.some((item) => item.id === protocolId)) setProtocolId(published[0].id)
  }, [protocolId, protocols.data])
  const schedules = useQuery({
    queryKey: ['monitor-schedules', projectId],
    queryFn: () => request<{ items: Schedule[] }>(`/api/v1/projects/${projectId}/monitoring-schedules`),
    enabled: Boolean(projectId),
    refetchInterval: 15000,
  })
  const alerts = useQuery({
    queryKey: ['monitor-alerts', projectId],
    queryFn: () => request<{ items: Alert[] }>(`/api/v1/projects/${projectId}/monitoring-alerts`),
    enabled: Boolean(projectId),
    refetchInterval: 15000,
  })
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['monitor-schedules', projectId] })
    await queryClient.invalidateQueries({ queryKey: ['monitor-alerts', projectId] })
  }
  const create = useMutation({
    mutationFn: () => request(`/api/v1/projects/${projectId}/monitoring-schedules`, {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ protocolId, intervalMinutes }),
    }),
    onSuccess: async () => { setMessage('监测计划已创建'); await refresh() },
    onError: (error) => setMessage(error.message),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => request(`/api/v1/monitoring-schedules/${id}`, {
      method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ enabled }),
    }),
    onSuccess: async () => { setMessage('监测状态已更新'); await refresh() },
    onError: (error) => setMessage(error.message),
  })
  const runNow = useMutation({
    mutationFn: (id: string) => request<{ auditId: string }>(`/api/v1/monitoring-schedules/${id}/run`, { method: 'POST' }),
    onSuccess: async (value) => { setMessage(`已创建审计 ${value.auditId}`); await refresh() },
    onError: (error) => setMessage(error.message),
  })
  const remove = useMutation({
    mutationFn: (id: string) => request(`/api/v1/monitoring-schedules/${id}`, { method: 'DELETE' }),
    onSuccess: async () => { setMessage('监测计划已删除'); await refresh() },
    onError: (error) => setMessage(error.message),
  })
  const resolve = useMutation({
    mutationFn: (id: string) => request(`/api/v1/monitoring-alerts/${id}/resolve`, { method: 'PATCH' }),
    onSuccess: async () => { setMessage('告警已解决'); await refresh() },
    onError: (error) => setMessage(error.message),
  })
  const published = protocols.data?.items.filter((item) => item.status === 'published') ?? []

  return <section className="workspace module-workspace">
    <div className="module-heading"><div><p className="eyebrow">CONTINUOUS MONITORING</p><h2>持续监测</h2><p>按固定间隔自动创建审计，失败会重试并生成告警。</p></div><label className="review-filter">项目<select value={projectId} onChange={(event) => { setProjectId(event.target.value); setProtocolId('') }}>{projects.data?.items.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label></div>
    {message && <p className="audit-message" role="status">{message}</p>}
    <div className="monitor-create">
      <label>已发布协议<select value={protocolId} onChange={(event) => setProtocolId(event.target.value)}><option value="">选择协议…</option>{published.map((protocol) => <option key={protocol.id} value={protocol.id}>v{protocol.semanticVersion}</option>)}</select></label>
      <label>间隔<select value={intervalMinutes} onChange={(event) => setIntervalMinutes(Number(event.target.value))}><option value={60}>每小时</option><option value={360}>每 6 小时</option><option value={1440}>每天</option><option value={10080}>每周</option></select></label>
      <button type="button" disabled={!protocolId || create.isPending} onClick={() => create.mutate()}>创建监测计划</button>
    </div>
    {published.length === 0 && <p className="settings-hint">该项目没有已发布协议，请先在审计页发布协议。</p>}
    <div className="monitor-grid">
      <section className="settings-panel">
        <h3>监测计划 · {schedules.data?.items.length ?? 0}</h3>
        {schedules.data?.items.length === 0 && <p className="empty-state">暂无监测计划。</p>}
        {schedules.data?.items.map((schedule) => <div className="monitor-row" key={schedule.id}>
          <div><strong>v{protocols.data?.items.find((item) => item.id === schedule.protocolId)?.semanticVersion ?? '—'} · {schedule.intervalMinutes / 60}h</strong><small>下次 {new Date(schedule.nextRunAt).toLocaleString()} · {schedule.enabled ? '启用' : '暂停'}{schedule.lastError ? ` · ${schedule.lastError}` : ''}</small></div>
          <span data-failures={schedule.consecutiveFailures > 0}>{schedule.consecutiveFailures > 0 ? `失败 ${schedule.consecutiveFailures}` : '正常'}</span>
          <div className="monitor-actions">
            <button type="button" onClick={() => toggle.mutate({ id: schedule.id, enabled: !schedule.enabled })}>{schedule.enabled ? '暂停' : '启用'}</button>
            <button type="button" disabled={runNow.isPending} onClick={() => runNow.mutate(schedule.id)}>立即运行</button>
            <button type="button" className="danger-link" onClick={() => remove.mutate(schedule.id)}>删除</button>
          </div>
        </div>)}
      </section>
      <section className="settings-panel">
        <h3>告警 · {alerts.data?.items.filter((item) => item.status === 'open').length ?? 0} 未解决</h3>
        {alerts.data?.items.length === 0 && <p className="empty-state">暂无告警。</p>}
        {alerts.data?.items.map((alert) => <div className="monitor-alert" key={alert.id} data-status={alert.status}>
          <div><strong>{alert.kind}</strong><small>{alert.message} · {new Date(alert.createdAt).toLocaleString()}</small></div>
          {alert.status === 'open' ? <button type="button" disabled={resolve.isPending} onClick={() => resolve.mutate(alert.id)}>解决</button> : <span>已解决</span>}
        </div>)}
      </section>
    </div>
  </section>
}
