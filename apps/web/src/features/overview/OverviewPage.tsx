import { Bot, ChevronRight, FolderKanban, Globe2, ShieldCheck } from 'lucide-react'

type Props = { activeProjects: number; websiteCount: number; configuredProviders: number; onCreateProject: () => void }

export function OverviewPage({ activeProjects, websiteCount, configuredProviders, onCreateProject }: Props) {
  return <>
    <section className="dashboard-intro"><div><p className="eyebrow">OVERVIEW</p><h2>早上好，开始今天的 GEO 分析</h2><p>集中管理品牌资产、网站抓取、模型测量与证据报告。</p></div><button className="primary-button" type="button" onClick={onCreateProject}>创建项目</button></section>
    <section className="metric-grid" aria-label="工作台概览">
      <article><FolderKanban /><div><strong>{activeProjects}</strong><span>活跃项目</span></div></article>
      <article><Globe2 /><div><strong>{websiteCount}</strong><span>已接入网站</span></div></article>
      <article><Bot /><div><strong>{configuredProviders}/2</strong><span>模型服务</span></div></article>
      <article><ShieldCheck /><div><strong>P0</strong><span>当前基线</span></div></article>
    </section>
    <section className="pipeline-card overview-pipeline"><div className="section-title"><div><p className="eyebrow">DELIVERY PIPELINE</p><h2>GEO Audit 工作流</h2></div><span>6 个阶段</span></div><div className="pipeline-steps">{['项目配置', '网站抓取', '问题空间', '模型采样', 'Answer Judge', '指标报告'].map((step, index) => <div key={step}><span>{index + 1}</span><strong>{step}</strong>{index < 5 && <ChevronRight />}</div>)}</div></section>
  </>
}
