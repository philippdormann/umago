<script lang="ts">
  // Root index of tracked websites, with add form and tracking code.
  import { api, siteIconURL, type Site } from '../lib/api'
  import { loadScriptName, setScriptName, snippetFor } from '../lib/script.svelte'
  import { confirmAction } from '../lib/confirm.svelte'
  import { fmtDelta, fmtNum } from '../lib/format'
  import { link } from '../lib/router.svelte'
  import { panel, reorder } from '../lib/motion'
  import Icon from '../lib/ui/Icon.svelte'
  import Sparkline from '../lib/ui/Sparkline.svelte'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'
  import SitesXError from '../lib/ui/SitesXError.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import { Skeleton } from '$lib/components/ui/skeleton/index.js'

  let sites = $state<Site[] | null>(null)
  let error = $state('')
  let dismissed = $state(false)
  let newName = $state('')
  let newDomain = $state('')
  let adding = $state(false)
  let showAdd = $state(false)
  let code = $state<string | null>(null) // site id whose tracking code is open
  let copiedId = $state<string | null>(null)
  let copyTimer: ReturnType<typeof setTimeout> | null = null
  let dragging = $state<string | null>(null)
  let over = $state<string | null>(null)

  async function load() {
    try {
      sites = (await api.sites()).sites
      error = ''
      dismissed = false
    } catch (e: any) {
      error = e.message
    }
  }
  $effect(() => {
    load()
    loadScriptName().then((n) => setScriptName(n))
    const t = setInterval(load, 60_000)
    return () => clearInterval(t)
  })

  async function add() {
    if (!newDomain.trim() || adding) return
    adding = true
    try {
      const s = await api.createSite({ name: newName.trim() || undefined, domain: newDomain.trim() })
      sites = [...(sites ?? []), s]
      newName = ''
      newDomain = ''
      code = s.id
      showAdd = false
      error = ''
      setTimeout(load, 3000) // favicon arrives in the background
    } catch (e: any) {
      error = e.message
    } finally {
      adding = false
    }
  }
  async function remove(s: Site) {
    const ok = await confirmAction({
      title: `Remove ${s.name}?`,
      message: 'All traffic, rollups and settings for this site are deleted. This cannot be undone.',
      confirmLabel: 'Remove',
    })
    if (!ok) return
    api
      .deleteSite(s.id)
      .then(() => (sites = (sites ?? []).filter((x) => x.id !== s.id)))
      .catch((e: any) => (error = e.message))
  }
  const snippet = (s: Site) => snippetFor(s.id)
  async function copy(s: Site) {
    try {
      await navigator.clipboard.writeText(snippet(s))
      copiedId = s.id
      if (copyTimer) clearTimeout(copyTimer)
      copyTimer = setTimeout(() => {
        if (copiedId === s.id) copiedId = null
      }, 1500)
    } catch {}
  }
  function retry() {
    dismissed = false
    load()
  }
  function dropOn(target: Site) {
    const from = dragging
    dragging = null
    over = null
    if (!from || from === target.id || !sites) return
    const ids = sites.map((x) => x.id)
    ids.splice(ids.indexOf(from), 1)
    ids.splice(ids.indexOf(target.id), 0, from)
    const byId = new Map(sites.map((s) => [s.id, s]))
    sites = ids.map((id) => byId.get(id)!)
    api.reorderSites(ids).then((r) => (sites = r.sites)).catch(() => load())
  }
  const total = $derived((sites ?? []).reduce((a, s) => a + (s.card?.visitors ?? 0), 0))
  const live = $derived((sites ?? []).reduce((a, s) => a + (s.live ?? 0), 0))
</script>

