import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Power, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { AccountPage, ModelRoute, ModelRoutePage, ModelRouteRequest, RouteCandidate } from '../api/types'
import { Button, Checkbox, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

interface RouteForm {
  publicModelID: string
  description: string
  accountID: string
  upstreamModelID: string
  protocol: 'openai' | 'anthropic' | 'gemini' | 'xai'
  capabilities: string
  enabled: boolean
}

const emptyForm: RouteForm = { publicModelID: '', description: '', accountID: '', upstreamModelID: '', protocol: 'openai', capabilities: 'text, stream', enabled: true }

export default function ModelRoutesPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<RouteForm>(emptyForm)
  const query = useQuery({ queryKey: ['model-routes'], queryFn: () => api<ModelRoutePage>('/model-routes') })
  const accounts = useQuery({ queryKey: ['accounts'], queryFn: () => api<AccountPage>('/accounts') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['model-routes'] })
  const create = useMutation({ mutationFn: (body: ModelRouteRequest) => api<ModelRoute>('/model-routes', { method: 'POST', body }), onSuccess: async () => { setOpen(false); setForm(emptyForm); await refresh() } })
  const update = useMutation({ mutationFn: ({ item, enabled }: { item: ModelRoute; enabled: boolean }) => api<ModelRoute>(`/model-routes/${item.id}`, { method: 'PATCH', version: item.version, body: routeBody(item, enabled) }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (item: ModelRoute) => api<void>(`/model-routes/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || create.error || update.error || remove.error

  const openForm = () => { setForm({ ...emptyForm, accountID: accounts.data?.items[0]?.id || '' }); setOpen(true) }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const candidate: RouteCandidate = {
      account_id: form.accountID, upstream_model_id: form.upstreamModelID, enabled: true,
      protocols: [form.protocol], capabilities: form.capabilities.split(',').map((value) => value.trim()).filter(Boolean),
    }
    create.mutate({ public_model_id: form.publicModelID, description: form.description, enabled: form.enabled, confirm_multiple_candidates: false, candidates: [candidate] })
  }
  return <>
    <PageHeader title={t('routes.title')} description={t('routes.description')} action={<Button onClick={openForm} disabled={!accounts.data?.items.length}><Plus size={17} />{t('routes.add')}</Button>} />
    <div className={styles.infoStrip}>{t('routes.singleCandidate')}</div>
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('routes.publicModel')}</th><th>{t('routes.account')}</th><th>{t('routes.protocol')}</th><th>{t('routes.capabilities')}</th><th>{t('common.status')}</th><th>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.public_model_id}</strong><small>{item.description || '—'}</small></td>
          <td>{item.candidates.map((candidate) => accounts.data?.items.find((account) => account.id === candidate.account_id)?.name || candidate.account_id).join(', ')}</td>
          <td>{unique(item.candidates.flatMap((candidate) => candidate.protocols)).join(', ')}</td>
          <td>{unique(item.candidates.flatMap((candidate) => candidate.capabilities)).join(', ') || '—'}</td>
          <td><StatusBadge value={item.enabled ? t('common.active') : t('common.disabled')} tone={item.enabled ? 'good' : 'neutral'} /></td>
          <td className={styles.actionCell}><IconButton label={item.enabled ? t('common.disable') : t('common.enable')} onClick={() => update.mutate({ item, enabled: !item.enabled })}><Power size={17} /></IconButton><IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton></td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={setOpen} title={t('routes.add')} description={t('routes.description')}>
      <form className={styles.form} onSubmit={submit}>
        {create.error && <ErrorPanel message={formatAPIError(create.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('routes.publicModel')}><Input required maxLength={200} value={form.publicModelID} onChange={(event) => setForm({ ...form, publicModelID: event.target.value })} /></Field>
          <Field label={t('routes.descriptionField')}><Input maxLength={500} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></Field>
          <Field label={t('routes.account')}><Select required value={form.accountID} onChange={(event) => setForm({ ...form, accountID: event.target.value })}>{(accounts.data?.items || []).map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}</Select></Field>
          <Field label={t('routes.upstreamModel')}><Input required maxLength={200} value={form.upstreamModelID} onChange={(event) => setForm({ ...form, upstreamModelID: event.target.value })} /></Field>
          <Field label={t('routes.protocol')}><Select value={form.protocol} onChange={(event) => setForm({ ...form, protocol: event.target.value as RouteForm['protocol'] })}><option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option><option value="xai">xAI</option></Select></Field>
          <Field label={t('routes.capabilities')}><Input placeholder="text, stream, tools" value={form.capabilities} onChange={(event) => setForm({ ...form, capabilities: event.target.value })} /></Field>
        </div>
        <Checkbox label={t('common.enabled')} checked={form.enabled} onChange={(checked) => setForm({ ...form, enabled: checked })} />
        <FormActions submitting={create.isPending} onCancel={() => setOpen(false)} />
      </form>
    </Modal>
  </>
}

function routeBody(item: ModelRoute, enabled: boolean): ModelRouteRequest {
  return {
    public_model_id: item.public_model_id, description: item.description, enabled,
    confirm_multiple_candidates: item.candidates.length > 1, candidates: item.candidates,
  }
}

function unique(values: string[]) { return [...new Set(values)] }
