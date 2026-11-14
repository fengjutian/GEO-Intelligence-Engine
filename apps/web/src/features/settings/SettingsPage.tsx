import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type Member = { id: string; userId: string; email: string; subject: string; role: string; status: string }
type Invitation = { id: string; email: string; role: string; status: string; expiresAt: string; token?: string }
type ServiceAccount = { id: string; name: string; status: string; createdAt: string }
type APIToken = { id: string; principalId: string; scopes: string[]; expiresAt: string | null; lastUsedAt: string | null; revokedAt: string | null; createdAt: string; token?: string }

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
  const [accountName, setAccountName] = useState('')
  const [tokenAccountId, setTokenAccountId] = useState('')
  const [tokenScopes, setTokenScopes] = useState('read')
  const [expiresDays, setExpiresDays] = useState(30)
  const [newToken, setNewToken] = useState<APIToken | null>(null)
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

  const accounts = useQuery({ queryKey: ['service-accounts'], queryFn: () => request<{ items: ServiceAccount[] }>('/api/v1/service-accounts') })
  const tokens = useQuery({ queryKey: ['api-tokens'], queryFn: () => request<{ items: APIToken[] }>('/api/v1/api-tokens') })
  const createAccount = useMutation({
    mutationFn: () => request<ServiceAccount>('/api/v1/service-accounts', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name: accountName }),
    }),
    onSuccess: async () => {
      setAccountName('')
      setMessage('服务账号已创建')
      await queryClient.invalidateQueries({ queryKey: ['service-accounts'] })
    },
    onError: (error) => setMessage(error.message),
  })
  const createToken = useMutation({
    mutationFn: () => request<APIToken>('/api/v1/api-tokens', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ serviceAccountId: tokenAccountId, scopes: tokenScopes.split(',').map((scope) => scope.trim()).filter(Boolean), expiresInDays: expiresDays }),
    }),
    onSuccess: async (result) => {
      setNewToken(result)
      setMessage('API Token 已创建，明文仅显示一次')
      await queryClient.invalidateQueries({ queryKey: ['api-tokens'] })
    },
    onError: (error) => setMessage(error.message),
  })
  const revokeToken = useMutation({
    mutationFn: (id: string) => request(`/api/v1/api-tokens/${id}`, { method: 'DELETE' }),
    onSuccess: async () => {
      setMessage('Token 已撤销')
      await queryClient.invalidateQueries({ queryKey: ['api-tokens'] })
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
    <div className="settings-grid">
      <section className="settings-panel">
        <h3>服务账号</h3>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (accountName.trim()) createAccount.mutate() }}>
          <label>名称<input value={accountName} onChange={(event) => setAccountName(event.target.value)} placeholder="ci-pipeline" maxLength={120} required /></label>
          <button type="submit" disabled={createAccount.isPending}>创建服务账号</button>
        </form>
        <div className="account-list">
          {accounts.data?.items.length === 0 && <p className="empty-state">暂无服务账号。</p>}
          {accounts.data?.items.map((account) => <div className="account-row" key={account.id}><strong>{account.name}</strong><span>{account.status}</span></div>)}
        </div>
      </section>
      <section className="settings-panel">
        <h3>API Token</h3>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (tokenAccountId) createToken.mutate() }}>
          <label>服务账号<select value={tokenAccountId} onChange={(event) => setTokenAccountId(event.target.value)} required><option value="">选择服务账号…</option>{accounts.data?.items.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>
          <label>Scopes（逗号分隔）<input value={tokenScopes} onChange={(event) => setTokenScopes(event.target.value)} placeholder="read,write" /></label>
          <label>有效期（天）<input type="number" min={1} max={365} value={expiresDays} onChange={(event) => setExpiresDays(Number(event.target.value))} /></label>
          <button type="submit" disabled={createToken.isPending || !tokenAccountId}>创建 Token</button>
        </form>
        {newToken?.token && <div className="invite-result">
          <p>Token 已创建，仅显示一次：</p>
          <code>{newToken.token}</code>
          <small>请立即保存；列表只显示哈希与元数据。</small>
        </div>}
        <div className="token-list">
          {tokens.data?.items.map((token) => <div className="token-row" key={token.id}>
            <div><strong>{token.scopes.join(', ')}</strong><small>{token.revokedAt ? '已撤销' : token.expiresAt ? `到期 ${new Date(token.expiresAt).toLocaleDateString()}` : '永不过期'}</small></div>
            {!token.revokedAt && <button type="button" disabled={revokeToken.isPending} onClick={() => revokeToken.mutate(token.id)}>撤销</button>}
          </div>)}
        </div>
      </section>
    </div>
  </section>
}
