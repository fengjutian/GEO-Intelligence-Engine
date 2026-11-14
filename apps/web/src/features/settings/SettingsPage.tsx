import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type Member = { id: string; userId: string; email: string; subject: string; role: string; status: string }
type Invitation = { id: string; email: string; role: string; status: string; expiresAt: string; token?: string }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    const problem = await response.json().catch(() => ({})) as { title?: string }
    throw new Error(problem.title ?? `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

const ROLES = ['Owner', 'ProjectAdmin', 'Analyst', 'Editor', 'Approver', 'Viewer']

export function SettingsPage() {
  const queryClient = useQueryClient()
  const [email, setEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('Analyst')
  const [invite, setInvite] = useState<Invitation | null>(null)
  const [message, setMessage] = useState('')
  const me = useQuery({ queryKey: ['me'], queryFn: () => request<{ tenantId: string; role: string }>('/api/v1/me') })
  const members = useQuery({
    queryKey: ['members', me.data?.tenantId],
    queryFn: () => request<{ items: Member[] }>(`/api/v1/tenants/${me.data?.tenantId}/members`),
    enabled: Boolean(me.data?.tenantId),
  })
  const createInvite = useMutation({
    mutationFn: () => request<Invitation>(`/api/v1/tenants/${me.data?.tenantId}/invitations`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ email, role: inviteRole }),
    }),
    onSuccess: (result) => {
      setInvite(result)
      setEmail('')
      setMessage('邀请已创建，Token 仅显示一次')
    },
    onError: (error) => setMessage(error.message),
  })
  const updateRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => request(`/api/v1/tenants/${me.data?.tenantId}/members/${id}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ role }),
    }),
    onSuccess: async () => {
      setMessage('成员角色已更新')
      await queryClient.invalidateQueries({ queryKey: ['members', me.data?.tenantId] })
    },
    onError: (error) => setMessage(error.message),
  })

  return <section className="workspace module-workspace">
    <div className="module-heading"><div><p className="eyebrow">TENANT SETTINGS</p><h2>成员与权限</h2><p>管理当前租户成员、邀请与角色；最后一名 Owner 不可降级或停用。</p></div><span>{members.data?.items.length ?? 0} 成员</span></div>
    {message && <p className="audit-message" role="status">{message}</p>}
    <div className="settings-grid">
      <section className="settings-panel">
        <h3>成员</h3>
        {members.isLoading && <p>正在加载…</p>}
        {members.error && <p className="error">{members.error.message}</p>}
        <div className="member-table">
          <div className="member-row member-head"><span>成员</span><span>角色</span><span>状态</span></div>
          {members.data?.items.map((member) => <div className="member-row" key={member.id}>
            <div><strong>{member.email}</strong><small>{member.subject}</small></div>
            <select value={member.role} onChange={(event) => updateRole.mutate({ id: member.id, role: event.target.value })} disabled={updateRole.isPending}>{ROLES.map((role) => <option key={role} value={role}>{role}</option>)}</select>
            <span data-status={member.status}>{member.status}</span>
          </div>)}
        </div>
      </section>
      <section className="settings-panel">
        <h3>邀请成员</h3>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (email.trim()) createInvite.mutate() }}>
          <label>邮箱<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="name@example.com" required /></label>
          <label>角色<select value={inviteRole} onChange={(event) => setInviteRole(event.target.value)}>{ROLES.filter((role) => role !== 'Owner').map((role) => <option key={role} value={role}>{role}</option>)}</select></label>
          <button type="submit" disabled={createInvite.isPending}>创建邀请</button>
        </form>
        {invite && <div className="invite-result">
          <p>邀请已创建：{invite.email} · {invite.role}</p>
          <code>{invite.token}</code>
          <small>Token 仅显示一次，请通过安全渠道发送。</small>
        </div>}
      </section>
    </div>
  </section>
}
