import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

type Acceptance = { id: string; description: string; status: string; position: number }
type TaskEvent = { id: string; eventType: string; fromState: string; toState: string; reason: string; actorId: string; occurredAt: string }
type Comment = { id: string; authorId: string; body: string; createdAt: string }
type Task = {
  id: string
  projectId: string
  findingId: string
  title: string
  description: string
  status: string
  priority: string
  version: number
  dueAt: string | null
  assigneeId: string | null
  acceptanceItems: Acceptance[]
  events: TaskEvent[]
  comments: Comment[]
}

type Props = { projectId: string; projectName: string; onClose: () => void }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

const NEXT_ACTIONS: Record<string, Array<{ target: string; label: string }>> = {
  todo: [{ target: 'in_progress', label: '开始处理' }],
  in_progress: [{ target: 'in_review', label: '提交审核' }],
  in_review: [{ target: 'done', label: '审核通过' }, { target: 'changes_requested', label: '要求修改' }],
  changes_requested: [{ target: 'in_progress', label: '继续修改' }],
}

export function TaskBoard({ projectId, projectName, onClose }: Props) {
  const queryClient = useQueryClient()
  const tasks = useQuery({ queryKey: ['tasks', projectId], queryFn: () => request<{ items: Task[] }>(`/api/v1/optimization-tasks?projectId=${projectId}`) })
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const detail = useQuery({ queryKey: ['task', selectedId], queryFn: () => request<Task>(`/api/v1/optimization-tasks/${selectedId}`), enabled: selectedId !== null })
  const [message, setMessage] = useState('')
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = async (id: string) => {
    await queryClient.invalidateQueries({ queryKey: ['task', id] })
    await queryClient.invalidateQueries({ queryKey: ['tasks', projectId] })
  }

  async function updateTask(task: Task, form: HTMLFormElement) {
    const data = new FormData(form)
    const body: Record<string, string> = { priority: String(data.get('priority')) }
    const assigneeId = String(data.get('assigneeId') ?? '').trim()
    if (assigneeId) body.assigneeId = assigneeId
    const dueLocal = String(data.get('dueAt') ?? '').trim()
    if (dueLocal) body.dueAt = new Date(dueLocal).toISOString()
    setBusy(true)
    try {
      await request<Task>(`/api/v1/optimization-tasks/${task.id}`, {
        method: 'PATCH',
        headers: { 'content-type': 'application/json', 'If-Match': `"${task.version}"` },
        body: JSON.stringify(body),
      })
      await refresh(task.id)
      setMessage('任务字段已更新')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '任务更新失败')
    } finally {
      setBusy(false)
    }
  }

  async function transition(task: Task, target: string, reason = '') {
    setBusy(true)
    try {
      await request<Task>(`/api/v1/optimization-tasks/${task.id}:transition`, {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'If-Match': `"${task.version}"` },
        body: JSON.stringify({ targetState: target, reason }),
      })
      await refresh(task.id)
      setMessage(`任务已转换为 ${target}`)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '状态转换失败')
    } finally {
      setBusy(false)
    }
  }

  async function toggleItem(task: Task, item: Acceptance) {
    setBusy(true)
    try {
      await request<Task>(`/api/v1/optimization-tasks/${task.id}/acceptance-items/${item.id}`, {
        method: 'PATCH',
        headers: { 'content-type': 'application/json', 'If-Match': `"${task.version}"` },
        body: JSON.stringify({ status: item.status === 'done' ? 'pending' : 'done' }),
      })
      await refresh(task.id)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '验收项更新失败')
    } finally {
      setBusy(false)
    }
  }

  async function submitComment(task: Task) {
    if (!comment.trim()) return
    setBusy(true)
    try {
      await request(`/api/v1/optimization-tasks/${task.id}/comments`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ body: comment }),
      })
      setComment('')
      await refresh(task.id)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '评论失败')
    } finally {
      setBusy(false)
    }
  }

  function act(task: Task, target: string) {
    if (target === 'changes_requested') {
      const reason = window.prompt('请输入要求修改的原因') ?? ''
      if (!reason.trim()) return
      void transition(task, target, reason)
      return
    }
    void transition(task, target)
  }

  const task = detail.data
  return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
    <section className="task-dialog" aria-labelledby="task-title" onMouseDown={(event) => event.stopPropagation()}>
      <div className="profile-header">
        <div><p className="eyebrow">OPTIMIZATION TASKS</p><h2 id="task-title">{projectName} · 任务看板</h2><p>从报告建议创建的任务、验收条件与完整事件历史。</p></div>
        <button type="button" aria-label="关闭" onClick={onClose}>×</button>
      </div>
      {message && <p className="audit-message" role="status">{message}</p>}
      <div className="task-layout">
        <div className="task-list">
          {tasks.isLoading && <p>正在加载…</p>}
          {tasks.error && <p className="error">{tasks.error.message}</p>}
          {tasks.data?.items.length === 0 && <p className="empty-state">暂无任务，可从报告优先级建议创建。</p>}
          {tasks.data?.items.map((item) => <button key={item.id} type="button" data-active={item.id === selectedId} onClick={() => setSelectedId(item.id)}>
            <span data-priority={item.priority}>{item.priority}</span>
            <strong>{item.title}</strong>
            <small>{item.status} · v{item.version}</small>
          </button>)}
        </div>
        <div className="task-detail">
          {!selectedId && <p className="empty-state">选择左侧任务查看详情。</p>}
          {detail.isLoading && <p>正在加载任务…</p>}
          {task && <>
            <h3>{task.title}</h3>
            <dl>
              <div><dt>状态</dt><dd>{task.status} · v{task.version}</dd></div>
              <div><dt>优先级</dt><dd>{task.priority}</dd></div>
              <div><dt>来源 Finding</dt><dd className="hash">{task.findingId}</dd></div>
              <div><dt>截止</dt><dd>{task.dueAt ? new Date(task.dueAt).toLocaleDateString() : '未设置'}</dd></div>
            </dl>
            <h4>任务字段</h4>
            <form className="task-edit" key={`${task.id}-${task.version}`} onSubmit={(event) => { event.preventDefault(); void updateTask(task, event.currentTarget) }}>
              <label>优先级<select name="priority" defaultValue={task.priority}><option value="P0">P0</option><option value="P1">P1</option><option value="P2">P2</option><option value="P3">P3</option></select></label>
              <label>负责人 UUID<input name="assigneeId" defaultValue={task.assigneeId ?? ''} placeholder="可选" /></label>
              <label>截止时间<input name="dueAt" type="datetime-local" defaultValue={task.dueAt ? new Date(task.dueAt).toISOString().slice(0, 16) : ''} /></label>
              <button type="submit" disabled={busy}>保存字段</button>
            </form>
            <h4>验收条件</h4>
            <ul className="acceptance-list">{task.acceptanceItems.map((item) => <li key={item.id}>
              <label><input type="checkbox" checked={item.status === 'done'} disabled={busy || ['done', 'cancelled'].includes(task.status)} onChange={() => void toggleItem(task, item)} /> {item.description}</label>
            </li>)}</ul>
            <div className="task-actions">
              {(NEXT_ACTIONS[task.status] ?? []).map((action) => <button key={action.target} type="button" disabled={busy} onClick={() => act(task, action.target)}>{action.label}</button>)}
              {!['done', 'cancelled'].includes(task.status) && <button type="button" className="danger" disabled={busy} onClick={() => { const reason = window.prompt('请输入取消原因') ?? ''; if (reason.trim()) void transition(task, 'cancelled', reason) }}>取消任务</button>}
            </div>
            <h4>评论</h4>
            <div className="comment-list">{task.comments.length === 0 && <p className="empty-state">暂无评论。</p>}{task.comments.map((entry) => <p key={entry.id}>{entry.body}</p>)}</div>
            <form className="comment-form" onSubmit={(event) => { event.preventDefault(); void submitComment(task) }}>
              <input value={comment} onChange={(event) => setComment(event.target.value)} placeholder="补充进展或说明…" maxLength={5000} />
              <button type="submit" disabled={busy || !comment.trim()}>发送</button>
            </form>
            <h4>事件历史</h4>
            <ul className="task-events">{task.events.map((event) => <li key={event.id}><strong>{event.eventType}</strong> {event.fromState}{event.fromState && event.toState ? ' → ' : ''}{event.toState} <small>{new Date(event.occurredAt).toLocaleString()}</small>{event.reason && <em>{event.reason}</em>}</li>)}</ul>
          </>}
        </div>
      </div>
    </section>
  </div>
}
