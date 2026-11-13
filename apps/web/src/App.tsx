import { Activity, Database, Radar, ShieldCheck } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'

type Version = { apiVersion: string; baseline: string }

async function fetchVersion(): Promise<Version> {
  const response = await fetch('/api/v1/version')
  if (!response.ok) throw new Error('API unavailable')
  return response.json() as Promise<Version>
}

const modules = [
  { title: 'Discovery', text: '网站发现、抓取与技术审计', icon: Radar },
  { title: 'Knowledge', text: '品牌实体、关系与一致性', icon: Database },
  { title: 'Measurement', text: '多模型测量与可信指标', icon: Activity },
  { title: 'Governance', text: '租户隔离、审批与审计', icon: ShieldCheck },
]

export function App() {
  const version = useQuery({ queryKey: ['version'], queryFn: fetchVersion })
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
    </div>
  </main>
}
