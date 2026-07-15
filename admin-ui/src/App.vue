<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, apiRequest } from './api'

type ConnectionState = 'connecting' | 'connected' | 'failed'
type SectionKey = 'overview' | 'brand' | 'search' | 'ui' | 'outgoing' | 'engines' | 'raw'
type EngineStatusFilter = 'all' | 'enabled' | 'disabled' | 'unavailable'
type JsonRecord = Record<string, unknown>

interface StatusResponse {
  status: string
  serviceUrl: string
  version?: string
}

interface ConfigResponse {
  revision?: string | number
  config?: JsonRecord
  options?: JsonRecord
  engineCatalog?: unknown[]
  engine_catalog?: unknown[]
}

interface EngineItem {
  name: string
  label: string
  group: string
  categories: string[]
  shortcut: string
  enabled: boolean
  defaultEnabled: boolean
  inactive: boolean
  origin: string
  locked: boolean
  warning: string
  description: string
}

interface EditableConfig {
  brand: {
    instance_name: string
    base_url: string
    privacy_policy_url: string
    donation_url: string
    contact_url: string
    docs_url: string
    public_instances_url: string
    wiki_url: string
    issue_url: string
  }
  search: {
    safe_search: number
    autocomplete: string
    autocomplete_min: number
    favicon_resolver: string
    default_lang: string
    max_page: number
  }
  ui: {
    default_theme: string
    theme_args: { simple_style: string }
    default_locale: string
    query_in_title: boolean
    center_alignment: boolean
    results_on_new_tab: boolean
    search_on_category_select: boolean
    hotkeys: string
    url_formatting: string
  }
  outgoing: {
    request_timeout: number
    max_request_timeout: number
    pool_connections: number
    pool_maxsize: number
    enable_http2: boolean
    proxies: string
    using_tor_proxy: boolean
    extra_proxy_timeout: number
  }
  engines: EngineItem[]
}

const sections: Array<{ key: SectionKey; label: string; hint: string }> = [
  { key: 'overview', label: '概览', hint: '服务与保存状态' },
  { key: 'brand', label: '品牌', hint: '名称、地址与图标' },
  { key: 'search', label: '搜索', hint: '建议与结果策略' },
  { key: 'ui', label: '界面', hint: '显示与交互偏好' },
  { key: 'outgoing', label: '网络', hint: '超时、连接池与代理' },
  { key: 'engines', label: '引擎', hint: '启用与禁用搜索源' },
  { key: 'raw', label: '高级', hint: '导入导出原始 YAML' },
]

const assetKinds = [
  { kind: 'wordmark', label: 'Wordmark', accept: '.svg,.png,.webp' },
  { kind: 'logo', label: 'Logo', accept: '.svg,.png,.webp' },
  { kind: 'favicon', label: 'Favicon', accept: '.ico,.png,.svg' },
  { kind: 'icon192', label: 'PWA 192', accept: '.png' },
  { kind: 'icon512', label: 'PWA 512', accept: '.png' },
]

const config = reactive<EditableConfig>(createDefaults())
const activeSection = ref<SectionKey>('overview')
const connectionState = ref<ConnectionState>('connecting')
const serviceAddress = ref('http://设备地址:8080')
const version = ref('')
const revision = ref<string | number>('')
const options = ref<JsonRecord>({})
const notice = ref('正在连接管理服务…')
const noticeTone = ref<'info' | 'success' | 'error' | 'warning'>('info')
const loading = ref(true)
const saving = ref(false)
const restartRequired = ref(false)
const engineQuery = ref('')
const engineGroup = ref('全部')
const engineStatus = ref<EngineStatusFilter>('all')
const rawYaml = ref('')
const rawLoading = ref(false)
const uploadBusy = ref('')

const connectionText = computed(() => ({
  connecting: '管理服务连接中',
  connected: '管理服务已连接',
  failed: '管理服务连接失败',
})[connectionState.value])

const enabledEngineCount = computed(() => config.engines.filter((engine) => engine.enabled).length)
const defaultEnabledEngineCount = computed(() => config.engines.filter((engine) => engine.defaultEnabled).length)
const unavailableEngineCount = computed(() => config.engines.filter((engine) => engine.inactive).length)
const engineGroups = computed(() => ['全部', ...new Set(config.engines.flatMap((engine) => engine.categories).filter(Boolean))])
const filteredEngines = computed(() => {
  const query = engineQuery.value.trim().toLocaleLowerCase()
  return config.engines.filter((engine) => {
    const groupMatches = engineGroup.value === '全部' || engine.categories.includes(engineGroup.value)
    const queryMatches = !query || `${engine.name} ${engine.label} ${engine.shortcut} ${engine.categories.join(' ')} ${engine.description}`.toLocaleLowerCase().includes(query)
    const statusMatches = engineStatus.value === 'all'
      || (engineStatus.value === 'enabled' && engine.enabled && !engine.inactive)
      || (engineStatus.value === 'disabled' && !engine.enabled && !engine.inactive)
      || (engineStatus.value === 'unavailable' && engine.inactive)
    return groupMatches && queryMatches && statusMatches
  })
})
const autocompleteOptions = computed(() => optionStrings('autocomplete', ['', 'bing', 'duckduckgo', 'google', 'brave', 'startpage']))
const faviconOptions = computed(() => optionStrings('faviconResolvers', ['', 'allesedv', 'duckduckgo', 'google', 'yandex']))
const themeOptions = computed(() => optionStrings('themes', ['simple']))
const styleOptions = computed(() => optionStrings('styles', ['auto', 'light', 'dark', 'black']))
const localeOptions = computed(() => optionStrings('locales', ['auto', 'zh-CN', 'zh-TW', 'en']))

