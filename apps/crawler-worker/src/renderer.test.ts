import { afterEach, describe, expect, it } from 'vitest'
import { isPrivateHost, shouldRender } from './renderer.js'

const original = process.env.GEO_CRAWLER_RENDER_JS

afterEach(() => {
  if (original === undefined) delete process.env.GEO_CRAWLER_RENDER_JS
  else process.env.GEO_CRAWLER_RENDER_JS = original
})

describe('shouldRender', () => {
  it('renders only thin pages by default', () => {
    delete process.env.GEO_CRAWLER_RENDER_JS
    expect(shouldRender(0)).toBe(true)
    expect(shouldRender(199)).toBe(true)
    expect(shouldRender(200)).toBe(false)
  })

  it('can be disabled through the environment', () => {
    for (const value of ['0', 'false', 'off', 'no']) {
      process.env.GEO_CRAWLER_RENDER_JS = value
      expect(shouldRender(0)).toBe(false)
    }
    process.env.GEO_CRAWLER_RENDER_JS = '1'
    expect(shouldRender(0)).toBe(true)
  })
})

describe('isPrivateHost', () => {
  it('blocks loopback, link-local and RFC1918 targets', () => {
    for (const host of ['localhost', '127.0.0.1', '10.1.2.3', '172.16.0.10', '172.31.255.1', '192.168.1.5', '169.254.169.254', '::1', '[::1]', 'svc.internal', 'printer.local']) {
      expect(isPrivateHost(host), host).toBe(true)
    }
  })

  it('allows public hosts and neighbouring private ranges', () => {
    for (const host of ['www.feishu.cn', 'cdn.example.com', '172.15.0.1', '172.32.0.1', '11.0.0.1', '8.8.8.8']) {
      expect(isPrivateHost(host), host).toBe(false)
    }
  })
})
