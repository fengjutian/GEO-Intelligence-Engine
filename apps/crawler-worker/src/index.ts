import { createServer } from 'node:http'
import { createHash, randomUUID } from 'node:crypto'
import { fetch } from 'undici'
import { parseHTML } from './parser.js'
import { assertPublicResolvedUrl } from './url-policy.js'

const port = Number.parseInt(process.env.PORT ?? '8082', 10)
const apiBaseUrl = process.env.GEO_API_BASE_URL ?? 'http://127.0.0.1:8080'
const workerToken = process.env.GEO_INTERNAL_WORKER_TOKEN ?? 'local-worker-token-change-me'
const workerId = process.env.GEO_WORKER_ID ?? `crawler-${randomUUID()}`
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

type Claim = { runId: string; crawlUrlId: string; websiteId: string; url: string; maxBytes: number }

async function api(path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(`${apiBaseUrl}${path}`, { ...init, headers: { authorization: `Bearer ${workerToken}`, 'x-worker-id': workerId, ...init.headers } }) as unknown as Response
}

async function fetchWithSafeRedirects(input: string, maxBytes: number) {
  let current = input
  for (let redirects = 0; redirects <= 5; redirects += 1) {
    await assertPublicResolvedUrl(current)
    const response = await fetch(current, { redirect: 'manual', headers: { 'user-agent': 'GEO-Intelligence-Crawler/1.0', accept: 'text/html,application/xhtml+xml' }, signal: AbortSignal.timeout(20_000) })
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
    const contentType = response.headers.get('content-type') ?? ''
    if (!contentType.includes('text/html') && !contentType.includes('application/xhtml+xml')) throw new Error('UNSUPPORTED_CONTENT_TYPE')
    return { response, bytes, finalUrl: current }
  }
  throw new Error('TOO_MANY_REDIRECTS')
}

async function processClaim(claim: Claim) {
  try {
    const { response, bytes, finalUrl } = await fetchWithSafeRedirects(claim.url, claim.maxBytes)
    const html = new TextDecoder().decode(bytes)
    const parsed = parseHTML(html, finalUrl)
    const contentHash = createHash('sha256').update(bytes).digest('hex')
    const completion = { crawlUrlId: claim.crawlUrlId, finalUrl, httpStatus: response.status, title: parsed.title, language: parsed.language ?? null, contentHash, parserVersion: 'cheerio-v1' }
    const result = await api(`/internal/crawler/v1/runs/${claim.runId}/complete`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(completion) })
    if (!result.ok) throw new Error(`COMPLETE_API_${result.status}`)
    process.stdout.write(JSON.stringify({ level: 'info', message: 'crawl completed', runId: claim.runId, url: finalUrl, status: response.status }) + '\n')
  } catch (error) {
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
