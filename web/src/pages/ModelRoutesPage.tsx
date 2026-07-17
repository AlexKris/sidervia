import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Power, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { AccountPage, ModelRoute, ModelRoutePage, ModelRouteRequest, RouteCandidate, UpstreamPage } from '../api/types'
import { Button, Checkbox, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

type RouteProtocol = 'openai' | 'anthropic' | 'gemini' | 'xai'

interface CandidateForm {
  accountID: string
  upstreamModelID: string
  protocol: RouteProtocol
  stream: boolean
  enabled: boolean
}

interface RouteForm {
  publicModelID: string
  description: string
  enabled: boolean
  candidates: CandidateForm[]
}

export default function ModelRoutesPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<ModelRoute | null>(null)
  const [form, setForm] = useState<RouteForm>(() => emptyRouteForm())
  const query = useQuery({ queryKey: ['model-routes'], queryFn: () => api<ModelRoutePage>('/model-routes') })
  const accounts = useQuery({ queryKey: ['accounts'], queryFn: () => api<AccountPage>('/accounts') })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamPage>('/upstreams') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['model-routes'] })
  const closeForm = () => { setOpen(false); setEditing(null); setForm(emptyRouteForm()) }
  const save = useMutation({
    mutationFn: ({ item, body }: { item: ModelRoute | null; body: ModelRouteRequest }) => item
      ? api<ModelRoute>(`/model-routes/${item.id}`, { method: 'PATCH', version: item.version, body })
      : api<ModelRoute>('/model-routes', { method: 'POST', body }),
    onSuccess: async () => { closeForm(); await refresh() },
  })
  const toggle = useMutation({ mutationFn: ({ item, enabled }: { item: ModelRoute; enabled: boolean }) => api<ModelRoute>(`/model-routes/${item.id}`, { method: 'PATCH', version: item.version, body: routeBody(item, enabled) }), onSuccess: refresh })
  const remove = useMutation({ mutationFn: (item: ModelRoute) => api<void>(`/model-routes/${item.id}`, { method: 'DELETE', version: item.version }), onSuccess: refresh })
  const error = query.error || accounts.error || upstreams.error || save.error || toggle.error || remove.error

  const providerForAccount = (accountID: string) => {
    const account = accounts.data?.items.find((item) => item.id === accountID)
    return upstreams.data?.items.find((item) => item.id === account?.upstream_id)?.provider_id || ''
  }
  const protocolOptions = (accountID: string) => protocolsForProvider(providerForAccount(accountID))
  const firstAccountID = () => accounts.data?.items[0]?.id || ''

  const openCreate = () => {
    const accountID = firstAccountID()
    setEditing(null)
    setForm({ ...emptyRouteForm(), candidates: [emptyCandidate(accountID, protocolOptions(accountID)[0])] })
    setOpen(true)
  }
  const openEdit = (item: ModelRoute) => {
    setEditing(item)
    setForm({
      publicModelID: item.public_model_id,
      description: item.description,
      enabled: item.enabled,
      candidates: item.candidates.map((candidate) => ({
        accountID: candidate.account_id,
        upstreamModelID: candidate.upstream_model_id,
        protocol: (candidate.protocols[0] || protocolOptions(candidate.account_id)[0]) as RouteProtocol,
        stream: candidate.capabilities.includes('stream'),
        enabled: candidate.enabled,
      })),
    })
    setOpen(true)
  }
  const updateCandidate = (index: number, update: Partial<CandidateForm>) => {
    setForm((current) => ({
      ...current,
      candidates: current.candidates.map((candidate, candidateIndex) => candidateIndex === index ? { ...candidate, ...update } : candidate),
    }))
  }
  const changeCandidateAccount = (index: number, accountID: string) => {
    updateCandidate(index, { accountID, protocol: protocolOptions(accountID)[0] })
  }
  const addCandidate = () => {
    const used = new Set(form.candidates.map((candidate) => candidate.accountID))
    const accountID = accounts.data?.items.find((account) => !used.has(account.id))?.id || firstAccountID()
    setForm((current) => ({ ...current, candidates: [...current.candidates, emptyCandidate(accountID, protocolOptions(accountID)[0])] }))
  }
  const removeCandidate = (index: number) => {
    if (form.candidates.length <= 1) return
    setForm((current) => ({ ...current, candidates: current.candidates.filter((_, candidateIndex) => candidateIndex !== index) }))
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const candidates: RouteCandidate[] = form.candidates.map((candidate) => ({
      account_id: candidate.accountID,
      upstream_model_id: candidate.upstreamModelID,
      enabled: candidate.enabled,
      protocols: [candidate.protocol],
      capabilities: candidate.stream ? ['text', 'stream'] : ['text'],
    }))
    save.mutate({
      item: editing,
      body: {
        public_model_id: form.publicModelID,
        description: form.description,
        enabled: form.enabled,
        confirm_multiple_candidates: candidates.length > 1,
        candidates,
      },
    })
  }

  return <>
    <PageHeader title={t('routes.title')} description={t('routes.description')} action={<Button onClick={openCreate} disabled={!accounts.data?.items.length || !upstreams.data?.items.length}><Plus size={17} />{t('routes.add')}</Button>} />
    <div className={styles.infoStrip}>{t('routes.poolHint')}</div>
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
          <td className={styles.actionCell}>
            <IconButton label={t('common.edit')} onClick={() => openEdit(item)}><Pencil size={17} /></IconButton>
            <IconButton label={item.enabled ? t('common.disable') : t('common.enable')} onClick={() => toggle.mutate({ item, enabled: !item.enabled })}><Power size={17} /></IconButton>
            <IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton>
          </td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={(next) => { if (!next) closeForm(); else setOpen(true) }} title={editing ? t('routes.edit') : t('routes.add')} description={t('routes.description')}>
      <form className={styles.form} onSubmit={submit}>
        {save.error && <ErrorPanel message={formatAPIError(save.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('routes.publicModel')}><Input required maxLength={200} value={form.publicModelID} onChange={(event) => setForm({ ...form, publicModelID: event.target.value })} /></Field>
          <Field label={t('routes.descriptionField')}><Input maxLength={500} value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></Field>
        </div>
        <div className={styles.candidateList}>{form.candidates.map((candidate, index) => <div className={styles.candidateCard} key={index}>
          <div className={styles.candidateHeader}><strong>{t('routes.candidateNumber', { number: index + 1 })}</strong><IconButton type="button" disabled={form.candidates.length <= 1} label={t('routes.removeCandidate')} onClick={() => removeCandidate(index)}><Trash2 size={16} /></IconButton></div>
          <div className={styles.formGrid}>
            <Field label={t('routes.account')}><Select required value={candidate.accountID} onChange={(event) => changeCandidateAccount(index, event.target.value)}>{(accounts.data?.items || []).map((account) => <option key={account.id} value={account.id}>{account.name} · {t(`common.${account.status}`)}</option>)}</Select></Field>
            <Field label={t('routes.upstreamModel')}><Input required maxLength={200} value={candidate.upstreamModelID} onChange={(event) => updateCandidate(index, { upstreamModelID: event.target.value })} /></Field>
            <Field label={t('routes.protocol')}><Select value={candidate.protocol} onChange={(event) => updateCandidate(index, { protocol: event.target.value as RouteProtocol })}>{protocolOptions(candidate.accountID).map((protocol) => <option value={protocol} key={protocol}>{protocolLabel(protocol)}</option>)}</Select></Field>
          </div>
          <div className={styles.candidateOptions}><Checkbox label={t('routes.stream')} checked={candidate.stream} onChange={(stream) => updateCandidate(index, { stream })} /><Checkbox label={t('routes.candidateEnabled')} checked={candidate.enabled} onChange={(enabled) => updateCandidate(index, { enabled })} /></div>
        </div>)}</div>
        <Button type="button" variant="secondary" onClick={addCandidate} disabled={!accounts.data?.items.length}><Plus size={16} />{t('routes.addCandidate')}</Button>
        <Checkbox label={t('common.enabled')} checked={form.enabled} onChange={(enabled) => setForm({ ...form, enabled })} />
        {form.candidates.length > 1 && <p className={styles.muted}>{t('routes.multipleConfirmed')}</p>}
        <FormActions submitting={save.isPending} onCancel={closeForm} />
      </form>
    </Modal>
  </>
}

function emptyRouteForm(): RouteForm {
  return { publicModelID: '', description: '', enabled: true, candidates: [emptyCandidate()] }
}

function emptyCandidate(accountID = '', protocol: RouteProtocol = 'openai'): CandidateForm {
  return { accountID, upstreamModelID: '', protocol, stream: true, enabled: true }
}

function protocolsForProvider(providerID: string): RouteProtocol[] {
  switch (providerID) {
    case 'anthropic': return ['anthropic']
    case 'google': return ['gemini']
    case 'xai': return ['xai', 'openai']
    default: return ['openai']
  }
}

function protocolLabel(protocol: RouteProtocol) {
  if (protocol === 'anthropic') return 'Anthropic'
  if (protocol === 'gemini') return 'Gemini'
  if (protocol === 'xai') return 'xAI'
  return 'OpenAI'
}

function routeBody(item: ModelRoute, enabled: boolean): ModelRouteRequest {
  return {
    public_model_id: item.public_model_id,
    description: item.description,
    enabled,
    confirm_multiple_candidates: item.candidates.length > 1,
    candidates: item.candidates,
  }
}

function unique(values: string[]) { return [...new Set(values)] }
