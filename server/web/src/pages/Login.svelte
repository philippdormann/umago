<script lang="ts">
  import { api } from '../lib/api'
  import Input from '../lib/ui/Input.svelte'
  import Button from '../lib/ui/Button.svelte'

  let { onsuccess }: { onsuccess: () => void } = $props()
  let username = $state('')
  let password = $state('')
  let showPassword = $state(false)
  let code = $state('')
  let challenge = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit() {
    busy = true
    error = ''
    try {
      const r = await api.login(username, password)
      if (r.requires_2fa && r.challenge) {
        challenge = r.challenge
        code = ''
      } else {
        onsuccess()
      }
    } catch (e: any) {
      error = e.status === 401 ? 'Wrong username or password.' : e.message
      password = ''
    } finally {
      busy = false
    }
  }

  async function verify() {
    busy = true
    error = ''
    try {
      await api.verify2FA(challenge, code.trim())
      onsuccess()
    } catch {
      error = 'Wrong code. Try again or use a backup code.'
      code = ''
    } finally {
      busy = false
    }
  }
</script>

<div class="login">
  <h2>Sign in</h2>
  {#if !challenge}
    <p class="secondary">Use your Umago account. The first account comes from the server's environment.</p>
    <form
      onsubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <Input bind:value={username} placeholder="Username" aria-label="Username" autocomplete="username" required />
      <div class="pw">
        <Input bind:value={password} placeholder="Password" aria-label="Password" type={showPassword ? 'text' : 'password'} autocomplete="current-password" required />
        <button type="button" class="toggle" aria-label={showPassword ? 'Hide password' : 'Show password'} aria-pressed={showPassword} onclick={() => (showPassword = !showPassword)}>
          {showPassword ? 'Hide' : 'Show'}
        </button>
      </div>
      {#if error}<p class="error">{error}</p>{/if}
      <Button type="submit" disabled={busy || !username || !password}>{busy ? 'Signing in' : 'Sign in'}</Button>
    </form>
  {:else}
    <p class="secondary">Two-factor check for {username}. Enter a code from your authenticator app or a backup code.</p>
    <form
      onsubmit={(e) => {
        e.preventDefault()
        verify()
      }}
    >
      <Input bind:value={code} placeholder="123456" aria-label="Two-factor code" autocomplete="one-time-code" inputmode="numeric" required mono />
      {#if error}<p class="error">{error}</p>{/if}
      <Button type="submit" disabled={busy || !code.trim()}>{busy ? 'Verifying' : 'Verify'}</Button>
      <button
        type="button"
        class="plain"
        onclick={() => {
          challenge = ''
          code = ''
          password = ''
        }}>← Back</button
      >
    </form>
  {/if}
</div>

<style>
  .login { max-width: 320px; width: 100%; margin: 8vh auto 0; display: flex; flex-direction: column; gap: 12px; text-align: center; }
  .pw { position: relative; display: flex; }
  .pw :global(input) { padding-right: 56px; width: 100%; }
  .toggle { position: absolute; right: 4px; top: 50%; transform: translateY(-50%); background: none; border: none; padding: 6px 8px; cursor: pointer; font: var(--up-type-small); color: var(--up-text-muted); }
  .toggle:hover { color: var(--up-ink); }
  h2 { font: var(--up-type-setting); }
  p { font: var(--up-type-meta); line-height: 1.5; }
  form { display: flex; flex-direction: column; gap: var(--up-space-3); margin-top: 8px; text-align: left; }
  .error { color: var(--up-degraded-strong); text-align: center; }
  .plain { background: none; border: none; padding: 0; cursor: pointer; font: var(--up-type-ui); color: var(--up-text-muted); align-self: center; }
  .plain:hover { color: var(--up-ink); }
</style>
