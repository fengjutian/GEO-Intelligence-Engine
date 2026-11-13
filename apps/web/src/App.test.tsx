import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { App } from './App'

describe('App', () => {
  it('renders the product modules', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => undefined)))
    render(<QueryClientProvider client={new QueryClient()}><App /></QueryClientProvider>)
    expect(screen.getByRole('heading', { name: 'GEO Intelligence' })).toBeTruthy()
    expect(screen.getByText('Discovery')).toBeTruthy()
  })
})
