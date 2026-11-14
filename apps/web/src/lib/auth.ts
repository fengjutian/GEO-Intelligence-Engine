export type AuthConfig = {
  mode: 'dev' | 'jwt' | 'oidc' | string
  issuer?: string
  clientId?: string
  audience?: string
  scope?: string
}

type Tokens = {
  accessToken: string
  idToken?: string
  refreshToken?: string
  expiresAt: number
}

type PkceState = { verifier: string; state: string; redirectUri: string }

const TOKEN_KEY = 'geo.auth.tokens'
const PKCE_KEY = 'geo.auth.pkce'
const unauthorizedEvent = 'geo:unauthorized'

let currentConfig: AuthConfig | null = null

export function setAuthConfig(config: AuthConfig) {
  currentConfig = config
}

export function getAuthConfig() {
  return currentConfig
}

function base64Url(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export function randomString(length = 64): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~'
  const values = new Uint8Array(length)
  crypto.getRandomValues(values)
  return Array.from(values, (value) => alphabet[value % alphabet.length]).join('')
}

export async function challengeForVerifier(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  return base64Url(new Uint8Array(digest))
}

export function buildAuthorizeUrl(config: AuthConfig, challenge: string, state: string, redirectUri: string): string {
  if (!config.issuer || !config.clientId) throw new Error('OIDC issuer/clientId 未配置')
  const url = new URL(`${config.issuer.replace(/\/$/, '')}/protocol/openid-connect/auth`)
  url.searchParams.set('client_id', config.clientId)
  url.searchParams.set('redirect_uri', redirectUri)
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', config.scope ?? 'openid profile email')
  url.searchParams.set('state', state)
  url.searchParams.set('code_challenge', challenge)
  url.searchParams.set('code_challenge_method', 'S256')
  return url.toString()
}

export async function beginLogin(config: AuthConfig = currentConfig as AuthConfig): Promise<void> {
  const verifier = randomString(64)
  const state = randomString(32)
  const redirectUri = window.location.origin + '/'
  const pkce: PkceState = { verifier, state, redirectUri }
  sessionStorage.setItem(PKCE_KEY, JSON.stringify(pkce))
  const challenge = await challengeForVerifier(verifier)
  window.location.assign(buildAuthorizeUrl(config, challenge, state, redirectUri))
}

function readPkce(): PkceState | null {
  try {
    return JSON.parse(sessionStorage.getItem(PKCE_KEY) ?? 'null') as PkceState | null
  } catch {
    return null
  }
}

export function readTokens(): Tokens | null {
  try {
    return JSON.parse(sessionStorage.getItem(TOKEN_KEY) ?? 'null') as Tokens | null
  } catch {
    return null
  }
}

export function writeTokens(tokens: Tokens) {
  sessionStorage.setItem(TOKEN_KEY, JSON.stringify(tokens))
}

export function clearSession() {
  sessionStorage.removeItem(TOKEN_KEY)
  sessionStorage.removeItem(PKCE_KEY)
}

export function accessToken(): string | null {
  return readTokens()?.accessToken ?? null
}

export function isExpiringSoon(tokens: Tokens | null, skewSeconds = 60): boolean {
  return !tokens || tokens.expiresAt <= Date.now() + skewSeconds * 1000
}

async function tokenRequest(config: AuthConfig, form: URLSearchParams): Promise<Tokens> {
  if (!config.issuer || !config.clientId) throw new Error('OIDC issuer/clientId 未配置')
  const response = await fetch(`${config.issuer.replace(/\/$/, '')}/protocol/openid-connect/token`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: form.toString(),
  })
  if (!response.ok) throw new Error(`OIDC token 交换失败 (${response.status})`)
  const payload = await response.json() as { access_token?: string; id_token?: string; refresh_token?: string; expires_in?: number }
  if (!payload.access_token) throw new Error('OIDC 响应缺少 access_token')
  return {
    accessToken: payload.access_token,
    idToken: payload.id_token,
    refreshToken: payload.refresh_token,
    expiresAt: Date.now() + (payload.expires_in ?? 300) * 1000,
  }
}

