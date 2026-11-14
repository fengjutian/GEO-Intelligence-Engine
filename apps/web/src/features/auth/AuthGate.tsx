import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { beginLogin, ensureSession, isExpiringSoon, logout, readTokens, refreshSession, setAuthConfig, subscribeUnauthorized, type AuthConfig } from '../../lib/auth'

type AuthInfo = { mode: string; logout: () => void }

const AuthContext = createContext<AuthInfo>({ mode: 'dev', logout: () => undefined })

export function useAuth() {
  return useContext(AuthContext)
}

type Props = { children: ReactNode }

export function AuthGate({ children }: Props) {
  const [config, setConfig] = useState<AuthConfig | null>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'login' | 'error'>('loading')
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    async function bootstrap() {
      try {
        const response = await fetch('/api/v1/auth-config')
        if (!response.ok) throw new Error(`认证配置加载失败 (${response.status})`)
        const loaded = await response.json() as AuthConfig
        if (!active) return
        setAuthConfig(loaded)
        setConfig(loaded)
        const result = await ensureSession(loaded)
        if (!active) return
        setState(result === 'login' ? 'login' : 'ready')
      } catch (cause) {
        if (!active) return
        setError(cause instanceof Error ? cause.message : '认证初始化失败')
        setState('error')
      }
    }
    void bootstrap()
    return () => { active = false }
  }, [])

  useEffect(() => subscribeUnauthorized(() => setState('login')), [])

  useEffect(() => {
    if (state !== 'ready' || config?.mode !== 'oidc') return
    const timer = window.setInterval(() => {
      const tokens = readTokens()
      if (tokens && isExpiringSoon(tokens, 120)) void refreshSession(config).then((ok) => { if (!ok) setState('login') })
    }, 30_000)
    return () => window.clearInterval(timer)
  }, [state, config])

  if (state === 'loading') {
    return <div className="auth-screen"><div className="auth-card"><div className="brand-mark"><span>G</span><div><strong>GEO</strong><small>Intelligence</small></div></div><p>正在初始化…</p></div></div>
  }

  if (state === 'error') {
    return <div className="auth-screen"><div className="auth-card"><h1>无法连接认证服务</h1><p className="auth-error">{error}</p><button type="button" onClick={() => window.location.reload()}>重试</button></div></div>
  }

  if (state === 'login') {
    return <div className="auth-screen"><div className="auth-card">
      <p className="eyebrow">GEO INTELLIGENCE ENGINE</p>
      <h1>登录工作空间</h1>
      <p>使用组织账号通过 Keycloak 登录，登录后回到当前工作台。</p>
      {error && <p className="auth-error">{error}</p>}
      <button type="button" onClick={() => { if (config) void beginLogin(config).catch((cause) => setError(cause instanceof Error ? cause.message : '登录跳转失败')) }}>使用 Keycloak 登录</button>
    </div></div>
  }

  return <AuthContext.Provider value={{ mode: config?.mode ?? 'dev', logout: () => { if (config) logout(config) } }}>{children}</AuthContext.Provider>
}