{#if sites}
  <div class="summary">
    <span class="dot" style="background: {live > 0 ? 'var(--up-accent)' : 'var(--up-text-faint)'}"></span>
    {sites.length} {sites.length === 1 ? 'website' : 'websites'} · {fmtNum(total)} visitors this week{#if live > 0}{' · '}{live} online now{/if}
  </div>

  <div class="list">
    {#each sites as s (s.id)}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="item"
        animate:reorder
        class:over={over === s.id && dragging !== s.id}
        class:dragging={dragging === s.id}
        ondragover={(e) => {
          e.preventDefault()
          over = s.id
        }}
        ondragleave={() => over === s.id && (over = null)}
        ondrop={(e) => {
          e.preventDefault()
          dropOn(s)
        }}
      >
        <div class="row">
          <span
            class="grip"
            draggable="true"
            title="Drag to reorder"
            ondragstart={(e) => {
              dragging = s.id
              code = null
              e.dataTransfer?.setData('text/plain', s.id)
            }}
            ondragend={() => {
              dragging = null
              over = null
            }}
          >
            <svg width="8" height="12" viewBox="0 0 8 12" fill="currentColor"><circle cx="2" cy="2" r="1.1" /><circle cx="6" cy="2" r="1.1" /><circle cx="2" cy="6" r="1.1" /><circle cx="6" cy="6" r="1.1" /><circle cx="2" cy="10" r="1.1" /><circle cx="6" cy="10" r="1.1" /></svg>
          </span>
          <a href="/s/{s.id}" onclick={link} class="main">
            <Icon src={s.has_favicon ? siteIconURL(s.id) : ''} size={22} />
            <div class="text">
              <div class="name">{s.name}</div>
              <div class="domain">{s.domain}</div>
            </div>
            <span class="spark"><Sparkline values={(s.card?.spark ?? []).map((p) => p.visitors)} /></span>
            <div class="stat">
              <div class="num"><span class="visitors">{fmtNum(s.card?.visitors ?? 0)}</span>{#if fmtDelta(s.card?.visitors ?? 0, s.card?.previous ?? 0)}<span class="delta" class:neg={fmtDelta(s.card?.visitors ?? 0, s.card?.previous ?? 0).startsWith('-')}>{fmtDelta(s.card?.visitors ?? 0, s.card?.previous ?? 0)}</span>{/if}</div>
              <div class="caption">visitors · 7d</div>
            </div>
            <span class="arrow"><svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M1 5 H9 M5.5 1.5 L9 5 L5.5 8.5" /></svg></span>
          </a>
          <button type="button" class="plain codebtn" class:on={code === s.id} onclick={() => (code = code === s.id ? null : s.id)}>Tracking code</button>
          <button type="button" class="plain remove" title="Remove website" aria-label="Remove website" onclick={() => remove(s)}>×</button>
        </div>
        {#if code === s.id}
          <div class="code-wrap" transition:panel>
            <div class="code">
              <span class="snippet">{snippet(s)}</span>
              <button type="button" class="copy" onclick={() => copy(s)}>{copiedId === s.id ? 'Copied' : 'Copy'}</button>
            </div>
            <div class="hint">Paste before the closing &lt;/head&gt; tag. No cookies, no consent banner needed. Site ID: {s.id}</div>
          </div>
        {/if}
      </div>
    {/each}
    {#if sites.length === 0}
      <div class="empty-hero">
        <div class="empty-status"><span class="pulse"></span>Waiting for first site</div>
        <h2 class="empty-title">Track your first website</h2>
        <p class="empty-sub">Add your site below, paste the snippet before the closing &lt;/head&gt; tag, and visits show up here in seconds. No cookies, no consent banner needed.</p>
        <div class="empty-steps">
          <div class="empty-step">
            <div class="step-n">1</div>
            <div class="step-body">
              <div class="step-h">Add your site</div>
              <p class="muted">Use the form below: name plus domain is enough.</p>
            </div>
          </div>
          <div class="empty-step">
            <div class="step-n">2</div>
            <div class="step-body">
              <div class="step-h">Paste the snippet</div>
              <p class="muted">Open Tracking code on the new row and paste it before <span class="mono">&lt;/head&gt;</span>.</p>
            </div>
          </div>
          <div class="empty-step">
            <div class="step-n">3</div>
            <div class="step-body">
              <div class="step-h">Open your site</div>
              <p class="muted">The first visit lands in seconds. Testing locally? <span class="mono">localhost</span> and LAN IPs are always accepted.</p>
            </div>
          </div>
        </div>
      </div>
    {/if}
    <div class="list-actions">
      <Button onclick={() => (showAdd = true)}>Add website</Button>
    </div>
    {#if error}<SitesXError message={error} onretry={retry} ondismiss={() => { error = ''; dismissed = true }} />{/if}
  </div>
{:else if error}
  <SitesXError message={error} onretry={retry} ondismiss={() => { error = ''; dismissed = true }} />
{:else if dismissed}
  <div class="load-fail">
    <p class="muted">Couldn't load websites.</p>
    <Button variant="secondary" size="sm" onclick={retry}>Retry</Button>
  </div>
{:else}
  <div class="skel-list" aria-busy="true" aria-label="Loading websites">
    {#each [0, 1, 2] as i (i)}
      <div class="skel-row">
        <Skeleton class="skel-icon" />
        <div class="skel-text">
          <Skeleton class="skel-line skel-line-a" />
          <Skeleton class="skel-line skel-line-b" />
        </div>
        <Skeleton class="skel-stat" />
      </div>
    {/each}
  </div>
{/if}

{#if showAdd}
  <Modal title="Add website" subtitle="Track traffic for a new domain." onclose={() => (showAdd = false)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        add()
      }}
    >
      <Input bind:value={newName} placeholder="Name" aria-label="Name" />
      <Input bind:value={newDomain} placeholder="yourdomain.com" aria-label="Domain" required />
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (showAdd = false)}>Cancel</Button>
        <Button type="submit" size="sm" disabled={adding || !newDomain.trim()}>{adding ? 'Adding…' : 'Add website'}</Button>
      </div>
    </form>
  </Modal>
{/if}

<style>
  .summary { display: flex; align-items: center; gap: 8px; font: var(--up-type-status-line); color: var(--up-text-secondary); }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
  .list { display: flex; flex-direction: column; margin-top: -8px; }
  .item { position: relative; border-bottom: 1px solid var(--up-border-hairline); }
  .item::before { content: ''; position: absolute; left: 0; right: 0; top: -1px; height: 2px; background: var(--up-accent); border-radius: 1px; transform: scaleX(0); transform-origin: left; transition: transform 160ms cubic-bezier(0.2, 0, 0, 1); pointer-events: none; }
  .item.over::before { transform: scaleX(1); }
  .item.dragging { opacity: 0.35; }
  .row { display: flex; align-items: center; gap: 12px; }
  .grip { display: flex; color: var(--up-text-faint); cursor: grab; padding: 4px 2px; margin-left: -14px; opacity: 0; transition: opacity 120ms ease-out; }
  .row:hover .grip { opacity: 1; }
  .main { flex: 1; display: flex; align-items: center; gap: 16px; padding: 20px 12px; border-radius: var(--up-radius-tooltip); color: var(--up-ink); min-width: 0; }
  .main:hover { background: var(--up-bg-hover); color: var(--up-ink); }
  .text { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .name { font: var(--up-type-row-title); }
  .domain { font: var(--up-type-meta); color: var(--up-text-muted); }
  .spark { display: flex; }
  .stat { width: 110px; display: flex; flex-direction: column; align-items: flex-end; gap: 2px; flex-shrink: 0; }
  .num { display: flex; align-items: baseline; gap: 6px; }
  .visitors { font: var(--up-type-row-title); font-weight: 700; }
  .delta { font: var(--up-type-small); color: var(--up-accent); }
  .delta.neg { color: var(--up-degraded-strong); }
  .caption { font: var(--up-type-small); font-weight: 500; color: var(--up-text-faint); }
  .arrow { color: var(--up-text-faint); display: flex; }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); white-space: nowrap; }
  .plain:hover { color: var(--up-ink); }
  .codebtn { color: var(--up-text-faint); }
  .codebtn.on, .codebtn:hover { color: var(--up-accent-hover); }
  .remove { font-size: 16px; font-weight: 500; color: var(--up-text-faint); line-height: 1; padding: 2px; }
  .remove:hover { color: var(--up-degraded-strong); }
  .code-wrap { padding: 0 0 18px 46px; display: flex; flex-direction: column; gap: 8px; }
  .code { background: var(--up-surface-dark); border-radius: var(--up-radius-tooltip); padding: 14px 16px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .snippet { font: var(--up-type-code); color: var(--up-text-on-dark); word-break: break-all; }
  .copy { background: none; border: none; padding: 2px 0; cursor: pointer; font: var(--up-type-small); color: var(--up-operational-strong); flex-shrink: 0; }
  .copy:hover { color: var(--up-text-on-dark); }
  .hint { font: var(--up-type-small); font-weight: 500; color: var(--up-text-muted); }
  .mono { font-family: var(--up-font-mono); }
  .empty-hero { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 28px 28px 24px; display: flex; flex-direction: column; gap: 12px; background: linear-gradient(180deg, var(--up-accent-tint) 0%, var(--up-bg) 55%); margin: 12px 0 4px; }
  .empty-status { display: inline-flex; align-items: center; gap: 8px; font: var(--up-type-small); color: var(--up-text-muted); }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--up-accent); animation: pulse 1.8s ease-out infinite; }
  @keyframes pulse { 0% { box-shadow: 0 0 0 0 var(--up-operational); } 70% { box-shadow: 0 0 0 8px transparent; } 100% { box-shadow: 0 0 0 0 transparent; } }
  .empty-title { font: var(--up-type-metric); letter-spacing: -0.01em; }
  .empty-sub { font: var(--up-type-meta); color: var(--up-text-muted); max-width: 52ch; }
  .empty-steps { display: flex; flex-direction: column; gap: 14px; margin-top: 8px; }
  .empty-step { display: flex; gap: 14px; align-items: flex-start; }
  .step-n { width: 26px; height: 26px; flex-shrink: 0; border-radius: 50%; background: var(--up-ink); color: var(--up-bg); font: var(--up-type-small); display: flex; align-items: center; justify-content: center; margin-top: 2px; }
  .step-body { display: flex; flex-direction: column; gap: 4px; min-width: 0; flex: 1; }
  .step-body .muted { font: var(--up-type-meta); }
  .step-h { font: var(--up-type-setting); }
  .skel-list { display: flex; flex-direction: column; margin-top: -8px; }
  .skel-row { display: flex; align-items: center; gap: 16px; padding: 20px 12px; border-bottom: 1px solid var(--up-border-hairline); }
  .skel-row :global(.skel-icon) { width: 22px; height: 22px; border-radius: 50%; flex-shrink: 0; }
  .skel-text { flex: 1; display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .skel-row :global(.skel-line) { height: 12px; }
  .skel-row :global(.skel-line-a) { width: 38%; }
  .skel-row :global(.skel-line-b) { width: 55%; }
  .skel-row :global(.skel-stat) { width: 110px; height: 20px; flex-shrink: 0; }
  .load-fail { display: flex; align-items: center; gap: 12px; padding: 18px 0; font: var(--up-type-meta); }
  .list-actions { display: flex; padding-top: var(--up-space-5); }
  .modal-form { display: flex; flex-direction: column; gap: 12px; }
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .bad { font: var(--up-type-meta); color: var(--up-degraded-strong); }
  @media (max-width: 600px) {
    .spark, .grip { display: none; }
    .stat { width: auto; }
    .code-wrap { padding-left: 0; }
    .empty-hero { padding: 20px 18px; }
    .skel-row :global(.skel-stat) { display: none; }
  }
</style>
