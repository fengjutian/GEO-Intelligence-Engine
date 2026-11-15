import { chromium, type Browser } from 'playwright'

// Thin pages are usually client-rendered shells: the static HTML carries almost
// no text, while the rendered DOM carries the real content. Rendering is on by
// default and only kicks in below the text threshold; set
// GEO_CRAWLER_RENDER_JS=0 to keep the crawler purely static.
export const RENDER_TEXT_THRESHOLD = 200

let browserPromise: Promise<Browser | null> | null = null
let rendererDisabled = false

export function renderEnabled(): boolean {
  const value = (process.env.GEO_CRAWLER_RENDER_JS ?? '').trim().toLowerCase()
  return !['0', 'false', 'off', 'no'].includes(value)
}

// shouldRender decides whether a page needs the browser fallback.
export function shouldRender(staticTextLength: number, threshold = RENDER_TEXT_THRESHOLD): boolean {
  return renderEnabled() && staticTextLength < threshold
}

// isPrivateHost blocks loopback and RFC1918/Link-local targets so the renderer
// cannot be used to reach internal services a plain fetch would reject.
export function isPrivateHost(hostname: string): boolean {
  const host = hostname.trim().toLowerCase().replace(/^\[|\]$/g, '')
  if (host === '' || host === 'localhost' || host === '::1' || host === '0.0.0.0') return true
  if (host.endsWith('.local') || host.endsWith('.internal') || host.endsWith('.localhost')) return true
  const ipv4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (ipv4) {
    const [a, b] = [Number(ipv4[1]), Number(ipv4[2])]
    if (a === 10 || a === 127 || a === 0) return true
    if (a === 169 && b === 254) return true
    if (a === 172 && b >= 16 && b <= 31) return true
    if (a === 192 && b === 168) return true
  }
  return false
}

async function launchBrowser(): Promise<Browser | null> {
  // Prefer a system browser: the bundled download is not always present.
  for (const options of [{ channel: 'chrome' as const }, { channel: 'msedge' as const }, {}]) {
    try {
      return await chromium.launch({ headless: true, ...options })
    } catch {
      continue
    }
  }
  return null
}

async function browser(): Promise<Browser | null> {
  if (rendererDisabled) return null
  if (!browserPromise) {
    browserPromise = launchBrowser().then((instance) => {
      if (!instance) rendererDisabled = true
      return instance
    })
  }
  return browserPromise
}

// renderHtml returns the post-JavaScript DOM, or null when rendering failed.
export async function renderHtml(url: string, timeoutMs = 20000): Promise<string | null> {
  const instance = await browser()
  if (!instance) return null
  const context = await instance.newContext({
    userAgent: 'Mozilla/5.0 (compatible; geo-crawler/1.0; +rendered)',
    viewport: { width: 1366, height: 900 },
    javaScriptEnabled: true,
  })
  try {
    await context.route('**/*', (route) => {
      const request = route.request()
      let target: URL
      try {
        target = new URL(request.url())
      } catch {
        return route.abort('blockedbyclient')
      }
      if (target.protocol !== 'http:' && target.protocol !== 'https:') return route.abort('blockedbyclient')
      if (isPrivateHost(target.hostname)) return route.abort('blockedbyclient')
      const type = request.resourceType()
      if (type === 'image' || type === 'media' || type === 'font') return route.abort('blockedbyclient')
      return route.continue()
    })
    const page = await context.newPage()
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: timeoutMs })
    await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => undefined)
    return await page.content()
  } catch {
    return null
  } finally {
    await context.close().catch(() => undefined)
  }
}

export async function closeRenderer(): Promise<void> {
  if (!browserPromise) return
  const instance = await browserPromise.catch(() => null)
  browserPromise = null
  rendererDisabled = true
  await instance?.close().catch(() => undefined)
}
