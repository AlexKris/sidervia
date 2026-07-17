import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Power, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Provider, ProxyPage, Upstream, UpstreamPage, UpstreamRequest } from '../api/types'
import { Button, Checkbox, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge, formatDate } from '../components/ui'
import styles from '../App.module.css'

const emptyForm: UpstreamRequest = { provider_id: 'openai', name: '', base_url: 'https://api.openai.com/v1', allow_private_network: false, enabled: true }

export default function UpstreamsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<UpstreamRequest>(emptyForm)
  const query = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamPage>('/upstreams') })
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: () => api<ProxyPage>('/proxies') })
  const providers = useQuery({ queryKey: ['providers'], queryFn: () => api<{ items: Provider[] }>('/providers') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['upstreams'] })
  const create = useMutation({ mutationFn: (body: UpstreamRequest) => api<Upstream>('/upstreams', { method: 'POST', body }), onSuccess: async () => { setOpen(false); setForm(emptyForm); await refresh() } })
  const update = useMutation({ mutationFn: ({ item, enabled }: { item: Upstream; enabled: boolean }) => api<Upstream>(`/upstreams/${item.id}`, { method: 'PATCH', version: item.version, body: upstreamBody(item, enabled) }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (item: Upstream) => api<void>(`/upstreams/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || create.error || update.error || remove.error
  const providerName = (id: string) => providers.data?.items.find((provider) => provider.id === id)?.name || id

  const submit = (event: FormEvent) => { event.preventDefault(); create.mutate(form) }
  return <>
    <PageHeader title={t('upstreams.title')} description={t('upstreams.description')} action={<Button onClick={() => setOpen(true)}><Plus size={17} />{t('upstreams.add')}</Button>} />
    <div className={styles.infoStrip}>{t('upstreams.plannedHint')}</div>
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('common.name')}</th><th>{t('upstreams.provider')}</th><th>{t('upstreams.baseUrl')}</th><th>{t('upstreams.proxy')}</th><th>{t('common.status')}</th><th>{t('common.createdAt')}</th><th className={styles.actionCell}>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.name}</strong>{item.allow_private_network && <small className={styles.warningText}>{t('upstreams.private')}</small>}</td>
          <td>{providerName(item.provider_id)} <StatusBadge value={t('common.planned')} tone="warn" /></td>
          <td><code className={styles.breakCode}>{item.base_url}</code></td>
          <td>{proxies.data?.items.find((proxy) => proxy.id === item.default_proxy_id)?.name || t('upstreams.noProxy')}</td>
          <td><StatusBadge value={item.enabled ? t('common.active') : t('common.disabled')} tone={item.enabled ? 'good' : 'neutral'} /></td>
          <td>{formatDate(item.created_at)}</td>
          <td className={styles.actionCell}><IconButton label={item.enabled ? t('common.disable') : t('common.enable')} onClick={() => update.mutate({ item, enabled: !item.enabled })}><Power size={17} /></IconButton><IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton></td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={setOpen} title={t('upstreams.add')} description={t('upstreams.description')}>
      <form className={styles.form} onSubmit={submit}>
        {create.error && <ErrorPanel message={formatAPIError(create.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('upstreams.provider')}><Select value={form.provider_id} onChange={(event) => setForm({ ...form, provider_id: event.target.value })}>{(providers.data?.items || []).map((provider) => <option value={provider.id} key={provider.id}>{provider.name}</option>)}</Select></Field>
          <Field label={t('common.name')}><Input required maxLength={100} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
          <Field label={t('upstreams.baseUrl')}><Input required type="url" value={form.base_url} onChange={(event) => setForm({ ...form, base_url: event.target.value })} /></Field>
          <Field label={t('upstreams.proxy')}><Select value={form.default_proxy_id || ''} onChange={(event) => setForm({ ...form, default_proxy_id: event.target.value || undefined })}><option value="">{t('upstreams.noProxy')}</option>{(proxies.data?.items || []).map((proxy) => <option value={proxy.id} key={proxy.id}>{proxy.name}</option>)}</Select></Field>
        </div>
        <Checkbox label={t('upstreams.private')} checked={form.allow_private_network} onChange={(checked) => setForm({ ...form, allow_private_network: checked })} />
        <Checkbox label={t('common.enabled')} checked={form.enabled} onChange={(checked) => setForm({ ...form, enabled: checked })} />
        <FormActions submitting={create.isPending} onCancel={() => setOpen(false)} />
      </form>
    </Modal>
  </>
}

function upstreamBody(item: Upstream, enabled: boolean): UpstreamRequest {
  return {
    provider_id: item.provider_id, name: item.name, base_url: item.base_url,
    default_proxy_id: item.default_proxy_id, allow_private_network: item.allow_private_network, enabled,
  }
}
