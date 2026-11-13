import { Activity, Database, Radar, ShieldCheck } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FormEvent, useState } from 'react'

type Version = { apiVersion: string; baseline: string }
type Project = {
  id: string
  name: string
  brandName: string
  officialUrl: string
  locale: string
  region: string
  timezone: string
  status: string
  version: number
}
type ProjectPage = { items: Project[]; page: { hasMore: boolean } }
type ProjectInput = Omit<Project, 'id' | 'status' | 'version'>
type ConfigVersion = { id: string; projectId: string; version: number; contentHash: string; publishedAt: string }
type BrandAlias = { id: string; alias: string; aliasType: string; locale: string | null; enabled: boolean }
type Product = { id: string; name: string; description: string | null; verificationStatus: string }
type BrandProfile = { projectId: string; brandId: string; canonicalName: string; officialDomain: string; aliases: BrandAlias[]; products: Product[] }

async function fetchVersion(): Promise<Version> {
  const response = await fetch('/api/v1/version')
  if (!response.ok) throw new Error('API unavailable')
  return response.json() as Promise<Version>
}

async function fetchProjects(): Promise<ProjectPage> {
  const response = await fetch('/api/v1/projects?limit=20')
  if (!response.ok) throw new Error('项目列表加载失败')
  return response.json() as Promise<ProjectPage>
}

async function createProject(input: ProjectInput): Promise<Project> {
  const response = await fetch('/api/v1/projects', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!response.ok) {
    const problem = await response.json().catch(() => ({ title: '项目创建失败' })) as { title?: string }
    throw new Error(problem.title ?? '项目创建失败')
  }
  return response.json() as Promise<Project>
}

async function updateProject(project: Project, patch: Partial<Pick<Project, 'name' | 'locale' | 'region' | 'timezone'>>): Promise<Project> {
  const response = await fetch(`/api/v1/projects/${project.id}`, {
    method: 'PATCH',
    headers: { 'content-type': 'application/merge-patch+json', 'if-match': `"${project.version}"` },
    body: JSON.stringify(patch),
  })
  if (!response.ok) throw new Error(response.status === 409 ? '项目已被其他用户修改，请刷新后重试' : '项目更新失败')
  return response.json() as Promise<Project>
}

async function archiveProject(project: Project): Promise<void> {
  const response = await fetch(`/api/v1/projects/${project.id}:archive`, {
    method: 'POST',
    headers: { 'if-match': `"${project.version}"` },
  })
  if (!response.ok) throw new Error(response.status === 409 ? '项目状态已变化，请刷新后重试' : '项目归档失败')
}

async function publishConfig(project: Project): Promise<ConfigVersion> {
  const response = await fetch(`/api/v1/projects/${project.id}/config-versions`, { method: 'POST' })
  if (!response.ok) throw new Error(response.status === 409 ? '归档项目不能发布配置' : '配置发布失败')
  return response.json() as Promise<ConfigVersion>
}

async function fetchBrandProfile(projectId: string): Promise<BrandProfile> {
  const response = await fetch(`/api/v1/projects/${projectId}/brand-profile`)
  if (!response.ok) throw new Error('品牌档案加载失败')
  return response.json() as Promise<BrandProfile>
}

async function addAlias(projectId: string, input: { alias: string; aliasType: string; locale: string }): Promise<BrandAlias> {
  const response = await fetch(`/api/v1/projects/${projectId}/brand-aliases`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) })
  if (!response.ok) throw new Error(response.status === 409 ? '该别名或等价写法已经存在' : '别名添加失败')
  return response.json() as Promise<BrandAlias>
}

async function setAliasEnabled(projectId: string, alias: BrandAlias): Promise<BrandAlias> {
  const response = await fetch(`/api/v1/projects/${projectId}/brand-aliases/${alias.id}`, { method: 'PATCH', headers: { 'content-type': 'application/merge-patch+json' }, body: JSON.stringify({ enabled: !alias.enabled }) })
  if (!response.ok) throw new Error('别名状态更新失败')
  return response.json() as Promise<BrandAlias>
}

async function addProduct(projectId: string, input: { name: string; description: string }): Promise<Product> {
  const response = await fetch(`/api/v1/projects/${projectId}/products`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) })
  if (!response.ok) throw new Error('产品添加失败')
  return response.json() as Promise<Product>
}

const modules = [
  { title: 'Discovery', text: '网站发现、抓取与技术审计', icon: Radar },
  { title: 'Knowledge', text: '品牌实体、关系与一致性', icon: Database },
  { title: 'Measurement', text: '多模型测量与可信指标', icon: Activity },
  { title: 'Governance', text: '租户隔离、审批与审计', icon: ShieldCheck },
]

