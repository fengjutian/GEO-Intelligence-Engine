import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

type Member = { id: string; userId: string; email: string; subject: string; role: string; status: string }
type Invitation = { id: string; email: string; role: string; status: string; expiresAt: string; token?: string }
type ServiceAccount = { id: string; name: string; status: string; createdAt: string }
type APIToken = { id: string; principalId: string; scopes: string[]; expiresAt: string | null; lastUsedAt: string | null; revokedAt: string | null; createdAt: string; token?: string }
type ProjectOption = { id: string; name: string }
type ProjectMember = { id: string; userId: string; email: string; roleOverride: string | null }

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
  const [projectId, setProjectId] = useState('')
  const [projectUserId, setProjectUserId] = useState('')
  const [projectRole, setProjectRole] = useState('')
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
  const projects = useQuery({ queryKey: ['settings-projects'], queryFn: () => request<{ items: ProjectOption[] }>('/api/v1/projects?limit=100') })
  const projectMembers = useQuery({
    queryKey: ['project-members', projectId],
    queryFn: () => request<{ items: ProjectMember[] }>(`/api/v1/projects/${projectId}/members`),
    enabled: Boolean(projectId),
  })
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

  const removeProjectMember = useMutation({
    mutationFn: (userId: string) => request(`/api/v1/projects/${projectId}/members/${userId}`, { method: 'DELETE' }),
    onSuccess: async () => {
      setMessage('项目成员已移除')
      await queryClient.invalidateQueries({ queryKey: ['project-members', projectId] })
    },
    onError: (error) => setMessage(error.message),
  })
  const addProjectMember = useMutation({
    mutationFn: () => request(`/api/v1/projects/${projectId}/members`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ userId: projectUserId, roleOverride: projectRole }),
    }),
    onSuccess: async () => {
      setProjectUserId('')
      setMessage('项目成员已添加')
      await queryClient.invalidateQueries({ queryKey: ['project-members', projectId] })
    },
    onError: (error) => setMessage(error.message),
  })

  return <section className="workspace module-workspace settings-page">
    <div className="module-heading"><div><p className="eyebrow">TENANT SETTINGS</p><h2>成员与权限</h2><p>管理当前租户成员、邀请与角色；最后一名 Owner 不可降级或停用。</p></div><span>{members.data?.items.length ?? 0} 成员</span></div>
    {message && <p className="audit-message" role="status">{message}</p>}
    <div className="settings-layout">
      <aside className="settings-index" aria-label="设置分区">
        <p>设置目录</p>
        <a href="#tenant-access"><span>01</span>成员与邀请</a>
        <a href="#machine-access"><span>02</span>机器身份</a>
        <a href="#project-access"><span>03</span>项目授权</a>
        <small>权限变更即时生效。Token 明文仅在创建时显示一次。</small>
      </aside>
      <div className="settings-content">
    <div className="settings-section-heading" id="tenant-access"><span>01</span><div><h3>成员与邀请</h3><p>维护租户成员及其默认权限。</p></div></div>
    <div className="settings-grid">
      <section className="settings-panel">
        <div className="settings-panel-header"><div><h3>当前成员</h3><p>{members.data?.items.length ?? 0} 位成员拥有租户访问权限</p></div></div>
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
        <div className="settings-panel-header"><div><h3>发送邀请</h3><p>选择初始角色，创建一次性邀请链接。</p></div></div>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (email.trim()) createInvite.mutate() }}>
          <label>邮箱<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="name@example.com" required /></label>
          <label>角色<select value={inviteRole} onChange={(event) => setInviteRole(event.target.value)}>{ROLES.filter((role) => role !== 'Owner').map((role) => <option key={role} value={role}>{role}</option>)}</select></label>
          <button className="settings-primary-button" type="submit" disabled={createInvite.isPending}>{createInvite.isPending ? '正在创建…' : '创建邀请'}</button>
        </form>
        {invite && <div className="invite-result">
          <p>邀请已创建：{invite.email} · {invite.role}</p>
          <code>{invite.token}</code>
          <small>Token 仅显示一次，请通过安全渠道发送。</small>
        </div>}
      </section>
    </div>
    <div className="settings-section-heading" id="machine-access"><span>02</span><div><h3>机器身份</h3><p>为自动化程序配置独立身份和凭据。</p></div></div>
    <div className="settings-grid">
      <section className="settings-panel">
        <div className="settings-panel-header"><div><h3>服务账号</h3><p>用于 CI、自动化任务和外部集成。</p></div></div>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (accountName.trim()) createAccount.mutate() }}>
          <label>名称<input value={accountName} onChange={(event) => setAccountName(event.target.value)} placeholder="ci-pipeline" maxLength={120} required /></label>
          <button className="settings-primary-button" type="submit" disabled={createAccount.isPending}>{createAccount.isPending ? '正在创建…' : '创建服务账号'}</button>
        </form>
        <div className="account-list">
          {accounts.data?.items.length === 0 && <p className="empty-state">暂无服务账号。</p>}
          {accounts.data?.items.map((account) => <div className="account-row" key={account.id}><strong>{account.name}</strong><span>{account.status}</span></div>)}
        </div>
      </section>
      <section className="settings-panel">
        <div className="settings-panel-header"><div><h3>访问 Token</h3><p>按服务账号签发最小权限凭据。</p></div></div>
        <form className="invite-form" onSubmit={(event) => { event.preventDefault(); if (tokenAccountId) createToken.mutate() }}>
          <label>服务账号<select value={tokenAccountId} onChange={(event) => setTokenAccountId(event.target.value)} required><option value="">选择服务账号…</option>{accounts.data?.items.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>
          <label>Scopes（逗号分隔）<input value={tokenScopes} onChange={(event) => setTokenScopes(event.target.value)} placeholder="read,write" /></label>
          <label>有效期（天）<input type="number" min={1} max={365} value={expiresDays} onChange={(event) => setExpiresDays(Number(event.target.value))} /></label>
          <button className="settings-primary-button" type="submit" disabled={createToken.isPending || !tokenAccountId}>{createToken.isPending ? '正在创建…' : '创建 Token'}</button>
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
    <div className="settings-section-heading" id="project-access"><span>03</span><div><h3>项目授权</h3><p>将成员访问范围收窄到指定项目。</p></div></div>
    <section className="settings-panel project-members-panel">
      <div className="settings-panel-header"><div><h3>授权规则</h3><p>可选地覆盖成员的租户默认角色。</p></div></div>
      <div className="project-member-form">
        <label>项目<select value={projectId} onChange={(event) => setProjectId(event.target.value)}><option value="">选择项目…</option>{projects.data?.items.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
        <label>用户 ID<input value={projectUserId} onChange={(event) => setProjectUserId(event.target.value)} placeholder="用户 UUID" /></label>
        <label>角色覆盖<select value={projectRole} onChange={(event) => setProjectRole(event.target.value)}><option value="">继承租户角色</option>{ROLES.map((role) => <option key={role} value={role}>{role}</option>)}</select></label>
        <button className="settings-primary-button" type="button" disabled={!projectId || !projectUserId || addProjectMember.isPending} onClick={() => addProjectMember.mutate()}>添加成员</button>
      </div>
      {projectId && <div className="member-table">
        <div className="member-row member-head"><span>成员</span><span>角色覆盖</span><span>操作</span></div>
        {projectMembers.data?.items.length === 0 && <p className="empty-state">该项目暂无成员记录，当前成员按租户权限访问。</p>}
        {projectMembers.data?.items.map((member) => <div className="member-row" key={member.id}>
          <div><strong>{member.email}</strong><small>{member.userId}</small></div>
          <span>{member.roleOverride ?? '继承'}</span>
          <button type="button" className="danger-link" disabled={removeProjectMember.isPending} onClick={() => removeProjectMember.mutate(member.userId)}>移除</button>
        </div>)}
      </div>}
    </section>
      </div>
    </div>
  </section>
}
