import { createServer } from 'node:http'
import { createHash, randomUUID } from 'node:crypto'
import { fetch, Headers, type RequestInit } from 'undici'
import { parseHTML } from './parser.js'
import { CRAWLER_USER_AGENT, emptyRobots, isAllowedByRobots, parseRobots, type RobotsFile } from './robots.js'
import { parseSitemap, SITEMAP_PARSER_VERSION } from './sitemap.js'
import { assertPublicResolvedUrl } from './url-policy.js'

const port = Number.parseInt(process.env.PORT ?? '8082', 10)
const apiBaseUrl = process.env.GEO_API_BASE_URL ?? 'http://127.0.0.1:8080'
const workerToken = process.env.GEO_INTERNAL_WORKER_TOKEN ?? 'local-worker-token-change-me'
const workerId = process.env.GEO_WORKER_ID ?? `crawler-${randomUUID()}`
const robotsTTLMS = 10 * 60_000
const robotsErrorTTLMS = 60_000
const maxSitemapBytes = 5 << 20
const maxSitemapDocuments = 10
const maxSitemapURLs = 500
let stopping = false
let polling = false
const server = createServer((request, response) => {
  if (request.url === '/healthz') {
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ status: 'ok', service: 'crawler-worker' }))
    return
  }

  response.writeHead(404, { 'content-type': 'application/problem+json' })
  response.end(JSON.stringify({ title: 'Not Found', status: 404 }))
})

server.listen(port, '0.0.0.0', () => {
  process.stdout.write(JSON.stringify({ level: 'info', message: 'crawler worker started', port }) + '\n')
})

type Claim = { runId: string; crawlUrlId: string; websiteId: string; url: string; depth: number; maxBytes: number }
type RobotsCacheEntry = { file: RobotsFile; status: number; contentHash: string; content: string; fetchedAt: number; persisted: boolean }
type SitemapDocument = { url: string; parentUrl?: string; httpStatus: number; contentHash: string; urlCount: number; parserVersion: string; errorCode?: string }

const robotsCache = new Map<string, RobotsCacheEntry>()
const sitemapRuns = new Set<string>()

class RobotsDisallowedError extends Error {
  constructor(readonly url: string) {
    super('ROBOTS_DISALLOWED')
    this.name = 'RobotsDisallowedError'
  }
}

async function api(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  headers.set('authorization', `Bearer ${workerToken}`)
  headers.set('x-worker-id', workerId)
  return fetch(`${apiBaseUrl}${path}`, { ...init, headers })
}

function sha256(bytes: Uint8Array): string {
  return createHash('sha256').update(bytes).digest('hex')
}

function sameHost(first: string, second: string): boolean {
  try {
    const a = new URL(first).hostname.toLowerCase().replace(/^www\./, '')
    const b = new URL(second).hostname.toLowerCase().replace(/^www\./, '')
    return a === b
  } catch {
    return false
  }
}

type FetchOptions = { accept: string; runId?: string; websiteId?: string; checkRobots?: boolean; contentTypes?: string[] }

async function fetchWithSafeRedirects(input: string, maxBytes: number, options: FetchOptions) {
  let current = input
  for (let redirects = 0; redirects <= 5; redirects += 1) {
    await assertPublicResolvedUrl(current)
    if (options.checkRobots && options.runId && options.websiteId) {
      const robots = await loadRobots(current, options.runId, options.websiteId)
      if (!isAllowedByRobots(robots.file, current)) throw new RobotsDisallowedError(current)
    }
    const response = await fetch(current, {
      redirect: 'manual',
      headers: { 'user-agent': CRAWLER_USER_AGENT, accept: options.accept },
      signal: AbortSignal.timeout(20_000),
    })
    if ([301, 302, 303, 307, 308].includes(response.status)) {
      const location = response.headers.get('location')
      if (!location) throw new Error('REDIRECT_WITHOUT_LOCATION')
      current = new URL(location, current).href
      continue
    }
    const contentLength = Number(response.headers.get('content-length') ?? '0')
    if (contentLength > maxBytes) throw new Error('RESPONSE_TOO_LARGE')
    const bytes = new Uint8Array(await response.arrayBuffer())
    if (bytes.byteLength > maxBytes) throw new Error('RESPONSE_TOO_LARGE')
    const contentType = (response.headers.get('content-type') ?? '').toLowerCase()
    if (options.contentTypes && !options.contentTypes.some((type) => contentType.includes(type))) {
      throw new Error('UNSUPPORTED_CONTENT_TYPE')
    }
    return { response, bytes, finalUrl: current }
  }
  throw new Error('TOO_MANY_REDIRECTS')
}

