import { useQuery } from '@tanstack/react-query'

type RobotsGroup = { agents: string[]; rules: Array<{ allow: boolean; pattern: string }> }
type RobotsEvidence = {
  host: string
  requestedUrl: string
  httpStatus: number
  contentHash: string
  parserVersion: string
  rules: { groups?: RobotsGroup[]; sitemaps?: string[] }
  content: string
  fetchedAt: string
}
type SitemapEvidence = {
  url: string
  parentUrl?: string
  httpStatus: number
  contentHash: string
  urlCount: number
  parserVersion: string
  errorCode?: string
  fetchedAt: string
}

async function fetchRobots(runId: string): Promise<{ items: RobotsEvidence[] }> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}/robots`)
  if (!response.ok) throw new Error('robots.txt 证据加载失败')
  return response.json() as Promise<{ items: RobotsEvidence[] }>
}

async function fetchSitemaps(runId: string): Promise<{ items: SitemapEvidence[] }> {
  const response = await fetch(`/api/v1/crawl-runs/${runId}/sitemaps`)
  if (!response.ok) throw new Error('Sitemap 证据加载失败')
  return response.json() as Promise<{ items: SitemapEvidence[] }>
}

export function EvidenceDialog({ runId, onClose }: { runId: string; onClose: () => void }) {
  const robots = useQuery({ queryKey: ['crawl-robots', runId], queryFn: () => fetchRobots(runId) })
  const sitemaps = useQuery({ queryKey: ['crawl-sitemaps', runId], queryFn: () => fetchSitemaps(runId) })

  return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
    <section className="evidence-dialog" aria-labelledby="evidence-title" onMouseDown={(event) => event.stopPropagation()}>
      <div className="profile-header">
        <div><p className="eyebrow">CRAWL EVIDENCE</p><h2 id="evidence-title">Robots 与 Sitemap</h2><p>{runId}</p></div>
        <button type="button" aria-label="关闭" onClick={onClose}>×</button>
      </div>
      <div className="evidence-block">
        <h3>robots.txt</h3>
        {robots.isLoading && <p>正在加载…</p>}
        {robots.error && <p className="error" role="alert">{robots.error.message}</p>}
        {robots.data?.items.length === 0 && <p className="empty-state">这次抓取没有保存 robots.txt 快照。</p>}
        {robots.data?.items.map((item) => <article className="robots-result" key={`${item.host}-${item.fetchedAt}`}>
          <header>
            <strong>{item.host}</strong>
            <span data-status={item.httpStatus}>{item.httpStatus === 0 ? 'unreachable' : `HTTP ${item.httpStatus}`}</span>
            <small>{item.parserVersion} · {new Date(item.fetchedAt).toLocaleString()}</small>
          </header>
          {item.rules.groups?.length ? <div className="robots-groups">{item.rules.groups.map((group, groupIndex) => <div key={groupIndex}>
            <code>User-agent: {group.agents.join(', ')}</code>
            {group.rules.length === 0 && <span className="robots-rule">（无规则）</span>}
            {group.rules.map((rule, ruleIndex) => <span className={`robots-rule ${rule.allow ? 'allow' : 'disallow'}`} key={ruleIndex}>{rule.allow ? 'Allow' : 'Disallow'}: {rule.pattern}</span>)}
          </div>)}</div> : <p className="empty-state">未解析出任何分组或规则。</p>}
          {Boolean(item.rules.sitemaps?.length) && <p className="sitemap-declared">Sitemap: {item.rules.sitemaps!.join(' · ')}</p>}
          {item.content && <details><summary>原始 robots.txt（{item.content.length} 字符）</summary><pre>{item.content}</pre></details>}
          {item.contentHash && <p className="hash">sha256 {item.contentHash}</p>}
        </article>)}
      </div>
      <div className="evidence-block">
        <h3>Sitemap</h3>
        {sitemaps.isLoading && <p>正在加载…</p>}
        {sitemaps.error && <p className="error" role="alert">{sitemaps.error.message}</p>}
        {sitemaps.data?.items.length === 0 && <p className="empty-state">没有发现或解析任何 Sitemap。</p>}
        {Boolean(sitemaps.data?.items.length) && <div className="sitemap-table">
          <div className="sitemap-row sitemap-head"><span>URL</span><span>状态</span><span>URL 数</span><span>解析器</span></div>
          {sitemaps.data!.items.map((item) => <div className="sitemap-row" key={item.url}>
            <span title={item.url}>{item.url}{item.parentUrl && <small>← {item.parentUrl}</small>}</span>
            <span data-error={Boolean(item.errorCode)}>{item.errorCode || item.httpStatus}</span>
            <span>{item.urlCount}</span>
            <span>{item.parserVersion}</span>
          </div>)}
        </div>}
      </div>
    </section>
  </div>
}
