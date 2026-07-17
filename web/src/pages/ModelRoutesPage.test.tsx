import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import ModelRoutesPage from './ModelRoutesPage'

const apiMock = vi.hoisted(() => vi.fn())

vi.mock('../api/client', () => ({
  api: (...arguments_: unknown[]) => apiMock(...arguments_),
  formatAPIError: () => 'request failed',
}))

describe('model routes page', () => {
  afterEach(cleanup)
  beforeEach(async () => {
    apiMock.mockReset()
    await setLocale('zh-CN')
  })

  it('creates an explicitly confirmed two-account pool', async () => {
    apiMock.mockImplementation((path: string, options?: { method?: string; body?: unknown }) => {
      if (path === '/model-routes' && options?.method === 'POST') return Promise.resolve({})
      if (path === '/model-routes') return Promise.resolve({ items: [] })
      if (path === '/accounts') return Promise.resolve({ items: [
        { id: 'sdr_acct_one', upstream_id: 'sdr_up_openai', name: 'OpenAI One', status: 'active' },
        { id: 'sdr_acct_two', upstream_id: 'sdr_up_openai', name: 'OpenAI Two', status: 'active' },
      ] })
      if (path === '/upstreams') return Promise.resolve({ items: [
        { id: 'sdr_up_openai', name: 'OpenAI', provider_id: 'openai' },
      ] })
      return Promise.reject(new Error(`unexpected path ${path}`))
    })
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: '新建路由' }))
    await user.type(screen.getByLabelText('公开模型 ID'), 'shared-model')
    await user.type(screen.getAllByLabelText('上游模型 ID')[0], 'provider-model-one')
    await user.click(screen.getByRole('button', { name: '添加候选账号' }))
    await user.selectOptions(screen.getAllByLabelText('候选账号')[1], 'sdr_acct_two')
    await user.type(screen.getAllByLabelText('上游模型 ID')[1], 'provider-model-two')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(apiMock).toHaveBeenCalledWith('/model-routes', expect.objectContaining({
      method: 'POST',
      body: expect.objectContaining({
        confirm_multiple_candidates: true,
        candidates: [
          expect.objectContaining({ account_id: 'sdr_acct_one', protocols: ['openai'], capabilities: ['text', 'stream'] }),
          expect.objectContaining({ account_id: 'sdr_acct_two', protocols: ['openai'], capabilities: ['text', 'stream'] }),
        ],
      }),
    })))
  })
})

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}><ModelRoutesPage /></QueryClientProvider>)
}
