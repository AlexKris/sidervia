import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { AuditEventPage } from '../api/types'
import { EmptyState, ErrorPanel, LoadingState, PageHeader, Panel, StatusBadge, formatDate } from '../components/ui'
import styles from '../App.module.css'

export default function AuditPage() {
  const { t } = useTranslation()
  const query = useQuery({ queryKey: ['audit-events'], queryFn: () => api<AuditEventPage>('/audit-events?limit=100') })
  return <>
    <PageHeader title={t('audit.title')} description={t('audit.description')} />
    {query.error && <ErrorPanel message={formatAPIError(query.error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('audit.event')}</th><th>{t('audit.actor')}</th><th>{t('audit.target')}</th><th>{t('audit.outcome')}</th><th>{t('audit.time')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.event_type}</strong>{item.request_id && <small>{item.request_id}</small>}</td>
          <td>{item.actor_kind}<small>{item.actor_id || '—'}</small></td>
          <td>{item.target_kind || '—'}<small>{item.target_id || ''}</small></td>
          <td><StatusBadge value={item.outcome} tone={item.outcome === 'success' ? 'good' : 'bad'} /></td><td>{formatDate(item.created_at)}</td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
  </>
}
