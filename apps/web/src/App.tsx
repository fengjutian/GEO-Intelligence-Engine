import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FormEvent, useState } from 'react'
import { OverviewPage } from './features/overview/OverviewPage'
import { ModelServicesPage } from './features/models/ModelServicesPage'
import { EvidenceDialog } from './features/crawls/EvidenceDialog'
import { AppShell, type PageId } from './layout/AppShell'

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
type Competitor = { id: string; name: string; domain: string | null; status: string }
type BrandProfile = { projectId: string; brandId: string; canonicalName: string; officialDomain: string; aliases: BrandAlias[]; products: Product[]; competitors: Competitor[] }
type CrawlRun = { id: string; projectId: string; websiteId: string; status: string; counts: { discovered: number; fetched: number; failed: number; blocked?: number }; startedAt: string | null; finishedAt: string | null; createdAt: string }
type PageSummary = { pageId: string; contentId: string; canonicalUrl: string | null; finalUrl: string; httpStatus: number; title: string; language: string | null; contentHash: string; parserVersion: string; createdAt: string }
type PageDetail = PageSummary & { links: string[] }
type Finding = { id: string; ruleCode: string; severity: string; status: string; summary: string; evidence: Record<string, unknown> }
type ModelProvider = { provider: 'deepseek' | 'minimax'; configured: boolean; baseUrl: string; defaultModel: string }
type BuyerQuestion = { id: string; text: string; language: string; questionType: string; intent?: string; weight: number; status: string; createdAt: string }

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

async function addCompetitor(projectId: string, input: { name: string; domain: string }): Promise<Competitor> {
  const response = await fetch(`/api/v1/projects/${projectId}/competitors`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) })
  if (!response.ok) throw new Error(response.status === 409 ? '竞品名称与主品牌或已有竞品冲突' : '竞品添加失败')
  return response.json() as Promise<Competitor>
}

async function startCrawl(projectId: string): Promise<CrawlRun> {
  const response = await fetch(`/api/v1/projects/${projectId}/crawl-runs`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ limits: { maxPages: 10, maxDepth: 2, concurrency: 2 } }) })
  if (!response.ok) throw new Error(response.status === 409 ? '归档项目不能启动抓取' : '抓取任务创建失败')
  return response.json() as Promise<CrawlRun>
}

async function fetchCrawl(runId: string): Promise<CrawlRun> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}`)
  if (!response.ok) throw new Error('抓取状态加载失败')
  return response.json() as Promise<CrawlRun>
}

async function fetchProjectCrawls(projectId: string): Promise<{ items: CrawlRun[] }> {
  const response = await fetch(`/api/v1/projects/${projectId}/crawl-runs`)
  if (!response.ok) throw new Error('抓取记录加载失败')
  return response.json() as Promise<{ items: CrawlRun[] }>
}

async function controlCrawl(runId: string, action: 'pause' | 'resume' | 'cancel'): Promise<CrawlRun> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}:${action}`, { method: 'POST' })
  if (!response.ok) throw new Error(response.status === 409 ? '当前状态不允许该操作' : '抓取控制失败')
  return response.json() as Promise<CrawlRun>
}

async function fetchCrawlPages(runId: string): Promise<{ items: PageSummary[] }> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}/pages`)
  if (!response.ok) throw new Error('页面列表加载失败')
  return response.json() as Promise<{ items: PageSummary[] }>
}

async function fetchPageDetail(contentId: string): Promise<PageDetail> {
  const response = await fetch(`/api/v1/page-contents/${contentId}`)
  if (!response.ok) throw new Error('页面详情加载失败')
  return response.json() as Promise<PageDetail>
}

async function fetchFindings(runId: string): Promise<{ items: Finding[] }> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}/findings`)
  if (!response.ok) throw new Error('技术问题加载失败')
  return response.json() as Promise<{ items: Finding[] }>
}

async function fetchModelProviders(): Promise<{ items: ModelProvider[] }> {
  const response = await fetch('/api/v1/model-providers')
  if (!response.ok) throw new Error('模型供应商状态加载失败')
  return response.json() as Promise<{ items: ModelProvider[] }>
}