export function App() {
  const version = useQuery({ queryKey: ['version'], queryFn: fetchVersion })
  const projects = useQuery({ queryKey: ['projects'], queryFn: fetchProjects })
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Project | null>(null)
  const [published, setPublished] = useState<ConfigVersion | null>(null)
  const [profileProject, setProfileProject] = useState<Project | null>(null)
  const profile = useQuery({ queryKey: ['brand-profile', profileProject?.id], queryFn: () => fetchBrandProfile(profileProject!.id), enabled: profileProject !== null })
  const create = useMutation({
    mutationFn: createProject,
    onSuccess: async () => {
      setFormOpen(false)
      await queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
  const update = useMutation({
    mutationFn: ({ project, patch }: { project: Project; patch: Partial<Pick<Project, 'name' | 'locale' | 'region' | 'timezone'>> }) => updateProject(project, patch),
    onSuccess: async () => {
      setEditing(null)
      await queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
  const archive = useMutation({
    mutationFn: archiveProject,
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['projects'] }),
  })
  const publish = useMutation({ mutationFn: publishConfig, onSuccess: setPublished })
  const aliasCreate = useMutation({ mutationFn: ({ projectId, input }: { projectId: string; input: { alias: string; aliasType: string; locale: string } }) => addAlias(projectId, input), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['brand-profile', profileProject?.id] }) })
  const aliasToggle = useMutation({ mutationFn: ({ projectId, alias }: { projectId: string; alias: BrandAlias }) => setAliasEnabled(projectId, alias), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['brand-profile', profileProject?.id] }) })
  const productCreate = useMutation({ mutationFn: ({ projectId, input }: { projectId: string; input: { name: string; description: string } }) => addProduct(projectId, input), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['brand-profile', profileProject?.id] }) })

  function submitProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    create.mutate({
      name: String(data.get('name')),
      brandName: String(data.get('brandName')),
      officialUrl: String(data.get('officialUrl')),
      locale: String(data.get('locale')),
      region: String(data.get('region')),
      timezone: String(data.get('timezone')),
    })
  }

  function submitEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!editing) return
    const data = new FormData(event.currentTarget)
    update.mutate({ project: editing, patch: { name: String(data.get('name')), locale: String(data.get('locale')), region: String(data.get('region')), timezone: String(data.get('timezone')) } })
  }

  function submitAlias(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!profileProject) return
    const form = event.currentTarget
    const data = new FormData(form)
    aliasCreate.mutate({ projectId: profileProject.id, input: { alias: String(data.get('alias')), aliasType: String(data.get('aliasType')), locale: String(data.get('locale')) } }, { onSuccess: () => form.reset() })
  }

  function submitProduct(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!profileProject) return
    const form = event.currentTarget
    const data = new FormData(form)
    productCreate.mutate({ projectId: profileProject.id, input: { name: String(data.get('name')), description: String(data.get('description')) } }, { onSuccess: () => form.reset() })
  }

  return <main className="min-h-screen px-6 py-10 sm:px-10">
    <div className="mx-auto max-w-6xl">
      <header className="flex flex-col gap-5 border-b border-line pb-8 sm:flex-row sm:items-end sm:justify-between">
        <div><p className="eyebrow">AI Search Intelligence</p><h1>GEO Intelligence</h1><p className="mt-3 max-w-2xl text-muted">以可追溯证据、版本化评估和受控实验构建可信的 GEO 优化闭环。</p></div>
        <div className="status" data-state={version.isSuccess ? 'ready' : 'loading'}><span />{version.isSuccess ? `${version.data.baseline} · API ${version.data.apiVersion}` : version.isError ? 'API unavailable' : 'Connecting'}</div>
      </header>
      <section className="grid gap-4 py-10 sm:grid-cols-2 lg:grid-cols-4" aria-label="核心模块">
        {modules.map(({ title, text, icon: Icon }) => <article className="card" key={title}><Icon aria-hidden="true"/><h2>{title}</h2><p>{text}</p></article>)}
      </section>
      <section className="panel"><div><p className="eyebrow">P0 DELIVERY</p><h2>GEO Audit</h2></div><p>项目配置 → 网站抓取 → 问题空间 → 多模型采样 → Answer Judge → 指标与报告</p></section>
      <section className="workspace" aria-labelledby="projects-title">
        <div className="section-heading">
          <div><p className="eyebrow">WORKSPACE</p><h2 id="projects-title">项目与品牌</h2></div>
          <button className="primary-button" type="button" onClick={() => setFormOpen((open) => !open)}>{formOpen ? '取消' : '创建项目'}</button>
        </div>
        {formOpen && <form className="project-form" onSubmit={submitProject}>
          <label>项目名称<input name="name" required maxLength={200} placeholder="中国区 GEO 基线" /></label>
          <label>品牌正式名<input name="brandName" required maxLength={300} placeholder="Acme" /></label>
          <label className="wide">官方网站<input name="officialUrl" required type="url" placeholder="https://example.com" /></label>
          <label>默认语言<input name="locale" required defaultValue="zh-CN" /></label>
          <label>地区<input name="region" required defaultValue="CN" /></label>
          <label>时区<input name="timezone" required defaultValue="Asia/Shanghai" /></label>
          <div className="form-actions"><button className="primary-button" disabled={create.isPending}>{create.isPending ? '创建中…' : '确认创建'}</button>{create.error && <span role="alert">{create.error.message}</span>}</div>
        </form>}
        {projects.isLoading && <p className="empty-state">正在加载项目…</p>}
        {projects.isError && <p className="empty-state error" role="alert">项目列表加载失败，请确认 API 与数据库状态。</p>}
        {projects.data?.items.length === 0 && <p className="empty-state">暂无项目。创建第一个品牌测量空间。</p>}
        <div className="project-grid">
          {projects.data?.items.map((project) => <article className="project-card" key={project.id}>
            <div><span className="project-status">{project.status}</span><h3>{project.name}</h3><p>{project.brandName} · {project.officialUrl}</p></div>
            <dl><div><dt>区域语言</dt><dd>{project.region} / {project.locale}</dd></div><div><dt>时区</dt><dd>{project.timezone}</dd></div><div><dt>版本</dt><dd>v{project.version}</dd></div></dl>
            <div className="project-actions"><button type="button" onClick={() => setProfileProject(project)}>品牌档案</button><button type="button" onClick={() => publish.mutate(project)} disabled={project.status === 'archived' || publish.isPending}>发布配置</button><button type="button" onClick={() => setEditing(project)} disabled={project.status === 'archived'}>编辑</button><button className="danger" type="button" onClick={() => { if (window.confirm(`确认归档“${project.name}”？历史数据仍可查看。`)) archive.mutate(project) }} disabled={project.status === 'archived' || archive.isPending}>归档</button></div>
          </article>)}
        </div>
        {editing && <div className="modal-backdrop" role="presentation" onMouseDown={() => setEditing(null)}><form className="edit-dialog" aria-labelledby="edit-title" onSubmit={submitEdit} onMouseDown={(event) => event.stopPropagation()}>
          <div><p className="eyebrow">PROJECT SETTINGS</p><h2 id="edit-title">编辑项目</h2></div>
          <label>项目名称<input name="name" required defaultValue={editing.name} /></label>
          <div className="edit-grid"><label>语言<input name="locale" required defaultValue={editing.locale} /></label><label>地区<input name="region" required defaultValue={editing.region} /></label></div>
          <label>时区<input name="timezone" required defaultValue={editing.timezone} /></label>
          {update.error && <p className="error" role="alert">{update.error.message}</p>}
          <div className="dialog-actions"><button type="button" onClick={() => setEditing(null)}>取消</button><button className="primary-button" disabled={update.isPending}>{update.isPending ? '保存中…' : '保存修改'}</button></div>
        </form></div>}
        {profileProject && <div className="modal-backdrop" role="presentation" onMouseDown={() => setProfileProject(null)}><section className="profile-dialog" aria-labelledby="profile-title" onMouseDown={(event) => event.stopPropagation()}>
          <div className="profile-header"><div><p className="eyebrow">BRAND PROFILE</p><h2 id="profile-title">{profile.data?.canonicalName ?? profileProject.brandName}</h2><p>{profile.data?.officialDomain}</p></div><button type="button" aria-label="关闭" onClick={() => setProfileProject(null)}>×</button></div>
          {profile.isLoading && <p>正在加载…</p>}
          {profile.error && <p className="error" role="alert">{profile.error.message}</p>}
          {profile.data && <div className="profile-columns">
            <div><h3>品牌别名</h3><form className="compact-form" onSubmit={submitAlias}><input name="alias" required placeholder="别名或旧称" /><select name="aliasType" defaultValue="alias"><option value="alias">别名</option><option value="old_name">旧称</option><option value="abbreviation">缩写</option><option value="product_short_name">产品简称</option></select><input name="locale" defaultValue={profileProject.locale} placeholder="语言" /><button className="primary-button">添加</button></form>
              <div className="profile-list">{profile.data.aliases.length === 0 && <p>暂无别名</p>}{profile.data.aliases.map((alias) => <div key={alias.id}><span><strong>{alias.alias}</strong><small>{alias.aliasType} · {alias.locale ?? '通用'}</small></span><button type="button" className={alias.enabled ? 'toggle active' : 'toggle'} onClick={() => aliasToggle.mutate({ projectId: profileProject.id, alias })}>{alias.enabled ? '已启用' : '已停用'}</button></div>)}</div>
            </div>
            <div><h3>产品</h3><form className="compact-form" onSubmit={submitProduct}><input name="name" required placeholder="产品名称" /><input name="description" placeholder="产品说明（可选）" /><button className="primary-button">添加</button></form>
              <div className="profile-list">{profile.data.products.length === 0 && <p>暂无产品</p>}{profile.data.products.map((product) => <div key={product.id}><span><strong>{product.name}</strong><small>{product.description || '无说明'}</small></span><em>{product.verificationStatus}</em></div>)}</div>
            </div>
          </div>}
          {(aliasCreate.error || aliasToggle.error || productCreate.error) && <p className="error" role="alert">{aliasCreate.error?.message ?? aliasToggle.error?.message ?? productCreate.error?.message}</p>}
        </section></div>}
        {archive.error && <p className="error" role="alert">{archive.error.message}</p>}
        {publish.error && <p className="error" role="alert">{publish.error.message}</p>}
        {published && <div className="publish-toast" role="status">配置 V{published.version} 已发布 · {published.contentHash.slice(0, 12)}…</div>}
      </section>
    </div>
  </main>
}
