import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, KeyRound, Link2, Pencil, Plus, Power, ShieldCheck, Trash2 } from 'lucide-react'
import { FormEvent, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Account, AccountPage, AccountRequest, OAuthAttempt, OAuthCompletion, ProxyPage, UpstreamPage } from '../api/types'
import { Button, EmptyState, ErrorPanel, Field, FormActions, IconButton, Input, LoadingState, Modal, PageHeader, Panel, Select, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

const emptyForm: AccountRequest = {
  upstream_id: '', name: '', auth_kind: 'api_key', credential: '', billing_kind: 'subscription',
  status: 'draft', priority: 10, max_concurrency: 1,
}

export default function AccountsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [searchParams] = useSearchParams()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Account | null>(null)
  const [form, setForm] = useState<AccountRequest>(emptyForm)
  const [attempt, setAttempt] = useState<OAuthAttempt | null>(null)
  const [callbackURL, setCallbackURL] = useState('')
  const query = useQuery({ queryKey: ['accounts'], queryFn: () => api<AccountPage>('/accounts') })
  const upstreams = useQuery({ queryKey: ['upstreams'], queryFn: () => api<UpstreamPage>('/upstreams') })
  const proxies = useQuery({ queryKey: ['proxies'], queryFn: () => api<ProxyPage>('/proxies') })
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['accounts'] })
  const closeForm = () => { setOpen(false); setEditing(null); setForm(emptyForm) }
  const create = useMutation({
    mutationFn: (body: AccountRequest) => api<Account>('/accounts', { method: 'POST', body }),
    onSuccess: async () => { closeForm(); await refresh() },
  })
  const saveUpdate = useMutation({
    mutationFn: ({ item, body }: { item: Account; body: AccountRequest }) => api<Account>(`/accounts/${item.id}`, { method: 'PATCH', version: item.version, body }),
    onSuccess: async () => { closeForm(); await refresh() },
  })
  const toggle = useMutation({
    mutationFn: ({ item, status }: { item: Account; status: 'draft' | 'disabled' }) => api<Account>(`/accounts/${item.id}`, { method: 'PATCH', version: item.version, body: accountBody(item, status) }),
    onSuccess: refresh,
  })
  const validate = useMutation({
    mutationFn: (item: Account) => api<Account>(`/accounts/${item.id}/validate`, { method: 'POST', version: item.version }),
    onSuccess: refresh,
  })
  const beginOAuth = useMutation({
    mutationFn: (item: Account) => api<OAuthAttempt>('/oauth-attempts', { method: 'POST', body: { account_id: item.id } }),
    onSuccess: (value) => { setAttempt(value); setCallbackURL('') },
  })
  const completeOAuth = useMutation({
    mutationFn: () => api<OAuthCompletion>(`/oauth-attempts/${attempt!.id}/callback`, { method: 'POST', body: { callback_url: callbackURL } }),
    onSuccess: async () => { setAttempt(null); setCallbackURL(''); await refresh() },
  })
  const cancelOAuth = useMutation({
    mutationFn: () => api<void>(`/oauth-attempts/${attempt!.id}`, { method: 'DELETE' }),
    onSuccess: () => { setAttempt(null); setCallbackURL('') },
  })
  const remove = useMutation({
    mutationFn: (item: Account) => api<void>(`/accounts/${item.id}`, { method: 'DELETE', version: item.version }),
    onSuccess: refresh,
  })
  const error = query.error || create.error || saveUpdate.error || toggle.error || validate.error || beginOAuth.error || remove.error
  const selectedUpstream = upstreams.data?.items.find((item) => item.id === form.upstream_id)
  const availableUpstreams = (upstreams.data?.items || []).filter((item) => editing?.auth_kind !== 'oauth' || item.provider_id === 'google')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const body = { ...form }
    if (body.auth_kind === 'oauth') {
      delete body.credential
      delete body.credential_expires_at
    } else {
      if (editing && !body.credential) delete body.credential
      if (body.credential_expires_at) body.credential_expires_at = new Date(body.credential_expires_at).toISOString()
    }
    if (editing) {
      saveUpdate.mutate({ item: editing, body })
    } else {
      create.mutate(body)
    }
  }
  const openForm = () => {
    setEditing(null)
    setForm({ ...emptyForm, upstream_id: upstreams.data?.items[0]?.id || '' })
    setOpen(true)
  }
  const openEdit = (item: Account) => {
    setEditing(item)
    setForm({
      upstream_id: item.upstream_id, name: item.name, auth_kind: item.auth_kind,
      credential: item.auth_kind === 'api_key' ? '' : undefined,
      credential_expires_at: toDateTimeLocal(item.credential_expires_at), proxy_id: item.proxy_id,
      billing_kind: item.billing_kind,
      status: item.status === 'active' || item.status === 'disabled' ? item.status : 'draft',
      priority: item.priority, max_concurrency: item.max_concurrency,
    })
    setOpen(true)
  }
  const changeUpstream = (upstreamID: string) => {
    const upstream = upstreams.data?.items.find((item) => item.id === upstreamID)
    setForm({
      ...form, upstream_id: upstreamID,
      auth_kind: upstream?.provider_id === 'google' ? form.auth_kind : 'api_key',
      status: editing?.status === 'active' && upstreamID !== editing.upstream_id ? 'draft' : form.status,
    })
  }
  const changeAuthKind = (authKind: AccountRequest['auth_kind']) => {
    if (authKind === 'oauth') {
      setForm({ ...form, auth_kind: authKind, credential: undefined, credential_expires_at: undefined, billing_kind: 'metered', priority: 20, max_concurrency: 4 })
      return
    }
    setForm({ ...form, auth_kind: authKind, credential: '', billing_kind: 'subscription', priority: 10, max_concurrency: 1 })
  }

  return <>
    <PageHeader title={t('accounts.title')} description={t('accounts.description')} action={<Button onClick={openForm} disabled={!upstreams.data?.items.length}><Plus size={17} />{t('accounts.add')}</Button>} />
    <div className={styles.infoStrip}><KeyRound size={16} />{t('accounts.authBoundary')}</div>
    {searchParams.get('oauth_status') && <div className={styles.infoStrip}><ShieldCheck size={16} />{searchParams.get('oauth_status') === 'success' ? t('accounts.oauthSuccess') : t('accounts.oauthFailed')}</div>}
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <Panel>
      {query.isLoading ? <LoadingState /> : !query.data?.items.length ? <EmptyState /> : <div className={styles.tableWrap}><table className={styles.table}>
        <thead><tr><th>{t('common.name')}</th><th>{t('accounts.upstream')}</th><th>{t('accounts.authKind')}</th><th>{t('accounts.billing')}</th><th>{t('accounts.priority')}</th><th>{t('accounts.concurrency')}</th><th>{t('common.status')}</th><th>{t('common.actions')}</th></tr></thead>
        <tbody>{query.data.items.map((item) => <tr key={item.id}>
          <td><strong>{item.name}</strong><small>{item.credential_configured ? t('accounts.credentialConfigured') : '—'}</small></td>
          <td>{upstreams.data?.items.find((upstream) => upstream.id === item.upstream_id)?.name || item.upstream_id}</td>
          <td>{item.auth_kind === 'oauth' ? t('accounts.googleOAuth') : t('accounts.apiKey')}</td>
          <td>{t(`accounts.${item.billing_kind}`)}</td><td>{item.priority}</td><td>{item.max_concurrency}</td>
          <td><StatusBadge value={t(`common.${item.status}`)} tone={accountTone(item.status)} /></td>
          <td className={styles.actionCell}>
            {item.status !== 'active' && item.status !== 'disabled' && item.status !== 'validating' && (item.auth_kind === 'oauth'
              ? <IconButton label={t('accounts.connectOAuth')} onClick={() => beginOAuth.mutate(item)}><Link2 size={17} /></IconButton>
              : <IconButton label={t('accounts.validate')} onClick={() => validate.mutate(item)}><ShieldCheck size={17} /></IconButton>)}
            <IconButton label={t('common.edit')} onClick={() => openEdit(item)}><Pencil size={17} /></IconButton>
            <IconButton label={item.status === 'disabled' ? t('common.enable') : t('common.disable')} onClick={() => toggle.mutate({ item, status: item.status === 'disabled' ? 'draft' : 'disabled' })}><Power size={17} /></IconButton>
            <IconButton label={t('common.delete')} onClick={() => { if (window.confirm(t('common.confirmDelete'))) remove.mutate(item) }}><Trash2 size={17} /></IconButton>
          </td>
        </tr>)}</tbody>
      </table></div>}
    </Panel>
    <Modal open={open} onOpenChange={(next) => { if (!next) closeForm(); else setOpen(true) }} title={editing ? t('accounts.edit') : t('accounts.add')} description={t('accounts.description')}>
      <form className={styles.form} onSubmit={submit}>
        {(create.error || saveUpdate.error) && <ErrorPanel message={formatAPIError(create.error || saveUpdate.error, t)} />}
        <div className={styles.formGrid}>
          <Field label={t('accounts.upstream')}><Select required value={form.upstream_id} onChange={(event) => changeUpstream(event.target.value)}>{availableUpstreams.map((upstream) => <option value={upstream.id} key={upstream.id}>{upstream.name}</option>)}</Select></Field>
          <Field label={t('common.name')}><Input required maxLength={100} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
          <Field label={t('accounts.authKind')}><Select disabled={Boolean(editing)} value={form.auth_kind} onChange={(event) => changeAuthKind(event.target.value as AccountRequest['auth_kind'])}><option value="api_key">{t('accounts.apiKey')}</option>{selectedUpstream?.provider_id === 'google' && <option value="oauth">{t('accounts.googleOAuthBeta')}</option>}</Select></Field>
          {form.auth_kind === 'api_key' && <Field label={t('accounts.credential')} hint={editing ? t('accounts.credentialKeep') : undefined}><Input required={!editing} type="password" autoComplete="new-password" value={form.credential || ''} onChange={(event) => setForm({ ...form, credential: event.target.value, status: editing?.status === 'active' && event.target.value ? 'draft' : form.status })} /></Field>}
          <Field label={t('accounts.billing')}><Select value={form.billing_kind} onChange={(event) => {
            const billing = event.target.value as AccountRequest['billing_kind']
            setForm({ ...form, billing_kind: billing, priority: billing === 'subscription' ? 10 : 20, max_concurrency: billing === 'subscription' ? 1 : 4 })
          }}><option value="subscription">{t('accounts.subscription')}</option><option value="metered">{t('accounts.metered')}</option><option value="custom">{t('accounts.custom')}</option></Select></Field>
          <Field label={t('accounts.priority')}><Input type="number" min={0} value={form.priority ?? 10} onChange={(event) => setForm({ ...form, priority: Number(event.target.value) })} /></Field>
          <Field label={t('accounts.concurrency')}><Input type="number" min={1} max={1024} value={form.max_concurrency ?? 1} onChange={(event) => setForm({ ...form, max_concurrency: Number(event.target.value) })} /></Field>
          <Field label={t('accounts.proxy')}><Select value={form.proxy_id || ''} onChange={(event) => setForm({ ...form, proxy_id: event.target.value || undefined, status: editing?.status === 'active' && (event.target.value || undefined) !== editing.proxy_id ? 'draft' : form.status })}><option value="">{t('upstreams.noProxy')}</option>{(proxies.data?.items || []).map((proxy) => <option value={proxy.id} key={proxy.id}>{proxy.name}</option>)}</Select></Field>
          {form.auth_kind === 'api_key' && <Field label={t('accounts.expires')}><Input type="datetime-local" value={form.credential_expires_at || ''} onChange={(event) => setForm({ ...form, credential_expires_at: event.target.value || undefined })} /></Field>}
          <Field label={t('common.status')}><Select value={form.status} onChange={(event) => setForm({ ...form, status: event.target.value as AccountRequest['status'] })}>{editing?.status === 'active' && <option value="active">{t('common.active')}</option>}<option value="draft">{t('common.draft')}</option><option value="disabled">{t('common.disabled')}</option></Select></Field>
        </div>
        {form.auth_kind === 'oauth' && <p className={styles.muted}>{t('accounts.oauthCreateHint')}</p>}
        {editing?.status === 'active' && form.status === 'draft' && <p className={styles.muted}>{t('accounts.revalidateHint')}</p>}
        <FormActions submitting={create.isPending || saveUpdate.isPending} onCancel={closeForm} />
      </form>
    </Modal>
    <Modal open={attempt !== null} onOpenChange={(next) => { if (!next) setAttempt(null) }} title={t('accounts.oauthTitle')} description={t('accounts.oauthInstructions')}>
      {attempt && <div className={styles.form}>
        {(completeOAuth.error || cancelOAuth.error) && <ErrorPanel message={formatAPIError(completeOAuth.error || cancelOAuth.error, t)} />}
        <Button type="button" onClick={() => window.open(attempt.authorization_url, '_blank', 'noopener,noreferrer')} disabled={!attempt.authorization_url}><ExternalLink size={17} />{t('accounts.openAuthorization')}</Button>
        <form className={styles.form} onSubmit={(event) => { event.preventDefault(); completeOAuth.mutate() }}>
          <Field label={t('accounts.callbackURL')} hint={t('accounts.callbackHint')}><Input type="url" required value={callbackURL} onChange={(event) => setCallbackURL(event.target.value)} /></Field>
          <div className={styles.formActions}><Button type="button" variant="secondary" onClick={() => cancelOAuth.mutate()}>{t('common.cancel')}</Button><Button type="submit" disabled={completeOAuth.isPending}>{t('accounts.completeOAuth')}</Button></div>
        </form>
      </div>}
    </Modal>
  </>
}

function accountBody(item: Account, status: 'draft' | 'disabled'): AccountRequest {
  return {
    upstream_id: item.upstream_id, name: item.name, auth_kind: item.auth_kind, billing_kind: item.billing_kind,
    credential_expires_at: item.credential_expires_at, proxy_id: item.proxy_id,
    status, priority: item.priority, max_concurrency: item.max_concurrency,
  }
}

function accountTone(status: Account['status']): 'good' | 'warn' | 'neutral' | 'bad' {
  if (status === 'active') return 'good'
  if (status === 'invalid' || status === 'reauth_required') return 'bad'
  if (status === 'draft' || status === 'validating') return 'warn'
  return 'neutral'
}

function toDateTimeLocal(value?: string) {
  if (!value) return undefined
  const date = new Date(value)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}