async function persistRobots(runId: string, entry: RobotsCacheEntry, origin: string) {
  if (entry.persisted) return
  try {
    const result = await api(`/internal/crawler/v1/runs/${runId}/robots`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        host: new URL(origin).hostname,
        requestedUrl: `${origin}/robots.txt`,
        httpStatus: entry.status,
        contentHash: entry.contentHash,
        parserVersion: entry.file.parserVersion,
        rules: { groups: entry.file.groups, sitemaps: entry.file.sitemaps },
        content: entry.content.slice(0, 512 * 1024),
      }),
    })
    if (result.ok) entry.persisted = true
  } catch (error) {
    process.stderr.write(JSON.stringify({ level: 'warn', message: 'robots snapshot failed', error: error instanceof Error ? error.message : String(error) }) + '\n')
  }
}

async function loadRobots(pageUrl: string, runId: string, websiteId: string): Promise<RobotsCacheEntry> {
  const origin = new URL(pageUrl).origin
  const cacheKey = `${websiteId}|${origin}`
  const cached = robotsCache.get(cacheKey)
  const ttl = cached && cached.status >= 200 && cached.status < 300 ? robotsTTLMS : robotsErrorTTLMS
  if (cached && Date.now() - cached.fetchedAt < ttl) {
    await persistRobots(runId, cached, origin)
    return cached
  }
  const requestedUrl = `${origin}/robots.txt`
  let entry: RobotsCacheEntry
  try {
    const { response, bytes } = await fetchWithSafeRedirects(requestedUrl, 512 * 1024, { accept: 'text/plain,text/*;q=0.9,*/*;q=0.1' })
    const content = new TextDecoder().decode(bytes)
    // 4xx means "no robots.txt" (allow). 5xx or network failure is treated conservatively.
    const file = response.status >= 500
      ? parseRobots('User-agent: *\nDisallow: /\n')
      : response.status >= 400 ? emptyRobots() : parseRobots(content)
    entry = {
      file,
      status: response.status,
      contentHash: sha256(bytes),
      content,
      fetchedAt: Date.now(),
      persisted: false,
    }
  } catch (error) {
    process.stderr.write(JSON.stringify({ level: 'warn', message: 'robots fetch failed', url: requestedUrl, error: error instanceof Error ? error.message : String(error) }) + '\n')
    entry = { file: parseRobots('User-agent: *\nDisallow: /\n'), status: 0, contentHash: sha256(new Uint8Array()), content: '', fetchedAt: Date.now(), persisted: false }
  }
  robotsCache.set(cacheKey, entry)
  await persistRobots(runId, entry, origin)
  return entry
}

async function discoverSitemaps(claim: Claim) {
  if (claim.depth !== 0 || sitemapRuns.has(claim.runId)) return
  sitemapRuns.add(claim.runId)
  const origin = new URL(claim.url).origin
  const robots = await loadRobots(claim.url, claim.runId, claim.websiteId)
  const queue: Array<{ url: string; parentUrl?: string; depth: number }> = []
  for (const raw of robots.file.sitemaps) {
    try {
      const absolute = new URL(raw, origin).href
      if (sameHost(absolute, origin)) queue.push({ url: absolute, depth: 0 })
    } catch {
      // Ignore malformed Sitemap directives.
    }
  }
  if (queue.length === 0) queue.push({ url: `${origin}/sitemap.xml`, depth: 0 })
  const documents: SitemapDocument[] = []
  const urls: string[] = []
  const seen = new Set<string>()
  while (queue.length > 0 && documents.length < maxSitemapDocuments) {
    const item = queue.shift()!
    if (seen.has(item.url)) continue
    seen.add(item.url)
    try {
      const { response, bytes } = await fetchWithSafeRedirects(item.url, maxSitemapBytes, { accept: 'application/xml,text/xml,text/plain,*/*;q=0.1' })
      const contentHash = sha256(bytes)
      if (response.status < 200 || response.status >= 300) {
        documents.push({ url: item.url, parentUrl: item.parentUrl, httpStatus: response.status, contentHash, urlCount: 0, parserVersion: SITEMAP_PARSER_VERSION, errorCode: `HTTP_${response.status}` })
        continue
      }
      const parsed = parseSitemap(bytes)
      documents.push({
        url: item.url,
        parentUrl: item.parentUrl,
        httpStatus: response.status,
        contentHash,
        urlCount: parsed.locations.length + parsed.sitemaps.length,
        parserVersion: parsed.parserVersion,
      })
      if (parsed.kind === 'index' && item.depth < 3) {
        for (const child of parsed.sitemaps) {
          try {
            const absolute = new URL(child, item.url).href
            if (sameHost(absolute, origin)) queue.push({ url: absolute, parentUrl: item.url, depth: item.depth + 1 })
          } catch {
            // Ignore malformed child sitemap URLs.
          }
        }
      } else {
        for (const location of parsed.locations) {
          try {
            const absolute = new URL(location, item.url).href
            if (sameHost(absolute, origin)) urls.push(absolute)
          } catch {
            // Ignore malformed URL entries.
          }
        }
      }
    } catch (error) {
      documents.push({
        url: item.url,
        parentUrl: item.parentUrl,
        httpStatus: 0,
        contentHash: '',
        urlCount: 0,
        parserVersion: SITEMAP_PARSER_VERSION,
        errorCode: (error instanceof Error ? error.message : 'SITEMAP_FAILED').slice(0, 64),
      })
    }
  }
  const result = await api(`/internal/crawler/v1/runs/${claim.runId}/sitemaps`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ documents, urls: [...new Set(urls)].slice(0, maxSitemapURLs) }),
  })
  if (!result.ok) throw new Error(`SITEMAP_API_${result.status}`)
  process.stdout.write(JSON.stringify({ level: 'info', message: 'sitemap discovery completed', runId: claim.runId, documents: documents.length, urls: urls.length }) + '\n')
}

