import { describe, expect, it } from 'vitest'
import { evaluateRobots, isAllowedByRobots, parseRobots } from './robots.js'

describe('parseRobots', () => {
  it('groups agents, rules, comments and sitemap directives', () => {
    const file = parseRobots(`
# global comment
User-agent: *
Disallow: /private/
Allow: /private/public/
Sitemap: https://example.com/sitemap.xml

User-agent: GEO-Intelligence-Crawler
User-agent: OtherBot
Disallow: /bot-only
`)
    expect(file.groups).toHaveLength(2)
    expect(file.groups[0]!.agents).toEqual(['*'])
    expect(file.groups[0]!.rules).toEqual([
      { allow: false, pattern: '/private/' },
      { allow: true, pattern: '/private/public/' },
    ])
    expect(file.groups[1]!.agents).toEqual(['geo-intelligence-crawler', 'otherbot'])
    expect(file.sitemaps).toEqual(['https://example.com/sitemap.xml'])
  })
})

describe('evaluateRobots', () => {
  const file = parseRobots(`
User-agent: *
Disallow: /private/
Allow: /private/public/
Disallow: /*.pdf$
Disallow:
`)
  it('uses the longest matching rule and lets Allow win', () => {
    expect(evaluateRobots(file, '/private/page').allowed).toBe(false)
    expect(evaluateRobots(file, '/private/public/page').allowed).toBe(true)
    expect(evaluateRobots(file, '/public/page').allowed).toBe(true)
  })
  it('supports wildcard and end-anchored patterns', () => {
    expect(evaluateRobots(file, '/docs/manual.pdf').allowed).toBe(false)
    expect(evaluateRobots(file, '/docs/manual.pdf?download=1').allowed).toBe(true)
    expect(evaluateRobots(file, '/docs/manual.pdfx').allowed).toBe(true)
  })
  it('applies an agent-specific group over the wildcard group', () => {
    const specific = parseRobots(`User-agent: *
Disallow: /
User-agent: Geo-Intelligence-Crawler
Disallow: /admin/
`)
    expect(evaluateRobots(specific, '/home', 'GEO-Intelligence-Crawler/1.0').allowed).toBe(true)
    expect(evaluateRobots(specific, '/admin/users', 'GEO-Intelligence-Crawler/1.0').allowed).toBe(false)
    expect(evaluateRobots(specific, '/home', 'Googlebot/2.1').allowed).toBe(false)
  })
})

describe('isAllowedByRobots', () => {
  it('matches the path and query of the URL', () => {
    const file = parseRobots('User-agent: *\nDisallow: /search?q=\n')
    expect(isAllowedByRobots(file, 'https://example.com/search?q=geo')).toBe(false)
    expect(isAllowedByRobots(file, 'https://example.com/search')).toBe(true)
  })
})
