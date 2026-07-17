import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Power, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Proxy, ProxyPage, ProxyRequest } from '../api/types'
import { Button, Checkbox, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge, formatDate } from '../components/ui'
import styles from '../App.module.css'

const emptyForm: ProxyRequest = { name: '', scheme: 'https', host: '', port: 443, allow_insecure_tls: false, enabled: true }

export default function ProxiesPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<ProxyRequest>(emptyForm)
  const query = useQuery({ queryKey: ['proxies'], queryFn: () => api<ProxyPage>('/proxies') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['proxies'] })
  const create = useMutation({ mutationFn: (body: ProxyRequest) => api<Proxy>('/proxies', { method: 'POST', body }), onSuccess: async () => { setOpen(false); setForm(emptyForm); await refresh() } })
  const update = useMutation({ mutationFn: ({ item, enabled }: { item: Proxy; enabled: boolean }) => api<Proxy>(`/proxies/${item.id}`, { method: 'PATCH', version: item.version, body: proxyBody(item, enabled) }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (item: Proxy) => api<void>(`/proxies/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || create.error || update.error || remove.error

  const submit = (event: FormEvent) => { event.preventDefault(); create.mutate(form) }
  const setModalOpen = (value: boolean) => {
    setOpen(value)
    if (!value) setForm(emptyForm)
  }
  return <>
    <PageHeader title={t('proxies.title')} description={t('proxies.description')} action={<Button onClick={() => { setForm(emptyForm); setOpen(true) }}><Plus size={17} />{t('proxies.add')}</Button>} />
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('common.name')}</th><th>{t('proxies.host')}</th><th>{t('proxies.secretConfigured')}</th><th>{t('common.status')}</th><th>{t('common.createdAt')}</th><th className={styles.actionCell}>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.name}</strong><small>{item.scheme.toUpperCase()}</small></td>
          <td><code>{item.host}:{item.port}</code>{item.allow_insecure_tls && <small className={styles.dangerText}>{t('proxies.insecure')}</small>}</td>
          <td>{item.username_configured || item.password_configured ? t('common.yes') : t('common.no')}</td>
          <td><StatusBadge value={item.enabled ? t('common.active') : t('common.disabled')} tone={item.enabled ? 'good' : 'neutral'} /></td>
          <td>{formatDate(item.created_at)}</td>
          <td className={styles.actionCell}><IconButton label={item.enabled ? t('common.disable') : t('common.enable')} onClick={() => update.mutate({ item, enabled: !item.enabled })}><Power size={17} /></IconButton><IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton></td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={setModalOpen} title={t('proxies.add')} description={t('proxies.description')}>
      <form className={styles.form} onSubmit={submit}>
        {create.error && <ErrorPanel message={formatAPIError(create.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('common.name')}><Input required maxLength={100} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
          <Field label={t('proxies.scheme')}><Select value={form.scheme} onChange={(event) => setForm({ ...form, scheme: event.target.value as ProxyRequest['scheme'] })}><option value="https">HTTPS</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></Select></Field>
          <Field label={t('proxies.host')}><Input required placeholder="proxy.example.com" value={form.host} onChange={(event) => setForm({ ...form, host: event.target.value })} /></Field>
          <Field label={t('proxies.port')}><Input required type="number" min={1} max={65535} value={form.port} onChange={(event) => setForm({ ...form, port: Number(event.target.value) })} /></Field>
          <Field label={t('proxies.username')}><Input autoComplete="off" value={form.username || ''} onChange={(event) => setForm({ ...form, username: event.target.value || undefined })} /></Field>
          <Field label={t('proxies.password')}><Input type="password" autoComplete="new-password" value={form.password || ''} onChange={(event) => setForm({ ...form, password: event.target.value || undefined })} /></Field>
          <Field label={t('proxies.tlsName')}><Input value={form.tls_server_name || ''} onChange={(event) => setForm({ ...form, tls_server_name: event.target.value || undefined })} /></Field>
        </div>
        <Checkbox label={t('proxies.insecure')} checked={form.allow_insecure_tls} onChange={(checked) => setForm({ ...form, allow_insecure_tls: checked })} />
        <Checkbox label={t('common.enabled')} checked={form.enabled} onChange={(checked) => setForm({ ...form, enabled: checked })} />
        <FormActions submitting={create.isPending} onCancel={() => setModalOpen(false)} />
      </form>
    </Modal>
  </>
}

function proxyBody(item: Proxy, enabled: boolean): ProxyRequest {
  return {
    name: item.name, scheme: item.scheme, host: item.host, port: item.port,
    tls_server_name: item.tls_server_name, allow_insecure_tls: item.allow_insecure_tls, enabled,
  }
}
