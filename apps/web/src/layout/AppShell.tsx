import { BarChart3, Bot, ClipboardCheck, FolderKanban, Gauge, Globe2, LogOut, Network, Settings } from 'lucide-react'
import type { ReactNode } from 'react'

export type PageId = 'overview' | 'projects' | 'audits' | 'entities' | 'questions' | 'reviews' | 'models' | 'settings'

const navItems = [
  { id: 'overview' as const, label: '概览', icon: Gauge },
  { id: 'projects' as const, label: '项目空间', icon: FolderKanban },
  { id: 'audits' as const, label: '网站审计', icon: Globe2 },
  { id: 'entities' as const, label: '实体知识', icon: Network },
  { id: 'questions' as const, label: '问题空间', icon: BarChart3 },
  { id: 'reviews' as const, label: '复核队列', icon: ClipboardCheck },
  { id: 'models' as const, label: '模型服务', icon: Bot },
  { id: 'settings' as const, label: '成员设置', icon: Settings },
]

const titles: Record<PageId, string> = { overview: '工作台', projects: '项目空间', audits: '网站审计', entities: '实体知识', questions: '问题空间', reviews: '复核队列', models: '模型服务', settings: '成员设置' }

type Props = {
  activePage: PageId
  onNavigate: (page: PageId) => void
  apiVersion?: string
  baseline?: string
  connected: boolean
  error: boolean
  onLogout?: () => void
  children: ReactNode
}

export function AppShell({ activePage, onNavigate, apiVersion, baseline, connected, error, onLogout, children }: Props) {
  return <main className="app-shell">
    <aside className="app-sidebar">
      <div className="brand-mark"><span>G</span><div><strong>GEO</strong><small>Intelligence</small></div></div>
      <nav aria-label="主导航">
        {navItems.map(({ id, label, icon: Icon }) => <button key={id} className={activePage === id ? 'active' : ''} onClick={() => onNavigate(id)}><Icon />{label}</button>)}
      </nav>
      <div className="sidebar-footer"><Settings /><span>系统设置</span><small>{baseline ?? 'P0-V1.0'}</small>{onLogout && <button type="button" className="logout-button" onClick={onLogout}><LogOut />退出登录</button>}</div>
    </aside>
    <div className="app-page">
      <header className="app-topbar"><div><p>GEO Intelligence Engine</p><h1>{titles[activePage]}</h1></div><div className="status" data-state={connected ? 'ready' : 'loading'}><span />{connected ? `服务正常 · API ${apiVersion}` : error ? 'API 连接异常' : '正在连接'}</div></header>
      <div className="app-content">{children}</div>
    </div>
  </main>
}