function createDefaults(): EditableConfig {
  return {
    brand: {
      instance_name: 'SearXNG',
      base_url: '',
      privacy_policy_url: '',
      donation_url: '',
      contact_url: '',
      docs_url: '',
      public_instances_url: '',
      wiki_url: '',
      issue_url: '',
    },
    search: {
      safe_search: 0,
      autocomplete: 'bing',
      autocomplete_min: 4,
      favicon_resolver: 'google',
      default_lang: 'auto',
      max_page: 0,
    },
    ui: {
      default_theme: 'simple',
      theme_args: { simple_style: 'auto' },
      default_locale: '',
      query_in_title: true,
      center_alignment: false,
      results_on_new_tab: false,
      search_on_category_select: true,
      hotkeys: 'default',
      url_formatting: 'pretty',
    },
    outgoing: {
      request_timeout: 3,
      max_request_timeout: 10,
      pool_connections: 100,
      pool_maxsize: 20,
      enable_http2: true,
      proxies: '',
      using_tor_proxy: false,
      extra_proxy_timeout: 0,
    },
    engines: [],
  }
}

function record(value: unknown): JsonRecord {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as JsonRecord : {}
}

function pick(source: JsonRecord, keys: string[], fallback: unknown): unknown {
  for (const key of keys) if (source[key] !== undefined && source[key] !== null) return source[key]
  return fallback
}

function text(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : value === undefined || value === null ? fallback : String(value)
}

