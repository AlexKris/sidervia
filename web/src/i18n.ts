import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

const resources = {
  'zh-CN': {
    translation: {
      common: {
        add: '新建', save: '保存', cancel: '取消', edit: '编辑', delete: '删除', revoke: '吊销', disable: '禁用', enable: '启用',
        name: '名称', status: '状态', actions: '操作', loading: '加载中…', empty: '暂无数据', close: '关闭', copy: '复制',
        copied: '已复制', optional: '可选', required: '必填', version: '版本', planned: '计划中', active: '启用', disabled: '禁用',
        draft: '草稿', validating: '验证中', invalid: '验证失败', reauth_required: '需要重新认证', beta: 'Beta', enabled: '已启用', createdAt: '创建时间', never: '从未', confirmDelete: '此操作不可撤销，确定继续吗？',
        requestFailed: '请求失败', securityNotice: '敏感信息不会写入浏览器存储。', details: '详情', yes: '是', no: '否',
      },
      brand: { subtitle: '多提供商 AI 网关控制平面', foundation: 'v0.2 可用网关' },
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
        noWarnings: '当前没有安全提醒。', providersWarning: '部分运行配置需要管理员处理。',
        totpWarning: '建议立即在“安全设置”中启用 TOTP。', scopeTitle: 'v0.2 能力边界',
        scopeBody: '本版本提供 OpenAI、Claude、Gemini 与 Grok 的原生文本/流式转发、Client Key、账号调度、固定出口与 Google 官方 OAuth Beta。跨协议转换、状态资源与媒体接口不在本版本范围内。',
      },
      proxies: {
        title: '出站代理', description: '管理账号与上游使用的固定出口；密码字段始终只写。', add: '新建代理', scheme: '协议', host: '主机', port: '端口',
        username: '用户名', password: '密码', tlsName: 'TLS Server Name', insecure: '允许不安全 TLS', secretConfigured: '凭据已配置',
        secretHint: '编辑时留空表示保留现有值。',
      },
      upstreams: {
        title: '上游', description: 'Provider 描述和受控 Base URL；私网目标必须显式确认。', add: '新建上游', provider: 'Provider', baseUrl: 'Base URL',
        proxy: '默认代理', private: '允许访问私网地址', noProxy: '不使用代理', plannedHint: '四个 Provider 的原生文本与流式适配器当前标记为 Beta。',
      },
      accounts: {
        title: '账号池', description: '配置 API Key 或 Google 官方 OAuth，并在验证通过后加入调度。', add: '添加账号', edit: '编辑账号', upstream: '上游', credential: 'API Key',
        billing: '计费类型', priority: '优先级', concurrency: '最大并发', proxy: '账号代理', expires: '凭据到期时间', subscription: '订阅',
        metered: '按量', custom: '自定义', credentialConfigured: '凭据已配置', authBoundary: '支持 API Key；Google Authorization Code + PKCE 为 Beta。浏览器授权使用管理员浏览器网络；服务端 token 交换、刷新、验证和推理使用账号的同一出口。',
        authKind: '认证方式', apiKey: 'API Key', googleOAuth: 'Google OAuth', googleOAuthBeta: 'Google OAuth（Beta）', validate: '验证并启用', connectOAuth: '连接 Google',
        oauthCreateHint: '先创建 OAuth 草稿账号，再从账号列表发起 Google 授权。Sidervia 不接收 Cookie、Session Key 或官方 CLI 凭据。',
        oauthTitle: '连接 Google OAuth', oauthInstructions: '在新标签页完成官方授权。若回调无法自动到达 Sidervia，可粘贴浏览器中的完整回调 URL。',
        openAuthorization: '打开 Google 授权页', callbackURL: '完整回调 URL', callbackHint: '必须包含 Sidervia 配置的回调地址、code 与 state；页面不会解析或保存该 URL。', completeOAuth: '完成授权',
        oauthSuccess: 'Google OAuth 授权和账号验证已完成。', oauthFailed: 'Google OAuth 未完成；请检查配置、出口和授权状态后重试。',
        credentialKeep: '留空表示保留现有 API Key。', revalidateHint: '凭据或出口已改变；保存后账号回到草稿，必须重新验证或授权。',
      },
      routes: {
        title: '模型路由', description: '把公开模型 ID 映射到已验证账号候选，供原生文本与流式请求调度。', add: '新建路由', publicModel: '公开模型 ID',
        account: '候选账号', upstreamModel: '上游模型 ID', protocol: '协议', capabilities: '能力', descriptionField: '说明',
        edit: '编辑路由', poolHint: '一个路由可以包含多个已确认候选；调度按账号状态、能力、优先级、负载率、轮转、并发和冷却选择。',
        candidateNumber: '候选 {{number}}', removeCandidate: '移除候选', stream: '允许流式', candidateEnabled: '候选启用',
        addCandidate: '添加候选账号', multipleConfirmed: '保存即明确确认这些候选可作为同一账号池调度。',
      },
      keys: {
        title: 'Client Keys', description: '下游调用身份；完整密钥只在创建成功后显示一次。', add: '创建 Client Key', prefix: '前缀', expires: '到期时间',
        lastUsed: '最后使用', secretTitle: '立即保存此 Client Key', secretWarning: '关闭后无法再次查看。请现在复制到密码管理器。',
        revokeConfirm: '吊销后不可恢复，确定吊销此 Client Key 吗？',
      },
      audit: {
        title: '审计事件', description: '仅记录控制面元数据，不记录提示词、响应正文或凭证明文。', event: '事件', actor: '操作者', target: '目标', outcome: '结果', time: '时间',
      },
      usage: {
        title: '用量与请求', description: '查看最近请求的非正文元数据和 Provider 返回的基础 token 用量。', requests24h: '24 小时请求', errors24h: '24 小时错误', streamed24h: '24 小时流式',
        inputTokens24h: '24 小时输入 Token', outputTokens24h: '24 小时输出 Token', privacyBoundary: '此页面不提供提示词、响应正文、工具参数、Header、凭证或媒体内容回放。完整成本计算不属于 v0.2。',
        time: '时间', request: 'Request ID', client: 'Client Key', providerModel: 'Provider / 模型', protocol: '协议', latency: '耗时', tokens: 'Token', stream: '流式', inputOutput: '输入 / 输出',
      },
      settings: {
        title: '安全设置', description: '管理密码、TOTP 与当前构建信息。安全变更会轮换当前 Session。', passwordTitle: '修改管理员密码',
        currentPassword: '当前密码', newPassword: '新密码', changePassword: '修改密码', passwordRule: '至少 14 个 Unicode 字符。',
        totpTitle: '双因素认证（TOTP）', totpEnabled: 'TOTP 已启用', totpDisabled: 'TOTP 未启用', setup: '开始设置', setupPassword: '先输入当前密码',
        scan: '使用认证器扫描二维码，或手工输入 Secret。', code: '6 位验证码', confirm: '确认启用', disable: '关闭 TOTP',
        disableWarning: '关闭 TOTP 会降低管理端安全性，并吊销其他 Session。', buildTitle: '构建信息', commit: 'Commit', goVersion: 'Go 版本', buildTime: '构建时间',
        googleOAuthTitle: 'Google OAuth Provider（Beta）', googleOAuthDescription: '配置 Google Cloud OAuth Web Client。Client Secret 仅加密保存在服务器端。', oauthClientID: 'OAuth Client ID', oauthClientSecret: 'OAuth Client Secret',
        oauthSecretKeep: '留空表示保留现有 Client Secret。', googleProjectID: 'Google Cloud Project ID', oauthRedirectURI: 'Redirect URI', oauthRedirectAfterSave: '保存配置后显示', oauthEnabled: '允许创建新的 Google OAuth 授权', oauthScopes: '固定官方 scopes', saveOAuth: '保存 OAuth 配置',
      },
      planned: {
        usageTitle: '用量与成本', usageBody: 'v0.2 已记录不含正文的请求元数据与基础 usage；完整价格目录、成本 line items 和聚合将在后续版本交付。',
        pricingTitle: '价格目录', pricingBody: '价格规则、发布历史和冲突校验属于后续里程碑；v0.2 不提供成本计算、计费或余额。',
      },
      errors: { unauthorized: '会话已过期，请重新登录。', version_conflict: '资源已被其他操作修改，请刷新后重试。', resource_in_use: '资源仍被其他对象引用。' },
    },
  },
  en: {
    translation: {
      common: {
        add: 'Create', save: 'Save', cancel: 'Cancel', edit: 'Edit', delete: 'Delete', revoke: 'Revoke', disable: 'Disable', enable: 'Enable',
        name: 'Name', status: 'Status', actions: 'Actions', loading: 'Loading…', empty: 'No data yet', close: 'Close', copy: 'Copy',
        copied: 'Copied', optional: 'Optional', required: 'Required', version: 'Version', planned: 'Planned', active: 'Active', disabled: 'Disabled',
        draft: 'Draft', validating: 'Validating', invalid: 'Invalid', reauth_required: 'Reauthorization required', beta: 'Beta', enabled: 'Enabled', createdAt: 'Created', never: 'Never', confirmDelete: 'This cannot be undone. Continue?',
        requestFailed: 'Request failed', securityNotice: 'Sensitive values are never stored in browser storage.', details: 'Details', yes: 'Yes', no: 'No',
      },
      brand: { subtitle: 'Multi-provider AI gateway control plane', foundation: 'v0.2 Usable Gateway' },
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
        noWarnings: 'No security warnings right now.', providersWarning: 'Some runtime configuration needs administrator attention.',
        totpWarning: 'Enable TOTP from Security as soon as possible.', scopeTitle: 'v0.2 capability boundary',
        scopeBody: 'This release provides native text and streaming for OpenAI, Claude, Gemini, and Grok, plus Client Keys, account scheduling, fixed egress, and Google official OAuth Beta. Cross-protocol conversion, stateful resources, and media APIs are out of scope.',
      },
      proxies: {
        title: 'Egress proxies', description: 'Manage stable egress for accounts and upstreams. Password fields are always write-only.', add: 'Create proxy', scheme: 'Scheme', host: 'Host', port: 'Port',
        username: 'Username', password: 'Password', tlsName: 'TLS Server Name', insecure: 'Allow insecure TLS', secretConfigured: 'Credentials configured',
        secretHint: 'Leave secret fields empty while editing to retain their values.',
      },
      upstreams: {
        title: 'Upstreams', description: 'Provider descriptors and controlled base URLs. Private targets require explicit confirmation.', add: 'Create upstream', provider: 'Provider', baseUrl: 'Base URL',
        proxy: 'Default proxy', private: 'Allow private-network addresses', noProxy: 'No proxy', plannedHint: 'Native text and streaming adapters for all four Providers are currently Beta.',
      },
      accounts: {
        title: 'Account pool', description: 'Configure API keys or official Google OAuth, then validate accounts before scheduling.', add: 'Add account', edit: 'Edit account', upstream: 'Upstream', credential: 'API Key',
        billing: 'Billing kind', priority: 'Priority', concurrency: 'Max concurrency', proxy: 'Account proxy', expires: 'Credential expiry', subscription: 'Subscription',
        metered: 'Metered', custom: 'Custom', credentialConfigured: 'Credential configured', authBoundary: 'API keys are supported. Google Authorization Code + PKCE is Beta. Browser authorization uses the admin browser network; server-side token exchange, refresh, validation, and inference share the account egress.',
        authKind: 'Authentication', apiKey: 'API Key', googleOAuth: 'Google OAuth', googleOAuthBeta: 'Google OAuth (Beta)', validate: 'Validate and activate', connectOAuth: 'Connect Google',
        oauthCreateHint: 'Create the OAuth draft first, then start Google authorization from the account list. Sidervia does not accept cookies, session keys, or official CLI credentials.',
        oauthTitle: 'Connect Google OAuth', oauthInstructions: 'Complete official authorization in a new tab. If the callback cannot reach Sidervia automatically, paste the complete callback URL from the browser.',
        openAuthorization: 'Open Google authorization', callbackURL: 'Complete callback URL', callbackHint: 'It must contain the configured Sidervia callback, code, and state. The page does not parse or persist it.', completeOAuth: 'Complete authorization',
        oauthSuccess: 'Google OAuth authorization and account validation completed.', oauthFailed: 'Google OAuth did not complete. Check the configuration, egress, and authorization state, then retry.',
        credentialKeep: 'Leave blank to retain the existing API key.', revalidateHint: 'The credential or egress changed. Saving returns the account to draft and requires validation or authorization again.',
      },
      routes: {
        title: 'Model routes', description: 'Map public model IDs to validated account candidates for native text and streaming requests.', add: 'Create route', publicModel: 'Public model ID',
        account: 'Candidate account', upstreamModel: 'Upstream model ID', protocol: 'Protocol', capabilities: 'Capabilities', descriptionField: 'Description',
        edit: 'Edit route', poolHint: 'A route may contain multiple confirmed candidates. Scheduling considers account state, capability, priority, load ratio, rotation, concurrency, and cooldown.',
        candidateNumber: 'Candidate {{number}}', removeCandidate: 'Remove candidate', stream: 'Allow streaming', candidateEnabled: 'Candidate enabled',
        addCandidate: 'Add candidate account', multipleConfirmed: 'Saving explicitly confirms that these candidates may be scheduled as one account pool.',
      },
      keys: {
        title: 'Client Keys', description: 'Downstream identities. The complete key appears only once after creation.', add: 'Create Client Key', prefix: 'Prefix', expires: 'Expires',
        lastUsed: 'Last used', secretTitle: 'Save this Client Key now', secretWarning: 'It cannot be shown again after this dialog closes. Copy it to a password manager now.',
        revokeConfirm: 'Revocation is permanent. Revoke this Client Key?',
      },
      audit: {
        title: 'Audit events', description: 'Control-plane metadata only—never prompts, response bodies, or plaintext credentials.', event: 'Event', actor: 'Actor', target: 'Target', outcome: 'Outcome', time: 'Time',
      },
      usage: {
        title: 'Usage & requests', description: 'Inspect body-free metadata for recent requests and basic token usage reported by Providers.', requests24h: '24-hour requests', errors24h: '24-hour errors', streamed24h: '24-hour streams',
        inputTokens24h: '24-hour input tokens', outputTokens24h: '24-hour output tokens', privacyBoundary: 'This page never replays prompts, response bodies, tool arguments, headers, credentials, or media. Full cost calculation is outside v0.2.',
        time: 'Time', request: 'Request ID', client: 'Client Key', providerModel: 'Provider / model', protocol: 'Protocol', latency: 'Latency', tokens: 'Tokens', stream: 'stream', inputOutput: 'input / output',
      },
      settings: {
        title: 'Security settings', description: 'Manage password, TOTP, and build information. Security changes rotate the current session.', passwordTitle: 'Change administrator password',
        currentPassword: 'Current password', newPassword: 'New password', changePassword: 'Change password', passwordRule: 'At least 14 Unicode characters.',
        totpTitle: 'Two-factor authentication (TOTP)', totpEnabled: 'TOTP is enabled', totpDisabled: 'TOTP is not enabled', setup: 'Start setup', setupPassword: 'Enter the current password first',
        scan: 'Scan the QR code with an authenticator, or enter the secret manually.', code: '6-digit code', confirm: 'Confirm and enable', disable: 'Disable TOTP',
        disableWarning: 'Disabling TOTP weakens administrator security and revokes other sessions.', buildTitle: 'Build information', commit: 'Commit', goVersion: 'Go version', buildTime: 'Build time',
        googleOAuthTitle: 'Google OAuth Provider (Beta)', googleOAuthDescription: 'Configure a Google Cloud OAuth web client. The client secret is encrypted and stored only on the server.', oauthClientID: 'OAuth Client ID', oauthClientSecret: 'OAuth Client Secret',
        oauthSecretKeep: 'Leave blank to retain the existing Client Secret.', googleProjectID: 'Google Cloud Project ID', oauthRedirectURI: 'Redirect URI', oauthRedirectAfterSave: 'Shown after saving', oauthEnabled: 'Allow new Google OAuth authorizations', oauthScopes: 'Fixed official scopes', saveOAuth: 'Save OAuth configuration',
      },
      planned: {
        usageTitle: 'Usage & cost', usageBody: 'v0.2 records body-free request metadata and basic usage. Full price catalogs, cost line items, and aggregation follow in a later release.',
        pricingTitle: 'Price catalog', pricingBody: 'Price rules, publication history, and conflict checks are a later milestone. v0.2 has no cost calculation, billing, or balances.',
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
