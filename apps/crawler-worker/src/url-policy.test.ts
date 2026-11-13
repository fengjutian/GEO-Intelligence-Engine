import { describe, expect, it } from 'vitest'
import { assertPublicHttpUrl } from './url-policy.js'

describe('assertPublicHttpUrl', () => {
  it('accepts public https', () => expect(assertPublicHttpUrl('https://example.com/a').hostname).toBe('example.com'))
  for (const value of ['http://127.0.0.1', 'http://10.0.0.1', 'http://169.254.169.254/latest', 'file:///etc/passwd', 'https://example.com:8443']) {
    it(`rejects ${value}`, () => expect(() => assertPublicHttpUrl(value)).toThrow())
  }
})
