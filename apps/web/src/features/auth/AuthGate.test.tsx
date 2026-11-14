import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthGate } from './AuthGate'

beforeEach(() => {
  sessionStorage.clear()
  window.history.replaceState({}, '', '/')
  vi.restoreAllMocks()
})

afterEach(cleanup)

function mockConfig(config: Record<string, string>) {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify(config), { status: 200 }))
}

describe('AuthGate', () => {
  it('renders the app in dev mode', async () => {
    mockConfig({ mode: 'dev' })
    render(<AuthGate><p>workspace ready</p></AuthGate>)
    expect(await screen.findByText('workspace ready')).toBeTruthy()
  })

  it('shows the login screen for oidc without a session', async () => {
    mockConfig({ mode: 'oidc', issuer: 'https://id.example.test/realms/geo', clientId: 'geo-web' })
    render(<AuthGate><p>workspace ready</p></AuthGate>)
    expect(await screen.findByText('使用 Keycloak 登录')).toBeTruthy()
    expect(screen.queryByText('workspace ready')).toBeNull()
  })

  it('surfaces an auth-config failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 502 }))
    render(<AuthGate><p>workspace ready</p></AuthGate>)
    await waitFor(() => expect(screen.getByText('无法连接认证服务')).toBeTruthy())
  })
})
