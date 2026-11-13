import { gunzipSync } from 'node:zlib'
import * as cheerio from 'cheerio'

export type ParsedSitemap = { kind: 'index' | 'urlset'; locations: string[]; sitemaps: string[]; parserVersion: string }

export const SITEMAP_PARSER_VERSION = 'sitemap-v1'

function decode(bytes: Uint8Array): string {
  const compressed = bytes.length >= 2 && bytes[0] === 0x1f && bytes[1] === 0x8b
  const plain = compressed ? gunzipSync(bytes) : Buffer.from(bytes)
  return plain.toString('utf8').replace(/^\uFEFF/, '').trim()
}

/** Parses a sitemap or sitemap index; gzip payloads (including .xml.gz) are decompressed. */
export function parseSitemap(input: Uint8Array): ParsedSitemap {
  const xml = decode(input)
  if (!xml) throw new Error('EMPTY_SITEMAP')
  const $ = cheerio.load(xml, { xmlMode: true })
  const sitemaps = $('sitemapindex sitemap loc').map((_, element) => $(element).text().trim()).get().filter(Boolean)
  const locations = $('urlset url loc').map((_, element) => $(element).text().trim()).get().filter(Boolean)
  if (sitemaps.length > 0) return { kind: 'index', locations, sitemaps, parserVersion: SITEMAP_PARSER_VERSION }
  if (locations.length > 0 || $('urlset').length > 0) return { kind: 'urlset', locations, sitemaps, parserVersion: SITEMAP_PARSER_VERSION }
  throw new Error('UNRECOGNIZED_SITEMAP')
}
