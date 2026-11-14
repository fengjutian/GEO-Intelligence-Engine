import * as cheerio from 'cheerio'

export type StructuredDataItem = Record<string, unknown>
export type ParsedPage = { title: string; language?: string; canonical?: string; robots: string[]; headings: Array<{ level: number; text: string }>; text: string; links: string[]; structuredData: StructuredDataItem[] }

export function parseStructuredData(html: string): StructuredDataItem[] {
  const $ = cheerio.load(html)
  const items: StructuredDataItem[] = []
  $('script[type="application/ld+json" i]').each((_, element) => {
    const raw = $(element).html()
    if (!raw) return
    try {
      const parsed = JSON.parse(raw) as unknown
      const values = Array.isArray(parsed) ? parsed : [parsed]
      for (const value of values) {
        if (value && typeof value === 'object') items.push(value as StructuredDataItem)
      }
    } catch {
      // Invalid JSON-LD is ignored; the technical audit can surface it later.
    }
  })
  return items.slice(0, 20)
}

export function parseHTML(html: string, baseUrl: string): ParsedPage {
  const structuredData = parseStructuredData(html)
  const $ = cheerio.load(html)
  $('script,style,noscript,template').remove()
  const headings = $('h1,h2,h3,h4,h5,h6').map((_, el) => ({ level: Number(el.tagName.slice(1)), text: $(el).text().trim() })).get()
  const links = $('a[href]').map((_, el) => { try { return new URL($(el).attr('href')!, baseUrl).href } catch { return null } }).get().filter((v): v is string => Boolean(v))
  const robots = $('meta[name="robots" i],meta[name="googlebot" i]').map((_, el) => $(el).attr('content')?.toLowerCase().split(',').map((value) => value.trim()) ?? []).get()
  return { title: $('title').first().text().trim(), language: $('html').attr('lang'), canonical: $('link[rel="canonical"]').attr('href'), robots, headings, text: $('body').text().replace(/\s+/g, ' ').trim(), links, structuredData }
}
