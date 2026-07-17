import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth/AuthContext'
import AppLayout from './components/AppLayout'
import { LoadingState } from './components/ui'
import styles from './App.module.css'

const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const ProxiesPage = lazy(() => import('./pages/ProxiesPage'))
const UpstreamsPage = lazy(() => import('./pages/UpstreamsPage'))
const AccountsPage = lazy(() => import('./pages/AccountsPage'))
const ModelRoutesPage = lazy(() => import('./pages/ModelRoutesPage'))
const ClientKeysPage = lazy(() => import('./pages/ClientKeysPage'))
const AuditPage = lazy(() => import('./pages/AuditPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const PlannedPage = lazy(() => import('./pages/PlannedPage'))
const UsagePage = lazy(() => import('./pages/UsagePage'))

function ProtectedLayout() {
  const { session, loading } = useAuth()
  if (loading) return <div className={styles.centered}><LoadingState /></div>
  if (!session) return <Navigate to="/login" replace />
  return <AppLayout />
}

export default function App() {
  return <Suspense fallback={<div className={styles.centered}><LoadingState /></div>}><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedLayout />}>
      <Route index element={<DashboardPage />} />
      <Route path="accounts" element={<AccountsPage />} />
      <Route path="upstreams" element={<UpstreamsPage />} />
      <Route path="proxies" element={<ProxiesPage />} />
      <Route path="model-routes" element={<ModelRoutesPage />} />
      <Route path="client-keys" element={<ClientKeysPage />} />
      <Route path="usage" element={<UsagePage />} />
      <Route path="price-catalog" element={<PlannedPage kind="pricing" />} />
      <Route path="audit" element={<AuditPage />} />
      <Route path="settings" element={<SettingsPage />} />
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes></Suspense>
}