export async function completeLogin(config: AuthConfig = currentConfig as AuthConfig): Promise<boolean> {
  const params = new URLSearchParams(window.location.search)
  if (params.get('error')) {
    throw new Error(`OIDC 登录失败：${params.get('error_description') ?? params.get('error')}`)
  }
  const code = params.get('code')
  if (!code) return false
  const returnedState = params.get('state') ?? ''
  const pkce = readPkce()
  if (!pkce || pkce.state !== returnedState) throw new Error('OIDC state 校验失败，请重新登录')
  const form = new URLSearchParams({
    grant_type: 'authorization_code',
    client_id: config.clientId ?? '',
    code,
    redirect_uri: pkce.redirectUri,
    code_verifier: pkce.verifier,
  })
  const tokens = await tokenRequest(config, form)
  writeTokens(tokens)
  sessionStorage.removeItem(PKCE_KEY)
  window.history.replaceState({}, '', window.location.pathname)
  return true
}

export async function refreshSession(config: AuthConfig = currentConfig as AuthConfig): Promise<boolean> {
  const tokens = readTokens()
  if (!tokens?.refreshToken) return false
  try {
    const form = new URLSearchParams({
      grant_type: 'refresh_token',
      client_id: config.clientId ?? '',
      refresh_token: tokens.refreshToken,
    })
    writeTokens(await tokenRequest(config, form))
    return true
  } catch {
    clearSession()
    return false
  }
}

export async function ensureSession(config: AuthConfig): Promise<'dev' | 'authenticated' | 'login'> {
  if (config.mode !== 'oidc') return 'dev'
  if (await completeLogin(config)) return 'authenticated'
  const tokens = readTokens()
  if (tokens && !isExpiringSoon(tokens)) return 'authenticated'
  if (tokens && await refreshSession(config)) return 'authenticated'
  return 'login'
}

export function logout(config: AuthConfig = currentConfig as AuthConfig) {
  const tokens = readTokens()
  clearSession()
  if (config.mode === 'oidc' && config.issuer) {
    const url = new URL(`${config.issuer.replace(/\/$/, '')}/protocol/openid-connect/logout`)
    url.searchParams.set('post_logout_redirect_uri', window.location.origin + '/')
    if (tokens?.idToken) url.searchParams.set('id_token_hint', tokens.idToken)
    window.location.assign(url.toString())
  }
}

export async function onUnauthorized() {
  clearSession()
  window.dispatchEvent(new Event(unauthorizedEvent))
}

export function subscribeUnauthorized(handler: () => void): () => void {
  window.addEventListener(unauthorizedEvent, handler)
  return () => window.removeEventListener(unauthorizedEvent, handler)
}

/** Installs a fetch wrapper that attaches the bearer token to API calls and
 * clears the session when the API returns 401. Keeps all existing call sites. */
export function installFetchAuth() {
  const original = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
    const isApi = url.startsWith('/api/') || url.includes('/api/v1/')
    const token = accessToken()
    let response: Response
    if (isApi && token) {
      const headers = new Headers(init?.headers)
      headers.set('Authorization', `Bearer ${token}`)
      response = await original(input, { ...init, headers })
    } else {
      response = await original(input, init)
    }
    if (isApi && response.status === 401) {
      const oidc = getAuthConfig()
      const tokens = readTokens()
      if (oidc?.mode === 'oidc' && tokens?.refreshToken && await refreshSession(oidc)) {
        const retryHeaders = new Headers(init?.headers)
        retryHeaders.set('Authorization', `Bearer ${accessToken()}`)
        return original(input, { ...init, headers: retryHeaders })
      }
      await onUnauthorized()
    }
    return response
  }
}
