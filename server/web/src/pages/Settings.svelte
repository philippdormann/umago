<script lang="ts">
  // General settings: overview, appearance, security, users, teams, keys, MCP and API tokens, retention, location, export.
  import { api, type APIKey, type GeneralSettings, type GeoIPStatus, type Member, type Status, type Team, type Token, type User } from '../lib/api'
  import { fmtAgo, fmtExact, fmtNum } from '../lib/format'
  import { isHex, setBaseAccent } from '../lib/accent'
  import { panel, reorder } from '../lib/motion'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Switch from '../lib/ui/Switch.svelte'
  import Segment from '../lib/ui/Segment.svelte'
  import Swatches from '../lib/ui/Swatches.svelte'
  import MetricStat from '../lib/ui/MetricStat.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import * as Tabs from '../lib/components/ui/tabs/index.js'
  import { confirmAction } from '../lib/confirm.svelte'

  let { ontitle }: { ontitle: (t: string) => void } = $props()
  let status = $state<Status | null>(null)
  let settings = $state<GeneralSettings | null>(null)
  let tokens = $state<Token[]>([])
  let envToken = $state(false)
  let error = $state('')
  let tokenName = $state('')
  let minted = $state<{ name: string; secret: string } | null>(null)
  let copiedId = $state<string | null>(null)
  let copyTimer: ReturnType<typeof setTimeout> | null = null
  let me = $state<{ username?: string; role?: string }>({})
  const isAdmin = $derived(!me.role || me.role === 'admin')
  // Settings tabs (shadcn Tabs). Deep-linked via location.hash so a tab
  // survives reload and back/forward; unknown hashes fall back to general.
  const ALL_TABS = ['general', 'access', 'tokens', 'keys', 'users', 'teams', 'location'] as const
  const ADMIN_TABS: readonly string[] = ['keys', 'users', 'teams', 'location']
  function tabFromHash(): string {
    if (typeof location === 'undefined') return 'general'
    const h = location.hash.replace(/^#/, '')
    // Backwards compat: every old anchor maps onto its tab.
    if (h === 'appearance' || h === 'retention' || h === 'data') return 'general'
    return (ALL_TABS as readonly string[]).includes(h) ? h : 'general'
  }
  let tab = $state<string>(tabFromHash())
  $effect(() => {
    const onHash = () => {
      const next = tabFromHash()
      if (next !== tab) tab = next
    }
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  })
  // Non-admins never see admin tabs: bounce them back to general.
  $effect(() => {
    if (!isAdmin && ADMIN_TABS.includes(tab)) tab = 'general'
  })
  // Keep the hash in sync without adding history entries or scrolling.
  $effect(() => {
    if (typeof location === 'undefined') return
    const current = tab
    if (location.hash !== `#${current}`) history.replaceState(null, '', `#${current}`)
  })
  // API keys (full access, unlike the read-only MCP tokens).
  let keys = $state<APIKey[]>([])
  let keyName = $state('')
  let keyScopes = $state('read,write')
  let mintedKey = $state<{ name: string; secret: string } | null>(null)
  // Users and teams (admins only; the endpoints 403 otherwise).
  let users = $state<User[]>([])
  let newUsername = $state('')
  let newPassword = $state('')
  let newRole = $state('viewer')
  let teams = $state<Team[]>([])
  let teamName = $state('')
  let members = $state<Record<string, Member[]>>({})
  let memberUser = $state<Record<string, string>>({})
  let memberRole = $state<Record<string, string>>({})
  // Two-factor for the signed-in account.
  let totpOn = $state(false)
  let totpSecret = $state('')
  let totpQR = $state('')
  let totpCode = $state('')
  let backupCodes = $state<string[]>([])
  let totpPassword = $state('')
  // Optional GeoIP location (admins only; the endpoints 403 otherwise).
  let geoip = $state<GeoIPStatus | null>(null)
  let geoKey = $state('')
  let geoBusy = $state(false)
  // Modal visibility for the add/create dialogs.
  let showMintToken = $state(false)
  let showMintKey = $state(false)
  let showAddUser = $state(false)
  let showCreateTeam = $state(false)
  let showTotpSetup = $state(false)
  let memberModalTid = $state<string | null>(null)
  // Debounced general-settings saves share one subtle indicator; GeoIP has its own.
  let saveState = $state<'idle' | 'saving' | 'saved'>('idle')
  let savedAt = $state<Date | null>(null)
  let geoSaveState = $state<'idle' | 'saving' | 'saved'>('idle')
  let geoSavedAt = $state<Date | null>(null)

  async function copy(text: string, id: string) {
    try {
      await navigator.clipboard.writeText(text)
      copiedId = id
      if (copyTimer) clearTimeout(copyTimer)
      copyTimer = setTimeout(() => {
        if (copiedId === id) copiedId = null
      }, 1500)
    } catch {}
  }

  async function load() {
    try {
      const meRes = await api.me().catch(() => null)
      me = meRes ?? {}
      const [st, g, tk] = await Promise.all([api.status(), api.settings(), api.tokens()])
      status = st
      settings = g
      tokens = tk.tokens
      envToken = tk.env_token_set
      const [tp, ks] = await Promise.all([api.totpStatus().catch(() => null), api.keys().catch(() => null)])
      if (tp) totpOn = tp.enabled
      if (ks) keys = ks.keys
      if (isAdmin) {
        const [us, ts, gi] = await Promise.all([api.users().catch(() => null), api.teams().catch(() => null), api.geoip().catch(() => null)])
        if (us) users = us.users
        if (ts) teams = ts.teams
        if (gi) geoip = gi
        for (const t of teams) void loadMembers(t.id)
      }
    } catch (e: any) {
      error = e.message
    }
  }
  async function loadMembers(tid: string) {
    // Initialise per-team form state: Input's value is $bindable('') and
    // Svelte throws props_invalid_value on bind:value={undefined}.
    memberUser[tid] ??= ''
    memberRole[tid] ??= 'member'
    try {
      members[tid] = (await api.members(tid)).members
    } catch (e: any) {
      error = e.message
    }
  }
  $effect(() => {
    load()
  })

  let timers: Record<string, ReturnType<typeof setTimeout>> = {}
  function save(patch: Parameters<typeof api.updateSettings>[0], key = 'x', delay = 0) {
    saveState = 'saving'
    clearTimeout(timers[key])
    timers[key] = setTimeout(() => {
      error = ''
      api
        .updateSettings(patch)
        .then((g) => {
          settings = g
          setBaseAccent(g.accent)
          ontitle(g.title)
          saveState = 'saved'
          savedAt = new Date()
        })
        .catch((e: any) => {
          error = e.message
          saveState = 'idle'
        })
    }, delay)
  }
  function pickAccent(hex: string) {
    if (!isHex(hex)) return
    setBaseAccent(hex) // instant preview
    save({ accent: hex })
  }
  async function mint() {
    try {
      const r = await api.createToken(tokenName.trim() || 'API token')
      minted = { name: r.token.name, secret: r.secret }
      tokenName = ''
      tokens = [r.token, ...tokens]
      showMintToken = false
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function geoSave(patch: Parameters<typeof api.updateGeoip>[0]) {
    geoSaveState = 'saving'
    try {
      geoip = await api.updateGeoip(patch)
      geoSaveState = 'saved'
      geoSavedAt = new Date()
      error = ''
    } catch (e: any) {
      error = e.message
      geoSaveState = 'idle'
    }
  }
  async function geoRefresh() {
    geoBusy = true
    try {
      geoip = await api.refreshGeoip()
      error = ''
    } catch (e: any) {
      error = e.message
    } finally {
      geoBusy = false
    }
  }
  async function geoUpload(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const f = input.files?.[0]
    if (!f) return
    geoBusy = true
    try {
      geoip = await api.uploadGeoip(f)
      error = ''
    } catch (err: any) {
      error = err.message
    } finally {
      geoBusy = false
      input.value = ''
    }
  }
  async function geoSaveKey() {
    await geoSave({ maxmind_key: geoKey.trim() })
    geoKey = ''
  }
  async function geoClearData() {
    const ok = await confirmAction({
      title: 'Delete GeoIP database?',
      message: 'Location falls back to time zones until a new one is added.',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    api
      .deleteGeoipData()
      .then((g) => {
        geoip = g
        error = ''
      })
      .catch((e: any) => (error = e.message))
  }
  async function revoke(t: Token) {
    const ok = await confirmAction({
      title: `Revoke "${t.name}"?`,
      message: 'Anything using it stops working immediately.',
      confirmLabel: 'Revoke',
    })
    if (!ok) return
    api
      .deleteToken(t.id)
      .then(() => (tokens = tokens.filter((x) => x.id !== t.id)))
      .catch((e: any) => (error = e.message))
  }
  async function mintKey() {
    try {
      const r = await api.createKey(keyName.trim() || 'API key', keyScopes)
      mintedKey = { name: r.key.name, secret: r.secret }
      keyName = ''
      keys = [r.key, ...keys]
      showMintKey = false
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function rotateKey(k: APIKey) {
    const ok = await confirmAction({
      title: `Rotate "${k.name}"?`,
      message: 'The old secret stops working immediately and the new one is shown once.',
      confirmLabel: 'Rotate',
    })
    if (!ok) return
    try {
      const r = await api.rotateKey(k.id)
      mintedKey = { name: r.key.name, secret: r.secret }
      keys = keys.map((x) => (x.id === k.id ? r.key : x))
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function revokeKey(k: APIKey) {
    const ok = await confirmAction({
      title: `Revoke "${k.name}"?`,
      message: 'Anything using it stops working immediately.',
      confirmLabel: 'Revoke',
    })
    if (!ok) return
    api
      .deleteKey(k.id)
      .then(() => (keys = keys.filter((x) => x.id !== k.id)))
      .catch((e: any) => (error = e.message))
  }
  async function createUser() {
    try {
      const u = await api.createUser({ username: newUsername.trim(), password: newPassword, role: newRole })
      users = [...users, u]
      newUsername = ''
      newPassword = ''
      showAddUser = false
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function setUserRole(u: User, role: string) {
    try {
      const next = await api.updateUser(u.id, { role })
      users = users.map((x) => (x.id === u.id ? next : x))
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeUser(u: User) {
    const ok = await confirmAction({
      title: `Delete "${u.username}"?`,
      message: 'Their sessions and team memberships go too.',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    api
      .deleteUser(u.id)
      .then(() => (users = users.filter((x) => x.id !== u.id)))
      .catch((e: any) => (error = e.message))
  }
  async function createTeam() {
    if (!teamName.trim()) return
    try {
      const t = await api.createTeam(teamName.trim())
      teams = [...teams, t]
      memberUser[t.id] ??= ''
      memberRole[t.id] ??= 'member'
      teamName = ''
      showCreateTeam = false
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeTeam(t: Team) {
    const ok = await confirmAction({
      title: `Delete team "${t.name}"?`,
      message: 'Its sites become visible to everyone signed in.',
      confirmLabel: 'Delete',
    })
    if (!ok) return
    api
      .deleteTeam(t.id)
      .then(() => (teams = teams.filter((x) => x.id !== t.id)))
      .catch((e: any) => (error = e.message))
  }
  async function addMember(tid: string) {
    const uid = (memberUser[tid] ?? '').trim()
    if (!uid) return
    try {
      members[tid] = (await api.addMember(tid, uid, memberRole[tid] ?? 'member')).members
      memberUser[tid] = ''
      memberModalTid = null
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function removeMember(tid: string, m: Member) {
    const ok = await confirmAction({
      title: `Remove "${m.username}"?`,
      message: 'They lose access to this team’s pinned sites.',
      confirmLabel: 'Remove',
    })
    if (!ok) return
    api
      .removeMember(tid, m.user_id)
      .then(() => loadMembers(tid))
      .catch((e: any) => (error = e.message))
  }
  async function setupTotp() {
    try {
      const r = await api.totpSetup()
      totpSecret = r.secret
      totpQR = r.qr
      totpCode = ''
      backupCodes = []
      error = ''
      showTotpSetup = true
    } catch (e: any) {
      error = e.message
    }
  }
  function closeTotpSetup() {
    showTotpSetup = false
    totpSecret = ''
    totpQR = ''
    totpCode = ''
  }
  async function confirmTotp() {
    try {
      const r = await api.totpConfirm(totpCode.trim())
      totpOn = r.enabled
      backupCodes = r.backup_codes
      closeTotpSetup()
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  async function disableTotp() {
    try {
      await api.totpDisable(totpPassword)
      totpOn = false
      totpPassword = ''
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }
  const mcpURL = `${location.origin}/mcp`
  const mcpConfig = $derived(JSON.stringify({ mcpServers: { umago: { url: mcpURL, headers: { Authorization: `Bearer ${minted?.secret ?? 'umago_tok_…'}` } } } }, null, 2))
  const fmtBytes = (b: number) => (b >= 1 << 30 ? (b / (1 << 30)).toFixed(1) + ' GB' : b >= 1 << 20 ? (b / (1 << 20)).toFixed(1) + ' MB' : Math.round(b / 1024) + ' KB')
  const fmtUptime = (s: number) => (s >= 86400 ? `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h` : s >= 3600 ? `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m` : `${Math.floor(s / 60)}m`)
  const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s)
  // Effective GeoIP state in one line: configured mode is not enough, a
  // database must actually be loaded for lookups to run.
  const geoStatus = $derived(
    !geoip
      ? ''
      : geoip.mode === 'off'
        ? 'Inactive. Using proxy headers and time zones.'
        : geoip.file_path
          ? `Active: ${geoip.mode} resolution via file.`
          : geoip.has_data
            ? `Active: ${geoip.mode} resolution via ${geoip.edition || 'stored database'}.`
            : 'On but no database stored. Using time zones until one is added.',
  )
</script>

{#if status && settings}
  <div class="metrics">
    <MetricStat label="Websites" value={String(status.sites)} />
    <MetricStat label="Raw events" value={fmtNum(status.raw_events)} />
    <MetricStat label="Rollup rows" value={fmtNum(status.daily_rows)} />
    <MetricStat label="Database" value={fmtBytes(status.db_bytes)} />
    {#if geoip}<MetricStat label="Location" value={geoip.mode === 'off' ? 'Off' : `${cap(geoip.mode)}${geoip.file_path || geoip.has_data ? '' : ' · no DB'}`} />{/if}
    <MetricStat label="Uptime" value={fmtUptime(status.uptime_seconds)} />
  </div>

  <Tabs.Root bind:value={tab} class="settings-tabs">
    <Tabs.List class="settings-tabs-list">
      <Tabs.Trigger value="general">General</Tabs.Trigger>
      <Tabs.Trigger value="access">Access</Tabs.Trigger>
      <Tabs.Trigger value="tokens">Tokens</Tabs.Trigger>
      {#if isAdmin}<Tabs.Trigger value="keys">Keys</Tabs.Trigger>{/if}
      {#if isAdmin}<Tabs.Trigger value="users">Users</Tabs.Trigger>{/if}
      {#if isAdmin}<Tabs.Trigger value="teams">Teams</Tabs.Trigger>{/if}
      {#if isAdmin}<Tabs.Trigger value="location">Location</Tabs.Trigger>{/if}
    </Tabs.List>

    <Tabs.Content value="general">
      <div class="tab-stack">
<section class="card">
    <div class="card-title">Appearance{#if saveState !== 'idle'}<span class="save-state">{saveState === 'saving' ? 'Saving…' : `Saved ${savedAt?.toLocaleTimeString() ?? ''}`}</span>{/if}</div>
    <div class="setting">
      <div class="text">
        <div class="label">Accent colour</div>
        <div class="hint">Used for the wordmark, links, charts and the map</div>
      </div>
      <Swatches value={settings.accent} onpick={pickAccent} />
    </div>
    <div class="setting">
      <div class="text">
        <div class="label">Title</div>
        <div class="hint">Shown as the wordmark in the header and the browser tab</div>
      </div>
      <div class="ctl"><Input value={settings.title} placeholder="Umago" aria-label="Title" oninput={(e) => save({ title: e.currentTarget.value }, 'title', 400)} /></div>
    </div>
  </section>

<section class="card">
    <div class="card-title">Retention{#if saveState !== 'idle'}<span class="save-state">{saveState === 'saving' ? 'Saving…' : `Saved ${savedAt?.toLocaleTimeString() ?? ''}`}</span>{/if}</div>
    <div class="setting">
      <div class="text">
        <div class="label">Keep raw events for</div>
        <div class="hint">{settings.retention_from_env ? 'Set by UMAGO_RETENTION_DAYS in the environment.' : 'Hourly and daily rollups are kept forever; raw rows only feed the live view and today\'s rebuild.'}</div>
      </div>
      <Segment options={[2, 7, 14, 30, 90].map((d) => ({ value: d, label: `${d}d` }))} value={settings.retention_days} gap={14} disabled={settings.retention_from_env} onchange={(d) => save({ retention_days: d })} />
    </div>
  </section>

<section class="card">
    <div class="card-title">Data</div>
    <div class="setting">
      <div class="text">
        <div class="label">Export</div>
        <div class="hint">Every site and daily rollup as one JSON file</div>
      </div>
      <a class="btn" href="/api/v1/export" download>Download</a>
    </div>
    <div class="setting">
      <div class="text">
        <div class="label">Ingest</div>
        <div class="hint">{fmtNum(status.written)} events written since start{#if status.dropped > 0} · <span class="bad">{status.dropped} dropped when the queue was full</span>{/if}</div>
      </div>
    </div>
  </section>
      </div>
    </Tabs.Content>

    <Tabs.Content value="access">
<section class="card">
    <div class="card-title">Access</div>
    <div class="setting">
      <div class="text">
        <div class="label">Two-factor authentication</div>
        <div class="hint">{totpOn ? 'Your account asks for an authenticator code at sign-in.' : 'Add an authenticator app code to your sign-in.'}</div>
      </div>
      {#if totpOn}
        <div class="ctl narrow"><Input bind:value={totpPassword} placeholder="Password to disable" aria-label="Password to disable 2FA" type="password" /></div>
        <Button variant="secondary" size="sm" disabled={!totpPassword} onclick={disableTotp}>Disable</Button>
      {:else}
        <Button variant="secondary" size="sm" onclick={setupTotp}>Set up</Button>
      {/if}
    </div>
    {#if backupCodes.length > 0}
      <div class="tokens">
        <div class="label">Backup codes</div>
        <div class="hint">Each works once. Store them somewhere safe; they are never shown again. <button type="button" class="plain" onclick={() => copy(backupCodes.join('\n'), 'backup-codes')}>{copiedId === 'backup-codes' ? 'Copied' : 'Copy'}</button></div>
        <pre class="config">{backupCodes.join('\n')}</pre>
      </div>
    {/if}
    <div class="setting">
      <div class="text">
        <div class="label">Signed in as</div>
        <div class="hint">{me.username || '-'}{me.role ? ` · ${me.role}` : ''}</div>
      </div>
    </div>
  </section>
    </Tabs.Content>

    <Tabs.Content value="tokens">
<section class="card">
    <div class="card-title">MCP{#if saveState !== 'idle'}<span class="save-state">{saveState === 'saving' ? 'Saving…' : `Saved ${savedAt?.toLocaleTimeString() ?? ''}`}</span>{/if}</div>
    <div class="setting">
      <div class="text">
        <div class="label">MCP endpoint</div>
        <div class="hint">Lets an AI agent read your analytics at <code>{mcpURL}</code>. Tokens minted below and the admin login work there{envToken ? ', as does UMAGO_MCP_TOKEN from the environment' : ''}.</div>
      </div>
      <Switch checked={settings.mcp_enabled} label="MCP endpoint" onchange={(v) => save({ mcp_enabled: v })} />
    </div>
    <div class="tokens">
      <div class="label">API tokens</div>
      <div class="hint">Read-only. Each token is shown once when minted; only a hash is kept.</div>
      {#if minted}
        <div class="minted" transition:panel>
          <div class="code">
            <span class="snippet">{minted.secret}</span>
            <button type="button" class="copy" onclick={() => copy(minted!.secret, 'minted-token')}>{copiedId === 'minted-token' ? 'Copied' : 'Copy'}</button>
          </div>
          <div class="hint">This is the only time "{minted.name}" is shown. Add it to your agent:</div>
          <pre class="config">{mcpConfig}</pre>
          <div><button type="button" class="plain" onclick={() => copy(mcpConfig, 'mcp-config')}>{copiedId === 'mcp-config' ? 'Copied' : 'Copy config'}</button></div>
          <button type="button" class="plain" onclick={() => (minted = null)}>Done</button>
        </div>
      {/if}
      {#each tokens as t (t.id)}
        <div class="row" animate:reorder transition:panel>
          <div class="text">
            <div class="name">{t.name}</div>
            <div class="hint mono">{t.prefix}… · <span title={t.created_at}>created {fmtExact(t.created_at)} ({fmtAgo(t.created_at)})</span>{#if t.last_used_at}<span title={t.last_used_at}>{' · '}last used {fmtExact(t.last_used_at)} ({fmtAgo(t.last_used_at)})</span>{:else}{' · '}never used{/if}</div>
          </div>
          <button type="button" class="plain remove" title="Revoke" aria-label="Revoke token" onclick={() => revoke(t)}>×</button>
        </div>
      {/each}
      <div class="list-actions">
        <Button variant="secondary" size="sm" onclick={() => (showMintToken = true)}>Mint token</Button>
      </div>
    </div>
  </section>
    </Tabs.Content>

    {#if isAdmin}
      <Tabs.Content value="keys">
<section class="card">
      <div class="card-title">API keys</div>
      <div class="tokens" style="padding-top: 0">
        <div class="hint">Full access, unlike the read-only MCP tokens above. Each secret is shown once; rotate replaces it in place.</div>
        {#if mintedKey}
          <div class="minted">
            <div class="code">
              <span class="snippet">{mintedKey.secret}</span>
              <button type="button" class="copy" onclick={() => copy(mintedKey!.secret, 'minted-key')}>{copiedId === 'minted-key' ? 'Copied' : 'Copy'}</button>
            </div>
            <div class="hint">This is the only time "{mintedKey.name}" is shown.</div>
            <button type="button" class="plain" onclick={() => (mintedKey = null)}>Done</button>
          </div>
        {/if}
        {#each keys as k (k.id)}
          <div class="row">
            <div class="text">
              <div class="name">{k.name} <span class="hint">· {k.scopes}</span></div>
              <div class="hint mono">{k.prefix}… · <span title={k.created_at}>created {fmtExact(k.created_at)} ({fmtAgo(k.created_at)})</span>{#if k.rotated_at}<span title={k.rotated_at}>{' · '}rotated {fmtExact(k.rotated_at)} ({fmtAgo(k.rotated_at)})</span>{/if}{#if k.last_used_at}<span title={k.last_used_at}>{' · '}last used {fmtExact(k.last_used_at)} ({fmtAgo(k.last_used_at)})</span>{:else}{' · '}never used{/if}</div>
            </div>
            <div class="row-actions">
              <button type="button" class="plain" title="Rotate" aria-label="Rotate key" onclick={() => rotateKey(k)}>Rotate</button>
              <button type="button" class="plain remove" title="Revoke" aria-label="Revoke key" onclick={() => revokeKey(k)}>×</button>
            </div>
          </div>
        {/each}
        <div class="list-actions">
          <Button size="sm" onclick={() => (showMintKey = true)}>Mint key</Button>
        </div>
      </div>
    </section>
      </Tabs.Content>

      <Tabs.Content value="users">
<section class="card">
      <div class="card-title">Users</div>
      <div class="hint">Admins manage everything, members manage sites, viewers only read. The env account is bootstrapped as an admin.</div>
      {#each users as u (u.id)}
        <div class="row">
          <div class="text">
            <div class="name">{u.username}{u.totp_enabled ? ' · 2FA' : ''}</div>
            <div class="hint mono"><span title={u.created_at}>since {fmtExact(u.created_at)} ({fmtAgo(u.created_at)})</span></div>
          </div>
          <div class="row-actions">
            <select value={u.role} aria-label="Role" onchange={(e) => setUserRole(u, e.currentTarget.value)}>
              <option value="admin">admin</option>
              <option value="member">member</option>
              <option value="viewer">viewer</option>
            </select>
            <button type="button" class="plain remove" title="Delete" aria-label="Delete user" onclick={() => removeUser(u)}>×</button>
          </div>
        </div>
      {/each}
      <div class="list-actions">
        <Button size="sm" onclick={() => (showAddUser = true)}>Add user</Button>
      </div>
    </section>
      </Tabs.Content>

      <Tabs.Content value="teams">
<section class="card">
      <div class="card-title">Teams</div>
      <div class="hint">Pin a site to a team in its settings and only that team's members see it. Unpinned sites stay visible to everyone signed in.</div>
      {#each teams as t (t.id)}
        <div class="team">
          <div class="row">
            <div class="text">
              <div class="name">{t.name}</div>
              <div class="hint mono"><span title={t.created_at}>created {fmtExact(t.created_at)} ({fmtAgo(t.created_at)})</span></div>
            </div>
            <button type="button" class="plain remove" title="Delete team" aria-label="Delete team" onclick={() => removeTeam(t)}>×</button>
          </div>
          {#each members[t.id] ?? [] as m (`${m.user_id}`)}
            <div class="row sub">
              <div class="hint">{m.username}</div>
              <div class="row-actions">
                <select value={m.role} aria-label="Team role" onchange={(e) => api.addMember(t.id, m.user_id, e.currentTarget.value).then((r) => (members[t.id] = r.members)).catch((err: any) => (error = err.message))}>
                  <option value="owner">owner</option>
                  <option value="manager">manager</option>
                  <option value="member">member</option>
                  <option value="viewer">viewer</option>
                </select>
                <button type="button" class="plain remove" title="Remove" aria-label="Remove member" onclick={() => removeMember(t.id, m)}>×</button>
              </div>
            </div>
          {/each}
          <div class="list-actions">
            <Button variant="secondary" size="sm" onclick={() => (memberModalTid = t.id)}>Add member</Button>
          </div>
        </div>
      {/each}
      <div class="list-actions">
        <Button size="sm" onclick={() => (showCreateTeam = true)}>Create team</Button>
      </div>
    </section>
      </Tabs.Content>

      <Tabs.Content value="location">
<section class="card">
      <div class="card-title">Location{#if geoSaveState !== 'idle'}<span class="save-state">{geoSaveState === 'saving' ? 'Saving…' : `Saved ${geoSavedAt?.toLocaleTimeString() ?? ''}`}</span>{/if}</div>
      {#if geoip}
        <div class="setting">
          <div class="text">
            <div class="label">Status</div>
            <div class="hint">{geoStatus}{#if geoip.mode !== 'off' && (geoip.file_path || geoip.has_data) && status}&nbsp;{fmtBytes(status.geoip_bytes)} stored.{/if}</div>
          </div>
        </div>
        <div class="setting">
          <div class="text">
            <div class="label">Location detail</div>
            <div class="hint">{geoip.mode_from_env ? 'Set by UMAGO_GEOIP_MODE in the environment.' : 'Off uses proxy headers and time zones only. Country resolves the country from an IP database; City additionally resolves the city into the region.'}</div>
          </div>
          <Segment options={[{ value: 'off', label: 'Off' }, { value: 'country', label: 'Country' }, { value: 'city', label: 'City' }]} value={geoip.mode} gap={14} disabled={geoip.mode_from_env} onchange={(v) => geoSave({ mode: v })} />
        </div>
        <div class="setting">
          <div class="text">
            <div class="label">Database source</div>
            <div class="hint">{geoip.source_from_env ? 'Set by UMAGO_GEOIP_SOURCE in the environment.' : 'DB-IP Lite downloads freely with no key. MaxMind GeoLite2 needs a free license key below.'}</div>
          </div>
          <div class="row-actions">
            <select bind:value={geoip.source} aria-label="GeoIP source" disabled={geoip.source_from_env} onchange={() => geoSave({ source: geoip!.source })}>
              <option value="dbip">DB-IP Lite</option>
              <option value="maxmind">MaxMind GeoLite2</option>
            </select>
          </div>
        </div>
        {#if geoip.source === 'maxmind' && !geoip.key_from_env}
          <div class="setting">
            <div class="text">
              <div class="label">MaxMind license key</div>
              <div class="hint">{geoip.has_maxmind_key ? 'A key is stored. Saving replaces it; the key itself is never shown.' : 'Free from your MaxMind account; required to download GeoLite2 files.'}</div>
            </div>
            <div class="row-actions">
              <div class="ctl narrow"><Input bind:value={geoKey} placeholder={geoip.has_maxmind_key ? '•••••• (stored)' : 'MaxMind license key'} aria-label="MaxMind license key" type="password" mono /></div>
              <Button size="sm" disabled={!geoKey.trim()} onclick={geoSaveKey}>Save</Button>
            </div>
          </div>
        {/if}
        <div class="setting">
          <div class="text">
            <div class="label">Database</div>
            {#if geoip.file_path}
              <div class="hint">Reading <code>{geoip.file_path}</code> from UMAGO_GEOIP_PATH; the stored database is ignored.</div>
            {:else if geoip.has_data}
              <div class="hint">{geoip.edition || 'Unknown edition'}{#if geoip.build_date}{' · built '}{geoip.build_date}{/if} · {(geoip.size_bytes / 1048576).toFixed(1)} MB{#if geoip.synced_at}<span title={geoip.synced_at}>{' · synced '}{fmtExact(geoip.synced_at)} ({fmtAgo(geoip.synced_at)})</span>{/if}</div>
              {#if geoip.sync_error}<div class="hint bad">Last refresh failed: {geoip.sync_error}</div>{/if}
            {:else}
              <div class="hint">No database stored. Refresh downloads one for the source above, or upload an .mmdb file.</div>
            {/if}
          </div>
          <div class="row-actions">
            <Button variant="secondary" size="sm" disabled={geoBusy} onclick={geoRefresh}>{geoBusy ? 'Working' : 'Refresh now'}</Button>
            {#if geoip.has_data && !geoip.file_path}<button type="button" class="plain" onclick={geoClearData}>Delete</button>{/if}
          </div>
        </div>
        {#if !geoip.file_path}
          <div class="setting">
            <div class="text">
              <div class="label">Upload .mmdb</div>
              <div class="hint">A DB-IP or MaxMind file, city or country edition, up to 300 MB.</div>
            </div>
            <input type="file" accept=".mmdb" aria-label="Upload .mmdb" onchange={geoUpload} disabled={geoBusy} />
          </div>
        {/if}
        <div class="setting">
          <div class="text">
            <div class="hint">Lookup order per hit: proxy country header, then this database, then the visitor's time zone. IPs are never stored, only the resolved country and city. DB-IP data is CC BY 4.0 and MaxMind GeoLite2 needs its attribution note; see the README.</div>
          </div>
        </div>
      {:else}
        <p class="muted">Loading…</p>
      {/if}
    </section>
      </Tabs.Content>
    {/if}
  </Tabs.Root>

{#if showMintToken}
  <Modal title="Mint API token" subtitle="Read-only. The secret is shown once." onclose={() => (showMintToken = false)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        mint()
      }}
    >
      <Input bind:value={tokenName} placeholder="Name, e.g. Claude" aria-label="Token name" />
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (showMintToken = false)}>Cancel</Button>
        <Button type="submit" size="sm">Mint token</Button>
      </div>
    </form>
  </Modal>
{/if}

{#if showMintKey}
  <Modal title="Mint API key" subtitle="Full access. The secret is shown once." onclose={() => (showMintKey = false)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        mintKey()
      }}
    >
      <Input bind:value={keyName} placeholder="Name, e.g. CI" aria-label="Key name" />
      <select bind:value={keyScopes} aria-label="Key scopes"><option value="read,write">read + write</option><option value="read">read</option></select>
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (showMintKey = false)}>Cancel</Button>
        <Button type="submit" size="sm">Mint key</Button>
      </div>
    </form>
  </Modal>
{/if}

{#if showTotpSetup}
  <Modal title="Set up two-factor authentication" subtitle="Scan the QR code, then enter a code to finish." onclose={closeTotpSetup}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        confirmTotp()
      }}
    >
      {#if totpQR}<img class="totp-qr" src={totpQR} alt="2FA setup QR code" width="160" height="160" />{/if}
      {#if totpSecret}
        <div class="hint">Secret: <code>{totpSecret}</code> <button type="button" class="plain" onclick={() => copy(totpSecret, 'totp-secret')}>{copiedId === 'totp-secret' ? 'Copied' : 'Copy'}</button></div>
      {/if}
      <Input bind:value={totpCode} placeholder="123456" aria-label="Authenticator code" inputmode="numeric" mono />
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={closeTotpSetup}>Cancel</Button>
        <Button type="submit" size="sm" disabled={!totpCode.trim()}>Confirm</Button>
      </div>
    </form>
  </Modal>
{/if}

{#if isAdmin}
{#if showAddUser}
  <Modal title="Add user" subtitle="Admins manage everything, members manage sites, viewers only read." onclose={() => (showAddUser = false)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        createUser()
      }}
    >
      <Input bind:value={newUsername} placeholder="Username" aria-label="New username" />
      <Input bind:value={newPassword} placeholder="Password (8+ chars)" aria-label="New password" type="password" />
      <select bind:value={newRole} aria-label="New role"><option value="viewer">viewer</option><option value="member">member</option><option value="admin">admin</option></select>
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (showAddUser = false)}>Cancel</Button>
        <Button type="submit" size="sm" disabled={!newUsername.trim() || newPassword.length < 8}>Add user</Button>
      </div>
    </form>
  </Modal>
{/if}

{#if memberModalTid}
  {@const mtid = memberModalTid}
  <Modal title="Add member" subtitle="They gain access to this team's pinned sites." onclose={() => (memberModalTid = null)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        addMember(mtid)
      }}
    >
      <Input bind:value={memberUser[mtid]} placeholder="User id" aria-label="User id" mono />
      <select bind:value={memberRole[mtid]} aria-label="Member role"><option value="member">member</option><option value="manager">manager</option><option value="owner">owner</option><option value="viewer">viewer</option></select>
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (memberModalTid = null)}>Cancel</Button>
        <Button type="submit" size="sm" disabled={!(memberUser[mtid] ?? '').trim()}>Add</Button>
      </div>
    </form>
  </Modal>
{/if}

{#if showCreateTeam}
  <Modal title="Create team" subtitle="Pin sites to the team from a site's settings." onclose={() => (showCreateTeam = false)}>
    <form
      class="modal-form"
      onsubmit={(e) => {
        e.preventDefault()
        createTeam()
      }}
    >
      <Input bind:value={teamName} placeholder="Team name" aria-label="Team name" />
      {#if error}<p class="bad">{error}</p>{/if}
      <div class="modal-actions">
        <Button variant="secondary" size="sm" type="button" onclick={() => (showCreateTeam = false)}>Cancel</Button>
        <Button type="submit" size="sm" disabled={!teamName.trim()}>Create team</Button>
      </div>
    </form>
  </Modal>
{/if}
{/if}

<footer class="version">Umago {status.version === 'dev' ? 'dev' : `v${status.version.replace(/^v/, '')}`} · <a href="https://github.com/philippdormann/umago" target="_blank" rel="noopener" aria-label="Umago GitHub repository (opens in new tab)">GitHub <span aria-hidden="true" class="ext">↗</span></a></footer>

  {#if error}<p class="bad">{error}</p>{/if}
{:else if error}
  <p class="bad">{error}</p>
{/if}

<style>
  .metrics { display: flex; gap: 36px; flex-wrap: wrap; }
  .card { border: 1px solid var(--up-border-hairline); border-radius: var(--up-radius-card); padding: 20px; display: flex; flex-direction: column; gap: 4px; scroll-margin-top: 12px; }
  .card-title { font: var(--up-type-setting); font-weight: 700; margin-bottom: 6px; }
  .save-state { font: var(--up-type-meta); font-weight: 400; color: var(--up-text-faint); margin-left: 8px; }
  /* Classes passed into the shadcn Tabs primitives render inside the child
     components, so they need :global to match at runtime. */
  :global(.settings-tabs) { display: flex; flex-direction: column; gap: 20px; }
  :global(.settings-tabs-list) { width: 100%; justify-content: flex-start; flex-wrap: wrap; height: auto; row-gap: 6px; }
  :global(.settings-tabs-list [data-slot='tabs-trigger']) { flex: 0 0 auto; height: auto; padding: 6px 12px; }
  .tab-stack { display: flex; flex-direction: column; gap: 36px; }
  .setting { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 14px 0; border-bottom: 1px solid var(--up-border-hairline); }
  .setting:last-child { border-bottom: none; }
  .text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .label { font: var(--up-type-setting); }
  .name { font: var(--up-type-row-title); }
  .hint { font: var(--up-type-meta); color: var(--up-text-muted); line-height: 1.5; }
  .hint code { font: var(--up-type-code); }
  .mono { font: var(--up-type-code); }
  .ctl { width: 220px; flex-shrink: 0; }
  .tokens { display: flex; flex-direction: column; gap: 10px; padding-top: 14px; }
  .totp-qr { width: 160px; height: 160px; background: #fff; padding: 8px; border-radius: 8px; border: 1px solid var(--up-border-hairline); }
  .row { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 0; border-top: 1px solid var(--up-border-hairline); }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); }
  .plain:hover { color: var(--up-ink); }
  .remove { font-size: 16px; color: var(--up-text-faint); line-height: 1; }
  .remove:hover { color: var(--up-degraded-strong); }
  .row-actions select, .modal-form select { font: var(--up-type-meta); color: var(--up-ink); background: var(--up-bg); border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); height: 30px; padding: 0 8px; }
  .list-actions { display: flex; padding-top: 6px; }
  .modal-form { display: flex; flex-direction: column; gap: 12px; }
  .modal-form select { height: 34px; }
  .modal-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .row-actions { display: flex; gap: 10px; align-items: center; }
  .team { display: flex; flex-direction: column; gap: 4px; padding: 10px 0; border-top: 1px solid var(--up-border-hairline); }
  .row.sub { border-top: none; padding: 4px 0 4px 16px; }
  .ctl.narrow { width: 180px; flex-shrink: 0; }
  .minted { display: flex; flex-direction: column; gap: 8px; padding: 12px 0 6px; }
  .code { background: var(--up-surface-dark); border-radius: var(--up-radius-tooltip); padding: 12px 14px; display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .snippet { font: var(--up-type-code); color: var(--up-text-on-dark); word-break: break-all; }
  .copy { background: none; border: none; padding: 2px 0; cursor: pointer; font: var(--up-type-small); color: var(--up-operational-strong); flex-shrink: 0; }
  .copy:hover { color: var(--up-text-on-dark); }
  .config { font: var(--up-type-code); color: var(--up-text-secondary); background: var(--up-bg-hover); border-radius: var(--up-radius-control); padding: 10px 12px; margin: 0; overflow-x: auto; }
  .btn { font: var(--up-type-ui); color: var(--up-ink); background: var(--up-bg); box-shadow: inset 0 0 0 1px var(--up-border-control); border-radius: var(--up-radius-control); height: 34px; padding: 0 16px; display: inline-flex; align-items: center; }
  .btn:hover { background: var(--up-bg-hover); color: var(--up-ink); }
  .bad { font: var(--up-type-meta); color: var(--up-degraded-strong); }
  .version { font: var(--up-type-meta); color: var(--up-text-faint); text-align: center; padding: 12px 0 4px; }
  .version a { color: inherit; text-decoration: none; }
  .version a:hover { color: var(--up-ink); text-decoration: underline; }
  .version .ext { font-size: 0.85em; line-height: 1; }
  @media (max-width: 600px) {
    .setting { flex-direction: column; align-items: flex-start; gap: 10px; }
    .ctl { width: 100%; }
  }
</style>
