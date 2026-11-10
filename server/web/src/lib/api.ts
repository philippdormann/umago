// Thin typed client for the Umago API.

export type Range = '24h' | '48h' | '7d' | '30d' | '90d' | '180d' | 'custom'
export const RANGES: Exclude<Range, 'custom'>[] = ['24h', '48h', '7d', '30d', '90d', '180d']
export const DEFAULT_RANGE: Range = '7d'
export const isRange = (v: string): v is Range => (RANGES as string[]).includes(v) || v === 'custom'

export interface Point {
  t: string
  pageviews: number
  visitors: number
}

export interface Row {
  key: string
  pageviews: number
  visitors: number
}

export interface Totals {
  pageviews: number
  visitors: number
  visits?: number
  bounces?: number
  duration?: number
}

export type Dim =
  | 'page' | 'ref' | 'entry' | 'exit' | 'channel' | 'country' | 'region' | 'language'
  | 'device' | 'browser' | 'os' | 'title' | 'hostname' | 'tag' | 'event'
  | 'utm_source' | 'utm_medium' | 'utm_campaign' | 'utm_content' | 'utm_term'
  | 'gclid' | 'fbclid' | 'msclkid' | 'ttclid' | 'li_fat_id' | 'twclid'
  | 'distinct_id'

export interface Marker {
  t: string
  ref: string
  visitors: number
}

/** Dimension to key. An empty value is a real filter (direct, unknown). */
export type Filters = Partial<Record<Dim, string>>

export function filterQuery(filters: Filters): string {
  const q = new URLSearchParams()
  for (const [dim, key] of Object.entries(filters)) if (key !== undefined) q.set(dim, key)
  const s = q.toString()
  return s ? '&' + s : ''
}

export interface Summary {
  range: Range
  from: string
  to: string
  bucket: 'hour' | 'day'
  totals: Totals
  previous: Totals
  series: Point[]
  markers: Marker[]
  breakdowns: Record<Dim, Row[]>
  filters?: Filters
  truncated?: boolean
  previous_unavailable?: boolean
  retention_days?: number
  prev_series?: Point[]
  prev_breakdowns?: Record<Dim, Row[]>
}

export interface Live {
  total: number
  countries: Row[]
  recent: { at: string; country: string; path: string }[]
  /** Top pages right now (last 5 minutes), most pageviews first. */
  pages: Row[]
  minutes: number[] // distinct visitors per minute, last 30, oldest first
  total_30m: number
}

export interface SiteCard {
  visitors: number
  previous: number
  pageviews: number
  visits?: number
  spark: Point[]
}

export interface Site {
  id: string
  name: string
  domain: string
  home_country: string
  /** Overrides the account-wide accent on this site's dashboard; '' follows it. */
  accent: string
  /** Range this site's dashboard opens on; '' uses DEFAULT_RANGE. */
  default_range: string
  /** Team owning the site; '' is visible to everyone signed in. */
  owner_team_id: string
  has_favicon: boolean
  position: number
  created_at: string
  updated_at: string
  card: SiteCard
  live: number
}

export interface AuthState {
  auth_required: boolean
  authenticated: boolean
  username?: string
  role?: string
  requires_2fa?: boolean
  challenge?: string
}

export interface Status {
  version: string
  sites: number
  raw_events: number
  daily_rows: number
  db_bytes: number
  geoip_bytes: number
  uptime_seconds: number
  written: number
  dropped: number
  admin_auth: boolean
  env_token_set: boolean
}

export interface GeneralSettings {
  accent: string
  title: string
  mcp_enabled: boolean
  retention_days: number
  retention_from_env: boolean
}

export interface Token {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
}

export interface GoogleConnection {
  site_id: string
  property: string
  email: string
  connected_at: string
  synced_at: string
  sync_error: string
}

export interface GoogleStatus {
  configured: boolean
  connected: boolean
  connection?: GoogleConnection
  available_properties?: string[]
  needs_reconnect: boolean
  latest_day?: string
}

