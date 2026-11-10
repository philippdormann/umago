<script lang="ts">
  // Public read-only dashboard for a share link. No login, no settings.
  import { api, DEFAULT_RANGE, isRange, RANGES, type Range, type Summary } from '../lib/api'
  import { countryName, fmtNum, fmtRatio } from '../lib/format'
  import { pushToast } from '../lib/toast.svelte'
  import AreaChart from '../lib/ui/AreaChart.svelte'
  import BarList from '../lib/ui/BarList.svelte'
  import Button from '../lib/ui/Button.svelte'
  import MetricStat from '../lib/ui/MetricStat.svelte'
  import Segment from '../lib/ui/Segment.svelte'

  let { slug }: { slug: string } = $props()
  let name = $state('')
  let domain = $state('')
  let stats = $state<Summary | null>(null)
  let live = $state(0)
  let missing = $state(false)
  let failed = $state(false)
  // The dashboard opens on the range the server answers with. Learning it
  // from the first call costs no extra round trip.
  let range = $state<Range | ''>('')

  // The window already asked for, so resolving the opening range does not
  // fire the same query a second time. A new slug starts over.
  let requested = ''
  let seen = ''
  const windowKey = () => `${slug}|${range}`

  async function load() {
    if (missing) return
    failed = false
    try {
      const r = await api.publicShare(slug, range || undefined)
      name = r.site.name
      domain = r.site.domain
      stats = r.stats
      live = r.live
      document.title = `${r.site.name} · Umago`
      if (!range) {
        range = isRange(r.stats.range) ? r.stats.range : DEFAULT_RANGE
        requested = windowKey() // this load already covered it
      }
    } catch (e: any) {
      if (e?.status === 404) {
        missing = true
      } else {
        failed = true
        pushToast(e?.message ? `Could not load this dashboard: ${e.message}` : 'Could not load this dashboard.')
      }
    }
  }

  $effect(() => {
    if (slug !== seen) {
      seen = slug
      stats = null
      missing = false
      failed = false
      range = ''
      requested = ''
      name = ''
      domain = ''
      live = 0
    }
    const key = windowKey()
    if (key === requested) return
    requested = key
    load()
  })

  const pages = $derived((stats?.breakdowns.page ?? []).map((r) => ({ key: r.key, label: r.key, value: r.visitors, title: `${fmtNum(r.pageviews)} views` })))
  const refs = $derived(
    (stats?.breakdowns.ref ?? []).map((r) => ({ key: r.key || 'direct', label: r.key || 'Direct', value: r.visitors, title: `${fmtNum(r.pageviews)} views` })),
  )
  const countries = $derived(
    (stats?.breakdowns.country ?? []).map((r) => ({ key: r.key || 'XX', label: countryName(r.key), value: r.visitors, title: `${fmtNum(r.pageviews)} views` })),
  )
  // Empty state: no pageviews and no visitors in range. The shared page
  // cannot show the tracking snippet, so the hero only waits.
  const isEmpty = $derived(!!stats && stats.totals.visitors === 0 && stats.totals.pageviews === 0)
  const chartRange = $derived(range || stats?.range || DEFAULT_RANGE)
</script>

