import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setLocale } from '../i18n'
import LoginPage from './LoginPage'

const auth = vi.hoisted(() => ({ login: vi.fn(), session: null }))

vi.mock('../auth/AuthContext', () => ({
  useAuth: () => ({ session: auth.session, login: auth.login }),
}))

describe('login page', () => {
  beforeEach(async () => {
    auth.login.mockReset()
    window.localStorage.clear()
    await setLocale('zh-CN')
  })

  it('submits password and optional TOTP without persisting either value', async () => {
    const user = userEvent.setup()
    render(<MemoryRouter><LoginPage /></MemoryRouter>)
    const password = screen.getByLabelText('管理员密码')
    const totp = screen.getByLabelText(/^TOTP 验证码/)
    expect(password).toHaveAttribute('type', 'password')
    await user.type(password, 'correct horse battery staple')
    await user.type(totp, '123456')
    await user.click(screen.getByRole('button', { name: '安全登录' }))
    await waitFor(() => expect(auth.login).toHaveBeenCalledWith('correct horse battery staple', '123456'))
    expect(window.localStorage.getItem('sidervia.locale')).toBe('zh-CN')
    expect(JSON.stringify(window.localStorage)).not.toContain('correct horse')
  })
})