export interface SearchTerm {
  query: string
  clicks: number
  impressions: number
  position: number
}

export interface PolarConnection {
  site_id: string
  server: string
  product_ids: string
  has_webhook_secret: boolean
  connected_at: string
  synced_at: string
  sync_error: string
}

export interface PolarStatus {
  connected: boolean
  connection?: PolarConnection
  webhook_url: string
  orders: number
}

export interface RevenuePoint {
  t: string
  revenue: number // minor units
  orders: number
}

export interface RevenueTotals {
  revenue: number
  orders: number
}

export interface RevenueRow {
  key: string
  revenue: number
  orders: number
}

export type RevenueDim = 'ref' | 'source' | 'campaign' | 'landing' | 'country' | 'product'

export interface Revenue {
  range: Range
  currency: string
  totals: RevenueTotals
  previous: RevenueTotals
  series: RevenuePoint[]
  breakdowns: Record<RevenueDim, RevenueRow[]>
}

export interface FunnelStep {
  type: 'page' | 'event'
  value: string
}

export interface Funnel {
  id: string
  site_id: string
  name: string
  steps: FunnelStep[]
  created_at: string
  updated_at: string
}

export interface FunnelStepResult {
  step: FunnelStep
  visitors: number
  rate: number
  drop: number
}

export interface FunnelResult {
  steps: FunnelStepResult[]
  truncated?: boolean
  conversion: number
}

export interface JourneyPath {
  pages: string[]
  visits: number
  rate: number
}

export interface JourneyStepPage {
  page: string
  visits: number
}

export interface JourneyTransition {
  step: number
  from: string
  to: string
  visits: number
}

export interface JourneyReport {
  steps: number
  visits: number
  paths: JourneyPath[]
  step_pages: JourneyStepPage[][]
  transitions: JourneyTransition[]
  truncated?: boolean
}

export interface VisitRow {
  visit_id: string
  started: string
  duration_secs: number
  pageviews: number
  events: number
  entry: string
  exit: string
  country: string
  browser: string
  device: string
}

export interface VitalStats {
  count: number
  p50: number
  p75: number
  p95: number
}

export interface VitalsReport {
  lcp: VitalStats
  inp: VitalStats
  cls: VitalStats
  fcp: VitalStats
  ttfb: VitalStats
  paths: { path: string; count: number; p75: number }[]
}

export interface EventKey {
  key: string
  events: number
}

export interface EventValue {
  value: string
  count: number
}

export interface Link {
  id: string
  site_id: string
  slug: string
  name: string
  url: string
  kind: string
  created_at: string
  created_by: string
  clicks: number
}

export interface User {
  id: string
  username: string
  role: string
  totp_enabled: boolean
  created_at: string
}

export interface Team {
  id: string
  name: string
  created_at: string
}

export interface Member {
  team_id: string
  user_id: string
  username: string
  role: string
}

export interface APIKey {
  id: string
  name: string
  prefix: string
  scopes: string
  expires_at?: string
  created_at: string
  last_used_at?: string
  rotated_at?: string
}

export interface Share {
  id: string
  site_id: string
  slug: string
  created_at: string
  created_by: string
}

export interface GeoIPStatus {
  mode: 'off' | 'country' | 'city'
  mode_from_env: boolean
  source: 'dbip' | 'maxmind'
  source_from_env: boolean
  has_maxmind_key: boolean
  key_from_env: boolean
  edition: string
  build_date: string
  size_bytes: number
  has_data: boolean
  sync_error: string
  synced_at: string
  file_path: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

let onLoginRequired: (() => void) | null = null
export function setLoginHandler(fn: (() => void) | null) {
  onLoginRequired = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let parsed: any = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }
  if (!res.ok) {
    if (res.status === 401 && parsed?.error === 'login_required') onLoginRequired?.()
    throw new ApiError(res.status, parsed?.error ?? 'error', parsed?.message ?? `Request failed (${res.status})`)
  }
  return parsed as T
}

