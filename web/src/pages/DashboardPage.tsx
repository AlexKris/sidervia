import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Boxes, CheckCircle2, Database, KeyRound, Network, Route, ShieldCheck, Waypoints } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Dashboard } from '../api/types'
import { ErrorPanel, LoadingState, PageHeader, Panel, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

const countCards = [
  ['accounts', Boxes], ['upstreams', Waypoints], ['proxies', Network], ['model_routes', Route], ['client_keys', KeyRound],
] as const

export default function DashboardPage() {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['dashboard'], queryFn: () => api<Dashboard>('/dashboard') })
  if (query.isLoading) return <LoadingState />
  if (query.error) return <ErrorPanel message={formatAPIError(query.error, t)} />
  const dashboard = query.data!
  return <>
    <PageHeader title={t('dashboard.title')} description={t('dashboard.description')} />
    <div className={styles.metricGrid}>
      <Panel className={styles.metricCard}><div className={styles.metricIcon}><Database size={21} /></div><div><span>{t('dashboard.ready')}</span><strong>{dashboard.database_ready ? t('dashboard.healthy') : t('common.disabled')}</strong></div><StatusBadge value={dashboard.database_ready ? 'ready' : 'disabled'} /></Panel>
      <Panel className={styles.metricCard}><div className={styles.metricIcon}><ShieldCheck size={21} /></div><div><span>{t('dashboard.totp')}</span><strong>{dashboard.admin_totp_enabled ? t('dashboard.totpOn') : t('dashboard.totpOff')}</strong></div><StatusBadge value={dashboard.admin_totp_enabled ? 'enabled' : 'draft'} /></Panel>
      {countCards.map(([key, Icon]) => <Panel className={styles.metricCard} key={key}><div className={styles.metricIcon}><Icon size={21} /></div><div><span>{t(`nav.${key === 'model_routes' ? 'routes' : key === 'client_keys' ? 'keys' : key}`)}</span><strong>{dashboard.counts[key] || 0}</strong></div></Panel>)}
    </div>
    <div className={styles.twoColumns}>
      <Panel>
        <div className={styles.panelTitle}><div><span className={styles.eyebrow}>{t('dashboard.attention')}</span><h2>{dashboard.warnings.length ? `${dashboard.warnings.length} ${t('dashboard.attention')}` : t('dashboard.noWarnings')}</h2></div>{dashboard.warnings.length ? <AlertTriangle size={21} /> : <CheckCircle2 size={21} />}</div>
        <div className={styles.warningList}>
          {dashboard.warnings.map((warning) => <div className={styles.warningItem} key={warning}><span />{warning === 'admin_totp_disabled' ? t('dashboard.totpWarning') : t('dashboard.providersWarning')}</div>)}
          {!dashboard.warnings.length && <p className={styles.muted}>{t('dashboard.noWarnings')}</p>}
        </div>
      </Panel>
      <Panel className={styles.scopePanel}>
        <div className={styles.scopeVisual}><span /><span /><span /><span /></div>
        <span className={styles.eyebrow}>{t('brand.foundation')}</span><h2>{t('dashboard.scopeTitle')}</h2><p>{t('dashboard.scopeBody')}</p>
      </Panel>
    </div>
  </>
}