function numberValue(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function boolValue(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value
  if (value === 'true' || value === 1) return true
  if (value === 'false' || value === 0) return false
  return fallback
}

function optionStrings(key: string, fallback: string[]): string[] {
  const direct = options.value[key]
  const nested = record(options.value.search)[key] ?? record(options.value.ui)[key]
  const candidate = Array.isArray(direct) ? direct : Array.isArray(nested) ? nested : fallback
  return [...new Set(candidate.map((item) => typeof item === 'object' ? text(record(item).value || record(item).name) : text(item)))]
}

function normalizeEngines(configValue: unknown, catalogValue: unknown): EngineItem[] {
  const settings = new Map<string, JsonRecord>()
  if (Array.isArray(configValue)) {
    for (const value of configValue) {
      const item = record(value)
      const name = text(item.name || item.id)
      if (name) settings.set(name, item)
    }
  } else {
    for (const [name, value] of Object.entries(record(configValue))) settings.set(name, record(value))
  }

  const catalog = Array.isArray(catalogValue) ? catalogValue : []
  const names = new Set<string>([...settings.keys()])
  for (const value of catalog) {
    const item = record(value)
    const name = text(item.name || item.id)
    if (name) names.add(name)
  }

  return [...names].map((name) => {
    const catalogItem = record(catalog.find((value) => text(record(value).name || record(value).id) === name))
    const setting = settings.get(name) || {}
    const privacyLocked = name.toLocaleLowerCase() === 'chinaso news'
    const locked = privacyLocked || boolValue(setting.locked ?? catalogItem.locked, false)
    const inactive = boolValue(setting.inactive ?? catalogItem.inactive ?? catalogItem.defaultInactive, false)
    const defaultEnabled = boolValue(setting.defaultEnabled ?? setting.default_enabled ?? catalogItem.defaultEnabled ?? catalogItem.default_enabled, !boolValue(setting.disabled ?? catalogItem.disabled, false) && !inactive)
    const rawCategories = setting.categories ?? setting.category ?? setting.group ?? catalogItem.categories ?? catalogItem.category ?? catalogItem.group
    const categories = Array.isArray(rawCategories)
      ? rawCategories.map((value) => text(value)).filter(Boolean)
      : text(rawCategories) ? [text(rawCategories)] : ['其他']
    const enabled = setting.enabled !== undefined
      ? boolValue(setting.enabled, true)
      : setting.disabled !== undefined
        ? !boolValue(setting.disabled, false)
        : boolValue(catalogItem.enabled, defaultEnabled)
    return {
      name,
      label: text(setting.label || setting.displayName || setting.title || catalogItem.label || catalogItem.displayName || catalogItem.title, name),
      group: categories[0] || '其他',
      categories,
      shortcut: text(setting.shortcut || catalogItem.shortcut, '-'),
      enabled: locked || inactive ? false : enabled,
      defaultEnabled,
      inactive,
      origin: text(catalogItem.origin || setting.origin, 'default'),
      locked,
      warning: privacyLocked
        ? '该引擎的结果链接存在隐私泄露风险，已强制禁用。'
        : text(setting.warning || catalogItem.warning),
      description: text(setting.description || setting.about || catalogItem.description || catalogItem.about),
    }
  }).sort((left, right) => left.group.localeCompare(right.group, 'zh-CN') || left.label.localeCompare(right.label, 'zh-CN'))
}

function applyConfig(payload: ConfigResponse) {
  const source = record(payload.config || payload)
  const brand = record(source.brand)
  const search = record(source.search)
  const ui = record(source.ui)
  const themeArgs = record(pick(ui, ['theme_args', 'themeArgs'], {}))
  const outgoing = record(source.outgoing)

  config.brand.instance_name = text(pick(brand, ['instance_name', 'instanceName'], config.brand.instance_name))
  config.brand.base_url = text(pick(brand, ['base_url', 'baseUrl'], config.brand.base_url))
  config.brand.privacy_policy_url = text(pick(brand, ['privacyPolicyUrl', 'privacy_policy_url'], config.brand.privacy_policy_url))
  config.brand.donation_url = text(pick(brand, ['donationUrl', 'donation_url'], config.brand.donation_url))
  config.brand.contact_url = text(pick(brand, ['contactUrl', 'contact_url'], config.brand.contact_url))
  config.brand.docs_url = text(pick(brand, ['docsUrl', 'docs_url'], config.brand.docs_url))
  config.brand.public_instances_url = text(pick(brand, ['publicInstancesUrl', 'public_instances_url'], config.brand.public_instances_url))
  config.brand.wiki_url = text(pick(brand, ['wikiUrl', 'wiki_url'], config.brand.wiki_url))
  config.brand.issue_url = text(pick(brand, ['issueUrl', 'issue_url'], config.brand.issue_url))
  config.search.safe_search = numberValue(pick(search, ['safe_search', 'safeSearch'], config.search.safe_search), 0)
  config.search.autocomplete = text(pick(search, ['autocomplete'], config.search.autocomplete), 'bing')
  config.search.autocomplete_min = numberValue(pick(search, ['autocomplete_min', 'autocompleteMin'], config.search.autocomplete_min), 4)
  config.search.favicon_resolver = text(pick(search, ['favicon_resolver', 'faviconResolver'], config.search.favicon_resolver), 'google')
  config.search.default_lang = text(pick(search, ['default_lang', 'defaultLang'], config.search.default_lang), 'auto')
  config.search.max_page = numberValue(pick(search, ['max_page', 'maxPage'], config.search.max_page), 0)
  config.ui.default_theme = text(pick(ui, ['default_theme', 'defaultTheme', 'theme'], config.ui.default_theme), 'simple')
  config.ui.theme_args.simple_style = text(pick(themeArgs, ['simple_style', 'simpleStyle'], pick(ui, ['simple_style', 'simpleStyle'], config.ui.theme_args.simple_style)))
  config.ui.default_locale = text(pick(ui, ['default_locale', 'defaultLocale', 'locale'], config.ui.default_locale))
  config.ui.query_in_title = boolValue(pick(ui, ['query_in_title', 'queryInTitle'], config.ui.query_in_title), true)
  config.ui.center_alignment = boolValue(pick(ui, ['center_alignment', 'centerAlignment'], config.ui.center_alignment), false)
  config.ui.results_on_new_tab = boolValue(pick(ui, ['results_on_new_tab', 'resultsOnNewTab'], config.ui.results_on_new_tab), false)
  config.ui.search_on_category_select = boolValue(pick(ui, ['searchOnCategorySelect', 'search_on_category_select', 'category_select'], config.ui.search_on_category_select), true)
  config.ui.hotkeys = text(pick(ui, ['hotkeys'], config.ui.hotkeys))
  config.ui.url_formatting = text(pick(ui, ['url_formatting', 'urlFormatting'], config.ui.url_formatting))
  config.outgoing.request_timeout = numberValue(pick(outgoing, ['request_timeout', 'requestTimeout'], config.outgoing.request_timeout), 3)
  config.outgoing.max_request_timeout = numberValue(pick(outgoing, ['max_request_timeout', 'maxRequestTimeout'], config.outgoing.max_request_timeout), 10)
  config.outgoing.pool_connections = numberValue(pick(outgoing, ['pool_connections', 'poolConnections'], config.outgoing.pool_connections), 100)
  config.outgoing.pool_maxsize = numberValue(pick(outgoing, ['pool_maxsize', 'poolMaxsize'], config.outgoing.pool_maxsize), 20)
  config.outgoing.enable_http2 = boolValue(pick(outgoing, ['enable_http2', 'enableHttp2'], config.outgoing.enable_http2), true)
  const proxies = pick(outgoing, ['proxies', 'proxy_url', 'proxyUrl'], config.outgoing.proxies)
  config.outgoing.proxies = typeof proxies === 'string' ? proxies : text(record(proxies).all || record(proxies).https || record(proxies).http)
  config.outgoing.using_tor_proxy = boolValue(pick(outgoing, ['using_tor_proxy', 'usingTorProxy'], config.outgoing.using_tor_proxy), false)
  config.outgoing.extra_proxy_timeout = numberValue(pick(outgoing, ['extra_proxy_timeout', 'extraProxyTimeout'], config.outgoing.extra_proxy_timeout), 0)
  config.engines = normalizeEngines(source.engines, payload.engineCatalog || payload.engine_catalog || options.value.engineCatalog)
}

function serializedConfig(): JsonRecord {
  return {
    brand: {
      instanceName: config.brand.instance_name,
      baseUrl: config.brand.base_url,
      privacyPolicyUrl: config.brand.privacy_policy_url,
      donationUrl: config.brand.donation_url,
      contactUrl: config.brand.contact_url,
      docsUrl: config.brand.docs_url,
      publicInstancesUrl: config.brand.public_instances_url,
      wikiUrl: config.brand.wiki_url,
      issueUrl: config.brand.issue_url,
    },
    search: {
      safeSearch: config.search.safe_search,
      autocomplete: config.search.autocomplete,
      autocompleteMin: config.search.autocomplete_min,
      faviconResolver: config.search.favicon_resolver,
      defaultLang: config.search.default_lang,
      maxPage: config.search.max_page,
    },
    ui: {
      defaultTheme: config.ui.default_theme,
      defaultLocale: config.ui.default_locale,
      simpleStyle: config.ui.theme_args.simple_style,
      queryInTitle: config.ui.query_in_title,
      centerAlignment: config.ui.center_alignment,
      resultsOnNewTab: config.ui.results_on_new_tab,
      searchOnCategorySelect: config.ui.search_on_category_select,
      hotkeys: config.ui.hotkeys,
      urlFormatting: config.ui.url_formatting,
    },
    outgoing: {
      requestTimeout: config.outgoing.request_timeout,
      maxRequestTimeout: config.outgoing.max_request_timeout,
      poolConnections: config.outgoing.pool_connections,
      poolMaxSize: config.outgoing.pool_maxsize,
      enableHttp2: config.outgoing.enable_http2,
      proxyUrl: config.outgoing.proxies,
      usingTorProxy: config.outgoing.using_tor_proxy,
      extraProxyTimeout: config.outgoing.extra_proxy_timeout,
    },
    engines: config.engines.map((engine) => ({
      name: engine.name,
      enabled: engine.locked || engine.inactive ? false : engine.enabled,
    })),
  }
}

function setNotice(message: string, tone: typeof noticeTone.value = 'info') {
  notice.value = message
  noticeTone.value = tone
}

function resultWarnings(result: JsonRecord): string[] {
  const value = result.warnings
  return Array.isArray(value) ? value.map((warning) => text(warning)).filter(Boolean) : []
}

function showApplyResult(result: JsonRecord, subject = '配置') {
  const applied = boolValue(result.applied, false)
  const rolledBack = boolValue(result.rolledBack ?? result.rolled_back, false)
  restartRequired.value = boolValue(result.restartRequired ?? result.restart_required, false)
  const warnings = resultWarnings(result)
  const suffix = warnings.length ? ` ${warnings.join('；')}` : ''

  if (rolledBack) {
    setNotice(`${subject}应用失败，已自动回滚到保存前的可用配置。${suffix}`, 'error')
  } else if (applied) {
    setNotice(`${subject}已保存并自动应用，当前服务已生效。${suffix}`, warnings.length ? 'warning' : 'success')
  } else if (restartRequired.value) {
    setNotice(`${subject}已保存，但未能自动应用，请手工重启 SearXNG 服务。${suffix}`, 'warning')
  } else {
    setNotice(`${subject}已保存，但管理服务未确认应用状态。${suffix}`, 'warning')
  }
}

async function loadAll() {
  loading.value = true
  connectionState.value = 'connecting'
  setNotice('正在读取服务状态与配置…')
  try {
    const [status, payload] = await Promise.all([
      apiRequest<StatusResponse>('status'),
      apiRequest<ConfigResponse>('config'),
    ])
    serviceAddress.value = status.serviceUrl
    version.value = status.version || ''
    revision.value = payload.revision ?? ''
    options.value = record(payload.options)
    applyConfig(payload)
    connectionState.value = 'connected'
    setNotice('配置已加载，可以直接修改并保存。', 'success')
  } catch (error) {
    connectionState.value = 'failed'
    setNotice(errorMessage(error, '管理服务连接失败。'), 'error')
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  saving.value = true
  restartRequired.value = false
  setNotice('正在保存并自动应用配置…')
  try {
    const result = await apiRequest<Record<string, unknown>>('config', {
      method: 'PUT',
      body: JSON.stringify({ revision: revision.value, config: serializedConfig() }),
    })
    revision.value = result.revision as string | number ?? revision.value
    showApplyResult(result)
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) {
      setNotice('配置已被其他窗口修改。请重新加载后再保存，避免覆盖新内容。', 'error')
    } else {
      setNotice(errorMessage(error, '保存配置失败。'), 'error')
    }
  } finally {
    saving.value = false
  }
}

async function uploadAsset(kind: string, event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploadBusy.value = kind
  setNotice(`正在上传并自动应用 ${file.name}…`)
  try {
    const body = new FormData()
    body.append('file', file)
    const result = await apiRequest<Record<string, unknown>>(`branding/${kind}`, { method: 'POST', body })
    showApplyResult(result, `${file.name} `)
  } catch (error) {
    setNotice(errorMessage(error, '文件上传失败。'), 'error')
  } finally {
    uploadBusy.value = ''
    input.value = ''
  }
}

async function loadRawYaml() {
  rawLoading.value = true
  setNotice('正在读取原始 settings.yml…')
  try {
    const payload = await apiRequest<unknown>('config/raw')
    if (typeof payload === 'string') rawYaml.value = payload
    else {
      const value = record(payload)
      rawYaml.value = text(value.raw || value.yaml || value.content)
      revision.value = value.revision as string | number ?? revision.value
    }
    setNotice('原始 YAML 已载入。保存前请确认格式正确。', 'success')
  } catch (error) {
    setNotice(errorMessage(error, '读取原始 YAML 失败。'), 'error')
  } finally {
    rawLoading.value = false
  }
}

async function saveRawYaml() {
  if (!rawYaml.value.trim()) {
    setNotice('原始 YAML 不能为空。', 'error')
    return
  }
  rawLoading.value = true
  setNotice('正在校验、保存并自动应用原始 YAML…')
  try {
    const result = await apiRequest<Record<string, unknown>>('config/raw', {
      method: 'PUT',
      body: JSON.stringify({ yaml: rawYaml.value }),
    })
    revision.value = result.revision as string | number ?? revision.value
    await loadAll()
    showApplyResult(result, '原始 YAML ')
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) setNotice('原始配置已变化，请重新读取后再导入。', 'error')
    else setNotice(errorMessage(error, '导入原始 YAML 失败。'), 'error')
  } finally {
    rawLoading.value = false
  }
}

