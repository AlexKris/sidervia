import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus, Power, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Account, AccountPage, AccountRequest, ProxyPage, UpstreamPage } from '../api/types'
import { Button, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

const emptyForm: AccountRequest = { upstream_id: '', name: '', credential: '', billing_kind: 'subscription', status: 'draft', priority: 10, max_concurrency: 1 }

export default function AccountsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<AccountRequest>(emptyForm)
  const query = useQuery({ queryKey: ['accounts'], queryFn: () => api<AccountPage>('/accounts') })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamPage>('/upstreams') })
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: () => api<ProxyPage>('/proxies') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['accounts'] })
  const create = useMutation({ mutationFn: (body: AccountRequest) => api<Account>('/accounts', { method: 'POST', body }), onSuccess: async () => { setOpen(false); setForm(emptyForm); await refresh() } })
  const update = useMutation({ mutationFn: ({ item, status }: { item: Account; status: 'draft' | 'disabled' }) => api<Account>(`/accounts/${item.id}`, { method: 'PATCH', version: item.version, body: accountBody(item, status) }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (item: Account) => api<void>(`/accounts/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || create.error || update.error || remove.error

  const submit = (event: FormEvent) => { event.preventDefault(); create.mutate({ ...form, credential_expires_at: form.credential_expires_at ? new Date(form.credential_expires_at).toISOString() : undefined }) }
  const openForm = () => {
    setForm({ ...emptyForm, upstream_id: upstreams.data?.items[0]?.id || '' })
    setOpen(true)
  }
  return <>
    <PageHeader title={t('accounts.title')} description={t('accounts.description')} action={<Button onClick={openForm} disabled={!upstreams.data?.items.length}><Plus size={17} />{t('accounts.add')}</Button>} />
    <div className={styles.infoStrip}><KeyRound size={16} />{t('accounts.apiKeyOnly')}</div>
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('common.name')}</th><th>{t('accounts.upstream')}</th><th>{t('accounts.billing')}</th><th>{t('accounts.priority')}</th><th>{t('accounts.concurrency')}</th><th>{t('common.status')}</th><th>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.name}</strong><small>{item.credential_configured ? t('accounts.credentialConfigured') : '—'}</small></td>
          <td>{upstreams.data?.items.find((upstream) => upstream.id === item.upstream_id)?.name || item.upstream_id}</td>
          <td>{t(`accounts.${item.billing_kind}`)}</td><td>{item.priority}</td><td>{item.max_concurrency}</td>
          <td><StatusBadge value={t(`common.${item.status}`)} tone={item.status === 'draft' ? 'warn' : 'neutral'} /></td>
          <td className={styles.actionCell}><IconButton label={item.status === 'draft' ? t('common.disable') : t('common.enable')} onClick={() => update.mutate({ item, status: item.status === 'draft' ? 'disabled' : 'draft' })}><Power size={17} /></IconButton><IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton></td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={setOpen} title={t('accounts.add')} description={t('accounts.description')}>
      <form className={styles.form} onSubmit={submit}>
        {create.error && <ErrorPanel message={formatAPIError(create.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('accounts.upstream')}><Select required value={form.upstream_id} onChange={(event) => setForm({ ...form, upstream_id: event.target.value })}>{(upstreams.data?.items || []).map((upstream) => <option value={upstream.id} key={upstream.id}>{upstream.name}</option>)}</Select></Field>
          <Field label={t('common.name')}><Input required maxLength={100} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
          <Field label={t('accounts.credential')}><Input required type="password" autoComplete="new-password" value={form.credential || ''} onChange={(event) => setForm({ ...form, credential: event.target.value })} /></Field>
          <Field label={t('accounts.billing')}><Select value={form.billing_kind} onChange={(event) => {
            const billing = event.target.value as AccountRequest['billing_kind']
            setForm({ ...form, billing_kind: billing, priority: billing === 'subscription' ? 10 : 20, max_concurrency: billing === 'subscription' ? 1 : 4 })
          }}><option value="subscription">{t('accounts.subscription')}</option><option value="metered">{t('accounts.metered')}</option><option value="custom">{t('accounts.custom')}</option></Select></Field>
          <Field label={t('accounts.priority')}><Input type="number" min={0} value={form.priority ?? 10} onChange={(event) => setForm({ ...form, priority: Number(event.target.value) })} /></Field>
          <Field label={t('accounts.concurrency')}><Input type="number" min={1} max={1024} value={form.max_concurrency ?? 1} onChange={(event) => setForm({ ...form, max_concurrency: Number(event.target.value) })} /></Field>
          <Field label={t('accounts.proxy')}><Select value={form.proxy_id || ''} onChange={(event) => setForm({ ...form, proxy_id: event.target.value || undefined })}><option value="">{t('upstreams.noProxy')}</option>{(proxies.data?.items || []).map((proxy) => <option value={proxy.id} key={proxy.id}>{proxy.name}</option>)}</Select></Field>
          <Field label={t('accounts.expires')}><Input type="datetime-local" value={form.credential_expires_at || ''} onChange={(event) => setForm({ ...form, credential_expires_at: event.target.value || undefined })} /></Field>
          <Field label={t('common.status')}><Select value={form.status} onChange={(event) => setForm({ ...form, status: event.target.value as AccountRequest['status'] })}><option value="draft">{t('common.draft')}</option><option value="disabled">{t('common.disabled')}</option></Select></Field>
        </div>
        <FormActions submitting={create.isPending} onCancel={() => setOpen(false)} />
      </form>
    </Modal>
  </>
}

function accountBody(item: Account, status: 'draft' | 'disabled'): AccountRequest {
  return {
    upstream_id: item.upstream_id, name: item.name, billing_kind: item.billing_kind,
    credential_expires_at: item.credential_expires_at, proxy_id: item.proxy_id,
    status, priority: item.priority, max_concurrency: item.max_concurrency,
  }
}
