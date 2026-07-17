import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

const resources = {
  'zh-CN': {
    translation: {
      common: {
        add: '新建', save: '保存', cancel: '取消', delete: '删除', revoke: '吊销', disable: '禁用', enable: '启用',
        name: '名称', status: '状态', actions: '操作', loading: '加载中…', empty: '暂无数据', close: '关闭', copy: '复制',
        copied: '已复制', optional: '可选', required: '必填', version: '版本', planned: '计划中', active: '启用', disabled: '禁用',
        draft: '草稿', enabled: '已启用', createdAt: '创建时间', never: '从未', confirmDelete: '此操作不可撤销，确定继续吗？',
        requestFailed: '请求失败', securityNotice: '敏感信息不会写入浏览器存储。', details: '详情', yes: '是', no: '否',
      },
      brand: { subtitle: '多提供商 AI 网关控制平面', foundation: 'v0.1 基础版' },
      nav: {
        dashboard: '概览', accounts: '账号池', upstreams: '上游', proxies: '出站代理', routes: '模型路由', keys: 'Client Keys',
        usage: '用量与成本', pricing: '价格目录', audit: '审计事件', settings: '安全设置', signOut: '退出登录',
      },
      login: {
        title: '登录 Sidervia', subtitle: '使用本机管理员密码进入控制平面。', password: '管理员密码', totp: 'TOTP 验证码',
        totpHint: '如尚未启用，可留空', submit: '安全登录', failed: '登录失败，请检查凭据或稍后重试。', footer: '同源 Session · CSRF 防护 · 不保存凭据',
      },
      dashboard: {
        title: '控制平面概览', description: '本机资源、安全状态和当前实现边界。', ready: '数据库就绪', totp: '管理员 TOTP',
        configured: '已配置资源', attention: '需要处理', healthy: '运行正常', totpOn: '已启用', totpOff: '未启用',
        noWarnings: '当前没有安全提醒。', providersWarning: '真实 Provider 调用尚未实现；当前资源保持草稿或禁用。',
        totpWarning: '建议立即在“安全设置”中启用 TOTP。', scopeTitle: 'v0.1 能力边界',
        scopeBody: '本版本提供安全管理面、账号池结构、路由配置、备份和密钥轮换。OpenAI、Claude、Gemini、Grok 的真实转发将在官方协议契约测试完成后逐步启用。',
      },
      proxies: {
        title: '出站代理', description: '管理账号与上游使用的固定出口；密码字段始终只写。', add: '新建代理', scheme: '协议', host: '主机', port: '端口',
        username: '用户名', password: '密码', tlsName: 'TLS Server Name', insecure: '允许不安全 TLS', secretConfigured: '凭据已配置',
        secretHint: '编辑时留空表示保留现有值。',
      },
      upstreams: {
        title: '上游', description: 'Provider 描述和受控 Base URL；私网目标必须显式确认。', add: '新建上游', provider: 'Provider', baseUrl: 'Base URL',
        proxy: '默认代理', private: '允许访问私网地址', noProxy: '不使用代理', plannedHint: 'Provider 适配器当前均为计划状态。',
      },
      accounts: {
        title: '账号池', description: 'v0.1 仅接受 API Key，并强制保持草稿或禁用。', add: '添加账号', upstream: '上游', credential: 'API Key',
        billing: '计费类型', priority: '优先级', concurrency: '最大并发', proxy: '账号代理', expires: '凭据到期时间', subscription: '订阅',
        metered: '按量', custom: '自定义', credentialConfigured: '凭据已配置', apiKeyOnly: '仅 API Key',
      },
      routes: {
        title: '模型路由', description: '把公开模型 ID 映射到明确账号候选；当前不执行真实流量。', add: '新建路由', publicModel: '公开模型 ID',
        account: '候选账号', upstreamModel: '上游模型 ID', protocol: '协议', capabilities: '能力', descriptionField: '说明',
        confirmMultiple: '我确认多个候选可被分组调度', singleCandidate: 'v0.1 表单先创建一个候选；后续可扩展多候选编辑。',
      },
      keys: {
        title: 'Client Keys', description: '下游调用身份；完整密钥只在创建成功后显示一次。', add: '创建 Client Key', prefix: '前缀', expires: '到期时间',
        lastUsed: '最后使用', secretTitle: '立即保存此 Client Key', secretWarning: '关闭后无法再次查看。请现在复制到密码管理器。',
        revokeConfirm: '吊销后不可恢复，确定吊销此 Client Key 吗？',
      },
      audit: {
        title: '审计事件', description: '仅记录控制面元数据，不记录提示词、响应正文或凭证明文。', event: '事件', actor: '操作者', target: '目标', outcome: '结果', time: '时间',
      },
      settings: {
        title: '安全设置', description: '管理密码、TOTP 与当前构建信息。安全变更会轮换当前 Session。', passwordTitle: '修改管理员密码',
        currentPassword: '当前密码', newPassword: '新密码', changePassword: '修改密码', passwordRule: '至少 14 个 Unicode 字符。',
        totpTitle: '双因素认证（TOTP）', totpEnabled: 'TOTP 已启用', totpDisabled: 'TOTP 未启用', setup: '开始设置', setupPassword: '先输入当前密码',
        scan: '使用认证器扫描二维码，或手工输入 Secret。', code: '6 位验证码', confirm: '确认启用', disable: '关闭 TOTP',
        disableWarning: '关闭 TOTP 会降低管理端安全性，并吊销其他 Session。', buildTitle: '构建信息', commit: 'Commit', goVersion: 'Go 版本', buildTime: '构建时间',
      },
      planned: {
        usageTitle: '用量与成本', usageBody: '请求元数据、用量明细和成本管线会随真实 Provider 转发能力一起交付。当前不会生成虚假统计。',
        pricingTitle: '价格目录', pricingBody: '价格规则、发布历史和冲突校验属于后续里程碑；v0.1 不提供计费或余额。',
      },
      errors: { unauthorized: '会话已过期，请重新登录。', version_conflict: '资源已被其他操作修改，请刷新后重试。', resource_in_use: '资源仍被其他对象引用。' },
    },
  },
  en: {
    translation: {
      common: {
        add: 'Create', save: 'Save', cancel: 'Cancel', delete: 'Delete', revoke: 'Revoke', disable: 'Disable', enable: 'Enable',
        name: 'Name', status: 'Status', actions: 'Actions', loading: 'Loading…', empty: 'No data yet', close: 'Close', copy: 'Copy',
        copied: 'Copied', optional: 'Optional', required: 'Required', version: 'Version', planned: 'Planned', active: 'Active', disabled: 'Disabled',
        draft: 'Draft', enabled: 'Enabled', createdAt: 'Created', never: 'Never', confirmDelete: 'This cannot be undone. Continue?',
        requestFailed: 'Request failed', securityNotice: 'Sensitive values are never stored in browser storage.', details: 'Details', yes: 'Yes', no: 'No',
      },
      brand: { subtitle: 'Multi-provider AI gateway control plane', foundation: 'v0.1 Foundation' },
      nav: {
        dashboard: 'Overview', accounts: 'Account pool', upstreams: 'Upstreams', proxies: 'Egress proxies', routes: 'Model routes', keys: 'Client Keys',
        usage: 'Usage & cost', pricing: 'Price catalog', audit: 'Audit events', settings: 'Security', signOut: 'Sign out',
      },
      login: {
        title: 'Sign in to Sidervia', subtitle: 'Use the local administrator password to open the control plane.', password: 'Administrator password', totp: 'TOTP code',
        totpHint: 'Leave empty until TOTP is enabled', submit: 'Sign in securely', failed: 'Sign-in failed. Check the credentials or try again later.', footer: 'Same-origin session · CSRF protected · Credentials never stored',
      },
      dashboard: {
        title: 'Control plane overview', description: 'Local resources, security posture, and current implementation boundaries.', ready: 'Database ready', totp: 'Admin TOTP',
        configured: 'Configured resources', attention: 'Needs attention', healthy: 'Healthy', totpOn: 'Enabled', totpOff: 'Not enabled',
        noWarnings: 'No security warnings right now.', providersWarning: 'Real Provider calls are not implemented yet; resources remain draft or disabled.',
        totpWarning: 'Enable TOTP from Security as soon as possible.', scopeTitle: 'v0.1 capability boundary',
        scopeBody: 'This release provides a secure control plane, account-pool structure, route configuration, backup, and key rotation. Real OpenAI, Claude, Gemini, and Grok forwarding will be enabled only after official-protocol contract tests pass.',
      },
      proxies: {
        title: 'Egress proxies', description: 'Manage stable egress for accounts and upstreams. Password fields are always write-only.', add: 'Create proxy', scheme: 'Scheme', host: 'Host', port: 'Port',
        username: 'Username', password: 'Password', tlsName: 'TLS Server Name', insecure: 'Allow insecure TLS', secretConfigured: 'Credentials configured',
        secretHint: 'Leave secret fields empty while editing to retain their values.',
      },
      upstreams: {
        title: 'Upstreams', description: 'Provider descriptors and controlled base URLs. Private targets require explicit confirmation.', add: 'Create upstream', provider: 'Provider', baseUrl: 'Base URL',
        proxy: 'Default proxy', private: 'Allow private-network addresses', noProxy: 'No proxy', plannedHint: 'All Provider adapters are currently planned.',
      },
      accounts: {
        title: 'Account pool', description: 'v0.1 accepts API keys only and keeps every account draft or disabled.', add: 'Add account', upstream: 'Upstream', credential: 'API Key',
        billing: 'Billing kind', priority: 'Priority', concurrency: 'Max concurrency', proxy: 'Account proxy', expires: 'Credential expiry', subscription: 'Subscription',
        metered: 'Metered', custom: 'Custom', credentialConfigured: 'Credential configured', apiKeyOnly: 'API Key only',
      },
      routes: {
        title: 'Model routes', description: 'Map public model IDs to explicit account candidates. No real traffic is executed yet.', add: 'Create route', publicModel: 'Public model ID',
        account: 'Candidate account', upstreamModel: 'Upstream model ID', protocol: 'Protocol', capabilities: 'Capabilities', descriptionField: 'Description',
        confirmMultiple: 'I confirm multiple candidates may be scheduled as a group', singleCandidate: 'The v0.1 form creates one candidate first; multi-candidate editing follows.',
      },
      keys: {
        title: 'Client Keys', description: 'Downstream identities. The complete key appears only once after creation.', add: 'Create Client Key', prefix: 'Prefix', expires: 'Expires',
        lastUsed: 'Last used', secretTitle: 'Save this Client Key now', secretWarning: 'It cannot be shown again after this dialog closes. Copy it to a password manager now.',
        revokeConfirm: 'Revocation is permanent. Revoke this Client Key?',
      },
      audit: {
        title: 'Audit events', description: 'Control-plane metadata only—never prompts, response bodies, or plaintext credentials.', event: 'Event', actor: 'Actor', target: 'Target', outcome: 'Outcome', time: 'Time',
      },
      settings: {
        title: 'Security settings', description: 'Manage password, TOTP, and build information. Security changes rotate the current session.', passwordTitle: 'Change administrator password',
        currentPassword: 'Current password', newPassword: 'New password', changePassword: 'Change password', passwordRule: 'At least 14 Unicode characters.',
        totpTitle: 'Two-factor authentication (TOTP)', totpEnabled: 'TOTP is enabled', totpDisabled: 'TOTP is not enabled', setup: 'Start setup', setupPassword: 'Enter the current password first',
        scan: 'Scan the QR code with an authenticator, or enter the secret manually.', code: '6-digit code', confirm: 'Confirm and enable', disable: 'Disable TOTP',
        disableWarning: 'Disabling TOTP weakens administrator security and revokes other sessions.', buildTitle: 'Build information', commit: 'Commit', goVersion: 'Go version', buildTime: 'Build time',
      },
      planned: {
        usageTitle: 'Usage & cost', usageBody: 'Request metadata, usage line items, and cost processing ship with real Provider forwarding. This release does not manufacture statistics.',
        pricingTitle: 'Price catalog', pricingBody: 'Price rules, publication history, and conflict checks are a later milestone. v0.1 has no billing or balances.',
      },
      errors: { unauthorized: 'Your session expired. Sign in again.', version_conflict: 'This resource changed elsewhere. Refresh and retry.', resource_in_use: 'This resource is still referenced by another object.' },
    },
  },
} as const

const savedLocale = window.localStorage.getItem('sidervia.locale')
const locale = savedLocale === 'en' || savedLocale === 'zh-CN' ? savedLocale : 'zh-CN'

void i18n.use(initReactI18next).init({
  resources,
  lng: locale,
  fallbackLng: 'zh-CN',
  interpolation: { escapeValue: false },
  showSupportNotice: false,
})

export async function setLocale(locale: 'zh-CN' | 'en') {
  window.localStorage.setItem('sidervia.locale', locale)
  document.documentElement.lang = locale
  await i18n.changeLanguage(locale)
}

document.documentElement.lang = locale

export default i18n
