import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth/AuthContext'
import AppLayout from './components/AppLayout'
import { LoadingState } from './components/ui'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import ProxiesPage from './pages/ProxiesPage'
import UpstreamsPage from './pages/UpstreamsPage'
import AccountsPage from './pages/AccountsPage'
import ModelRoutesPage from './pages/ModelRoutesPage'
import ClientKeysPage from './pages/ClientKeysPage'
import AuditPage from './pages/AuditPage'
import SettingsPage from './pages/SettingsPage'
import PlannedPage from './pages/PlannedPage'
import styles from './App.module.css'

function ProtectedLayout() {
  const { session, loading } = useAuth()
  if (loading) return <div className={styles.centered}><LoadingState /></div>
  if (!session) return <Navigate to="/login" replace />
  return <AppLayout />
}

export default function App() {
  return <Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedLayout />}>
      <Route index element={<DashboardPage />} />
      <Route path="accounts" element={<AccountsPage />} />
      <Route path="upstreams" element={<UpstreamsPage />} />
      <Route path="proxies" element={<ProxiesPage />} />
      <Route path="model-routes" element={<ModelRoutesPage />} />
      <Route path="client-keys" element={<ClientKeysPage />} />
      <Route path="usage" element={<PlannedPage kind="usage" />} />
      <Route path="price-catalog" element={<PlannedPage kind="pricing" />} />
      <Route path="audit" element={<AuditPage />} />
      <Route path="settings" element={<SettingsPage />} />
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
