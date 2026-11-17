import { ArrowRight, Bot, Check, Circle, FolderKanban, Gauge, Globe2, Plus, ShieldCheck } from 'lucide-react'
import { Button } from '../../components/ui/button'

type OverviewProject = { id: string; name: string; brandName: string; officialUrl: string; locale: string; status: string }
type Props = {
  projects: OverviewProject[]
  configuredProviders: number
  onCreateProject: () => void
  onNavigate: (page: 'projects' | 'audits' | 'questions' | 'models') => void
}

const workflow = ['项目配置', '网站抓取', '问题空间', '模型采样', 'Answer Judge', '指标报告']

export function OverviewPage({ projects, configuredProviders, onCreateProject, onNavigate }: Props) {
  const activeProjects = projects.filter((project) => project.status !== 'archived')
  const readinessPercent = Math.round([activeProjects.length > 0, configuredProviders > 0].filter(Boolean).length / 2 * 100)

  return <section className="overview-workspace">
    <header className="overview-hero">
      <div><p className="eyebrow">WORKSPACE</p><h2>今天从哪里开始？</h2><p>查看品牌资产状态，继续尚未完成的 GEO 测量工作。</p></div>
      <Button type="button" onClick={onCreateProject}><Plus />创建项目</Button>
    </header>
    <div className="overview-metrics" aria-label="工作台概览">
      <article><FolderKanban /><div><span>活跃项目</span><strong>{activeProjects.length}</strong></div><small>共 {projects.length} 个项目</small></article>
      <article><Globe2 /><div><span>品牌档案</span><strong>{projects.length}</strong></div><small>与项目一一关联</small></article>
      <article><Bot /><div><span>模型服务</span><strong>{configuredProviders}<em>/2</em></strong></div><small>{configuredProviders === 2 ? '全部可用' : '仍需配置'}</small></article>
      <article><Gauge /><div><span>环境就绪度</span><strong>{readinessPercent}<em>%</em></strong></div><small>{readinessPercent === 100 ? '可以开始测量' : '完成基础配置'}</small></article>
    </div>
    <div className="overview-main-grid">
      <section className="overview-panel recent-projects">
        <header><div><p className="eyebrow">RECENT PROJECTS</p><h3>最近项目</h3></div><button type="button" onClick={() => onNavigate('projects')}>查看全部 <ArrowRight /></button></header>
        {activeProjects.length === 0 ? <div className="overview-empty"><FolderKanban /><strong>还没有活跃项目</strong><p>先创建项目，建立品牌和官网档案。</p><button type="button" onClick={onCreateProject}>创建第一个项目</button></div> : <div className="overview-project-list">
          {activeProjects.slice(0, 5).map((project) => <button type="button" key={project.id} onClick={() => onNavigate('projects')}><span>{project.name.slice(0, 1).toUpperCase()}</span><div><strong>{project.name}</strong><small>{project.brandName} · {project.officialUrl}</small></div><em>{project.locale}</em><ArrowRight /></button>)}
        </div>}
      </section>
      <aside className="overview-panel readiness-panel">
        <header><div><p className="eyebrow">GET STARTED</p><h3>开始 GEO 测量</h3></div><ShieldCheck /></header><p>完成基础配置后，即可运行可复现的模型审计。</p>
        <ol>
          <li data-done={activeProjects.length > 0}>{activeProjects.length > 0 ? <Check /> : <Circle />}<button type="button" onClick={() => onNavigate('projects')}><span>建立品牌项目</span><small>{activeProjects.length > 0 ? `${activeProjects.length} 个项目可用` : '品牌、官网与地区信息'}</small></button></li>
          <li data-done={configuredProviders > 0}>{configuredProviders > 0 ? <Check /> : <Circle />}<button type="button" onClick={() => onNavigate('models')}><span>配置模型服务</span><small>{configuredProviders > 0 ? `${configuredProviders} 个服务可用` : '至少配置一个供应商'}</small></button></li>
          <li><Circle /><button type="button" onClick={() => onNavigate('questions')}><span>准备问题空间</span><small>发布用于测量的买家问题</small></button></li>
          <li><Circle /><button type="button" onClick={() => onNavigate('audits')}><span>运行首次审计</span><small>采样、判定并生成指标</small></button></li>
        </ol>
      </aside>
    </div>
    <section className="overview-flow"><header><div><p className="eyebrow">MEASUREMENT FLOW</p><h3>从品牌配置到证据报告</h3></div><span>6 个阶段</span></header><div>{workflow.map((step, index) => <button type="button" key={step} onClick={() => onNavigate(index < 2 ? 'projects' : index === 2 ? 'questions' : 'audits')}><span>{String(index + 1).padStart(2, '0')}</span><strong>{step}</strong>{index < workflow.length - 1 && <ArrowRight />}</button>)}</div></section>
  </section>
}