{#if stats}
  <div class="title">
    <span class="name">{name}</span>
    <span class="domain">{domain}</span>
    {#if live > 0}<span class="live"><span class="dot"></span>{live} online</span>{/if}
    <span class="spacer"></span>
    <div class="ranges"><Segment options={RANGES.map((r) => ({ value: r, label: r }))} value={chartRange} gap={14} onchange={(r) => (range = r)} /></div>
  </div>

  {#if isEmpty}
    <div class="empty-hero">
      <div class="empty-status"><span class="pulse"></span>Waiting for first visit</div>
      <h2 class="empty-title">No visits yet</h2>
      <p class="empty-sub">This shared dashboard fills in by itself once {name || 'this site'} gets its first visit. No cookies, no banner.</p>
    </div>
  {:else}
    <div class="strip">
      <div class="metrics">
        <MetricStat label="Visitors" value={fmtNum(stats.totals.visitors)} />
        <MetricStat label="Visits" value={fmtNum(stats.totals.visits ?? 0)} />
        <MetricStat label="Page views" value={fmtNum(stats.totals.pageviews)} />
        <MetricStat label="Views / visitor" value={fmtRatio(stats.totals.pageviews, stats.totals.visitors)} />
      </div>
    </div>
    <AreaChart series={stats.series} markers={[]} bucket={stats.bucket} range={chartRange} />
    <div class="grid">
      <BarList title="Pages" rows={pages.slice(0, 10)} empty="No page views yet" />
      <BarList title="Referrers" rows={refs.slice(0, 10)} empty="No referrers yet" />
      <BarList title="Countries" rows={countries.slice(0, 10)} empty="No locations yet" />
    </div>
  {/if}
{:else if missing}
  <div class="missing-card">
    <div class="missing-title">This link is no longer available</div>
    <p class="muted">This share link does not exist or was revoked. Ask the site owner for a fresh link.</p>
  </div>
{:else if failed}
  <div class="missing-card">
    <div class="missing-title">Could not load this dashboard</div>
    <p class="muted">Check your connection and try again.</p>
    <div class="missing-actions"><Button size="sm" onclick={load}>Retry</Button></div>
  </div>
{:else}
  <div class="title"><div class="skel skel-title"></div></div>
  <div class="strip">
    <div class="metrics">
      <div class="skel skel-metric"></div>
      <div class="skel skel-metric"></div>
      <div class="skel skel-metric"></div>
      <div class="skel skel-metric"></div>
    </div>
  </div>
  <div class="skel skel-chart" aria-hidden="true"></div>
  <div class="grid">
    <div class="skel skel-card"></div>
    <div class="skel skel-card"></div>
  </div>
{/if}

<style>
  .title { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
  .name { font: var(--up-type-row-title); }
  .domain { font: var(--up-type-meta); color: var(--up-text-muted); }
  .live { display: flex; align-items: center; gap: 6px; font: var(--up-type-meta); color: var(--up-text-muted); margin-left: 6px; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--up-accent); }
  .spacer { flex: 1; }
  .strip { display: flex; }
  .metrics { display: flex; gap: 28px; flex-wrap: wrap; }
  .grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; }
  .grid > :global(*) { min-width: 0; }
  .muted { font: var(--up-type-meta); color: var(--up-text-muted); margin: 0; }
  .missing-card { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 28px; display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
  .missing-title { font: var(--up-type-setting); }
  .missing-actions { margin-top: 8px; }
  .empty-hero { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 28px 28px 24px; display: flex; flex-direction: column; gap: 12px; background: linear-gradient(180deg, var(--up-accent-tint) 0%, var(--up-bg) 55%); }
  .empty-status { display: inline-flex; align-items: center; gap: 8px; font: var(--up-type-small); color: var(--up-text-muted); }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--up-accent); animation: pulse 1.8s ease-out infinite; }
  @keyframes pulse { 0% { box-shadow: 0 0 0 0 var(--up-operational); } 70% { box-shadow: 0 0 0 8px transparent; } 100% { box-shadow: 0 0 0 0 transparent; } }
  .empty-title { font: var(--up-type-metric); letter-spacing: -0.01em; margin: 0; }
  .empty-sub { font: var(--up-type-meta); color: var(--up-text-muted); max-width: 52ch; margin: 0; }
  .skel { position: relative; overflow: hidden; background: var(--up-bg-hover); border-radius: var(--up-radius-tooltip); }
  .skel::after { content: ''; position: absolute; inset: 0; background: linear-gradient(90deg, transparent, var(--up-bg) 50%, transparent); opacity: 0.6; animation: shimmer 1.4s ease-in-out infinite; }
  @keyframes shimmer { 0% { transform: translateX(-100%); } 100% { transform: translateX(100%); } }
  .skel-title { width: 240px; max-width: 60%; height: 22px; }
  .skel-metric { width: 120px; height: 44px; }
  .skel-chart { height: 220px; margin-top: 8px; }
  .skel-card { height: 180px; }
  @media (prefers-reduced-motion: reduce) {
    .pulse, .skel::after { animation: none; }
  }
  @media (max-width: 600px) {
    .grid { grid-template-columns: minmax(0, 1fr); }
    .title { flex-wrap: wrap; row-gap: 8px; }
    .title .ranges { width: 100%; order: 9; }
    .empty-hero, .missing-card { padding: 20px 18px; }
  }
</style>
