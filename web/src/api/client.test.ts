import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, clearCSRFToken, setCSRFToken } from './client'

describe('admin API client', () => {
  afterEach(() => {
    clearCSRFToken()
    vi.restoreAllMocks()
    window.localStorage.clear()
  })

  it('keeps CSRF in memory and sends it only on unsafe requests', async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })))
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-canary-secret')

    await api('/proxies', { method: 'POST', body: { name: 'proxy' }, version: 4 })
    const [, options] = fetchMock.mock.calls[0]
    const headers = options.headers as Headers
    expect(headers.get('X-CSRF-Token')).toBe('csrf-canary-secret')
    expect(headers.get('If-Match')).toBe('"v4"')
    expect(window.localStorage.length).toBe(0)

    await api('/dashboard')
    const getHeaders = fetchMock.mock.calls[1][1].headers as Headers
    expect(getHeaders.has('X-CSRF-Token')).toBe(false)
  })

  it('announces an expired session without exposing the response body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'authentication required', request_id: 'req-test' } }), {
      status: 401,
      headers: { 'Content-Type': 'application/json' },
    })))
    const unauthorized = vi.fn()
    window.addEventListener('sidervia:unauthorized', unauthorized)
    await expect(api('/dashboard')).rejects.toMatchObject({ code: 'unauthorized', requestID: 'req-test' })
    expect(unauthorized).toHaveBeenCalledOnce()
    window.removeEventListener('sidervia:unauthorized', unauthorized)
  })
})