export const api = {
  me: () => request<AuthState>('GET', '/api/v1/auth/me'),
  login: (username: string, password: string) => request<AuthState>('POST', '/api/v1/auth/login', { username, password }),
  logout: () => request<void>('POST', '/api/v1/auth/logout'),
  verify2FA: (token: string, code: string) => request<AuthState>('POST', '/api/v1/auth/2fa/verify', { token, code }),
  totpStatus: () => request<{ enabled: boolean }>('GET', '/api/v1/auth/2fa/status'),
  totpSetup: () => request<{ secret: string; otpauth_url: string; qr: string }>('POST', '/api/v1/auth/2fa/setup'),
  totpConfirm: (code: string) => request<{ enabled: boolean; backup_codes: string[] }>('POST', '/api/v1/auth/2fa/confirm', { code }),
  totpDisable: (password: string) => request<{ enabled: boolean }>('POST', '/api/v1/auth/2fa/disable', { password }),
  users: () => request<{ users: User[] }>('GET', '/api/v1/users'),
  createUser: (input: { username: string; password: string; role?: string }) => request<User>('POST', '/api/v1/users', input),
  updateUser: (id: string, patch: { role?: string; password?: string }) => request<User>('PATCH', `/api/v1/users/${id}`, patch),
  deleteUser: (id: string) => request<void>('DELETE', `/api/v1/users/${id}`),
  teams: () => request<{ teams: Team[] }>('GET', '/api/v1/teams'),
  createTeam: (name: string) => request<Team>('POST', '/api/v1/teams', { name }),
  renameTeam: (id: string, name: string) => request<Team>('PATCH', `/api/v1/teams/${id}`, { name }),
  deleteTeam: (id: string) => request<void>('DELETE', `/api/v1/teams/${id}`),
  members: (tid: string) => request<{ members: Member[] }>('GET', `/api/v1/teams/${tid}/members`),
  addMember: (tid: string, user_id: string, role: string) => request<{ members: Member[] }>('POST', `/api/v1/teams/${tid}/members`, { user_id, role }),
  removeMember: (tid: string, uid: string) => request<void>('DELETE', `/api/v1/teams/${tid}/members/${uid}`),
  keys: () => request<{ keys: APIKey[] }>('GET', '/api/v1/keys'),
  createKey: (name: string, scopes = 'read,write') => request<{ key: APIKey; secret: string }>('POST', '/api/v1/keys', { name, scopes }),
  rotateKey: (id: string) => request<{ key: APIKey; secret: string }>('POST', `/api/v1/keys/${id}/rotate`),
  deleteKey: (id: string) => request<void>('DELETE', `/api/v1/keys/${id}`),
  shares: (id: string) => request<{ shares: Share[] }>('GET', `/api/v1/sites/${id}/shares`),
  createShare: (id: string) => request<Share>('POST', `/api/v1/sites/${id}/shares`),
  deleteShare: (id: string, sid: string) => request<void>('DELETE', `/api/v1/sites/${id}/shares/${sid}`),
  publicShare: (slug: string, range?: string) => request<{ site: { name: string; domain: string }; live: number; stats: Summary }>('GET', `/api/v1/share/${slug}${range ? `?range=${range}` : ''}`),

  sites: () => request<{ sites: Site[] }>('GET', '/api/v1/sites'),
  site: (id: string) => request<Site>('GET', `/api/v1/sites/${id}`),
  createSite: (input: { name?: string; domain: string }) => request<Site>('POST', '/api/v1/sites', input),
  updateSite: (id: string, patch: Partial<Pick<Site, 'name' | 'domain' | 'home_country' | 'accent' | 'default_range' | 'owner_team_id'>>) => request<Site>('PATCH', `/api/v1/sites/${id}`, patch),
  deleteSite: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}`),
  reorderSites: (ids: string[]) => request<{ sites: Site[] }>('POST', '/api/v1/sites/reorder', { ids }),
  refreshFavicon: (id: string) => request<Site>('POST', `/api/v1/sites/${id}/refresh-favicon`),
  live: (id: string) => request<Live>('GET', `/api/v1/sites/${id}/live`),
  breakdown: (id: string, dim: Dim, range: Range, filters: Filters = {}, opts: { from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    return request<{ dim: Dim; range: Range; rows: Row[] }>('GET', `/api/v1/sites/${id}/breakdown?dim=${dim}&range=${range}&limit=500${extra}${filterQuery(filters)}`)
  },
  // An empty range asks the server for the site's own default; the answer says which it used.
  stats: (id: string, range: Range | '', filters: Filters = {}, opts: { compare?: boolean; from?: string; to?: string } = {}) => {
    const extra = (range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : '') + (opts.compare ? '&compare=prev' : '')
    return request<{ site: Site; live: number; stats: Summary }>('GET', `/api/v1/sites/${id}/stats?range=${range}${extra}${filterQuery(filters)}`)
  },
  eventData: (id: string, event: string, range: Range, key = '', opts: { from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    return request<{ event: string; key?: string; range: Range; keys?: EventKey[]; values?: EventValue[]; truncated?: boolean }>(
      'GET', `/api/v1/sites/${id}/event-data?event=${encodeURIComponent(event)}${key ? `&key=${encodeURIComponent(key)}` : ''}&range=${range}${extra}`,
    )
  },
  vitals: (id: string, range: Range, opts: { from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    return request<{ range: Range; vitals: VitalsReport }>('GET', `/api/v1/sites/${id}/vitals?range=${range}${extra}`)
  },
  sessions: (id: string, range: Range, opts: { from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    return request<{ range: Range; sessions: VisitRow[]; truncated?: boolean }>('GET', `/api/v1/sites/${id}/sessions?range=${range}${extra}`)
  },
  resetSite: (id: string) => request<void>('POST', `/api/v1/sites/${id}/reset`),
  funnels: (id: string) => request<{ funnels: Funnel[] }>('GET', `/api/v1/sites/${id}/funnels`),
  createFunnel: (id: string, input: { name: string; steps: FunnelStep[] }) => request<Funnel>('POST', `/api/v1/sites/${id}/funnels`, input),
  updateFunnel: (id: string, fid: string, input: { name: string; steps: FunnelStep[] }) => request<Funnel>('PUT', `/api/v1/sites/${id}/funnels/${fid}`, input),
  deleteFunnel: (id: string, fid: string) => request<void>('DELETE', `/api/v1/sites/${id}/funnels/${fid}`),
  funnel: (id: string, fid: string, range: Range, opts: { from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    return request<{ funnel: Funnel; range: Range; result: FunnelResult }>('GET', `/api/v1/sites/${id}/funnels/${fid}?range=${range}${extra}`)
  },
  journeys: (id: string, range: Range, opts: { steps?: number; limit?: number; start?: string; from?: string; to?: string } = {}) => {
    const extra = range === 'custom' && opts.from && opts.to ? `&from=${opts.from}&to=${opts.to}` : ''
    const qp = (opts.steps ? `&steps=${opts.steps}` : '') + (opts.limit ? `&limit=${opts.limit}` : '') + (opts.start ? `&start=${encodeURIComponent(opts.start)}` : '')
    return request<{ range: Range; journeys: JourneyReport; truncated?: boolean }>('GET', `/api/v1/sites/${id}/journeys?range=${range}${extra}${qp}`)
  },
  links: (id: string) => request<{ links: Link[] }>('GET', `/api/v1/sites/${id}/links`),
  createLink: (id: string, input: { slug: string; name?: string; url: string; kind?: string }) => request<Link>('POST', `/api/v1/sites/${id}/links`, input),
  deleteLink: (id: string, linkID: string) => request<void>('DELETE', `/api/v1/sites/${id}/links/${linkID}`),
  status: () => request<Status>('GET', '/api/v1/status'),
  theme: () => request<{ accent: string; title: string; script_name: string }>('GET', '/api/v1/theme'),
  settings: () => request<GeneralSettings>('GET', '/api/v1/settings'),
  updateSettings: (patch: Partial<Omit<GeneralSettings, 'retention_from_env'>>) => request<GeneralSettings>('PATCH', '/api/v1/settings', patch),
  tokens: () => request<{ tokens: Token[]; env_token_set: boolean }>('GET', '/api/v1/tokens'),
  createToken: (name: string) => request<{ token: Token; secret: string }>('POST', '/api/v1/tokens', { name }),
  deleteToken: (id: string) => request<void>('DELETE', `/api/v1/tokens/${id}`),
  rollup: () => request<void>('POST', '/api/v1/rollup'),

  geoip: () => request<GeoIPStatus>('GET', '/api/v1/geoip'),
  updateGeoip: (patch: { mode?: string; source?: string; maxmind_key?: string }) => request<GeoIPStatus>('PUT', '/api/v1/geoip', patch),
  refreshGeoip: () => request<GeoIPStatus>('POST', '/api/v1/geoip/refresh'),
  deleteGeoipData: () => request<GeoIPStatus>('DELETE', '/api/v1/geoip/data'),
  uploadGeoip: async (file: File): Promise<GeoIPStatus> => {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch('/api/v1/geoip/upload', { method: 'POST', body: form })
    if (res.status === 204) return undefined as unknown as GeoIPStatus
    const text = await res.text()
    let parsed: any = null
    try {
      parsed = text ? JSON.parse(text) : null
    } catch {
      parsed = null
    }
    if (!res.ok) throw new ApiError(res.status, parsed?.error ?? 'error', parsed?.message ?? `Request failed (${res.status})`)
    return parsed as GeoIPStatus
  },

  google: (id: string) => request<{ status: GoogleStatus; redirect_uri: string }>('GET', `/api/v1/sites/${id}/google`),
  googleSetProperty: (id: string, property: string) => request<{ status: GoogleStatus; redirect_uri: string }>('PATCH', `/api/v1/sites/${id}/google`, { property }),
  googleDisconnect: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}/google`),
  googleSync: (id: string) => request<{ status: GoogleStatus; redirect_uri: string }>('POST', `/api/v1/sites/${id}/google/sync`),
  searchTerms: (id: string, range: Range) => request<{ range: Range; rows: SearchTerm[] }>('GET', `/api/v1/sites/${id}/search-terms?range=${range}&limit=500`),
}

export const polarApi = {
  status: (id: string) => request<PolarStatus>('GET', `/api/v1/sites/${id}/polar`),
  connect: (id: string, input: { access_token?: string; server?: string; product_ids?: string; webhook_secret?: string }) => request<PolarStatus>('PUT', `/api/v1/sites/${id}/polar`, input),
  disconnect: (id: string) => request<void>('DELETE', `/api/v1/sites/${id}/polar`),
  sync: (id: string) => request<PolarStatus>('POST', `/api/v1/sites/${id}/polar/sync`),
  revenue: (id: string, range: Range, limit = 10) => request<Revenue>('GET', `/api/v1/sites/${id}/revenue?range=${range}&limit=${limit}`),
}

/** Browser destination that starts the Google Search Console connect flow. */
export const googleConnectURL = (id: string) => `/api/v1/sites/${id}/google/connect`

/** URL of a site's stored icon (404 when none). */
export const siteIconURL = (id: string) => `/api/v1/sites/${id}/favicon`
/** URL of a cached referrer icon (404 when none). */
export const refIconURL = (host: string) => `/api/v1/favicon?host=${encodeURIComponent(host)}`
