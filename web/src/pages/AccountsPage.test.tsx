import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import AccountsPage from './AccountsPage'

const apiMock = vi.hoisted(() => vi.fn())

vi.mock('../api/client', () => ({
  api: (...arguments_: unknown[]) => apiMock(...arguments_),
  formatAPIError: () => 'request failed',
}))

describe('accounts page', () => {
  afterEach(cleanup)
  beforeEach(async () => {
    apiMock.mockReset()
    await setLocale('zh-CN')
  })

  it('validates an API-key account without returning its credential to the browser', async () => {
    const account = {
      id: 'sdr_acct_test', upstream_id: 'sdr_up_test', name: 'Primary', auth_kind: 'api_key',
      billing_kind: 'metered', credential_configured: true, status: 'draft', priority: 20,
      max_concurrency: 4, version: 3, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    apiMock.mockImplementation((path: string) => {
      if (path === '/accounts') return Promise.resolve({ items: [account] })
      if (path === '/upstreams') return Promise.resolve({ items: [{ id: 'sdr_up_test', name: 'OpenAI', provider_id: 'openai' }] })
      if (path === '/proxies') return Promise.resolve({ items: [] })
      if (path === '/accounts/sdr_acct_test/validate') return Promise.resolve({ ...account, status: 'active', version: 4 })
      return Promise.reject(new Error(`unexpected path ${path}`))
    })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: '验证并启用' }))
    await waitFor(() => expect(apiMock).toHaveBeenCalledWith('/accounts/sdr_acct_test/validate', { method: 'POST', version: 3 }))
    expect(JSON.stringify(apiMock.mock.calls)).not.toContain('api-key-canary')
  })

  it('creates a Google OAuth draft without a credential field', async () => {
    apiMock.mockImplementation((path: string, options?: { method?: string; body?: unknown }) => {
      if (path === '/accounts' && options?.method === 'POST') return Promise.resolve({})
      if (path === '/accounts') return Promise.resolve({ items: [] })
      if (path === '/upstreams') return Promise.resolve({ items: [{ id: 'sdr_up_google', name: 'Gemini', provider_id: 'google' }] })
      if (path === '/proxies') return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`unexpected path ${path}`))
    })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: '添加账号' }))
    await user.type(screen.getByLabelText('名称'), 'Google OAuth')
    await user.selectOptions(screen.getByLabelText('认证方式'), 'oauth')
    expect(screen.queryByLabelText('API Key')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(apiMock).toHaveBeenCalledWith('/accounts', expect.objectContaining({
      method: 'POST',
      body: expect.objectContaining({ auth_kind: 'oauth', status: 'draft' }),
    })))
    const createCall = apiMock.mock.calls.find(([path, options]) => path === '/accounts' && options?.method === 'POST')
    expect(createCall?.[1].body).not.toHaveProperty('credential')
  })

  it('edits active scheduling controls without resending the API key or bypassing validation', async () => {
    const account = {
      id: 'sdr_acct_active', upstream_id: 'sdr_up_test', name: 'Primary', auth_kind: 'api_key',
      billing_kind: 'metered', credential_configured: true, status: 'active', priority: 20,
      max_concurrency: 4, version: 7, created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    apiMock.mockImplementation((path: string, options?: { method?: string; body?: unknown }) => {
      if (path === '/accounts/sdr_acct_active' && options?.method === 'PATCH') return Promise.resolve({ ...account, ...(options.body as object), version: 8 })
      if (path === '/accounts') return Promise.resolve({ items: [account] })
      if (path === '/upstreams') return Promise.resolve({ items: [{ id: 'sdr_up_test', name: 'OpenAI', provider_id: 'openai' }] })
      if (path === '/proxies') return Promise.resolve({ items: [] })
      return Promise.reject(new Error(`unexpected path ${path}`))
    })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: '编辑' }))
    const priority = screen.getByLabelText('优先级')
    await user.clear(priority)
    await user.type(priority, '7')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(apiMock).toHaveBeenCalledWith('/accounts/sdr_acct_active', expect.objectContaining({
      method: 'PATCH', version: 7,
      body: expect.objectContaining({ priority: 7, status: 'active' }),
    })))
    const updateCall = apiMock.mock.calls.find(([path, options]) => path === '/accounts/sdr_acct_active' && options?.method === 'PATCH')
    expect(updateCall?.[1].body).not.toHaveProperty('credential')
  })
})

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<MemoryRouter><QueryClientProvider client={queryClient}><AccountsPage /></QueryClientProvider></MemoryRouter>)
}
