import type { components } from './schema'

type ErrorEnvelope = components['schemas']['ErrorEnvelope']

let csrfToken = ''

export class APIError extends Error {
  readonly code: string
  readonly requestID: string
  readonly status: number

  constructor(status: number, envelope?: ErrorEnvelope) {
    const payload = envelope?.error
    super(payload?.message || `HTTP ${status}`)
    this.name = 'APIError'
    this.status = status
    this.code = payload?.code || 'request_failed'
    this.requestID = payload?.request_id || ''
  }
}

export function setCSRFToken(value: string) {
  csrfToken = value
}

export function clearCSRFToken() {
  csrfToken = ''
}

export interface APIOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
  version?: number
}

export async function api<T>(path: string, options: APIOptions = {}): Promise<T> {
  const method = options.method || 'GET'
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase()) && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }
  if (options.version !== undefined) {
    headers.set('If-Match', `"v${options.version}"`)
  }
  const response = await fetch(`/api/admin/v1${path}`, {
    ...options,
    method,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    credentials: 'same-origin',
    cache: 'no-store',
  })
  if (response.status === 204) {
    return undefined as T
  }
  const contentType = response.headers.get('content-type') || ''
  const payload = contentType.includes('application/json') ? await response.json() : undefined
  if (!response.ok) {
    const error = new APIError(response.status, payload as ErrorEnvelope | undefined)
    if (response.status === 401) {
      clearCSRFToken()
      window.dispatchEvent(new CustomEvent('sidervia:unauthorized'))
    }
    throw error
  }
  return payload as T
}

export function formatAPIError(error: unknown, translate: (key: string) => string): string {
  if (error instanceof APIError) {
    const translated = translate(`errors.${error.code}`)
    if (translated !== `errors.${error.code}`) return translated
    return error.requestID ? `${error.message} · ${error.requestID}` : error.message
  }
  return translate('common.requestFailed')
}
