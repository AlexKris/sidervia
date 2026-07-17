import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import ProxiesPage from './ProxiesPage'

const apiMock = vi.hoisted(() => vi.fn())

vi.mock('../api/client', () => ({
  api: (...arguments_: unknown[]) => apiMock(...arguments_),
  formatAPIError: () => 'request failed',
}))

describe('proxies page', () => {
  beforeEach(async () => {
    apiMock.mockReset()
    apiMock.mockResolvedValue({ items: [] })
    await setLocale('zh-CN')
  })

  it('clears write-only credentials when creation is cancelled', async () => {
    const user = userEvent.setup()
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={queryClient}><ProxiesPage /></QueryClientProvider>)

    await user.click(screen.getByRole('button', { name: '新建代理' }))
    const password = screen.getByLabelText('密码')
    await user.type(password, 'proxy-password-canary')
    await user.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: '新建代理' }))
    expect(screen.getByLabelText('密码')).toHaveValue('')
  })
})
