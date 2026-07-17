import { CircleDashed, LockKeyhole } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PageHeader, Panel, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

export default function PlannedPage({ kind }: { kind: 'usage' | 'pricing' }) {
  const { t } = useTranslation()
  const title = t(`planned.${kind}Title`)
  return <>
    <PageHeader title={title} description={t(`planned.${kind}Body`)} />
    <Panel className={styles.plannedPanel}>
      <div className={styles.plannedIcon}>{kind === 'usage' ? <CircleDashed size={28} /> : <LockKeyhole size={28} />}</div>
      <StatusBadge value={t('common.planned')} tone="warn" /><h2>{title}</h2><p>{t(`planned.${kind}Body`)}</p>
    </Panel>
  </>
}
