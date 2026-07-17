import { useQuery } from '@tanstack/react-query'
import { Activity, AlertTriangle, ArrowDownToLine, ArrowUpFromLine, Radio } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { RequestRecordPage, UsageSummary } from '../api/types'
import { EmptyState, ErrorPanel, formatDate, LoadingState, PageHeader, Panel, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

const number = new Intl.NumberFormat()

export default function UsagePage() {
  const { t } = useTranslation()
  const summary = useQuery({ queryKey: ['usage-summary'], queryFn: () => api<UsageSummary>('/usage') })
  const requests = useQuery({ queryKey: ['requests'], queryFn: () => api<RequestRecordPage>('/requests?limit=100') })
  const error = summary.error || requests.error
  if (summary.isLoading || requests.isLoading) return <LoadingState />
  return <>
    <PageHeader title={t('usage.title')} description={t('usage.description')} />
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    {summary.data && <div className={styles.metricGrid}>
      <Metric icon={<Activity size={21} />} label={t('usage.requests24h')} value={number.format(summary.data.requests)} />
      <Metric icon={<AlertTriangle size={21} />} label={t('usage.errors24h')} value={number.format(summary.data.errors)} />
      <Metric icon={<Radio size={21} />} label={t('usage.streamed24h')} value={number.format(summary.data.streamed)} />
      <Metric icon={<ArrowDownToLine size={21} />} label={t('usage.inputTokens24h')} value={number.format(summary.data.input_tokens)} />
      <Metric icon={<ArrowUpFromLine size={21} />} label={t('usage.outputTokens24h')} value={number.format(summary.data.output_tokens)} />
    </div>}
    <div className={styles.infoStrip}>{t('usage.privacyBoundary')}</div>
    <Panel>
      {!requests.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('usage.time')}</th><th>{t('usage.request')}</th><th>{t('usage.client')}</th><th>{t('usage.providerModel')}</th><th>{t('usage.protocol')}</th><th>{t('common.status')}</th><th>{t('usage.latency')}</th><th>{t('usage.tokens')}</th></tr></thead>
        <tbody>{requests.data.items.map((item) => <tr key={item.id}>
          <td>{formatDate(item.started_at)}</td>
          <td><code>{item.id}</code><small>{item.endpoint_kind}{item.streamed ? ` · ${t('usage.stream')}` : ''}</small></td>
          <td>{item.client_key_name}<small><code>{item.client_key_id}</code></small></td>
          <td>{item.provider_id || '—'}<small>{item.public_model_id}</small></td>
          <td>{item.protocol}</td>
          <td><StatusBadge value={item.error_code || String(item.status_code)} tone={item.status_code >= 400 ? 'bad' : 'good'} /></td>
          <td>{item.duration_ms} ms<small>{item.ttft_ms === undefined ? '—' : `TTFT ${item.ttft_ms} ms`}</small></td>
          <td>{number.format(item.usage.input_tokens || 0)} / {number.format(item.usage.output_tokens || 0)}<small>{t('usage.inputOutput')}</small></td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
  </>
}

function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return <Panel className={styles.metricCard}><div className={styles.metricIcon}>{icon}</div><div><span>{label}</span><strong>{value}</strong></div></Panel>
}
