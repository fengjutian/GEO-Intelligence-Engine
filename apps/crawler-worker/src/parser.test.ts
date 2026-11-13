import { expect, it } from 'vitest'
import { parseHTML } from './parser.js'

it('extracts semantic page data', () => {
  const page = parseHTML('<html lang="zh-CN"><head><title>A</title><link rel="canonical" href="/a"><meta name="robots" content="noindex, follow"></head><body><h1>Title</h1><a href="/b">B</a></body></html>', 'https://example.com')
  expect(page.title).toBe('A'); expect(page.headings[0]).toEqual({ level: 1, text: 'Title' }); expect(page.links).toEqual(['https://example.com/b']); expect(page.robots).toEqual(['noindex', 'follow'])
})
