import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { App } from './App'

describe('App', () => {
  it('renders the operations workspace', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => undefined)))
    render(<QueryClientProvider client={new QueryClient()}><App /></QueryClientProvider>)
    expect(screen.getByRole('heading', { name: '工作台' })).toBeTruthy()
    expect(screen.getByRole('navigation', { name: '主导航' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: '项目空间' })).toBeTruthy()
  })
})
