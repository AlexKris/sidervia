import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { Activity, Boxes, Cable, ChevronDown, CircleDollarSign, FileClock, KeyRound, Languages, LogOut, Network, Route, Settings, ShieldCheck, Waypoints } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { setLocale } from '../i18n'
import styles from '../App.module.css'

const navigation = [
  { to: '/', key: 'dashboard', icon: Activity, end: true },
  { to: '/accounts', key: 'accounts', icon: Boxes },
  { to: '/upstreams', key: 'upstreams', icon: Waypoints },
  { to: '/proxies', key: 'proxies', icon: Network },
  { to: '/model-routes', key: 'routes', icon: Route },
  { to: '/client-keys', key: 'keys', icon: KeyRound },
  { to: '/usage', key: 'usage', icon: CircleDollarSign },
  { to: '/price-catalog', key: 'pricing', icon: Cable },
  { to: '/audit', key: 'audit', icon: FileClock },
  { to: '/settings', key: 'settings', icon: Settings },
] as const

export default function AppLayout() {
  const { t, i18n } = useTranslation()
  const { logout } = useAuth()
  return <div className={styles.shell}>
    <aside className={styles.sidebar}>
      <div className={styles.brand}>
        <div className={styles.brandMark}><Boxes size={23} /></div>
        <div><strong>Sidervia</strong><span>{t('brand.foundation')}</span></div>
      </div>
      <nav className={styles.navigation} aria-label="Primary">
        {navigation.map(({ to, key, icon: Icon }) => <NavLink key={to} to={to} end={to === '/'} className={({ isActive }) => isActive ? styles.navActive : styles.navItem}>
          <Icon size={18} /><span>{t(`nav.${key}`)}</span>
        </NavLink>)}
      </nav>
      <div className={styles.sidebarFoot}><ShieldCheck size={16} /><span>{t('common.securityNotice')}</span></div>
    </aside>
    <div className={styles.mainColumn}>
      <header className={styles.topbar}>
        <div className={styles.mobileBrand}><div className={styles.brandMark}><Boxes size={20} /></div><strong>Sidervia</strong></div>
        <span className={styles.topbarSubtitle}>{t('brand.subtitle')}</span>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className={styles.languageTrigger}><Languages size={16} /><span>{i18n.language === 'en' ? 'EN' : '简中'}</span><ChevronDown size={14} /></DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content className={styles.dropdown} align="end" sideOffset={8}>
              <DropdownMenu.Item className={styles.dropdownItem} onSelect={() => void setLocale('zh-CN')}>简体中文</DropdownMenu.Item>
              <DropdownMenu.Item className={styles.dropdownItem} onSelect={() => void setLocale('en')}>English</DropdownMenu.Item>
              <DropdownMenu.Separator className={styles.dropdownSeparator} />
              <DropdownMenu.Item className={styles.dropdownItemDanger} onSelect={() => void logout()}><LogOut size={15} />{t('nav.signOut')}</DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </header>
      <nav className={styles.mobileNav} aria-label="Mobile primary">
        {navigation.slice(0, 6).map(({ to, key, icon: Icon }) => <NavLink key={to} to={to} end={to === '/'} className={({ isActive }) => isActive ? styles.mobileNavActive : styles.mobileNavItem}><Icon size={18} /><span>{t(`nav.${key}`)}</span></NavLink>)}
      </nav>
      <main className={styles.content}><Outlet /></main>
    </div>
  </div>
}
