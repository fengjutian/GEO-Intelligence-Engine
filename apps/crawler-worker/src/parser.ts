import * as cheerio from 'cheerio'

export type ParsedPage = { title: string; language?: string; canonical?: string; headings: Array<{ level: number; text: string }>; text: string; links: string[] }

export function parseHTML(html: string, baseUrl: string): ParsedPage {
  const $ = cheerio.load(html)
  $('script,style,noscript,template').remove()
  const headings = $('h1,h2,h3,h4,h5,h6').map((_, el) => ({ level: Number(el.tagName.slice(1)), text: $(el).text().trim() })).get()
  const links = $('a[href]').map((_, el) => { try { return new URL($(el).attr('href')!, baseUrl).href } catch { return null } }).get().filter((v): v is string => Boolean(v))
  return { title: $('title').first().text().trim(), language: $('html').attr('lang'), canonical: $('link[rel="canonical"]').attr('href'), headings, text: $('body').text().replace(/\s+/g, ' ').trim(), links }
}
