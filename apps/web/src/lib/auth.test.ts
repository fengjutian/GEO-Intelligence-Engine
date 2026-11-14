import { beforeEach, describe, expect, it, vi } from 'vitest'
import { webcrypto } from 'node:crypto'
import {
  buildAuthorizeUrl, challengeForVerifier, clearSession, completeLogin, ensureSession,
  isExpiringSoon, readTokens, refreshSession, setAuthConfig, writeTokens,
} from './auth'

if (!globalThis.crypto?.subtle) {
  Object.defineProperty(globalThis, 'crypto', { value: webcrypto })
}

const config = { mode: 'oidc', issuer: 'https://id.example.test/realms/geo', clientId: 'geo-web', scope: 'openid profile email' }

beforeEach(() => {
  sessionStorage.clear()
  setAuthConfig(config)
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('PKCE helpers', () => {
  it('computes the S256 challenge', async () => {
    expect(await challengeForVerifier('abc')).toBe('ungWv48Bz-pBQUDeXa4iI7ADYaOWF3qctBD_YfIAFa0')
  })

  it('builds an authorize URL with PKCE and state', () => {
    const url = new URL(buildAuthorizeUrl(config, 'challenge', 'state-1', 'http://localhost:5174/'))
    expect(url.pathname).toBe('/realms/geo/protocol/openid-connect/auth')
    expect(url.searchParams.get('client_id')).toBe('geo-web')
    expect(url.searchParams.get('response_type')).toBe('code')
    expect(url.searchParams.get('code_challenge_method')).toBe('S256')
    expect(url.searchParams.get('code_challenge')).toBe('challenge')
    expect(url.searchParams.get('state')).toBe('state-1')
    expect(url.searchParams.get('redirect_uri')).toBe('http://localhost:5174/')
  })
})

describe('session lifecycle', () => {
  it('exchanges a callback code and stores tokens', async () => {
    window.history.replaceState({}, '', '/?code=code-1&state=state-1')
    sessionStorage.setItem('geo.auth.pkce', JSON.stringify({ verifier: 'verifier-1', state: 'state-1', redirectUri: 'http://localhost:5174/' }))
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ access_token: 'access-1', id_token: 'id-1', refresh_token: 'refresh-1', expires_in: 300 }), { status: 200 }))

    expect(await completeLogin(config)).toBe(true)
    expect(fetchMock).toHaveBeenCalledWith('https://id.example.test/realms/geo/protocol/openid-connect/token', expect.objectContaining({ method: 'POST' }))
    const body = String((fetchMock.mock.calls[0][1] as RequestInit).body)
    expect(body).toContain('code_verifier=verifier-1')
    expect(readTokens()?.accessToken).toBe('access-1')
    expect(window.location.search).toBe('')
  })

  it('rejects a state mismatch', async () => {
    window.history.replaceState({}, '', '/?code=code-1&state=wrong')
    sessionStorage.setItem('geo.auth.pkce', JSON.stringify({ verifier: 'verifier-1', state: 'state-1', redirectUri: 'http://localhost:5174/' }))
    await expect(completeLogin(config)).rejects.toThrow('state')
  })

  it('refreshes an expired session', async () => {
    writeTokens({ accessToken: 'old', refreshToken: 'refresh-1', expiresAt: Date.now() - 1000 })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ access_token: 'new', refresh_token: 'refresh-2', expires_in: 300 }), { status: 200 }))
    expect(await ensureSession(config)).toBe('authenticated')
    expect(readTokens()?.accessToken).toBe('new')
  })

  it('clears an expired session when refresh fails', async () => {
    writeTokens({ accessToken: 'old', refreshToken: 'refresh-1', expiresAt: Date.now() - 1000 })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 401 }))
    expect(await ensureSession(config)).toBe('login')
    expect(readTokens()).toBeNull()
  })

  it('treats tokens within the skew window as expiring', () => {
    expect(isExpiringSoon({ accessToken: 'a', expiresAt: Date.now() + 10_000 })).toBe(true)
    expect(isExpiringSoon({ accessToken: 'a', expiresAt: Date.now() + 120_000 })).toBe(false)
  })

  it('clears the session on logout', () => {
    writeTokens({ accessToken: 'a', expiresAt: Date.now() + 300_000 })
    clearSession()
    expect(readTokens()).toBeNull()
  })
})
