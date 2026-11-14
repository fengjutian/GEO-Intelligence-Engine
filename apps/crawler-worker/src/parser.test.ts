import { expect, it } from 'vitest'
import { parseHTML, parseStructuredData } from './parser.js'

it('extracts semantic page data', () => {
  const page = parseHTML('<html lang="zh-CN"><head><title>A</title><link rel="canonical" href="/a"><meta name="robots" content="noindex, follow"></head><body><h1>Title</h1><a href="/b">B</a></body></html>', 'https://example.com')
  expect(page.title).toBe('A'); expect(page.headings[0]).toEqual({ level: 1, text: 'Title' }); expect(page.links).toEqual(['https://example.com/b']); expect(page.robots).toEqual(['noindex', 'follow']); expect(page.structuredData).toEqual([])
})

it('extracts JSON-LD objects and arrays', () => {
  const html = `<script type="application/ld+json">{"@type":"Organization","name":"Acme"}</script><script type="application/ld+json">[{"@type":"Product","name":"Widget"}]</script>`
  expect(parseStructuredData(html)).toEqual([
    { '@type': 'Organization', name: 'Acme' },
    { '@type': 'Product', name: 'Widget' },
  ])
})

it('ignores malformed JSON-LD', () => {
  expect(parseStructuredData('<script type="application/ld+json">{oops</script>')).toEqual([])
})

it('extracts author, published date and FAQ answers', () => {
  const html = `<html><head><meta name="author" content="Acme Team"><meta property="article:published_time" content="2026-01-02T00:00:00Z"></head><body><h1>FAQ</h1><script type="application/ld+json">{"@type":"FAQPage","mainEntity":[{"@type":"Question","name":"What is GEO?","acceptedAnswer":{"@type":"Answer","text":"GEO is AI visibility analytics."}}]}</script></body></html>`
  const page = parseHTML(html, 'https://example.com/faq')
  expect(page.author).toBe('Acme Team')
  expect(page.publishedAt).toBe('2026-01-02T00:00:00Z')
  expect(page.faq).toEqual([{ question: 'What is GEO?', answer: 'GEO is AI visibility analytics.' }])
})

it('flags repeated undefined acronyms', () => {
  const page = parseHTML('<html><body>GEO tools improve GEO results. GEO matters.</body></html>', 'https://example.com')
  expect(page.terms).toEqual([{ term: 'GEO', count: 3, defined: false }])
})

it('recognizes a defined acronym near its first use', () => {
  const page = parseHTML('<html><body>Generative Engine Optimization (GEO) is useful. GEO improves GEO results.</body></html>', 'https://example.com')
  expect(page.terms[0]).toMatchObject({ term: 'GEO', count: 3, defined: true })
})
