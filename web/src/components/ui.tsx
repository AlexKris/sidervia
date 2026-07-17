import * as Dialog from '@radix-ui/react-dialog'
import { AlertTriangle, Check, Copy, X } from 'lucide-react'
import { useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from 'react'
import { useTranslation } from 'react-i18next'
import styles from '../App.module.css'

export function Button({ variant = 'primary', className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'danger' | 'ghost' }) {
  return <button className={`${styles.button} ${styles[variant]} ${className}`} {...props} />
}

export function IconButton({ label, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return <button className={styles.iconButton} aria-label={label} title={label} {...props} />
}

export function PageHeader({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return <header className={styles.pageHeader}>
    <div><h1>{title}</h1><p>{description}</p></div>
    {action && <div className={styles.pageAction}>{action}</div>}
  </header>
}

export function Panel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`${styles.panel} ${className}`}>{children}</section>
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className={styles.field}><span>{label}</span>{children}{hint && <small>{hint}</small>}</label>
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={styles.input} {...props} />
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={styles.input} {...props} />
}

export function Checkbox({ label, checked, onChange, disabled }: { label: string; checked: boolean; onChange: (checked: boolean) => void; disabled?: boolean }) {
  return <label className={styles.checkbox}><input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} disabled={disabled} /><span>{label}</span></label>
}

export function Modal({ open, onOpenChange, title, description, children }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description?: string; children: ReactNode }) {
  const { t } = useTranslation()
  return <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Overlay className={styles.overlay} />
      <Dialog.Content className={styles.modal} onOpenAutoFocus={(event) => event.preventDefault()}>
        <div className={styles.modalHeader}>
          <div><Dialog.Title>{title}</Dialog.Title>{description && <Dialog.Description>{description}</Dialog.Description>}</div>
          <Dialog.Close asChild><IconButton label={t('common.close')}><X size={18} /></IconButton></Dialog.Close>
        </div>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
}

export function StatusBadge({ value, tone }: { value: string; tone?: 'good' | 'warn' | 'neutral' | 'bad' }) {
  return <span className={`${styles.badge} ${styles[tone || statusTone(value)]}`}>{value}</span>
}

function statusTone(value: string) {
  if (['active', 'enabled', 'success', 'healthy', 'ready'].includes(value.toLowerCase())) return 'good'
  if (['draft', 'planned'].includes(value.toLowerCase())) return 'warn'
  if (['revoked', 'failure'].includes(value.toLowerCase())) return 'bad'
  return 'neutral'
}

export function LoadingState() {
  const { t } = useTranslation()
  return <div className={styles.state}><span className={styles.spinner} />{t('common.loading')}</div>
}

export function EmptyState({ text }: { text?: string }) {
  const { t } = useTranslation()
  return <div className={styles.empty}>{text || t('common.empty')}</div>
}

export function ErrorPanel({ message }: { message: string }) {
  return <div className={styles.error}><AlertTriangle size={18} /><span>{message}</span></div>
}

export function CopyValue({ value }: { value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }
  return <div className={styles.copyValue}><code>{value}</code><IconButton label={copied ? t('common.copied') : t('common.copy')} onClick={copy}>{copied ? <Check size={17} /> : <Copy size={17} />}</IconButton></div>
}

export function FormActions({ submitting, onCancel, submitLabel }: { submitting?: boolean; onCancel: () => void; submitLabel?: string }) {
  const { t } = useTranslation()
  return <div className={styles.formActions}><Button type="button" variant="secondary" onClick={onCancel}>{t('common.cancel')}</Button><Button type="submit" disabled={submitting}>{submitLabel || t('common.save')}</Button></div>
}

export function formatDate(value?: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