async function reportBlocked(claim: Claim, url: string) {
  const blocked = await api(`/internal/crawler/v1/runs/${claim.runId}/blocked`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ crawlUrlId: claim.crawlUrlId, reason: 'ROBOTS_DISALLOWED' }),
  }).catch(() => undefined)
  process.stdout.write(JSON.stringify({ level: 'info', message: 'crawl blocked by robots', runId: claim.runId, url, blocked: blocked?.ok ?? false }) + '\n')
}

async function processClaim(claim: Claim) {
  try {
    if (claim.depth === 0) {
      // Check the start URL before sitemap work so a disallowed entry point does not pay for it.
      const robots = await loadRobots(claim.url, claim.runId, claim.websiteId)
      if (!isAllowedByRobots(robots.file, claim.url)) {
        await reportBlocked(claim, claim.url)
        return
      }
      try {
        await discoverSitemaps(claim)
      } catch (error) {
        process.stderr.write(JSON.stringify({ level: 'warn', message: 'sitemap discovery failed', runId: claim.runId, error: error instanceof Error ? error.message : String(error) }) + '\n')
      }
    }
    const { response, bytes, finalUrl } = await fetchWithSafeRedirects(claim.url, claim.maxBytes, {
      accept: 'text/html,application/xhtml+xml',
      contentTypes: ['text/html', 'application/xhtml+xml'],
      runId: claim.runId,
      websiteId: claim.websiteId,
      checkRobots: true,
    })
    const html = new TextDecoder().decode(bytes)
    const parsed = parseHTML(html, finalUrl)
    const contentHash = sha256(bytes)
    let canonicalUrl: string | null = null
    try { if (parsed.canonical) canonicalUrl = new URL(parsed.canonical, finalUrl).href } catch { canonicalUrl = null }
    const links = [...new Set(parsed.links.filter((link) => link.startsWith('http://') || link.startsWith('https://')))].slice(0, 100)
    const completion = { crawlUrlId: claim.crawlUrlId, finalUrl, httpStatus: response.status, title: parsed.title, language: parsed.language ?? null, contentHash, parserVersion: 'cheerio-v1', canonicalUrl, links, noIndex: parsed.robots.includes('noindex'), textLength: parsed.text.length }
    const result = await api(`/internal/crawler/v1/runs/${claim.runId}/complete`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(completion) })
    if (!result.ok) throw new Error(`COMPLETE_API_${result.status}`)
    process.stdout.write(JSON.stringify({ level: 'info', message: 'crawl completed', runId: claim.runId, url: finalUrl, status: response.status }) + '\n')
  } catch (error) {
    if (error instanceof RobotsDisallowedError) {
      await reportBlocked(claim, error.url)
      return
    }
    const errorCode = error instanceof Error ? error.message.slice(0, 64) : 'FETCH_FAILED'
    await api(`/internal/crawler/v1/runs/${claim.runId}/fail`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ crawlUrlId: claim.crawlUrlId, errorCode }) }).catch(() => undefined)
    process.stderr.write(JSON.stringify({ level: 'error', message: 'crawl failed', runId: claim.runId, error: errorCode }) + '\n')
  }
}

async function poll() {
  if (polling || stopping) return
  polling = true
  try {
    const response = await api('/internal/crawler/v1/claim', { method: 'POST' })
    if (response.status === 204) return
    if (!response.ok) throw new Error(`CLAIM_API_${response.status}`)
    await processClaim(await response.json() as Claim)
  } catch (error) {
    process.stderr.write(JSON.stringify({ level: 'error', message: 'crawler poll failed', error: error instanceof Error ? error.message : String(error) }) + '\n')
  } finally {
    polling = false
  }
}

const pollTimer = setInterval(() => void poll(), 2_000)
void poll()

function shutdown(signal: string) {
  stopping = true
  clearInterval(pollTimer)
  process.stdout.write(JSON.stringify({ level: 'info', message: 'shutting down', signal }) + '\n')
  server.close((error) => {
    process.exitCode = error ? 1 : 0
  })
}

process.on('SIGINT', () => shutdown('SIGINT'))
process.on('SIGTERM', () => shutdown('SIGTERM'))
