import { FormEvent, useState } from 'react'
import { Boxes, Languages, LockKeyhole, ShieldCheck } from 'lucide-react'
import { Navigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth/AuthContext'
import { setLocale } from '../i18n'
import { Button, ErrorPanel, Field, Input } from '../components/ui'
import styles from '../App.module.css'

export default function LoginPage() {
  const { t, i18n } = useTranslation()
  const { session, login } = useAuth()
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  if (session) return <Navigate to="/" replace />

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      await login(password, code)
    } catch {
      setError(t('login.failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return <main className={styles.loginPage}>
    <button className={styles.loginLanguage} onClick={() => void setLocale(i18n.language === 'en' ? 'zh-CN' : 'en')}><Languages size={16} />{i18n.language === 'en' ? '简中' : 'EN'}</button>
    <section className={styles.loginCard}>
      <div className={styles.loginBrand}><div className={styles.brandMarkLarge}><Boxes size={28} /></div><div><strong>Sidervia</strong><span>{t('brand.foundation')}</span></div></div>
      <div className={styles.loginIntro}><div className={styles.eyebrow}><ShieldCheck size={15} />Local control plane</div><h1>{t('login.title')}</h1><p>{t('login.subtitle')}</p></div>
      {error && <ErrorPanel message={error} />}
      <form className={styles.form} onSubmit={submit}>
        <Field label={t('login.password')}><Input autoFocus type="password" autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} /></Field>
        <Field label={t('login.totp')} hint={t('login.totpHint')}><Input type="text" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></Field>
        <Button className={styles.fullButton} type="submit" disabled={submitting}><LockKeyhole size={17} />{submitting ? t('common.loading') : t('login.submit')}</Button>
      </form>
      <p className={styles.loginFooter}>{t('login.footer')}</p>
    </section>
  </main>
}
