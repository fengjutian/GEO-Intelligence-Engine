import { gzipSync } from 'node:zlib'
import { describe, expect, it } from 'vitest'
import { parseSitemap } from './sitemap.js'

const urlset = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc><lastmod>2026-01-01</lastmod></url>
  <url><loc> https://example.com/about </loc></url>
</urlset>`

const sitemapIndex = `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-1.xml</loc></sitemap>
  <sitemap><loc>https://example.com/sitemap-2.xml.gz</loc></sitemap>
</sitemapindex>`

describe('parseSitemap', () => {
  it('parses a urlset', () => {
    const parsed = parseSitemap(Buffer.from(urlset))
    expect(parsed.kind).toBe('urlset')
    expect(parsed.locations).toEqual(['https://example.com/', 'https://example.com/about'])
  })

  it('parses a sitemap index', () => {
    const parsed = parseSitemap(Buffer.from(sitemapIndex))
    expect(parsed.kind).toBe('index')
    expect(parsed.sitemaps).toHaveLength(2)
  })

  it('decompresses gzip payloads', () => {
    const parsed = parseSitemap(gzipSync(Buffer.from(urlset)))
    expect(parsed.kind).toBe('urlset')
    expect(parsed.locations).toHaveLength(2)
  })

  it('rejects unknown documents', () => {
    expect(() => parseSitemap(Buffer.from('<html><body>not a sitemap</body></html>'))).toThrow('UNRECOGNIZED_SITEMAP')
  })
})
