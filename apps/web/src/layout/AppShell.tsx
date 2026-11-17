import { Activity, BarChart3, Bot, ClipboardCheck, FolderKanban, Gauge, Globe2, LogOut, Network, Settings } from 'lucide-react'
import type { ReactNode } from 'react'
import { Button } from '../components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '../components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'

export type PageId = 'overview' | 'projects' | 'audits' | 'entities' | 'questions' | 'reviews' | 'models' | 'monitoring' | 'settings'

const navGroups = [
  { label: '工作台', items: [{ id: 'overview' as const, label: '概览', icon: Gauge }] },
  { label: '品牌资产', items: [
    { id: 'projects' as const, label: '项目空间', icon: FolderKanban },
    { id: 'audits' as const, label: '网站审计', icon: Globe2 },
    { id: 'entities' as const, label: '实体知识', icon: Network },
    { id: 'questions' as const, label: '问题空间', icon: BarChart3 },
  ] },
  { label: '评估运营', items: [
    { id: 'reviews' as const, label: '复核队列', icon: ClipboardCheck },
    { id: 'monitoring' as const, label: '持续监测', icon: Activity },
  ] },
  { label: '系统', items: [
    { id: 'models' as const, label: '模型服务', icon: Bot },
    { id: 'settings' as const, label: '成员设置', icon: Settings },
  ] },
]

const titles: Record<PageId, string> = { overview: '工作台', projects: '项目空间', audits: '网站审计', entities: '实体知识', questions: '问题空间', reviews: '复核队列', monitoring: '持续监测', models: '模型服务', settings: '成员设置' }

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
  return <TooltipProvider><main className="app-shell">
    <aside className="app-sidebar">
      <div className="brand-mark"><span>G</span><div><strong>GEO</strong><small>Intelligence</small></div></div>
      <nav aria-label="主导航">
        {navGroups.map((group) => <div className="nav-group" key={group.label}><p>{group.label}</p>{group.items.map(({ id, label, icon: Icon }) => <Tooltip key={id}><TooltipTrigger asChild><Button variant="ghost" className={activePage === id ? 'active' : ''} onClick={() => onNavigate(id)}><Icon />{label}</Button></TooltipTrigger><TooltipContent side="right">{label}</TooltipContent></Tooltip>)}</div>)}
      </nav>
      <DropdownMenu><DropdownMenuTrigger asChild><Button variant="ghost" className="sidebar-footer" aria-label="打开账户菜单"><Settings /><span>系统设置</span><small>{baseline ?? 'P0-V1.0'}</small></Button></DropdownMenuTrigger><DropdownMenuContent side="top" align="start"><DropdownMenuLabel>GEO Intelligence</DropdownMenuLabel><DropdownMenuSeparator /><DropdownMenuItem onSelect={() => onNavigate('settings')}><Settings />成员与系统设置</DropdownMenuItem>{onLogout && <DropdownMenuItem onSelect={onLogout}><LogOut />退出登录</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu>
    </aside>
    <div className="app-page">
      <header className="app-topbar"><div><p>GEO Intelligence Engine</p><h1>{titles[activePage]}</h1></div><div className="status" data-state={connected ? 'ready' : 'loading'}><span />{connected ? `服务正常 · API ${apiVersion}` : error ? 'API 连接异常' : '正在连接'}</div></header>
      <div className="app-content">{children}</div>
    </div>
  </main></TooltipProvider>
}
