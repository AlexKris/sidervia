import { useMutation, useQuery } from '@tanstack/react-query'
import { KeyRound, ShieldCheck, ShieldOff } from 'lucide-react'
import QRCode from 'qrcode'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, formatAPIError } from '../api/client'
import type { Session, SystemHealth, TOTPSetup } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Button, CopyValue, ErrorPanel, Field, Input, PageHeader, Panel, StatusBadge } from '../components/ui'
import styles from '../App.module.css'

export default function SettingsPage() {
  const { t } = useTranslation()
  const { session, replaceSession } = useAuth()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [setupPassword, setSetupPassword] = useState('')
  const [disablePassword, setDisablePassword] = useState('')
  const [code, setCode] = useState('')
  const [setup, setSetup] = useState<TOTPSetup | null>(null)
  const [qrCode, setQRCode] = useState('')
  const health = useQuery({ queryKey: ['system-health'], queryFn: () => api<SystemHealth>('/system/health') })
  const passwordMutation = useMutation({
    mutationFn: () => api<Session>('/auth/password', { method: 'PUT', body: { current_password: currentPassword, new_password: newPassword } }),
    onSuccess: (next) => { replaceSession(next); setCurrentPassword(''); setNewPassword('') },
  })
  const setupMutation = useMutation({
    mutationFn: () => api<TOTPSetup>('/auth/totp/setup', { method: 'POST', body: { password: setupPassword } }),
    onSuccess: async (value) => { setSetup(value); setQRCode(await QRCode.toDataURL(value.uri, { width: 220, margin: 1, color: { dark: '#0b1019', light: '#ffffff' } })); setSetupPassword('') },
  })
  const confirmMutation = useMutation({
    mutationFn: () => api<Session>('/auth/totp/confirm', { method: 'POST', body: { code } }),
    onSuccess: (next) => { replaceSession(next); setSetup(null); setQRCode(''); setCode('') },
  })
  const disableMutation = useMutation({
    mutationFn: () => api<Session>('/auth/totp', { method: 'DELETE', body: { password: disablePassword, code } }),
    onSuccess: (next) => { replaceSession(next); setDisablePassword(''); setCode('') },
  })
  const error = passwordMutation.error || setupMutation.error || confirmMutation.error || disableMutation.error || health.error

  const changePassword = (event: FormEvent) => { event.preventDefault(); passwordMutation.mutate() }
  const beginSetup = (event: FormEvent) => { event.preventDefault(); setupMutation.mutate() }
  const confirmSetup = (event: FormEvent) => { event.preventDefault(); confirmMutation.mutate() }
  const disable = (event: FormEvent) => { event.preventDefault(); disableMutation.mutate() }
  return <>
    <PageHeader title={t('settings.title')} description={t('settings.description')} />
    {error && <ErrorPanel message={formatAPIError(error, t)} />}
    <div className={styles.settingsGrid}>
      <Panel>
        <div className={styles.panelTitle}><div className={styles.sectionIcon}><KeyRound size={20} /></div><div><h2>{t('settings.passwordTitle')}</h2><p>{t('settings.passwordRule')}</p></div></div>
        <form className={styles.form} onSubmit={changePassword}>
          <Field label={t('settings.currentPassword')}><Input type="password" autoComplete="current-password" required value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></Field>
          <Field label={t('settings.newPassword')} hint={t('settings.passwordRule')}><Input type="password" autoComplete="new-password" minLength={14} required value={newPassword} onChange={(event) => setNewPassword(event.target.value)} /></Field>
          <Button type="submit" disabled={passwordMutation.isPending}>{passwordMutation.isPending ? t('common.loading') : t('settings.changePassword')}</Button>
        </form>
      </Panel>
      <Panel>
        <div className={styles.panelTitle}><div className={styles.sectionIcon}>{session?.totp_enabled ? <ShieldCheck size={20} /> : <ShieldOff size={20} />}</div><div><h2>{t('settings.totpTitle')}</h2><StatusBadge value={session?.totp_enabled ? t('settings.totpEnabled') : t('settings.totpDisabled')} tone={session?.totp_enabled ? 'good' : 'warn'} /></div></div>
        {!session?.totp_enabled && !setup && <form className={styles.form} onSubmit={beginSetup}><Field label={t('settings.setupPassword')}><Input type="password" autoComplete="current-password" required value={setupPassword} onChange={(event) => setSetupPassword(event.target.value)} /></Field><Button type="submit" disabled={setupMutation.isPending}>{t('settings.setup')}</Button></form>}
        {!session?.totp_enabled && setup && <form className={styles.form} onSubmit={confirmSetup}>
          <p className={styles.muted}>{t('settings.scan')}</p>{qrCode && <img className={styles.qrCode} src={qrCode} alt="TOTP QR code" />}<CopyValue value={setup.secret} />
          <Field label={t('settings.code')}><Input autoFocus inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></Field>
          <Button type="submit" disabled={confirmMutation.isPending}>{t('settings.confirm')}</Button>
        </form>}
        {session?.totp_enabled && <form className={styles.form} onSubmit={disable}><p className={styles.warningText}>{t('settings.disableWarning')}</p><Field label={t('settings.currentPassword')}><Input type="password" autoComplete="current-password" required value={disablePassword} onChange={(event) => setDisablePassword(event.target.value)} /></Field><Field label={t('settings.code')}><Input inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></Field><Button type="submit" variant="danger" disabled={disableMutation.isPending}>{t('settings.disable')}</Button></form>}
      </Panel>
      <Panel className={styles.buildPanel}>
        <h2>{t('settings.buildTitle')}</h2><dl>
          <div><dt>{t('common.version')}</dt><dd>{health.data?.build.version || '—'}</dd></div><div><dt>{t('settings.commit')}</dt><dd><code>{health.data?.build.commit || '—'}</code></dd></div>
          <div><dt>{t('settings.goVersion')}</dt><dd>{health.data?.build.go_version || '—'}</dd></div><div><dt>{t('settings.buildTime')}</dt><dd>{health.data?.build.build_time || '—'}</dd></div>
        </dl>
      </Panel>
    </div>
  </>
}
