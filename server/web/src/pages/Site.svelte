<script lang="ts">
  // Per-site dashboard: metrics, chart, breakdowns, world map, settings.
  import { api, DEFAULT_RANGE, googleConnectURL, isRange, polarApi, RANGES, refIconURL, siteIconURL, type Dim, type EventKey, type EventValue, type Filters, type Funnel, type FunnelStep, type GoogleStatus, type JourneyReport, type Link, type Live, type PolarStatus, type Range, type Revenue, type RevenueDim, type Row, type SearchTerm, type Share, type Site, type Summary, type Team, type VisitRow, type VitalStats, type VitalsReport } from '../lib/api'
  import { setAccentOverride } from '../lib/accent'
  import { countryName, flag, fmtAgo, fmtAvgDur, fmtBounce, fmtDelta, fmtDur, fmtExact, fmtMoney, fmtNum, fmtRatio } from '../lib/format'
  import { pageIn, panel } from '../lib/motion'
  import Icon from '../lib/ui/Icon.svelte'
  import Segment from '../lib/ui/Segment.svelte'
  import Swatches from '../lib/ui/Swatches.svelte'
  import MetricStat from '../lib/ui/MetricStat.svelte'
  import BarList, { type BarRow } from '../lib/ui/BarList.svelte'
  import AreaChart from '../lib/ui/AreaChart.svelte'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import BrandIcon from '../lib/ui/BrandIcon.svelte'
  import Realtime from '../lib/ui/Realtime.svelte'
  import { loadScriptName, setScriptName, snippetFor } from '../lib/script.svelte'
  import { confirmAction } from '../lib/confirm.svelte'

  let { id }: { id: string } = $props()
  let site = $state<Site | null>(null)
  let stats = $state<Summary | null>(null)
  let live = $state(0)
  // The dashboard opens on the range saved against the site. The first stats
  // call leaves the range out and the server answers with the one it used, so
  // learning the site's default costs no extra round trip.
  let range = $state<Range | ''>('')
  let customFrom = $state('')
  let customTo = $state('')
  let compare = $state(false)
  const rangeOpts = $derived(range === 'custom' && customFrom && customTo ? { from: customFrom, to: customTo } : {})
  let error = $state('')
  // Click-to-filter: dimension to key, mirrored into the URL so back and
  // share work. Filtered views come from raw events, so the server may
  // truncate them to the retention window.
  const DIMS: Dim[] = ['page', 'ref', 'entry', 'exit', 'channel', 'country', 'region', 'language', 'device', 'browser', 'os', 'title', 'hostname', 'tag', 'event', 'utm_source', 'utm_medium', 'utm_campaign', 'utm_content', 'utm_term', 'gclid', 'fbclid', 'msclkid', 'ttclid', 'li_fat_id', 'twclid', 'distinct_id']
  function filtersFromURL(): Filters {
    const q = new URLSearchParams(location.search)
    const f: Filters = {}
    for (const d of DIMS) if (q.has(d)) f[d] = q.get(d) ?? ''
    return f
  }
  let filters = $state<Filters>(filtersFromURL())
  const hasFilters = $derived(Object.keys(filters).length > 0)
  function setFilters(next: Filters) {
    filters = next
    const q = new URLSearchParams(location.search)
    for (const d of DIMS) q.delete(d)
    for (const [d, k] of Object.entries(next)) if (k !== undefined) q.set(d, k)
    const s = q.toString()
    history.pushState(null, '', location.pathname + (s ? '?' + s : ''))
  }
  function toggleFilter(dim: Dim, key: string) {
    const next = { ...filters }
    if (next[dim] === key) delete next[dim]
    else next[dim] = key
    setFilters(next)
  }
  const select = (dim: Dim) => (r: BarRow) => toggleFilter(dim, r.key === '∅' ? '' : r.key === 'direct' ? '' : r.key === 'XX' ? '' : r.key)
  $effect(() => {
    const onPop = () => (filters = filtersFromURL())
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  })
  const filterLabel = (dim: Dim, key: string) =>
    dim === 'country' ? countryName(key) || 'Unknown' : dim === 'ref' ? key || 'Direct' : key || 'Unknown'
  const FILTER_DIM: Record<Dim, string> = {
    page: 'Page', ref: 'Referrer', entry: 'Entry', exit: 'Exit', channel: 'Channel',
    utm_source: 'Source', utm_medium: 'Medium', utm_campaign: 'Campaign', utm_content: 'Content', utm_term: 'Term',
    gclid: 'gclid', fbclid: 'fbclid', msclkid: 'msclkid', ttclid: 'ttclid', li_fat_id: 'li_fat_id', twclid: 'twclid',
    country: 'Country', region: 'Region', language: 'Language',
    browser: 'Browser', device: 'Device', os: 'OS', title: 'Title', hostname: 'Host', tag: 'Tag',
    event: 'Event', distinct_id: 'User',
  }
  const selectedKey = (dim: Dim) => (filters[dim] === undefined ? undefined : filters[dim] === '' ? (dim === 'ref' ? 'direct' : dim === 'country' ? 'XX' : '∅') : filters[dim])
  let settingsOpen = $state(false)
  let copiedSnippet = $state(false)
  let copiedShareId = $state<string | null>(null)
  let pageVisible = $state(typeof document === 'undefined' ? true : !document.hidden)
  let meRole = $state('')
  const canWrite = $derived(meRole !== 'viewer')
  $effect(() => {
    api.me().then((m) => (meRole = m.role ?? '')).catch(() => {})
  })
  // Public share links: read-only dashboard URLs for one site.
  let shares = $state<Share[]>([])
  let teams = $state<Team[]>([])
  async function loadShares() {
    try {
      shares = (await api.shares(id)).shares
    } catch {
      shares = []
    }
  }
  async function loadTeams() {
    try {
      teams = (await api.teams()).teams
    } catch {
      teams = []
    }
  }
  $effect(() => {
    loadShares()
    loadTeams()
  })
  async function addShare() {
    try {
      const sh = await api.createShare(id)
      shares = [...shares, sh]
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeShare(sh: Share) {
    const ok = await confirmAction({
      title: 'Revoke share link?',
      message: 'Anyone with the link loses access immediately.',
      confirmLabel: 'Revoke',
    })
    if (!ok) return
    api
      .deleteShare(id, sh.id)
      .then(() => (shares = shares.filter((x) => x.id !== sh.id)))
      .catch((e: any) => (error = e.message))
  }
  const shareURL = (slug: string) => `${location.origin}/share/${slug}`
  async function copyShare(slug: string) {
    try {
      await navigator.clipboard.writeText(shareURL(slug))
      copiedShareId = slug
      setTimeout(() => {
        if (copiedShareId === slug) copiedShareId = null
      }, 1500)
    } catch {}
  }
  // The map pulls in MapLibre; load it only once the dashboard is up.
  let WorldMap = $state<typeof import('../lib/map/WorldMap.svelte').default | null>(null)
  let LiveMap = $state<typeof import('../lib/map/LiveMap.svelte').default | null>(null)
  let mapView = $state<'live' | 'range'>('live')
  let liveData = $state<Live | null>(null)

  // Google Search Console: connection status for the settings panel and
  // the search terms it feeds. Google's data trails by two to three days.
  let google = $state<GoogleStatus | null>(null)
  let googleRedirect = $state('')
  let googleBusy = $state(false)
  let googleNotice = $state('')
  let terms = $state<SearchTerm[]>([])

  async function loadGoogle() {
    try {
      const r = await api.google(id)
      google = r.status
      googleRedirect = r.redirect_uri
    } catch (e: any) {
      google = null
    }
  }
  async function loadTerms() {
    if (!google?.connected || !range) {
      terms = []
      return
    }
    try {
      terms = (await api.searchTerms(id, range)).rows
    } catch {
      terms = []
    }
  }
  async function googleAction(run: () => Promise<unknown>) {
    googleBusy = true
    try {
      await run()
      await loadGoogle()
      await loadTerms()
    } catch (e: any) {
      error = e.message
    } finally {
      googleBusy = false
    }
  }
  async function disconnectGoogle() {
    const ok = await confirmAction({
      title: 'Disconnect Google?',
      message: 'Stored search terms for this site are deleted.',
      confirmLabel: 'Disconnect',
    })
    if (!ok) return
    googleAction(() => api.googleDisconnect(id))
  }
  // The callback lands here with ?google=connected or ?google_error=…
  $effect(() => {
    const params = new URLSearchParams(location.search)
    const err = params.get('google_error')
    if (params.get('google') === 'connected') {
      googleNotice = 'Google Search Console connected. The first pull can take a minute.'
      settingsOpen = true
    } else if (err) {
      error = 'Google: ' + err
      settingsOpen = true
    }
    if (params.has('google') || params.has('google_error')) history.replaceState(null, '', location.pathname)
    loadGoogle()
  })
  $effect(() => {
    range
    google?.connected
    loadTerms()
  })
  // The card shows the same top 10 as the other breakdowns; the modal has everything.
  const TOP_TERMS = 10
  // Polar: revenue next to traffic, attributed to first touch when the
  // site passes it into checkout metadata.
  let polar = $state<PolarStatus | null>(null)
  let revenue = $state<Revenue | null>(null)
  let polarBusy = $state(false)
  let polarForm = $state({ access_token: '', server: '', product_ids: '', webhook_secret: '' })
  let polarOpen = $state(false)
  let revenueTab = $state<RevenueDim>('ref')

  async function loadPolar() {
    try {
      polar = await polarApi.status(id)
      if (polar.connection) polarForm = { access_token: '', server: polar.connection.server, product_ids: polar.connection.product_ids, webhook_secret: '' }
    } catch {
      polar = null
    }
  }
  async function loadRevenue() {
    if (!polar?.connected || !range) {
      revenue = null
      return
    }
    try {
      revenue = await polarApi.revenue(id, range)
    } catch {
      revenue = null
    }
  }
  async function polarAction(run: () => Promise<unknown>) {
    polarBusy = true
    try {
      await run()
      error = ''
      polarOpen = false
      await loadPolar()
      await loadRevenue()
    } catch (e: any) {
      error = e.message
    } finally {
      polarBusy = false
    }
  }
  function savePolar() {
    const input: Parameters<typeof polarApi.connect>[1] = { server: polarForm.server, product_ids: polarForm.product_ids }
    if (polarForm.access_token.trim()) input.access_token = polarForm.access_token.trim()
    if (polarForm.webhook_secret.trim()) input.webhook_secret = polarForm.webhook_secret.trim()
    polarAction(() => polarApi.connect(id, input))
  }
  async function disconnectPolar() {
    const ok = await confirmAction({
      title: 'Disconnect Polar?',
      message: 'Stored orders for this site are deleted.',
      confirmLabel: 'Disconnect',
    })
    if (!ok) return
    polarAction(() => polarApi.disconnect(id))
  }
  $effect(() => {
    loadPolar()
  })
  $effect(() => {
    range
    polar?.connected
    loadRevenue()
  })
  const REVENUE_DIMS: { value: RevenueDim; label: string }[] = [
    { value: 'ref', label: 'Referrer' }, { value: 'source', label: 'Source' }, { value: 'campaign', label: 'Campaign' },
    { value: 'landing', label: 'Landing' }, { value: 'country', label: 'Country' }, { value: 'product', label: 'Product' },
  ]
  const revenueRows = $derived(
    (revenue?.breakdowns[revenueTab] ?? []).map((r) => ({
      key: r.key || '∅',
      label: revenueTab === 'country' ? countryName(r.key) || 'Unknown' : revenueTab === 'ref' ? r.key || 'Direct or unattributed' : r.key || 'Unattributed',
      value: r.revenue,
      prefix: revenueTab === 'country' ? flag(r.key) : undefined,
      icon: revenueTab === 'ref' && r.key ? refIconURL(r.key) : '',
      direct: revenueTab === 'ref' && r.key === '',
      title: `${r.orders} ${r.orders === 1 ? 'order' : 'orders'}`,
    })),
  )
  const money = $derived((v: number) => fmtMoney(v, revenue?.currency ?? ''))
  const revenueSeries = $derived(revenue && stats && revenue.series.length === stats.series.length ? revenue.series.map((p) => p.revenue) : [])

  const termRows = $derived(
    terms.map((t) => ({
      key: t.query,
      label: t.query,
      value: t.clicks,
      title: `${fmtNum(t.impressions)} impressions · position ${t.position.toFixed(1)}`,
    })),
  )

  // Sessions: recent visits for the range.
  let sessions = $state<VisitRow[]>([])
  let sessionsTruncated = $state(false)
  async function loadSessions() {
    if (!range || (range === 'custom' && (!customFrom || !customTo))) {
      sessions = []
      return
    }
    try {
      const r = await api.sessions(id, range, rangeOpts)
      sessions = r.sessions
      sessionsTruncated = !!r.truncated
    } catch {
      sessions = []
    }
  }
  $effect(() => {
    range
    customFrom
    customTo
    loadSessions()
  })

  // Web vitals: shown when the snippet reports them (data-performance).
  let vitals = $state<VitalsReport | null>(null)
  async function loadVitals() {
    if (!range || (range === 'custom' && (!customFrom || !customTo))) {
      vitals = null
      return
    }
    try {
      vitals = (await api.vitals(id, range, rangeOpts)).vitals
    } catch {
      vitals = null
    }
  }
  $effect(() => {
    range
    customFrom
    customTo
    loadVitals()
  })
  const hasVitals = $derived(!!vitals && (vitals.lcp.count > 0 || vitals.inp.count > 0 || vitals.cls.count > 0))
  const vitalTiles = $derived<[string, VitalStats, string][]>(
    vitals ? [['LCP', vitals.lcp, 'ms'], ['INP', vitals.inp, 'ms'], ['CLS', vitals.cls, ''], ['FCP', vitals.fcp, 'ms'], ['TTFB', vitals.ttfb, 'ms']] : [],
  )

  // Funnels: saved definitions plus the evaluated one.
  let funnels = $state<Funnel[]>([])
  let funnelResults = $state<Record<string, { visitors: number; conversion: number; steps: { value: string; visitors: number; rate: number }[] }>>({})
  let funnelForm = $state<{ id: string; name: string; steps: FunnelStep[] } | null>(null)
  async function loadFunnels() {
    try {
      funnels = (await api.funnels(id)).funnels
      for (const f of funnels) void loadFunnelResult(f.id)
    } catch {
      funnels = []
    }
  }
  async function loadFunnelResult(fid: string) {
    if (!range || (range === 'custom' && (!customFrom || !customTo))) return
    try {
      const r = await api.funnel(id, fid, range, rangeOpts)
      funnelResults[fid] = { visitors: r.result.steps[0]?.visitors ?? 0, conversion: r.result.conversion, steps: r.result.steps.map((s) => ({ value: s.step.value, visitors: s.visitors, rate: s.rate })) }
    } catch {
      delete funnelResults[fid]
    }
  }
  $effect(() => {
    loadFunnels()
  })
  $effect(() => {
    range
    customFrom
    customTo
    for (const f of funnels) void loadFunnelResult(f.id)
  })
  function editFunnel(f?: Funnel) {
    funnelForm = f ? { id: f.id, name: f.name, steps: f.steps.map((s) => ({ ...s })) } : { id: '', name: '', steps: [{ type: 'page', value: '/' }, { type: 'event', value: '' }] }
  }
  async function saveFunnel() {
    if (!funnelForm || !funnelForm.name.trim()) return
    const steps = funnelForm.steps.map((s) => ({ type: s.type, value: s.value.trim() })).filter((s) => s.value)
    try {
      if (funnelForm.id) await api.updateFunnel(id, funnelForm.id, { name: funnelForm.name.trim(), steps })
      else await api.createFunnel(id, { name: funnelForm.name.trim(), steps })
      funnelForm = null
      error = ''
      await loadFunnels()
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeFunnel(fid: string) {
    const ok = await confirmAction({
      title: 'Delete funnel?',
      message: 'This funnel definition is deleted. Past visits stay in events.',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    try {
      await api.deleteFunnel(id, fid)
      await loadFunnels()
    } catch (e: any) {
      error = e.message
    }
  }

  // Journeys: the paths visits take, from raw page views.
  let journeys = $state<JourneyReport | null>(null)
  let journeysTruncated = $state(false)
  let journeySteps = $state(4)
  let journeyStart = $state('')
  let journeyStartDraft = $state('')
  async function loadJourneys() {
    if (!range || (range === 'custom' && (!customFrom || !customTo))) {
      journeys = null
      return
    }
    try {
      const r = await api.journeys(id, range, { steps: journeySteps, start: journeyStart || undefined, ...rangeOpts })
      journeys = r.journeys
      journeysTruncated = !!r.truncated || !!r.journeys.truncated
      error = ''
    } catch (e: any) {
      journeys = null
    }
  }
  $effect(() => {
    range
    customFrom
    customTo
    journeySteps
    journeyStart
    loadJourneys()
  })
  const journeyTop = $derived(journeys?.paths[0]?.visits ?? 1)

  // Short links and tracking pixels.
  let links = $state<Link[]>([])
  let linkForm = $state({ slug: '', name: '', url: '', kind: 'link' })
  let showLinkForm = $state(false)
  async function loadLinks() {
    try {
      links = (await api.links(id)).links
    } catch {
      links = []
    }
  }
  $effect(() => {
    loadLinks()
  })
  async function saveLink() {
    try {
      await api.createLink(id, { slug: linkForm.slug.trim(), name: linkForm.name.trim(), url: linkForm.url.trim(), kind: linkForm.kind })
      linkForm = { slug: '', name: '', url: '', kind: 'link' }
      showLinkForm = false
      error = ''
      await loadLinks()
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeLink(linkID: string) {
    const ok = await confirmAction({
      title: 'Delete link?',
      message: 'Recorded clicks stay in events.',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    try {
      await api.deleteLink(id, linkID)
      await loadLinks()
    } catch (e: any) {
      error = e.message
    }
  }
  async function resetData() {
    const ok = await confirmAction({
      title: `Reset ${site?.name ?? 'site'} data?`,
      message: 'Deletes every event and rollup for this site. The site, links and funnels stay.',
      confirmLabel: 'Reset data',
    })
    if (!ok) return
    try {
      await api.resetSite(id)
      error = ''
      requested = ''
      await load()
    } catch (e: any) {
      error = e.message
    }
  }

  // The window already asked for, so resolving the opening range below does
  // not fire the same query a second time.
  let requested = ''
  const windowKey = () => JSON.stringify([range, customFrom, customTo, compare, filters])

  async function load() {
    if (range === 'custom' && (!customFrom || !customTo)) return
    try {
      const r = await api.stats(id, range, filters, { compare, ...rangeOpts })
      site = r.site
      stats = r.stats
      live = r.live
      error = ''
      document.title = `${r.site.name} · Umago`
      if (!range) {
        range = isRange(r.stats.range) && r.stats.range !== 'custom' ? r.stats.range : DEFAULT_RANGE
        requested = windowKey() // this load already covered it
      }
    } catch (e: any) {
      error = e.message
    }
  }
  $effect(() => {
    const key = windowKey()
    if (key === requested) return
    requested = key
    load()
  })
  // A site's own colour takes over the whole UI while its dashboard is open,
  // and hands back to the account-wide accent on the way out.
  $effect(() => {
    setAccentOverride(site?.accent ?? '')
    return () => setAccentOverride('')
  })
  $effect(() => {
    import('../lib/map/WorldMap.svelte').then((m) => (WorldMap = m.default)).catch(() => {})
    import('../lib/map/LiveMap.svelte').then((m) => (LiveMap = m.default)).catch(() => {})
  })
  // Pause live polling while the tab is hidden so background tabs stay quiet.
  $effect(() => {
    const onVis = () => {
      pageVisible = !document.hidden
      if (!document.hidden) void load()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => document.removeEventListener('visibilitychange', onVis)
  })
  $effect(() => {
    if (!pageVisible) return
    const t = setInterval(() => {
      if (!document.hidden) void load()
    }, 60_000)
    return () => clearInterval(t)
  })
  // Live snapshot every 5s: feeds the realtime card and the live globe.
  $effect(() => {
    if (!pageVisible) return
    const poll = () => {
      if (document.hidden) return
      api.live(id).then((l) => (liveData = l)).catch(() => {})
    }
    poll()
    const t = setInterval(poll, 5000)
    return () => clearInterval(t)
  })

  // One mapping per dimension so the cards and the "view all" modal agree.
  const toRows = (dim: Dim, src: Row[]): BarRow[] => {
    const sorted = [...src].sort((a, b) => (dim === 'event' ? b.pageviews - a.pageviews : b.visitors - a.visitors))
    switch (dim) {
      case 'ref':
        return sorted.map((r) => ({ key: r.key || 'direct', label: r.key || 'Direct', value: r.visitors, direct: r.key === '', icon: r.key ? refIconURL(r.key) : '', title: `${fmtNum(r.pageviews)} views` }))
      case 'country':
        return sorted.map((r) => ({ key: r.key || 'XX', label: countryName(r.key), value: r.visitors, prefix: flag(r.key), title: `${fmtNum(r.pageviews)} views` }))
      case 'event':
        return sorted.map((r) => ({ key: r.key, label: r.key, value: r.pageviews, title: `${fmtNum(r.visitors)} visitors` }))
      case 'language':
        return sorted.map((r) => ({ key: r.key || '∅', label: r.key || 'Unknown', value: r.visitors, title: `${fmtNum(r.pageviews)} views` }))
      default:
        return sorted.map((r) => ({ key: r.key || '∅', label: r.key || 'Unknown', value: r.visitors, title: `${fmtNum(r.pageviews)} views` }))
    }
  }
  const DIM_TITLE: Record<Dim, string> = {
    page: 'Pages', ref: 'Referrers', entry: 'Entry pages', exit: 'Exit pages', channel: 'Channels',
    utm_source: 'Sources', utm_medium: 'Mediums', utm_campaign: 'Campaigns', utm_content: 'Content', utm_term: 'Terms',
    gclid: 'gclid clicks', fbclid: 'fbclid clicks', msclkid: 'msclkid clicks', ttclid: 'ttclid clicks', li_fat_id: 'LinkedIn clicks', twclid: 'X/Twitter clicks',
    country: 'Countries', region: 'Regions', language: 'Languages',
    browser: 'Browsers', device: 'Devices', os: 'Operating systems', title: 'Titles', hostname: 'Hosts', tag: 'Tags',
    event: 'Events', distinct_id: 'Users',
  }
  // Card tabs, as in the reference: one card per group, a dimension per tab.
  let sourceTab = $state<'ref' | 'channel' | 'utm_source' | 'utm_medium' | 'utm_campaign' | 'utm_content' | 'utm_term'>('ref')
  let adsTab = $state<'gclid' | 'fbclid' | 'msclkid' | 'ttclid' | 'li_fat_id' | 'twclid'>('gclid')
  let pagesTab = $state<'page' | 'entry' | 'exit'>('page')
  let locationTab = $state<'country' | 'region'>('country')
  let deviceTab = $state<'browser' | 'os' | 'device' | 'language'>('browser')
  const rowsFor = (dim: Dim) => toRows(dim, stats?.breakdowns[dim] ?? [])
  const pages = $derived(rowsFor(pagesTab))
  // Live visitors per page right now (last 5 minutes), keyed by page path.
  // Only the Pages tab has per-page live data; Entry/Exit keep range titles.
  const livePages = $derived(new Map((liveData?.pages ?? []).map((p) => [p.key, p])))
  const livePagesTotal = $derived((liveData?.pages ?? []).reduce((a, p) => a + p.visitors, 0))
  const pagesTitle = $derived(livePagesTotal > 0 ? `Pages · ${livePagesTotal} online` : 'Pages')
  const pagesWithLive = $derived(
    pagesTab === 'page' && livePages.size > 0
      ? pages.map((r) => {
          const lp = livePages.get(r.key)
          if (!lp) return r
          return {
            ...r,
            label: `${r.label} · ${lp.visitors} live`,
            title: `${r.title ?? r.label} · ${lp.visitors} online now (${lp.pageviews} ${lp.pageviews === 1 ? 'view' : 'views'} in the last 5 minutes)`,
          }
        })
      : pages,
  )
  const sources = $derived(rowsFor(sourceTab))
  const ads = $derived(rowsFor(adsTab))
  const locations = $derived(rowsFor(locationTab))
  const devices = $derived(rowsFor(deviceTab))
  const events = $derived(rowsFor('event'))

  // "View all" modal.
  let modal = $state<Dim | 'search' | null>(null)
  let modalRows = $state<BarRow[] | null>(null)
  let filter = $state('')
  // Event properties drilldown inside the modal.
  let propsEvent = $state('')
  let propsKeys = $state<EventKey[]>([])
  let propsKey = $state('')
  let propsValues = $state<EventValue[]>([])
  async function openProps(event: string) {
    propsEvent = event
    propsKey = ''
    propsValues = []
    try {
      propsKeys = (await api.eventData(id, event, range || DEFAULT_RANGE, '', rangeOpts)).keys ?? []
    } catch (e: any) {
      error = e.message
      propsKeys = []
    }
  }
  async function openPropValues(key: string) {
    propsKey = key
    try {
      propsValues = (await api.eventData(id, propsEvent, range || DEFAULT_RANGE, key, rangeOpts)).values ?? []
    } catch (e: any) {
      error = e.message
      propsValues = []
    }
  }
  function openAll(dim: Dim | 'search') {
    modal = dim
    modalRows = null
    filter = ''
    propsEvent = ''
    propsKeys = []
    propsKey = ''
    propsValues = []
    if (dim === 'search') {
      modalRows = termRows
      return
    }
    api
      .breakdown(id, dim, range || DEFAULT_RANGE, filters, rangeOpts)
      .then((r) => (modalRows = toRows(dim, r.rows)))
      .catch((e: any) => (error = e.message))
  }
  const filtered = $derived((modalRows ?? []).filter((r) => !filter || r.label.toLowerCase().includes(filter.toLowerCase())))
  const more = (dim: Dim | 'search') => () => openAll(dim)
  const topCountry = $derived(stats?.breakdowns.country?.[0]?.key ?? '')

  const snippet = $derived(site ? snippetFor(site.id) : '')
  // Keep the canonical script name fresh (first UMAGO_SCRIPT_NAMES entry).
  $effect(() => {
    loadScriptName().then((n) => setScriptName(n))
  })
  async function copy() {
    try {
      await navigator.clipboard.writeText(snippet)
      copiedSnippet = true
      setTimeout(() => (copiedSnippet = false), 1500)
    } catch {}
  }
  // Empty state: no pageviews and no visitors in range and no events yet.
  // Show an onboarding hero instead of a wall of zero cards.
  const isEmpty = $derived(!!stats && stats.totals.visitors === 0 && stats.totals.pageviews === 0 && events.length === 0 && !hasFilters)
  let checking = $state(false)
  async function checkAgain() {
    if (checking) return
    checking = true
    try {
      await load()
      const l = await api.live(id)
      liveData = l
    } catch (e: any) {
      error = e.message
    } finally {
      checking = false
    }
  }
  let timers: Record<string, ReturnType<typeof setTimeout>> = {}
  function save(patch: Parameters<typeof api.updateSite>[1], key: string) {
    clearTimeout(timers[key])
    timers[key] = setTimeout(() => {
      api
        .updateSite(id, patch)
        .then((s) => (site = { ...site!, ...s }))
        .catch((e: any) => (error = e.message))
    }, 400)
  }
  /** Per-site accent; '' hands the page back to the account-wide colour. */
  function pickAccent(hex: string) {
    setAccentOverride(hex) // instant preview
    save({ accent: hex }, 'accent')
  }
  /** The range this dashboard opens on, and the one it switches to now. */
  function pickDefaultRange(r: Range) {
    range = r
    save({ default_range: r }, 'default_range')
  }
</script>

{#if error}
  <div class="error-banner" role="alert">
    <span class="error-text">{error}</span>
    <span class="spacer"></span>
    <button type="button" class="plain" onclick={() => load()}>Retry</button>
    <button type="button" class="plain" aria-label="Dismiss error" onclick={() => (error = '')}>Dismiss</button>
  </div>
{/if}

{#if site && stats}
  <div class="title">
    <Icon src={site.has_favicon ? siteIconURL(site.id) : ''} size={22} />
    <span class="name">{site.name}</span>
    <span class="domain">{site.domain}</span>
    {#if live > 0}<span class="live"><span class="dot"></span>{live} online</span>{/if}
    <span class="spacer"></span>
    <div class="ranges"><Segment options={RANGES.map((r) => ({ value: r, label: r }))} value={range === 'custom' ? ((site.default_range || DEFAULT_RANGE) as Exclude<Range, 'custom'>) : range || DEFAULT_RANGE} gap={14} onchange={(r) => { range = r; compare = false }} /></div>
    <button type="button" class="plain" class:on={range === 'custom'} onclick={() => { range = 'custom' }}>Custom</button>
    <button type="button" class="plain" class:on={compare} disabled={hasFilters} title={hasFilters ? 'Compare is unavailable while filters are active. Clear filters to overlay the previous window' : 'Overlay the previous window'} onclick={() => (compare = !compare)}>Compare</button>
    {#if canWrite}<button type="button" class="plain" class:on={settingsOpen} onclick={() => (settingsOpen = !settingsOpen)}>Settings</button>{/if}
  </div>
  {#if range === 'custom'}
    <div class="custom-range">
      <label>From <input type="date" value={customFrom} max={customTo || undefined} oninput={(e) => (customFrom = e.currentTarget.value)} /></label>
      <label>To <input type="date" value={customTo} min={customFrom || undefined} oninput={(e) => (customTo = e.currentTarget.value)} /></label>
      {#if customFrom && customTo && customTo < customFrom}<span class="bad">Ends before it starts</span>{/if}
    </div>
  {/if}

  {#if settingsOpen}
    <div class="settings" transition:panel>
      <div class="setting">
        <div class="text"><div class="label">Tracking code</div><div class="hint">Paste before the closing &lt;/head&gt; tag. No cookies, no consent banner needed.</div></div>
      </div>
      <div class="code">
        <span class="snippet">{snippet}</span>
        <button type="button" class="copy" onclick={copy}>{copiedSnippet ? 'Copied' : 'Copy'}</button>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Name</div></div>
        <div class="ctl"><Input value={site.name} aria-label="Name" oninput={(e) => save({ name: e.currentTarget.value }, 'name')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Domain</div><div class="hint">Events from other hosts are ignored</div></div>
        <div class="ctl"><Input value={site.domain} aria-label="Domain" oninput={(e) => save({ domain: e.currentTarget.value }, 'domain')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Default date range</div><div class="hint">The range this dashboard opens on, remembered for {site.name}</div></div>
        <div class="ranges-setting"><Segment options={RANGES.map((r) => ({ value: r, label: r }))} value={(site.default_range || DEFAULT_RANGE) as Range} gap={14} onchange={pickDefaultRange} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Accent colour</div><div class="hint">Replaces the account colour while you are looking at this site</div></div>
        <Swatches value={site.accent} inherit="Account colour" onpick={pickAccent} />
      </div>
      <div class="setting">
        <div class="text"><div class="label">Home country</div><div class="hint">Where the map arcs converge. Blank uses your top country ({countryName(topCountry) || 'none yet'}).</div></div>
        <div class="ctl short"><Input value={site.home_country} placeholder={topCountry || 'GB'} maxlength={2} aria-label="Home country" oninput={(e) => save({ home_country: e.currentTarget.value.toUpperCase() }, 'home')} /></div>
      </div>
      <div class="setting">
        <div class="text"><div class="label">Favicon</div><div class="hint">Fetched from your site by Umago, never from a third party</div></div>
        <Button variant="secondary" size="sm" onclick={() => api.refreshFavicon(id).then((s) => (site = { ...site!, ...s }))}>Refresh</Button>
      </div>
      {#if meRole === 'admin' && teams.length > 0}
        <div class="setting">
          <div class="text"><div class="label">Team</div><div class="hint">Pin this site so only that team's members see it. Unpinned sites stay visible to everyone signed in.</div></div>
          <select
            class="team-select"
            value={site.owner_team_id}
            aria-label="Owning team"
            onchange={(e) => {
              const v = e.currentTarget.value
              api
                .updateSite(id, { owner_team_id: v })
                .then((s) => (site = { ...site!, ...s }))
                .catch((err: any) => (error = err.message))
            }}
          >
            <option value="">Everyone</option>
            {#each teams as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
          </select>
        </div>
      {/if}
      <div class="setting links-setting">
        <div class="text"><div class="label">Share links</div><div class="hint">Public read-only dashboard URLs for this site. Anyone with the link sees traffic, never settings.</div></div>
        <Button variant="secondary" size="sm" onclick={addShare}>New link</Button>
      </div>
      {#if shares.length > 0}
        <div class="table slim3">
          {#each shares as sh (sh.id)}
            <div class="tr share-row"><span class="mono">{shareURL(sh.slug)}</span><span><button type="button" class="plain" onclick={() => copyShare(sh.slug)}>{copiedShareId === sh.slug ? 'Copied' : 'Copy'}</button></span><span><button type="button" class="plain" onclick={() => removeShare(sh)}>Revoke</button></span><span class="meta" title={sh.created_at}>by {sh.created_by || 'unknown'} · {fmtExact(sh.created_at)} ({fmtAgo(sh.created_at)})</span></div>
          {/each}
        </div>
      {/if}
      <div class="setting links-setting">
        <div class="text"><div class="label">Short links & pixels</div><div class="hint">/l/slug redirects and counts a click · /p/slug serves a tracking pixel</div></div>
        <Button variant="secondary" size="sm" onclick={() => (showLinkForm = true)}>New link</Button>
      </div>
      {#if links.length > 0}
        <div class="table slim">
          {#each links as l (l.id)}
            <div class="tr link-row"><span class="mono">/{l.kind === 'pixel' ? 'p' : 'l'}/{l.slug}</span><span class="muted">{l.url}</span><span>{fmtNum(l.clicks)} clicks</span><span><button type="button" class="plain" onclick={() => removeLink(l.id)}>Delete</button></span><span class="meta" title={l.created_at}>by {l.created_by || 'unknown'} · {fmtExact(l.created_at)} ({fmtAgo(l.created_at)})</span></div>
          {/each}
        </div>
      {/if}
      {#if showLinkForm}
        <Modal title="New short link" subtitle="Redirects /l/slug and counts a click, or serves a tracking pixel." onclose={() => (showLinkForm = false)}>
          <form
            class="modal-form"
            onsubmit={(e) => {
              e.preventDefault()
              saveLink()
            }}
          >
            <Input bind:value={linkForm.slug} placeholder="slug" aria-label="Slug" mono />
            <Input bind:value={linkForm.url} placeholder="https://example.com/landing" aria-label="Target URL" mono />
            <Input bind:value={linkForm.name} placeholder="Name (optional)" aria-label="Link name" />
            <select bind:value={linkForm.kind} aria-label="Link kind"><option value="link">Link</option><option value="pixel">Pixel</option></select>
            {#if error}<p class="bad">{error}</p>{/if}
            <div class="modal-actions">
              <Button variant="secondary" size="sm" type="button" onclick={() => (showLinkForm = false)}>Cancel</Button>
              <Button size="sm" type="submit" disabled={!linkForm.slug.trim() || !linkForm.url.trim()}>Add</Button>
            </div>
          </form>
        </Modal>
      {/if}
      <div class="setting">
        <div class="text"><div class="label">Reset data</div><div class="hint">Deletes every event and rollup for this site. The site, links and funnels stay.</div></div>
        <Button variant="danger" size="sm" onclick={resetData}>Reset</Button>
      </div>
      {#if polar}
        <div class="setting google">
          <div class="text">
            <div class="label">Polar</div>
            {#if polar.connected && polar.connection}
              <div class="hint">
                {polar.connection.server.replace('https://', '')}{#if polar.connection.product_ids} · {polar.connection.product_ids.split(',').length} {polar.connection.product_ids.includes(',') ? 'products' : 'product'}{/if}
                · {fmtNum(polar.orders)} orders
                {#if polar.connection.sync_error}
                  · <span class="bad">{polar.connection.sync_error}</span>
                {:else if polar.connection.synced_at}
                  · synced {new Date(polar.connection.synced_at).toLocaleString()}
                {:else}
                  · first pull pending
                {/if}
                {#if !polar.connection.has_webhook_secret}
                  · <span class="warn">no webhook, sales appear daily</span>
                {/if}
              </div>
            {:else}
              <div class="hint">Show revenue next to traffic. Needs an organization access token from Polar (Settings, Developers) with the orders:read scope.</div>
            {/if}
            {#if polarOpen}
              <div class="polar-form" transition:panel>
                <Input bind:value={polarForm.access_token} placeholder={polar.connected ? 'Access token (leave blank to keep)' : 'polar_oat_…'} aria-label="Polar access token" type="password" mono />
                <Input bind:value={polarForm.product_ids} placeholder="Product ids, comma separated (blank = all)" aria-label="Polar product ids" mono />
                <Input bind:value={polarForm.webhook_secret} placeholder={polar.connection?.has_webhook_secret ? 'Webhook secret (leave blank to keep)' : 'Webhook secret (optional)'} aria-label="Polar webhook secret" type="password" mono />
                <Input bind:value={polarForm.server} placeholder="https://api.polar.sh" aria-label="Polar API server" mono />
                <div class="hint">Webhook URL for Polar, subscribe to the order events: <code>{polar.webhook_url}</code></div>
                <div class="google-actions">
                  <Button size="sm" disabled={polarBusy} onclick={savePolar}>{polarBusy ? 'Checking' : polar.connected ? 'Save' : 'Connect'}</Button>
                  <Button variant="secondary" size="sm" onclick={() => (polarOpen = false)}>Cancel</Button>
                </div>
              </div>
            {/if}
          </div>
          <div class="google-actions">
            {#if polar.connected}
              <Button variant="secondary" size="sm" disabled={polarBusy} onclick={() => polarAction(() => polarApi.sync(id))}>{polarBusy ? 'Working' : 'Sync now'}</Button>
              <Button variant="secondary" size="sm" onclick={() => (polarOpen = !polarOpen)}>Edit</Button>
              <Button variant="secondary" size="sm" disabled={polarBusy} onclick={disconnectPolar}>Disconnect</Button>
            {:else}
              <Button variant="secondary" size="sm" onclick={() => (polarOpen = !polarOpen)}>Connect Polar</Button>
            {/if}
          </div>
        </div>
      {/if}
      {#if google}
        <div class="setting google">
          <div class="text">
            <div class="label">Google Search Console</div>
            {#if !google.configured}
              <div class="hint">Shows the search terms Google sends here. Create an OAuth client in Google Cloud, add <code>{googleRedirect}</code> as a redirect URI, then set <code>UMAGO_GOOGLE_CLIENT_ID</code> and <code>UMAGO_GOOGLE_CLIENT_SECRET</code>.</div>
            {:else if google.connected && google.connection}
              <div class="hint">
                {google.connection.email || 'Connected'}{#if google.connection.property} · {google.connection.property}{/if}
                {#if google.needs_reconnect}
                  · <span class="bad">access expired, connect again</span>
                {:else if google.connection.sync_error}
                  · <span class="bad">{google.connection.sync_error}</span>
                {:else if google.latest_day}
                  · data to {google.latest_day}
                {:else if google.connection.property}
                  · first pull pending
                {/if}
              </div>
              {#if !google.connection.property && google.available_properties?.length}
                <div class="hint">No property matches {site.domain}. Pick one:</div>
                <div class="props">
                  {#each google.available_properties as p (p)}
                    <button type="button" class="prop" disabled={googleBusy} onclick={() => googleAction(() => api.googleSetProperty(id, p))}>{p}</button>
                  {/each}
                </div>
              {:else if !google.connection.property}
                <div class="hint bad">This Google account has no Search Console property for {site.domain}. Verify the site in Search Console, then connect again.</div>
              {/if}
            {:else}
              <div class="hint">Shows the search terms Google sends here. Read-only access; Umago pulls once a day.</div>
            {/if}
            {#if googleNotice}<div class="hint ok">{googleNotice}</div>{/if}
          </div>
          <div class="google-actions">
            {#if google.connected && !google.needs_reconnect}
              <Button variant="secondary" size="sm" disabled={googleBusy || !google.connection?.property} onclick={() => googleAction(() => api.googleSync(id))}>{googleBusy ? 'Working' : 'Sync now'}</Button>
              <Button variant="secondary" size="sm" disabled={googleBusy} onclick={disconnectGoogle}>Disconnect</Button>
            {:else if google.configured}
              <a class="btn" href={googleConnectURL(id)}>{google.needs_reconnect ? 'Reconnect Google' : 'Connect Google Search Console'}</a>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  <div class="strip">
    <div class="metrics">
      <MetricStat label="Visitors" value={fmtNum(stats.totals.visitors)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.visitors, stats.previous.visitors)} />
      <MetricStat label="Visits" value={fmtNum(stats.totals.visits ?? 0)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.visits ?? 0, stats.previous.visits ?? 0)} />
      <MetricStat label="Page views" value={fmtNum(stats.totals.pageviews)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.pageviews, stats.previous.pageviews)} />
      <MetricStat label="Views / visitor" value={fmtRatio(stats.totals.pageviews, stats.totals.visitors)} />
      <MetricStat label="Bounce rate" value={fmtBounce(stats.totals.bounces ?? 0, stats.totals.visits ?? 0)} delta={stats.previous_unavailable ? '' : fmtDelta(stats.totals.bounces ?? 0, stats.previous.bounces ?? 0)} />
      <MetricStat label="Avg visit" value={fmtAvgDur(stats.totals.duration ?? 0, stats.totals.visits ?? 0)} />
      {#if revenue && !hasFilters}
        <div class="group">
          <MetricStat label="Revenue" value={money(revenue.totals.revenue)} delta={fmtDelta(revenue.totals.revenue, revenue.previous.revenue)} />
          <MetricStat label="Orders" value={fmtNum(revenue.totals.orders)} delta={fmtDelta(revenue.totals.orders, revenue.previous.orders)} />
          <MetricStat label="Per visitor" value={stats.totals.visitors ? money(Math.round(revenue.totals.revenue / stats.totals.visitors)) : money(0)} />
        </div>
      {/if}
    </div>
  </div>

  {#if hasFilters}
    <div class="filters" transition:panel>
      <span class="filters-label">Showing visitors who match</span>
      {#each Object.entries(filters) as [dim, key] (dim)}
        <button type="button" class="chip" onclick={() => toggleFilter(dim as Dim, key ?? '')} title="Remove filter">
          <span class="chip-dim">{FILTER_DIM[dim as Dim]}</span>{filterLabel(dim as Dim, key ?? '')}<span class="chip-x">×</span>
        </button>
      {/each}
      <button type="button" class="plain" onclick={() => setFilters({})}>Clear</button>
      <span class="filters-note">
        {#if stats.truncated}Filters read raw events, kept {stats.retention_days} days, so this range is cut short. Raise retention in Settings to filter further back.{:else}Realtime, revenue and search terms cannot be filtered; revenue and search terms are hidden.{/if}
      </span>
    </div>
  {/if}

  {#key stats.range}
    {#if isEmpty}
      <div in:pageIn class="empty-hero">
        <div class="empty-status"><span class="pulse"></span>{liveData?.total ? `${liveData.total} visitor${liveData.total === 1 ? '' : 's'} here now, collecting` : 'Waiting for first visit'}</div>
        <h2 class="empty-title">Get your first visit</h2>
        <p class="empty-sub">Paste the snippet, open your site, and this dashboard fills in by itself. No cookies, no banner.</p>
        <div class="empty-steps">
          <div class="empty-step">
            <div class="step-n">1</div>
            <div class="step-body">
              <div class="step-h">Paste before <span class="mono">&lt;/head&gt;</span></div>
              <div class="code dark">
                <span class="snippet">{snippet}</span>
                <button type="button" class="copy" onclick={copy}>{copiedSnippet ? 'Copied' : 'Copy'}</button>
              </div>
            </div>
          </div>
          <div class="empty-step">
            <div class="step-n">2</div>
            <div class="step-body">
              <div class="step-h">Open {site?.domain ?? 'your site'}</div>
              <p class="muted">Testing locally? <span class="mono">localhost</span>, <span class="mono">*.local</span>, <span class="mono">*.test</span> and LAN IPs are always accepted.</p>
            </div>
          </div>
          <div class="empty-step">
            <div class="step-n">3</div>
            <div class="step-body">
              <div class="step-h">Come back here</div>
              <p class="muted">Data lands in seconds and refreshes every minute. Bots, DNT and wrong-domain hits are dropped silently.</p>
            </div>
          </div>
        </div>
        <div class="empty-actions">
          <Button size="sm" disabled={checking} onclick={checkAgain}>{checking ? 'Checking…' : liveData?.total ? 'Refresh now' : 'I visited. Check again'}</Button>
          {#if canWrite}<Button variant="secondary" size="sm" onclick={() => (settingsOpen = true)}>Open settings</Button>{/if}
        </div>
      </div>
    {:else}
    <div in:pageIn class="stack">
      <div class="chart-wrap">
        {#if stats.truncated}<span class="trunc-badge" title="Filtered views come from raw events, which are kept {stats.retention_days ?? ''} days">Cut to retention{stats.retention_days ? `, ${stats.retention_days} days` : ''}</span>{/if}
        <AreaChart series={stats.series} markers={stats.markers} bucket={stats.bucket} {range} revenue={hasFilters ? [] : revenueSeries} currency={revenue?.currency ?? ''} previous={compare && !hasFilters ? (stats.prev_series ?? []) : []} />
      </div>

      <div class="grid">
        {#if revenue && !hasFilters}
          <div class="wide">
          <BarList
            title="Revenue"
            rows={revenueRows}
            empty={revenueTab === 'ref' || revenueTab === 'source' || revenueTab === 'campaign' || revenueTab === 'landing' ? 'No attributed orders in this range. Pass attribution into checkout to see where sales come from.' : 'No orders in this range'}
            tabs={REVENUE_DIMS}
            tab={revenueTab}
            ontab={(v) => (revenueTab = v)}
            format={money}
          >
            {#snippet icon(r)}{#if revenueTab === 'ref'}<Icon src={r.icon} direct={r.direct} />{/if}{/snippet}
          </BarList>
          </div>
        {/if}
        <Realtime minutes={liveData?.minutes ?? Array(30).fill(0)} total={liveData?.total_30m ?? 0} onmore={() => { mapView = 'live'; document.querySelector('.map-section')?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }} />
        {#if google?.connected && !hasFilters}
          <BarList title="Search terms" rows={termRows.slice(0, TOP_TERMS)} empty={range === '24h' ? 'Google reports search terms two to three days late' : 'No Google search terms for this range yet'} onmore={more('search')} />
        {/if}
        <BarList
          title={pagesTitle}
          rows={pagesWithLive}
          empty="No page views yet"
          onmore={more(pagesTab)}
          onselect={select(pagesTab)}
          selected={selectedKey(pagesTab)}
          tabs={[{ value: 'page', label: 'Pages' }, { value: 'entry', label: 'Entry' }, { value: 'exit', label: 'Exit' }]}
          tab={pagesTab}
          ontab={(v) => (pagesTab = v)}
        />
        <BarList
          title="Sources"
          rows={sources}
          empty="No referrers yet"
          onmore={more(sourceTab)}
          onselect={select(sourceTab)}
          selected={selectedKey(sourceTab)}
          tabs={[{ value: 'ref', label: 'Referrer' }, { value: 'channel', label: 'Channel' }, { value: 'utm_source', label: 'Source' }, { value: 'utm_medium', label: 'Medium' }, { value: 'utm_campaign', label: 'Campaign' }, { value: 'utm_content', label: 'Content' }, { value: 'utm_term', label: 'Term' }]}
          tab={sourceTab}
          ontab={(v) => (sourceTab = v)}
        >
          {#snippet icon(r)}{#if sourceTab === 'ref'}<Icon src={r.icon} direct={r.direct} />{/if}{/snippet}
        </BarList>
        <BarList
          title="Paid ads"
          rows={ads}
          empty="No ad click ids yet. Paid traffic with gclid, fbclid, msclkid, ttclid, li_fat_id or twclid lands here."
          onmore={more(adsTab)}
          onselect={select(adsTab)}
          selected={selectedKey(adsTab)}
          tabs={[{ value: 'gclid', label: 'gclid' }, { value: 'fbclid', label: 'fbclid' }, { value: 'msclkid', label: 'msclkid' }, { value: 'ttclid', label: 'ttclid' }, { value: 'li_fat_id', label: 'li_fat_id' }, { value: 'twclid', label: 'twclid' }]}
          tab={adsTab}
          ontab={(v) => (adsTab = v)}
        />
        <BarList
          title="Locations"
          rows={locations}
          empty={locationTab === 'country' ? 'No locations yet' : 'No regions yet'}
          onmore={more(locationTab)}
          onselect={select(locationTab)}
          selected={selectedKey(locationTab)}
          tabs={[{ value: 'country', label: 'Countries' }, { value: 'region', label: 'Regions' }]}
          tab={locationTab}
          ontab={(v) => (locationTab = v)}
        />
        <BarList
          title="Devices"
          rows={devices}
          onmore={more(deviceTab)}
          onselect={select(deviceTab)}
          selected={selectedKey(deviceTab)}
          tabs={[{ value: 'browser', label: 'Browsers' }, { value: 'os', label: 'OS' }, { value: 'device', label: 'Devices' }, { value: 'language', label: 'Languages' }]}
          tab={deviceTab}
          ontab={(v) => (deviceTab = v)}
        >
          {#snippet icon(r)}{#if deviceTab !== 'language'}<BrandIcon kind={deviceTab} name={r.key} />{/if}{/snippet}
        </BarList>
        {#if events.length > 0}
          <BarList title="Events" rows={events} onmore={more('event')} onselect={select('event')} selected={selectedKey('event')} />
        {/if}
      </div>

      <div class="map-section">
        <div class="head">
          <div class="card-title">{mapView === 'live' ? 'Live' : 'Visitors by country'}</div>
          <div class="map-controls">
            <span class="hint">{mapView === 'live' ? `${liveData?.total ?? live} online` : `${stats.breakdowns.country.length} ${stats.breakdowns.country.length === 1 ? 'country' : 'countries'}`}</span>
            <Segment options={[{ value: 'live', label: 'Live' }, { value: 'range', label: range }]} value={mapView} gap={14} onchange={(v) => (mapView = v)} />
          </div>
        </div>
        {#if mapView === 'live'}
          {#if LiveMap && liveData}
            <LiveMap live={liveData} home={site.home_country || topCountry} />
          {:else}
            <div class="map-placeholder tall"></div>
          {/if}
        {:else if WorldMap}
          <WorldMap rows={stats.breakdowns.country} home={site.home_country || topCountry} />
        {:else}
          <div class="map-placeholder"></div>
        {/if}
      </div>

      {#if hasVitals && vitals}
        <div class="panel">
          <div class="head"><div class="card-title">Web vitals</div><span class="hint">Needs data-performance on the snippet</span></div>
          <div class="vitals">
            {#each vitalTiles as [label, st, unit] (label)}
              <div class="vital"><span class="vital-label">{label}</span><b>{st.count ? fmtNum(Math.round(st.p75)) + unit : '-'}</b><span class="hint">p75 · {fmtNum(st.count)} samples</span></div>
            {/each}
          </div>
          {#if vitals.paths.length > 0}
            <div class="slow-paths">
              {#each vitals.paths.slice(0, 5) as p (p.path)}
                <div class="prop-row"><span>{p.path}</span><span class="muted">LCP p75 {fmtNum(Math.round(p.p75))}ms · {fmtNum(p.count)}</span></div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      <div class="panel">
        <div class="head"><div class="card-title">Sessions</div><span class="hint">{sessionsTruncated ? 'Cut to retention' : `${sessions.length} recent visits`}</span></div>
        {#if sessions.length > 0}
          <div class="table-scroll">
          <div class="table sessions-table">
            <div class="tr th"><span>Started</span><span>Length</span><span>Pages</span><span>Events</span><span>Path</span><span>Where</span></div>
            {#each sessions.slice(0, 20) as s (s.visit_id)}
              <div class="tr"><span class="muted">{new Date(s.started).toLocaleString()}</span><span>{s.duration_secs ? fmtDur(s.duration_secs) : '-'}</span><span>{s.pageviews}</span><span>{s.events}</span><span class="mono">{s.entry}{s.exit !== s.entry ? ' → ' + s.exit : ''}</span><span class="muted">{countryName(s.country)}{s.browser ? ' · ' + s.browser : ''}</span></div>
            {/each}
          </div>
          </div>
        {:else}
          <p class="muted">No visits in this range.</p>
        {/if}
      </div>

      <div class="panel">
        <div class="head"><div class="card-title">Funnels</div><button type="button" class="plain" onclick={() => editFunnel()}>New funnel</button></div>
        {#if funnelForm}
          <Modal title={funnelForm.id ? 'Edit funnel' : 'New funnel'} subtitle="Pages then events, in order." onclose={() => (funnelForm = null)}>
            <div class="modal-form">
              <Input bind:value={funnelForm.name} placeholder="Funnel name" aria-label="Funnel name" />
              {#each funnelForm.steps as st, i (i)}
                <div class="funnel-step">
                  <select bind:value={st.type} aria-label="Step type"><option value="page">Page</option><option value="event">Event</option></select>
                  <Input bind:value={st.value} placeholder={st.type === 'page' ? '/pricing' : 'signup'} aria-label="Step value" mono />
                  <button type="button" class="plain" disabled={funnelForm.steps.length <= 2} onclick={() => (funnelForm!.steps = funnelForm!.steps.filter((_, j) => j !== i))}>×</button>
                </div>
              {/each}
              <div class="row-actions">
                <Button variant="secondary" size="sm" disabled={funnelForm.steps.length >= 8} onclick={() => (funnelForm!.steps = [...funnelForm!.steps, { type: 'event', value: '' }])}>Add step</Button>
                <Button size="sm" onclick={saveFunnel}>Save</Button>
                <Button variant="secondary" size="sm" onclick={() => (funnelForm = null)}>Cancel</Button>
              </div>
            </div>
          </Modal>
        {/if}
        {#each funnels as f (f.id)}
          {@const res = funnelResults[f.id]}
          <div class="funnel">
            <div class="funnel-head"><b>{f.name}</b><span class="hint">{f.steps.length} steps</span><span class="spacer"></span>
              <button type="button" class="plain" onclick={() => editFunnel(f)}>Edit</button>
              <button type="button" class="plain" onclick={() => removeFunnel(f.id)}>Delete</button>
            </div>
            {#if res}
              {#each res.steps as st, i (i)}
                <div class="funnel-row">
                  <span class="funnel-step-label">{i + 1}. {st.value}</span>
                  <div class="funnel-bar"><div class="funnel-fill" style="width: {(st.rate * 100).toFixed(1)}%"></div></div>
                  <span class="muted">{fmtNum(st.visitors)} · {(st.rate * 100).toFixed(1)}%</span>
                </div>
              {/each}
              <div class="hint">Conversion {(res.conversion * 100).toFixed(1)}% of {fmtNum(res.visitors)} entering visits</div>
            {:else}
              <p class="muted">Evaluating…</p>
            {/if}
          </div>
        {:else}
          {#if !funnelForm}<p class="muted">Track a path like signup: pages then events, in order.</p>{/if}
        {/each}
      </div>

      <div class="panel">
        <div class="head">
          <div class="card-title">Journeys</div>
          <span class="hint">{journeys ? `${fmtNum(journeys.visits)} visits${journeysTruncated ? ' · cut to retention' : ''}` : journeysTruncated ? 'Cut to retention' : 'Loading…'}</span>
        </div>
        <div class="journey-controls">
          <label>Steps
            <select bind:value={journeySteps} aria-label="Journey steps">
              {#each [2, 3, 4, 5, 6, 7] as n (n)}<option value={n}>{n}</option>{/each}
            </select>
          </label>
          <Input bind:value={journeyStartDraft} placeholder="Starts with / (blank = all visits)" aria-label="Journey entry page" mono onchange={() => (journeyStart = journeyStartDraft.trim())} />
        </div>
        {#if journeys && journeys.paths.length > 0}
          <div class="subhead">Top paths</div>
          {#each journeys.paths.slice(0, 10) as p (p.pages.join(' '))}
            <div class="funnel-row">
              <span class="funnel-step-label mono">{p.pages.join(' → ')}</span>
              <div class="funnel-bar"><div class="funnel-fill" style="width: {((p.visits / journeyTop) * 100).toFixed(1)}%"></div></div>
              <span class="muted">{fmtNum(p.visits)} · {(p.rate * 100).toFixed(1)}%</span>
            </div>
          {/each}
          {#if journeys.transitions.length > 0}
            <div class="subhead">Top flows</div>
            {#each journeys.transitions.slice(0, 10) as t (`${t.step}${t.from}${t.to}`)}
              <div class="funnel-row">
                <span class="funnel-step-label"><span class="muted">{t.step}.</span> <span class="mono">{t.from} → {t.to}</span></span>
                <div class="funnel-bar"><div class="funnel-fill" style="width: {((t.visits / journeyTop) * 100).toFixed(1)}%"></div></div>
                <span class="muted">{fmtNum(t.visits)}</span>
              </div>
            {/each}
          {/if}
          {#if journeys.step_pages.length > 0}
            <div class="subhead">Pages by step</div>
            {#each journeys.step_pages as step, i (i)}
              {#if step.length > 0}
                <div class="prop-row"><span><span class="muted">{i + 1}.</span> {step.slice(0, 5).map((s) => `${s.page} ${fmtNum(s.visits)}`).join(' · ')}{#if step.length > 5}<span class="muted"> · +{step.length - 5} more</span>{/if}</span></div>
              {/if}
            {/each}
          {/if}
        {:else if journeys}
          <p class="muted">No multi-page visits in this range{journeyStart ? ` starting at ${journeyStart}` : ''}.</p>
        {:else}
          <p class="muted">Loading…</p>
        {/if}
      </div>
    </div>
    {/if}
  {/key}
{:else}
  <div class="skel-wrap" aria-busy="true" aria-label="Loading dashboard">
    <div class="skel-header"><div class="skel skel-title"></div><div class="skel skel-domain"></div><span class="spacer"></span><div class="skel skel-ctl"></div><div class="skel skel-ctl"></div></div>
    <div class="skel-metrics">{#each Array(6) as _, i (i)}<div class="skel-metric"><div class="skel skel-label"></div><div class="skel skel-value"></div></div>{/each}</div>
    <div class="skel skel-chart"></div>
    <div class="skel-grid">{#each Array(4) as _, i (i)}<div class="skel-card"><div class="skel skel-card-title"></div><div class="skel skel-row"></div><div class="skel skel-row"></div><div class="skel skel-row"></div><div class="skel skel-row"></div></div>{/each}</div>
  </div>
{/if}

{#if modal}
  <Modal title={modal === 'search' ? 'Search terms' : DIM_TITLE[modal]} subtitle="{site?.name} · {range} · {modalRows ? `${modalRows.length} ${modal === 'event' ? 'events' : modal === 'search' ? 'terms' : 'rows'}` : 'loading'}" onclose={() => (modal = null)}>
    <div class="modal-search"><Input bind:value={filter} placeholder="Filter" aria-label="Filter rows" /></div>
    {#if modal === 'event' && propsEvent}
      <div class="props-drill">
        <button type="button" class="plain" onclick={() => (propsEvent = '')}>← All events</button>
        {#if !propsKey}
          {#each propsKeys as k (k.key)}
            <button type="button" class="prop-row" onclick={() => openPropValues(k.key)}><span>{k.key}</span><span class="muted">{fmtNum(k.events)} events</span></button>
          {:else}
            <p class="muted">No properties recorded for this event in range{sessionsTruncated || stats?.truncated ? ' (cut to retention)' : ''}.</p>
          {/each}
        {:else}
          <button type="button" class="plain" onclick={() => (propsKey = '')}>← {propsEvent}</button>
          <div class="prop-key">{propsKey}</div>
          {#each propsValues as v (v.value)}
            <div class="prop-row"><span>{v.value || '∅'}</span><span class="muted">{fmtNum(v.count)}</span></div>
          {:else}
            <p class="muted">No values.</p>
          {/each}
        {/if}
      </div>
    {:else if modalRows}
      <BarList bare rows={filtered} empty="No matches" onselect={modal === 'event' ? (r) => openProps(r.key) : modal === 'search' ? undefined : (r) => toggleFilter(modal as Dim, r.key === '∅' ? '' : r.key === 'direct' ? '' : r.key === 'XX' ? '' : r.key)}>
        {#snippet icon(r)}
          {#if modal === 'ref'}<Icon src={r.icon} direct={r.direct} />
          {:else if modal === 'browser' || modal === 'os' || modal === 'device'}<BrandIcon kind={modal} name={r.key} />{/if}
        {/snippet}
      </BarList>
    {/if}
  </Modal>
{/if}

<style>
  .modal-search { position: sticky; top: 0; background: var(--up-bg); padding: 4px 0 12px; z-index: 1; }
  .title { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
  .title .ranges { margin-right: 22px; }
  .name { font: var(--up-type-row-title); }
  .domain { font: var(--up-type-meta); color: var(--up-text-muted); }
  .live { display: flex; align-items: center; gap: 6px; font: var(--up-type-meta); color: var(--up-text-muted); margin-left: 6px; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--up-accent); }
  .spacer { flex: 1; }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); }
  .plain:hover, .plain.on { color: var(--up-ink); }

  .settings { display: flex; flex-direction: column; gap: 14px; padding: 16px 18px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); margin-top: -16px; }
  .setting { display: flex; align-items: center; justify-content: space-between; gap: 24px; }
  .text { display: flex; flex-direction: column; gap: 2px; }
  .label { font: var(--up-type-setting); }
  .hint { font: var(--up-type-meta); color: var(--up-text-muted); }
  .ctl { width: 240px; flex-shrink: 0; }
  .ctl.short { width: 90px; }
  .ranges-setting { flex-shrink: 0; }
  .code { background: var(--up-surface-dark); border-radius: var(--up-radius-tooltip); padding: 12px 14px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .snippet { font: var(--up-type-code); color: var(--up-text-on-dark); word-break: break-all; }
  .copy { background: none; border: none; padding: 2px 0; cursor: pointer; font: var(--up-type-small); color: var(--up-operational-strong); flex-shrink: 0; }
  .copy:hover { color: var(--up-text-on-dark); }
  .google { align-items: flex-start; }
  .google .text { gap: 6px; }
  .hint code { font: var(--up-type-code); user-select: all; }
  .hint.ok { color: var(--up-operational-strong); }
  .warn { color: var(--up-text-muted); }
  .polar-form { display: flex; flex-direction: column; gap: 8px; width: 100%; max-width: 460px; padding-top: 4px; }
  .google-actions { display: flex; gap: 8px; flex-shrink: 0; }
  .btn { font: var(--up-type-ui); color: var(--up-ink); background: var(--up-bg); box-shadow: inset 0 0 0 1px var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 12px; display: inline-flex; align-items: center; white-space: nowrap; }
  .btn:hover { background: var(--up-bg-hover); color: var(--up-ink); }
  .props { display: flex; flex-wrap: wrap; gap: 6px; }
  .prop { font: var(--up-type-code); color: var(--up-ink); background: var(--up-bg-hover); border: none; border-radius: var(--up-radius-control); padding: 4px 8px; cursor: pointer; }
  .prop:hover { box-shadow: inset 0 0 0 1px var(--up-border-control); }
  .prop:disabled { opacity: 0.5; cursor: default; }

  .filters { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: -12px; }
  .filters-label { font: var(--up-type-meta); color: var(--up-text-muted); }
  .chip { display: inline-flex; align-items: center; gap: 6px; font: var(--up-type-meta); color: var(--up-ink); background: var(--up-accent-tint); border: none; border-radius: var(--up-radius-pill); padding: 4px 8px 4px 10px; cursor: pointer; }
  .chip:hover { box-shadow: inset 0 0 0 1px var(--up-accent); }
  .chip-dim { color: var(--up-text-muted); }
  .chip-x { color: var(--up-text-muted); font-size: 14px; line-height: 1; }
  .filters .plain { margin-left: 4px; }
  .filters-note { font: var(--up-type-meta); color: var(--up-text-muted); width: 100%; }
  .strip { display: flex; align-items: flex-start; }
  .metrics { display: flex; gap: 28px; flex-wrap: nowrap; }
  .group { display: flex; gap: 28px; padding-left: 28px; border-left: 1px solid var(--up-border-hairline); }
  .stack { display: flex; flex-direction: column; gap: 36px; min-width: 0; }
  .stack > :global(*) { min-width: 0; }
  /* minmax(0, 1fr): a plain 1fr column grows to fit its widest child, which is how cards escaped the viewport on phones. */
  .grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; }
  .grid > :global(*) { min-width: 0; }
  .wide { grid-column: 1 / -1; }
  .map-section { display: flex; flex-direction: column; gap: 12px; }
  .head { display: flex; align-items: baseline; justify-content: space-between; }
  .card-title { font: var(--up-type-setting); font-weight: 700; }
  .map-placeholder { height: 340px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); }
  .map-placeholder.tall { height: 420px; }
  .map-controls { display: flex; align-items: center; gap: 18px; }
  .bad { font: var(--up-type-meta); color: var(--up-degraded-strong); }
  .custom-range { display: flex; align-items: center; gap: 12px; font: var(--up-type-meta); color: var(--up-text-muted); }
  .custom-range label { display: flex; align-items: center; gap: 6px; }
  .custom-range input[type='date'], .funnel-step select, .modal-form select { font: var(--up-type-meta); color: var(--up-ink); background: var(--up-bg); border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 8px; }
  .panel { display: flex; flex-direction: column; gap: 12px; }
  .vitals { display: flex; gap: 24px; flex-wrap: wrap; }
  .vital { display: flex; flex-direction: column; gap: 2px; }
  .vital-label { font: var(--up-type-meta); color: var(--up-text-muted); }
  .vital b { font: var(--up-type-metric); }
  .slow-paths { display: flex; flex-direction: column; gap: 4px; }
  .table { display: flex; flex-direction: column; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); overflow: hidden; }
  .tr { display: grid; grid-template-columns: 170px 70px 60px 60px 1fr 1fr; gap: 8px; padding: 8px 12px; font: var(--up-type-meta); align-items: baseline; }
  .tr:nth-child(odd) { background: var(--up-bg-hover); }
  .tr.th { font: var(--up-type-small); color: var(--up-text-muted); }
  .table.slim .tr { grid-template-columns: 160px 1fr 90px 70px; }
  .table.slim3 .tr { grid-template-columns: 1fr auto auto; }
  .tr .meta { grid-column: 1 / -1; font: var(--up-type-small); color: var(--up-text-muted); }
  .team-select { font: var(--up-type-meta); color: var(--up-ink); background: var(--up-bg); border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 8px; max-width: 240px; }
  .mono { font: var(--up-type-code); word-break: break-all; }
  .funnel { display: flex; flex-direction: column; gap: 6px; padding: 12px 0; border-top: 1px solid var(--up-border-hairline); }
  .funnel-head { display: flex; align-items: baseline; gap: 10px; }
  .funnel-row { display: grid; grid-template-columns: 220px 1fr 130px; gap: 10px; align-items: center; font: var(--up-type-meta); }
  .funnel-step-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .funnel-bar { height: 8px; background: var(--up-nodata); border-radius: var(--up-radius-bar); overflow: hidden; }
  .funnel-fill { height: 100%; background: var(--up-accent); border-radius: var(--up-radius-bar); }
  .funnel-step { display: grid; grid-template-columns: 90px 1fr 30px; gap: 8px; align-items: center; }
  .row-actions { display: flex; gap: 8px; }
  .modal-form { display: flex; flex-direction: column; gap: 12px; }
  .modal-form select { height: 34px; }
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .links-setting { padding-bottom: 0; }
  .journey-controls { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .journey-controls label { display: flex; flex-direction: column; gap: 4px; font: var(--up-type-meta); color: var(--up-text-muted); }
  .journey-controls select { font: var(--up-type-meta); color: var(--up-ink); background: var(--up-bg); border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 8px; }
  .journey-controls :global(input) { width: 260px; }
  .subhead { font: var(--up-type-small); color: var(--up-text-muted); margin: 12px 0 2px; }
  .prop-row { display: flex; justify-content: space-between; gap: 12px; width: 100%; background: none; border: none; padding: 6px 0; cursor: pointer; font: var(--up-type-meta); color: var(--up-ink); text-align: left; }
  button.prop-row:hover { color: var(--up-accent-hover); }
  div.prop-row { cursor: default; }
  .props-drill { display: flex; flex-direction: column; gap: 2px; }
  .prop-key { font: var(--up-type-setting); margin: 8px 0 4px; }
  .empty-hero { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 28px 28px 24px; display: flex; flex-direction: column; gap: 12px; background: linear-gradient(180deg, var(--up-accent-tint) 0%, var(--up-bg) 55%); }
  .empty-status { display: inline-flex; align-items: center; gap: 8px; font: var(--up-type-small); color: var(--up-text-muted); }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--up-accent); animation: pulse 1.8s ease-out infinite; }
  @keyframes pulse { 0% { box-shadow: 0 0 0 0 var(--up-operational); } 70% { box-shadow: 0 0 0 8px transparent; } 100% { box-shadow: 0 0 0 0 transparent; } }
  .empty-title { font: var(--up-type-metric); letter-spacing: -0.01em; }
  .empty-sub { font: var(--up-type-meta); color: var(--up-text-muted); max-width: 52ch; }
  .empty-steps { display: flex; flex-direction: column; gap: 14px; margin-top: 8px; }
  .empty-step { display: flex; gap: 14px; align-items: flex-start; }
  .step-n { width: 26px; height: 26px; flex-shrink: 0; border-radius: 50%; background: var(--up-ink); color: var(--up-bg); font: var(--up-type-small); display: flex; align-items: center; justify-content: center; margin-top: 2px; }
  .step-body { display: flex; flex-direction: column; gap: 8px; min-width: 0; flex: 1; }
  .step-h { font: var(--up-type-setting); }
  .code.dark { background: var(--up-surface-dark); border-radius: var(--up-radius-tooltip); padding: 12px 14px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .empty-actions { display: flex; gap: 8px; margin-top: 6px; flex-wrap: wrap; }
  .plain:disabled { opacity: 0.45; cursor: not-allowed; }
  .plain:disabled:hover { color: var(--up-text-muted); }
  .error-banner { display: flex; align-items: center; gap: 12px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 10px 14px; margin-bottom: 16px; background: var(--up-bg); }
  .error-text { font: var(--up-type-meta); color: var(--up-degraded-strong); }
  .chart-wrap { position: relative; min-width: 0; }
  .trunc-badge { position: absolute; top: 8px; right: 12px; z-index: 1; font: var(--up-type-small); color: var(--up-text-muted); background: var(--up-bg-hover); border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-pill); padding: 2px 10px; }
  .table-scroll { overflow-x: auto; }
  .table-scroll .table { min-width: 640px; }
  .skel-wrap { display: flex; flex-direction: column; gap: 20px; }
  .skel { background: var(--up-bg-hover); border-radius: var(--up-radius-tooltip); animation: skel-pulse 1.6s ease-in-out infinite; }
  @keyframes skel-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.45; } }
  .skel-header { display: flex; align-items: center; gap: 10px; }
  .skel-title { width: 180px; height: 22px; }
  .skel-domain { width: 120px; height: 14px; }
  .skel-ctl { width: 90px; height: 30px; border-radius: var(--up-radius-control); }
  .skel-metrics { display: flex; gap: 28px; flex-wrap: wrap; }
  .skel-metric { display: flex; flex-direction: column; gap: 8px; }
  .skel-label { width: 70px; height: 12px; }
  .skel-value { width: 90px; height: 26px; }
  .skel-chart { height: 220px; border-radius: var(--up-radius-card); }
  .skel-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; }
  .skel-card { display: flex; flex-direction: column; gap: 10px; border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 16px; }
  .skel-card-title { width: 110px; height: 16px; }
  .skel-row { height: 14px; }
  @media (max-width: 600px) {
    .empty-hero { padding: 20px 18px; }
    .grid { grid-template-columns: minmax(0, 1fr); }
    .skel-grid { grid-template-columns: minmax(0, 1fr); }
    .title { flex-wrap: wrap; row-gap: 8px; }
    .strip { min-width: 0; overflow: hidden; }
    .title .ranges { margin-right: 0; width: 100%; order: 9; }
    .metrics { gap: 24px; flex-wrap: wrap; row-gap: 16px; }
    .group { gap: 24px; padding-left: 0; border-left: none; width: 100%; }
    .setting { flex-direction: column; align-items: flex-start; gap: 8px; }
    .ctl { width: 100%; }
  }
</style>
