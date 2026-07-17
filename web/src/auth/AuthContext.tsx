import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, clearCSRFToken, setCSRFToken } from '../api/client'
import type { Session } from '../api/types'

interface AuthValue {
  session: Session | null
  loading: boolean
  login: (password: string, totpCode: string) => Promise<void>
  logout: () => Promise<void>
  replaceSession: (session: Session) => void
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)

  const replaceSession = useCallback((next: Session) => {
    setCSRFToken(next.csrf_token)
    setSession(next)
  }, [])

  useEffect(() => {
    let active = true
    api<Session>('/auth/session')
      .then((value) => { if (active) replaceSession(value) })
      .catch(() => { if (active) setSession(null) })
      .finally(() => { if (active) setLoading(false) })
    const unauthorized = () => setSession(null)
    window.addEventListener('sidervia:unauthorized', unauthorized)
    return () => {
      active = false
      window.removeEventListener('sidervia:unauthorized', unauthorized)
    }
  }, [replaceSession])

  const login = useCallback(async (password: string, totpCode: string) => {
    const next = await api<Session>('/auth/login', {
      method: 'POST',
      body: { password, ...(totpCode ? { totp_code: totpCode } : {}) },
    })
    replaceSession(next)
  }, [replaceSession])

  const logout = useCallback(async () => {
    try {
      await api<void>('/auth/logout', { method: 'POST' })
    } finally {
      clearCSRFToken()
      setSession(null)
    }
  }, [])

  const value = useMemo(() => ({ session, loading, login, logout, replaceSession }), [session, loading, login, logout, replaceSession])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}