async function exportRawYaml() {
  try {
    const payload = await apiRequest<unknown>('config/raw')
    const yaml = typeof payload === 'string' ? payload : text(record(payload).raw || record(payload).yaml || record(payload).content)
    if (!yaml) throw new Error('管理服务未返回可导出的 YAML。')
    const blob = new Blob([yaml], { type: 'text/yaml;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `searxng-settings-${new Date().toISOString().slice(0, 10)}.yml`
    link.click()
    URL.revokeObjectURL(url)
    setNotice('配置文件已导出。', 'success')
  } catch (error) {
    setNotice(errorMessage(error, '导出配置失败。'), 'error')
  }
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

function openSection(section: SectionKey) {
  activeSection.value = section
  if (section === 'raw' && !rawYaml.value) void loadRawYaml()
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function setAllVisible(enabled: boolean) {
  for (const engine of filteredEngines.value) if (!engine.locked && !engine.inactive) engine.enabled = enabled
}

onMounted(loadAll)
</script>

<template>
  <main class="app-shell">
    <header class="topbar">
      <div class="brand-lockup">
        <span class="brand-mark">S</span>
        <div><strong>SearXNG 配置</strong><small v-if="version">v{{ version }}</small></div>
      </div>
      <span class="status-pill" :class="connectionState"><i></i>{{ connectionText }}</span>
    </header>

    <div class="workspace">
      <aside class="sidebar">
        <div class="sidebar-heading">
          <p class="eyebrow">飞牛 FPK</p>
          <h1>配置管理</h1>
          <p>修改会持久化到 SearXNG 配置文件。</p>
        </div>
        <nav aria-label="配置分区">
          <button v-for="section in sections" :key="section.key" type="button" :class="{ active: activeSection === section.key }" @click="openSection(section.key)">
            <span>{{ section.label }}</span><small>{{ section.hint }}</small>
          </button>
        </nav>
        <div class="sidebar-summary">
          <span>已启用引擎</span><strong>{{ enabledEngineCount }} / {{ config.engines.length }}</strong>
        </div>
      </aside>

      <section class="content">
        <div v-if="loading" class="loading-card"><span class="spinner"></span><b>正在加载配置</b><p>正在连接飞牛管理服务，请稍候。</p></div>

        <template v-else>
          <header class="content-header">
            <div>
              <p class="eyebrow">{{ sections.find((section) => section.key === activeSection)?.hint }}</p>
              <h2>{{ sections.find((section) => section.key === activeSection)?.label }}</h2>
            </div>
            <div class="header-actions">
              <button class="button ghost" type="button" :disabled="loading || saving" @click="loadAll">重新加载</button>
              <button v-if="activeSection !== 'raw'" class="button primary" type="button" :disabled="saving || connectionState !== 'connected'" @click="saveConfig">
                {{ saving ? '保存并应用中…' : '保存并应用' }}
              </button>
            </div>
          </header>

          <section v-if="activeSection === 'overview'" class="section-stack">
            <article class="service-card panel">
              <div><span class="field-label">SearXNG 服务地址</span><strong>{{ serviceAddress }}</strong><small>主服务仍通过原生 IP + 端口访问</small></div>
              <a class="button secondary" :href="serviceAddress" target="_blank" rel="noreferrer">打开搜索服务</a>
            </article>
            <div class="metric-grid">
              <article class="metric panel"><span>配置版本</span><strong>{{ revision || '—' }}</strong><small>保存时用于防止覆盖冲突</small></article>
              <article class="metric panel"><span>搜索建议</span><strong>{{ config.search.autocomplete || '关闭' }}</strong><small>默认使用 Bing 建议</small></article>
              <article class="metric panel"><span>网站图标</span><strong>{{ config.search.favicon_resolver || '关闭' }}</strong><small>默认使用 Google 图标服务</small></article>
              <article class="metric panel"><span>启用引擎</span><strong>{{ enabledEngineCount }}</strong><small>可在引擎页逐项调整</small></article>
            </div>
            <article class="panel quick-grid">
              <button v-for="section in sections.slice(1)" :key="section.key" type="button" @click="openSection(section.key)"><b>{{ section.label }}</b><span>{{ section.hint }}</span><i>→</i></button>
            </article>
          </section>

          <section v-else-if="activeSection === 'brand'" class="section-stack">
            <article class="panel form-panel">
              <div class="panel-title"><div><h3>站点身份</h3><p>设置搜索站点名称、公开地址与默认主题。</p></div></div>
              <div class="form-grid">
                <label><span>站点名称</span><input v-model.trim="config.brand.instance_name" type="text" placeholder="SearXNG"></label>
                <label><span>基础 URL</span><input v-model.trim="config.brand.base_url" type="url" placeholder="https://search.example.com/"><small>留空时使用当前访问地址。</small></label>
                <label><span>默认主题</span><select v-model="config.ui.default_theme"><option v-for="theme in themeOptions" :key="theme" :value="theme">{{ theme }}</option></select></label>
              </div>
              <details class="advanced-fields">
                <summary>品牌链接（可选）</summary>
                <div class="form-grid">
                  <label><span>隐私政策 URL</span><input v-model.trim="config.brand.privacy_policy_url" type="url" placeholder="https://..."></label>
                  <label><span>捐赠 URL</span><input v-model.trim="config.brand.donation_url" type="url" placeholder="https://..."></label>
                  <label><span>联系地址</span><input v-model.trim="config.brand.contact_url" type="text" placeholder="mailto:admin@example.com"></label>
                  <label><span>文档 URL</span><input v-model.trim="config.brand.docs_url" type="url" placeholder="https://..."></label>
                  <label><span>公共实例 URL</span><input v-model.trim="config.brand.public_instances_url" type="url" placeholder="https://..."></label>
                  <label><span>Wiki URL</span><input v-model.trim="config.brand.wiki_url" type="url" placeholder="https://..."></label>
                  <label><span>问题反馈 URL</span><input v-model.trim="config.brand.issue_url" type="url" placeholder="https://..."></label>
                </div>
              </details>
            </article>
            <article class="panel form-panel">
              <div class="panel-title"><div><h3>品牌资源</h3><p>上传后端会校验格式并保存到持久化目录。</p></div></div>
              <div class="upload-grid">
                <label v-for="asset in assetKinds" :key="asset.kind" class="upload-card" :class="{ busy: uploadBusy === asset.kind }">
                  <span class="upload-icon">↑</span><b>{{ asset.label }}</b><small>{{ asset.accept.split(',').join(' · ') }}</small>
                  <input type="file" :accept="asset.accept" :disabled="uploadBusy !== ''" @change="uploadAsset(asset.kind, $event)">
                  <em>{{ uploadBusy === asset.kind ? '上传中…' : '选择文件' }}</em>
                </label>
              </div>
            </article>
          </section>

          <section v-else-if="activeSection === 'search'" class="section-stack">
            <article class="panel form-panel">
              <div class="panel-title"><div><h3>搜索行为</h3><p>控制安全过滤、建议来源和结果范围。</p></div></div>
              <div class="form-grid">
                <label><span>安全搜索</span><select v-model.number="config.search.safe_search"><option :value="0">关闭</option><option :value="1">适中</option><option :value="2">严格</option></select></label>
                <label><span>搜索建议</span><select v-model="config.search.autocomplete"><option v-for="item in autocompleteOptions" :key="item" :value="item">{{ item || '关闭' }}</option></select><small>推荐并默认使用 Bing。</small></label>
                <label><span>触发建议的最少字符</span><input v-model.number="config.search.autocomplete_min" type="number" min="1" max="20"></label>
                <label><span>网站图标服务</span><select v-model="config.search.favicon_resolver"><option v-for="item in faviconOptions" :key="item" :value="item">{{ item || '关闭' }}</option></select><small>默认 Google；也可选择 DuckDuckGo 等服务。</small></label>
                <label><span>默认语言</span><input v-model.trim="config.search.default_lang" type="text" list="locale-list" placeholder="auto"><datalist id="locale-list"><option v-for="locale in localeOptions" :key="locale" :value="locale"></option></datalist></label>
                <label><span>最大结果页数</span><input v-model.number="config.search.max_page" type="number" min="0"><small>0 表示不限制。</small></label>
              </div>
            </article>
          </section>

          <section v-else-if="activeSection === 'ui'" class="section-stack">
            <article class="panel form-panel">
              <div class="panel-title"><div><h3>外观</h3><p>调整 SearXNG 原生界面的主题与布局。</p></div></div>
              <div class="form-grid">
                <label><span>主题</span><select v-model="config.ui.default_theme"><option v-for="theme in themeOptions" :key="theme" :value="theme">{{ theme }}</option></select></label>
                <label><span>Simple 风格</span><select v-model="config.ui.theme_args.simple_style"><option v-for="style in styleOptions" :key="style" :value="style">{{ { auto: '跟随系统', light: '浅色', dark: '深色', black: '纯黑' }[style] || style }}</option></select></label>
                <label><span>默认界面语言</span><input v-model.trim="config.ui.default_locale" type="text" placeholder="留空自动识别"></label>
                <label><span>快捷键方案</span><select v-model="config.ui.hotkeys"><option value="default">默认</option><option value="vim">Vim</option></select></label>
                <label><span>URL 显示格式</span><select v-model="config.ui.url_formatting"><option value="pretty">美化</option><option value="full">完整</option><option value="host">仅主机名</option></select></label>
              </div>
              <div class="switch-grid">
                <label class="switch-row"><span><b>标题显示查询词</b><small>浏览器标签页标题包含当前查询。</small></span><input v-model="config.ui.query_in_title" type="checkbox"><i></i></label>
                <label class="switch-row"><span><b>内容居中</b><small>使用居中对齐的结果布局。</small></span><input v-model="config.ui.center_alignment" type="checkbox"><i></i></label>
                <label class="switch-row"><span><b>新标签页打开结果</b><small>点击搜索结果时保留当前页面。</small></span><input v-model="config.ui.results_on_new_tab" type="checkbox"><i></i></label>
                <label class="switch-row"><span><b>选择分类后立即搜索</b><small>切换分类时自动提交当前查询。</small></span><input v-model="config.ui.search_on_category_select" type="checkbox"><i></i></label>
              </div>
            </article>
          </section>

          <section v-else-if="activeSection === 'outgoing'" class="section-stack">
            <article class="panel form-panel">
              <div class="panel-title"><div><h3>请求与连接池</h3><p>不确定时建议保留默认值，过低的超时可能导致引擎失败。</p></div></div>
              <div class="form-grid">
                <label><span>请求超时（秒）</span><input v-model.number="config.outgoing.request_timeout" type="number" min="0.1" step="0.1"></label>
                <label><span>最大请求超时（秒）</span><input v-model.number="config.outgoing.max_request_timeout" type="number" min="0.1" step="0.1"></label>
                <label><span>连接池数量</span><input v-model.number="config.outgoing.pool_connections" type="number" min="1"></label>
                <label><span>单池最大连接</span><input v-model.number="config.outgoing.pool_maxsize" type="number" min="1"></label>
                <label><span>代理额外超时（秒）</span><input v-model.number="config.outgoing.extra_proxy_timeout" type="number" min="0" step="0.1"></label>
                <label class="span-two"><span>代理 URL</span><input v-model.trim="config.outgoing.proxies" type="text" placeholder="http://user:pass@proxy:8080"><small>留空表示直连，请妥善保护含凭据的代理地址。</small></label>
              </div>
              <div class="switch-grid">
                <label class="switch-row"><span><b>启用 HTTP/2</b><small>允许支持的搜索引擎复用 HTTP/2 连接。</small></span><input v-model="config.outgoing.enable_http2" type="checkbox"><i></i></label>
                <label class="switch-row"><span><b>使用 Tor 代理</b><small>仅在已正确部署 Tor 代理时启用。</small></span><input v-model="config.outgoing.using_tor_proxy" type="checkbox"><i></i></label>
              </div>
            </article>
          </section>

          <section v-else-if="activeSection === 'engines'" class="section-stack">
            <div class="engine-summary-grid">
              <article class="engine-summary panel"><span>引擎总数</span><strong>{{ config.engines.length }}</strong><small>固定镜像目录与自定义引擎</small></article>
              <article class="engine-summary panel"><span>当前启用</span><strong>{{ enabledEngineCount }}</strong><small>本次保存后实际启用</small></article>
              <article class="engine-summary panel"><span>默认启用</span><strong>{{ defaultEnabledEngineCount }}</strong><small>SearXNG 上游默认状态</small></article>
              <article class="engine-summary panel"><span>不可用</span><strong>{{ unavailableEngineCount }}</strong><small>inactive，需高级配置后启用</small></article>
            </div>
            <article class="panel engine-toolbar">
              <div class="search-box"><span>⌕</span><input v-model="engineQuery" type="search" placeholder="搜索名称、shortcut 或分类"></div>
              <select v-model="engineGroup"><option v-for="group in engineGroups" :key="group">{{ group }}</option></select>
              <select v-model="engineStatus" aria-label="引擎状态筛选">
                <option value="all">全部状态</option>
                <option value="enabled">已启用</option>
                <option value="disabled">已禁用</option>
                <option value="unavailable">不可用</option>
              </select>
              <button class="button ghost" type="button" @click="setAllVisible(true)">全部启用</button>
              <button class="button ghost" type="button" @click="setAllVisible(false)">全部禁用</button>
            </article>
            <p class="engine-result-count">显示 {{ filteredEngines.length }} / {{ config.engines.length }} 项；批量操作不会修改锁定或 inactive 引擎。</p>
            <div class="engine-list">
              <article v-for="engine in filteredEngines" :key="engine.name" class="engine-card panel" :class="{ locked: engine.locked, inactive: engine.inactive }">
                <div class="engine-main"><span class="engine-avatar">{{ engine.label.slice(0, 1).toUpperCase() }}</span><div><h3>{{ engine.label }}</h3><p><code>{{ engine.name }}</code><span>!{{ engine.shortcut }}</span><span v-for="category in engine.categories.slice(0, 3)" :key="category">{{ category }}</span></p></div></div>
                <div class="engine-badges">
                  <span :class="engine.defaultEnabled ? 'positive' : 'neutral'">默认{{ engine.defaultEnabled ? '启用' : '禁用' }}</span>
                  <span v-if="engine.origin === 'custom'" class="custom">自定义</span>
                  <span v-if="engine.inactive" class="unavailable">inactive / 不可用</span>
                </div>
                <p v-if="engine.description" class="engine-description">{{ engine.description }}</p>
                <p v-if="engine.inactive" class="engine-warning">该引擎被 SearXNG 标记为 inactive，无法在此直接启用；请先在高级配置中补齐依赖或参数。</p>
                <p v-if="engine.warning" class="engine-warning">⚠ {{ engine.warning }}</p>
                <label class="compact-switch"><input v-model="engine.enabled" type="checkbox" :disabled="engine.locked || engine.inactive"><i></i><span>{{ engine.inactive ? '不可用' : engine.enabled ? '已启用' : '已禁用' }}</span></label>
              </article>
              <div v-if="filteredEngines.length === 0" class="empty-state">没有找到匹配的搜索引擎。</div>
            </div>
          </section>

          <section v-else class="section-stack">
            <article class="panel raw-panel">
              <div class="panel-title"><div><h3>原始 settings.yml</h3><p>适合高级配置。后端会校验 YAML，并保留密钥与未知字段。</p></div><div class="header-actions"><button class="button ghost" type="button" :disabled="rawLoading" @click="loadRawYaml">重新读取</button><button class="button ghost" type="button" @click="exportRawYaml">导出 YAML</button></div></div>
              <textarea v-model="rawYaml" spellcheck="false" aria-label="原始 YAML"></textarea>
              <div class="raw-actions"><span>导入会替换可编辑配置，请先导出备份。</span><button class="button primary" type="button" :disabled="rawLoading || !rawYaml.trim()" @click="saveRawYaml">{{ rawLoading ? '处理中…' : '校验并导入' }}</button></div>
            </article>
          </section>
        </template>
      </section>
    </div>

    <div class="notice" :class="noticeTone" aria-live="polite"><span>{{ notice }}</span><button v-if="restartRequired" type="button" @click="restartRequired = false">知道了</button></div>
  </main>
</template>
