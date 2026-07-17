import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, Power, ShieldX } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { ClientKey, ClientKeyPage, CreatedClientKey } from '../api/types'
import { Button, CopyValue, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, StatusBadge, formatDate } from '../components/ui'
import styles from '../App.module.css'

export default function ClientKeysPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [expires, setExpires] = useState('')
  const [secret, setSecret] = useState('')
  const query = useQuery({ queryKey: ['client-keys'], queryFn: () => api<ClientKeyPage>('/client-keys') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['client-keys'] })
  const create = useMutation({
    mutationFn: (body: { name: string; expires_at?: string }) => api<CreatedClientKey>('/client-keys', { method: 'POST', body }),
    onSuccess: async (created) => { setOpen(false); setName(''); setExpires(''); setSecret(created.secret); await refresh() },
  })
  const update = useMutation({ mutationFn: ({ item, status }: { item: ClientKey; status: 'active' | 'disabled' }) => api<ClientKey>(`/client-keys/${item.id}`, { method: 'PATCH', version: item.version, body: { name: item.name, status, expires_at: item.expires_at } }), onSuccess: refresh })
  const revoke = useMutation({ mutationFn: (item: ClientKey) => api<void>(`/client-keys/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || create.error || update.error || revoke.error
  const submit = (event: FormEvent) => { event.preventDefault(); create.mutate({ name, ...(expires ? { expires_at: new Date(expires).toISOString() } : {}) }) }
  return <>
    <PageHeader title={t('keys.title')} description={t('keys.description')} action={<Button onClick={() => setOpen(true)}><Plus size={17} />{t('keys.add')}</Button>} />
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('common.name')}</th><th>{t('keys.prefix')}</th><th>{t('common.status')}</th><th>{t('keys.expires')}</th><th>{t('keys.lastUsed')}</th><th>{t('common.createdAt')}</th><th>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.name}</strong></td><td><code>{item.prefix}</code></td>
          <td><StatusBadge value={item.status} tone={item.status === 'active' ? 'good' : item.status === 'revoked' ? 'bad' : 'neutral'} /></td>
          <td>{formatDate(item.expires_at)}</td><td>{item.last_used_at ? formatDate(item.last_used_at) : t('common.never')}</td><td>{formatDate(item.created_at)}</td>
          <td className={styles.actionCell}>{item.status !== 'revoked' && <><IconButton label={item.status === 'active' ? t('common.disable') : t('common.enable')} onClick={() => update.mutate({ item, status: item.status === 'active' ? 'disabled' : 'active' })}><Power size={17} /></IconButton><IconButton label={t('common.revoke')} onClick={() => { if (window.confirm(t('keys.revokeConfirm'))) revoke.mutate(item) }}><ShieldX size={17} /></IconButton></>}</td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={setOpen} title={t('keys.add')} description={t('keys.description')}>
      <form className={styles.form} onSubmit={submit}>
        {create.error && <ErrorPanel message={formatAPIError(create.error, t)} />}
        <Field label={t('common.name')}><Input required maxLength={100} value={name} onChange={(event) => setName(event.target.value)} /></Field>
        <Field label={t('keys.expires')} hint={t('common.optional')}><Input type="datetime-local" value={expires} onChange={(event) => setExpires(event.target.value)} /></Field>
        <FormActions submitting={create.isPending} onCancel={() => setOpen(false)} />
      </form>
    </Modal>
    <Modal open={Boolean(secret)} onOpenChange={(value) => { if (!value) setSecret('') }} title={t('keys.secretTitle')} description={t('keys.secretWarning')}>
      <div className={styles.secretPanel}><div className={styles.secretIcon}><KeyRound size={23} /></div><p>{t('keys.secretWarning')}</p><CopyValue value={secret} /><Button className={styles.fullButton} onClick={() => setSecret('')}>{t('common.close')}</Button></div>
    </Modal>
  </>
}
