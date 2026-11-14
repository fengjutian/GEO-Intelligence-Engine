import * as cheerio from 'cheerio'

export type StructuredDataItem = Record<string, unknown>
export type FaqItem = { question: string; answer: string }
export type TermSignal = { term: string; count: number; defined: boolean }
export type ParsedPage = { title: string; language?: string; canonical?: string; robots: string[]; headings: Array<{ level: number; text: string }>; text: string; links: string[]; structuredData: StructuredDataItem[]; author?: string; publishedAt?: string; faq: FaqItem[]; terms: TermSignal[] }

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

function textValue(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (value && typeof value === 'object') {
    const record = value as Record<string, unknown>
    return textValue(record.name) || textValue(record.text) || textValue(record['@value'])
  }
  return ''
}

function extractFaq(items: StructuredDataItem[]): FaqItem[] {
  const result: FaqItem[] = []
  const visit = (value: unknown) => {
    if (Array.isArray(value)) { value.forEach(visit); return }
    if (!value || typeof value !== 'object') return
    const node = value as Record<string, unknown>
    const type = Array.isArray(node['@type']) ? node['@type'] : [node['@type']]
    if (type.some((entry) => String(entry).toLowerCase() === 'faqpage')) {
      const questions = Array.isArray(node.mainEntity) ? node.mainEntity : [node.mainEntity]
      for (const question of questions) {
        if (!question || typeof question !== 'object') continue
        const record = question as Record<string, unknown>
        const answerNode = record.acceptedAnswer ?? record.suggestedAnswer
        const answer = answerNode && typeof answerNode === 'object' ? textValue((answerNode as Record<string, unknown>).text) : textValue(answerNode)
        result.push({ question: textValue(record.name), answer })
      }
    }
    if (node['@graph']) visit(node['@graph'])
  }
  items.forEach(visit)
  return result.slice(0, 50)
}

function firstContent($: cheerio.CheerioAPI, selectors: string[]): string {
  for (const selector of selectors) {
    const value = $(selector).first().attr('content') ?? $(selector).first().text()
    if (value?.trim()) return value.trim()
  }
  return ''
}

const TERM_STOP_WORDS = new Set(['THE', 'AND', 'FOR', 'WITH', 'THIS', 'THAT', 'FROM', 'YOUR', 'FAQ', 'API', 'URL'])

export function extractTerms(text: string): TermSignal[] {
  const counts = new Map<string, number>()
  for (const match of text.matchAll(/\b[A-Z][A-Z0-9]{1,5}\b/g)) {
    const term = match[0]
    if (TERM_STOP_WORDS.has(term)) continue
    counts.set(term, (counts.get(term) ?? 0) + 1)
  }
  const signals: TermSignal[] = []
  for (const [term, count] of counts) {
    if (count < 3) continue
    const first = text.indexOf(term)
    const context = text.slice(Math.max(0, first - 80), first + term.length + 60)
    const defined = /[（(][^）)]{2,}[）)]/.test(context)
    signals.push({ term, count, defined })
  }
  return signals.sort((a, b) => b.count - a.count || a.term.localeCompare(b.term)).slice(0, 10)
}

export function parseHTML(html: string, baseUrl: string): ParsedPage {
  const structuredData = parseStructuredData(html)
  const $ = cheerio.load(html)
  const author = firstContent($, ['meta[name="author" i]', '[rel="author"]'])
  const publishedAt = firstContent($, ['meta[property="article:published_time" i]', 'meta[name="date" i]', 'meta[name="publish-date" i]', 'time[datetime]'])
  const faq = extractFaq(structuredData)
  $('script,style,noscript,template').remove()
  const bodyText = $('body').text().replace(/\s+/g, ' ').trim()
  const terms = extractTerms(bodyText)
  const headings = $('h1,h2,h3,h4,h5,h6').map((_, el) => ({ level: Number(el.tagName.slice(1)), text: $(el).text().trim() })).get()
  const links = $('a[href]').map((_, el) => { try { return new URL($(el).attr('href')!, baseUrl).href } catch { return null } }).get().filter((v): v is string => Boolean(v))
  const robots = $('meta[name="robots" i],meta[name="googlebot" i]').map((_, el) => $(el).attr('content')?.toLowerCase().split(',').map((value) => value.trim()) ?? []).get()
  return { title: $('title').first().text().trim(), language: $('html').attr('lang'), canonical: $('link[rel="canonical"]').attr('href'), robots, headings, text: bodyText, links, structuredData, author, publishedAt, faq, terms }
}
