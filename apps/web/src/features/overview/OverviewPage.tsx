import { Bot, ChevronRight, FolderKanban, Globe2, ShieldCheck } from 'lucide-react'
import { Button } from '../../components/ui/button'
import { Card } from '../../components/ui/card'

type Props = { activeProjects: number; websiteCount: number; configuredProviders: number; onCreateProject: () => void }

export function OverviewPage({ activeProjects, websiteCount, configuredProviders, onCreateProject }: Props) {
  return <>
    <section className="dashboard-intro"><div><p className="eyebrow">OVERVIEW</p><h2>早上好，开始今天的 GEO 分析</h2><p>集中管理品牌资产、网站抓取、模型测量与证据报告。</p></div><Button type="button" onClick={onCreateProject}>创建项目</Button></section>
    <section className="metric-grid" aria-label="工作台概览">
      <Card className="metric-card"><FolderKanban /><div><strong>{activeProjects}</strong><span>活跃项目</span></div></Card>
      <Card className="metric-card"><Globe2 /><div><strong>{websiteCount}</strong><span>已接入网站</span></div></Card>
      <Card className="metric-card"><Bot /><div><strong>{configuredProviders}/2</strong><span>模型服务</span></div></Card>
      <Card className="metric-card"><ShieldCheck /><div><strong>P0</strong><span>当前基线</span></div></Card>
    </section>
    <Card className="pipeline-card overview-pipeline"><div className="section-title"><div><p className="eyebrow">DELIVERY PIPELINE</p><h2>GEO Audit 工作流</h2></div><span>6 个阶段</span></div><div className="pipeline-steps">{['项目配置', '网站抓取', '问题空间', '模型采样', 'Answer Judge', '指标报告'].map((step, index) => <div key={step}><span>{index + 1}</span><strong>{step}</strong>{index < 5 && <ChevronRight />}</div>)}</div></Card>
  </>
}