async function checkModelProvider(provider: ModelProvider['provider']): Promise<string> {
  const response = await fetch(`/api/v1/model-providers/${provider}/health`, { method: 'POST' })
  if (!response.ok) throw new Error(response.status === 409 ? '请先在服务端配置 API Key' : '供应商健康检查失败')
  return provider
}

async function fetchQuestions(projectId: string): Promise<{ items: BuyerQuestion[] }> {
  const response = await fetch(`/api/v1/projects/${projectId}/questions?limit=100`)
  if (!response.ok) throw new Error('问题空间加载失败')
  return response.json() as Promise<{ items: BuyerQuestion[] }>
}

async function createQuestion(projectId: string, input: { text: string; language: string; questionType: string; intent: string; weight: number }): Promise<BuyerQuestion> {
  const response = await fetch(`/api/v1/projects/${projectId}/questions`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) })
  if (!response.ok) { const problem = await response.json().catch(() => ({ title: '问题创建失败' })) as { title?: string }; throw new Error(problem.title ?? '问题创建失败') }
  return response.json() as Promise<BuyerQuestion>
}

export function App() {
  const version = useQuery({ queryKey: ['version'], queryFn: fetchVersion })
  const projects = useQuery({ queryKey: ['projects'], queryFn: fetchProjects })
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Project | null>(null)
  const [published, setPublished] = useState<ConfigVersion | null>(null)
  const [profileProject, setProfileProject] = useState<Project | null>(null)
  const [activeCrawlId, setActiveCrawlId] = useState<string | null>(null)
  const [pagesOpen, setPagesOpen] = useState(false)
  const [selectedContentId, setSelectedContentId] = useState<string | null>(null)
  const [historyProject, setHistoryProject] = useState<Project | null>(null)
  const [questionProject, setQuestionProject] = useState<Project | null>(null)
  const [evidenceRunId, setEvidenceRunId] = useState<string | null>(null)
  const [activePage, setActivePage] = useState<PageId>('overview')
  const profile = useQuery({ queryKey: ['brand-profile', profileProject?.id], queryFn: () => fetchBrandProfile(profileProject!.id), enabled: profileProject !== null })
  const crawlStatus = useQuery({ queryKey: ['crawl-run', activeCrawlId], queryFn: () => fetchCrawl(activeCrawlId!), enabled: activeCrawlId !== null, refetchInterval: (query) => ['succeeded', 'failed', 'cancelled'].includes(query.state.data?.status ?? '') ? false : 2000 })
  const crawlPages = useQuery({ queryKey: ['crawl-pages', activeCrawlId], queryFn: () => fetchCrawlPages(activeCrawlId!), enabled: pagesOpen && activeCrawlId !== null })
  const pageDetail = useQuery({ queryKey: ['page-content', selectedContentId], queryFn: () => fetchPageDetail(selectedContentId!), enabled: selectedContentId !== null })
  const findings = useQuery({ queryKey: ['crawl-findings', activeCrawlId], queryFn: () => fetchFindings(activeCrawlId!), enabled: pagesOpen && activeCrawlId !== null })
  const modelProviders = useQuery({ queryKey: ['model-providers'], queryFn: fetchModelProviders })
  const buyerQuestions = useQuery({ queryKey: ['questions', questionProject?.id], queryFn: () => fetchQuestions(questionProject!.id), enabled: questionProject !== null })
  const crawlHistory = useQuery({ queryKey: ['project-crawls', historyProject?.id], queryFn: () => fetchProjectCrawls(historyProject!.id), enabled: historyProject !== null, refetchInterval: (query) => query.state.data?.items.some((run) => ['queued', 'running'].includes(run.status)) ? 2000 : false })
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
  const competitorCreate = useMutation({ mutationFn: ({ projectId, input }: { projectId: string; input: { name: string; domain: string } }) => addCompetitor(projectId, input), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['brand-profile', profileProject?.id] }) })
  const crawlCreate = useMutation({ mutationFn: startCrawl, onSuccess: async (run) => { setActiveCrawlId(run.id); await queryClient.invalidateQueries({ queryKey: ['project-crawls', run.projectId] }) } })
  const crawlControl = useMutation({ mutationFn: ({ runId, action }: { runId: string; action: 'pause' | 'resume' | 'cancel' }) => controlCrawl(runId, action), onSuccess: async (run) => { await queryClient.setQueryData(['crawl-run', run.id], run); await queryClient.invalidateQueries({ queryKey: ['crawl-run', run.id] }) } })
  const providerHealth = useMutation({ mutationFn: checkModelProvider })
  const questionCreate = useMutation({ mutationFn: ({ projectId, input }: { projectId: string; input: { text: string; language: string; questionType: string; intent: string; weight: number } }) => createQuestion(projectId, input), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['questions', questionProject?.id] }) })

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

  function submitCompetitor(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!profileProject) return
    const form = event.currentTarget
    const data = new FormData(form)
    competitorCreate.mutate({ projectId: profileProject.id, input: { name: String(data.get('name')), domain: String(data.get('domain')) } }, { onSuccess: () => form.reset() })
  }

  function submitQuestion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!questionProject) return
    const form = event.currentTarget
    const data = new FormData(form)
    questionCreate.mutate({ projectId: questionProject.id, input: { text: String(data.get('text')), language: String(data.get('language')), questionType: String(data.get('questionType')), intent: String(data.get('intent')), weight: Number(data.get('weight')) } }, { onSuccess: () => form.reset() })
  }

  const activeProjects = projects.data?.items.filter((project) => project.status !== 'archived').length ?? 0
  const configuredProviders = modelProviders.data?.items.filter((provider) => provider.configured).length ?? 0

  const workspaceTitle = activePage === 'audits' ? '网站审计' : activePage === 'questions' ? '问题空间' : '项目空间'
  const workspaceDescription = activePage === 'audits' ? '选择项目发起官网抓取或查看历史证据。' : activePage === 'questions' ? '选择项目维护买家问题和测量问题集。' : '管理品牌基础资料、抓取任务和配置版本。'

  return <AppShell activePage={activePage} onNavigate={setActivePage} apiVersion={version.data?.apiVersion} baseline={version.data?.baseline} connected={version.isSuccess} error={version.isError}>
      {activePage === 'overview' && <OverviewPage activeProjects={activeProjects} websiteCount={projects.data?.items.length ?? 0} configuredProviders={configuredProviders} onCreateProject={() => { setActivePage('projects'); setFormOpen(true) }} />}
      {activePage === 'models' && <ModelServicesPage providers={modelProviders.data?.items} checking={providerHealth.isPending} message={providerHealth.data} error={providerHealth.error?.message} onCheck={(provider) => providerHealth.mutate(provider)} />}
      {!['overview', 'models'].includes(activePage) && <section className="workspace module-workspace" id="projects" aria-labelledby="projects-title">
        <div className="section-heading">
          <div><p className="eyebrow">{activePage.toUpperCase()}</p><h2 id="projects-title">{workspaceTitle}</h2><p>{workspaceDescription}</p></div>
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
            <div className="project-actions"><button type="button" onClick={() => crawlCreate.mutate(project.id)} disabled={project.status === 'archived' || crawlCreate.isPending}>抓取官网</button><button type="button" onClick={() => setHistoryProject(project)}>抓取记录</button><button type="button" onClick={() => setProfileProject(project)}>品牌档案</button><button type="button" onClick={() => setQuestionProject(project)}>问题空间</button><button type="button" onClick={() => publish.mutate(project)} disabled={project.status === 'archived' || publish.isPending}>发布配置</button><button type="button" onClick={() => setEditing(project)} disabled={project.status === 'archived'}>编辑</button><button className="danger" type="button" onClick={() => { if (window.confirm(`确认归档“${project.name}”？历史数据仍可查看。`)) archive.mutate(project) }} disabled={project.status === 'archived' || archive.isPending}>归档</button></div>
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
            <div><h3>竞品</h3><form className="compact-form" onSubmit={submitCompetitor}><input name="name" required placeholder="竞品名称" /><input name="domain" placeholder="官网域名（可选）" /><button className="primary-button">添加</button></form>
              <div className="profile-list">{profile.data.competitors.length === 0 && <p>暂无竞品</p>}{profile.data.competitors.map((competitor) => <div key={competitor.id}><span><strong>{competitor.name}</strong><small>{competitor.domain || '未配置域名'}</small></span><em>{competitor.status}</em></div>)}</div>
            </div>
          </div>}
          {(aliasCreate.error || aliasToggle.error || productCreate.error || competitorCreate.error) && <p className="error" role="alert">{aliasCreate.error?.message ?? aliasToggle.error?.message ?? productCreate.error?.message ?? competitorCreate.error?.message}</p>}
        </section></div>}
        {historyProject && <div className="modal-backdrop" role="presentation" onMouseDown={() => setHistoryProject(null)}><section className="history-dialog" aria-labelledby="history-title" onMouseDown={(event) => event.stopPropagation()}><div className="profile-header"><div><p className="eyebrow">CRAWL HISTORY</p><h2 id="history-title">{historyProject.name} · 抓取记录</h2></div><button onClick={() => setHistoryProject(null)} aria-label="关闭">×</button></div>{crawlHistory.isLoading && <p>正在加载…</p>}{crawlHistory.data?.items.length === 0 && <p className="empty-state">暂无抓取记录</p>}<div className="history-list">{crawlHistory.data?.items.map((run) => <article key={run.id}><span className="run-state" data-state={run.status}>{run.status}</span><div><strong>{new Date(run.createdAt ?? Date.now()).toLocaleString()}</strong><small>{run.id}</small></div><div className="run-counts"><span>发现 {run.counts.discovered}</span><span>成功 {run.counts.fetched}</span><span>失败 {run.counts.failed}</span><span>拦截 {run.counts.blocked ?? 0}</span></div><button onClick={() => { setActiveCrawlId(run.id); setHistoryProject(null); if (run.counts.fetched > 0) setPagesOpen(true) }}>打开</button></article>)}</div></section></div>}
        {questionProject && <div className="modal-backdrop" role="presentation" onMouseDown={() => setQuestionProject(null)}><section className="question-dialog" aria-labelledby="questions-title" onMouseDown={(event) => event.stopPropagation()}><div className="profile-header"><div><p className="eyebrow">BUYER QUESTIONS</p><h2 id="questions-title">{questionProject.name} · 问题空间</h2><p>问题将进入默认草稿集，发布后形成不可变测量快照。</p></div><button onClick={() => setQuestionProject(null)} aria-label="关闭">×</button></div><form className="question-form" onSubmit={submitQuestion}><label className="question-text">买家问题<textarea name="text" required maxLength={2000} placeholder="例如：适合中国团队使用的 GEO 分析工具有哪些？" /></label><label>语言<input name="language" required defaultValue={questionProject.locale} /></label><label>类型<select name="questionType" defaultValue="category"><option value="brand">品牌理解</option><option value="category">类目发现</option><option value="comparison">比较决策</option><option value="scenario">场景问题</option><option value="decision">采购决策</option></select></label><label>意图<input name="intent" placeholder="discovery" /></label><label>权重<input name="weight" type="number" min="0" max="1000" step="0.1" defaultValue="1" /></label><button className="primary-button" disabled={questionCreate.isPending}>{questionCreate.isPending ? '添加中…' : '添加问题'}</button></form>{questionCreate.error && <p className="error" role="alert">{questionCreate.error.message}</p>}{buyerQuestions.isLoading && <p>正在加载…</p>}<div className="question-list">{buyerQuestions.data?.items.length === 0 && <p className="empty-state">暂无问题，先添加一个买家问题。</p>}{buyerQuestions.data?.items.map((question) => <article key={question.id}><span data-type={question.questionType}>{question.questionType}</span><div><strong>{question.text}</strong><small>{question.language} · {question.intent || '未分类意图'} · 权重 {question.weight}</small></div><em>{question.status}</em></article>)}</div></section></div>}
        {evidenceRunId && <EvidenceDialog runId={evidenceRunId} onClose={() => setEvidenceRunId(null)} />}
        {archive.error && <p className="error" role="alert">{archive.error.message}</p>}
        {publish.error && <p className="error" role="alert">{publish.error.message}</p>}
        {published && <div className="publish-toast" role="status">配置 V{published.version} 已发布 · {published.contentHash.slice(0, 12)}…</div>}
        {crawlCreate.error && <p className="error" role="alert">{crawlCreate.error.message}</p>}
        {crawlStatus.data && <div className="crawl-toast" role="status"><strong>官网抓取：{crawlStatus.data.status}</strong><span>发现 {crawlStatus.data.counts.discovered} · 成功 {crawlStatus.data.counts.fetched} · 失败 {crawlStatus.data.counts.failed} · 拦截 {crawlStatus.data.counts.blocked ?? 0}</span><div className="crawl-actions">{['queued', 'running'].includes(crawlStatus.data.status) && <button onClick={() => crawlControl.mutate({ runId: crawlStatus.data.id, action: 'pause' })}>暂停</button>}{crawlStatus.data.status === 'paused' && <button onClick={() => crawlControl.mutate({ runId: crawlStatus.data.id, action: 'resume' })}>继续</button>}{['queued', 'running', 'paused'].includes(crawlStatus.data.status) && <button onClick={() => crawlControl.mutate({ runId: crawlStatus.data.id, action: 'cancel' })}>取消</button>}{crawlStatus.data.counts.fetched > 0 && <button onClick={() => setPagesOpen(true)}>查看页面</button>}<button onClick={() => setEvidenceRunId(crawlStatus.data.id)}>Robots / Sitemap</button></div></div>}
        {crawlControl.error && <p className="error" role="alert">{crawlControl.error.message}</p>}
        {pagesOpen && <div className="modal-backdrop" role="presentation" onMouseDown={() => { setPagesOpen(false); setSelectedContentId(null) }}><section className="pages-dialog" aria-labelledby="pages-title" onMouseDown={(event) => event.stopPropagation()}><div className="profile-header"><div><p className="eyebrow">CRAWL EVIDENCE</p><h2 id="pages-title">抓取页面</h2></div><button onClick={() => setPagesOpen(false)} aria-label="关闭">×</button></div>{crawlPages.isLoading && <p>正在加载…</p>}<div className="finding-strip">{findings.data?.items.length === 0 && <p>未发现首批技术规则问题。</p>}{findings.data?.items.map((finding) => <article key={finding.id} data-severity={finding.severity}><span>{finding.severity}</span><strong>{finding.ruleCode}</strong><p>{finding.summary}</p></article>)}</div><div className="pages-layout"><div className="page-list">{crawlPages.data?.items.map((page) => <button key={page.contentId} onClick={() => setSelectedContentId(page.contentId)} data-active={selectedContentId === page.contentId}><span>{page.httpStatus}</span><strong>{page.title || '无标题页面'}</strong><small>{page.finalUrl}</small></button>)}</div><div className="page-evidence">{!selectedContentId && <p>选择页面查看不可变版本证据。</p>}{pageDetail.isLoading && <p>正在加载详情…</p>}{pageDetail.data && <><h3>{pageDetail.data.title || '无标题页面'}</h3><dl><div><dt>最终 URL</dt><dd>{pageDetail.data.finalUrl}</dd></div><div><dt>Canonical</dt><dd>{pageDetail.data.canonicalUrl ?? '未声明'}</dd></div><div><dt>内容哈希</dt><dd className="hash">{pageDetail.data.contentHash}</dd></div><div><dt>解析器</dt><dd>{pageDetail.data.parserVersion}</dd></div></dl><h4>出站链接（{pageDetail.data.links.length}）</h4><ul>{pageDetail.data.links.slice(0, 20).map((link) => <li key={link}>{link}</li>)}</ul></>}</div></div></section></div>}
      </section>}
  </AppShell>
}
