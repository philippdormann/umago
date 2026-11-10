<!--
  ErrorBanner.svelte — dismissible error banner (destructive tint, shadcn-style).

  Usage (ErrorBanner with retry + dismiss):
    message={err} onretry={() => load()} ondismiss={() => (err = '')}
    message="Failed to load" retryLabel="Try again" onretry={load}
    without retry: message={err} ondismiss={clear}

  Props:
    message: string (empty hides the banner)
    title?: string (default "Something went wrong")
    retryLabel?: string — shown only when `onretry` is provided
    onretry?: () => void — Retry action
    ondismiss?: () => void — Dismiss (x) action; hidden without handler
-->
<script lang="ts">
  let {
    message,
    title = 'Something went wrong',
    retryLabel = 'Retry',
    onretry,
    ondismiss,
  }: {
    message: string
    title?: string
    retryLabel?: string
    onretry?: () => void
    ondismiss?: () => void
  } = $props()
</script>

{#if message}
  <div class="banner" role="alert">
    <div class="body">
      <p class="title">{title}</p>
      <p class="msg">{message}</p>
    </div>
    <div class="actions">
      {#if onretry}
        <button type="button" class="retry" onclick={onretry}>{retryLabel}</button>
      {/if}
      {#if ondismiss}
        <button type="button" class="dismiss" onclick={ondismiss} aria-label="Dismiss error">×</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    border: 1px solid color-mix(in srgb, var(--destructive) 35%, transparent);
    background: color-mix(in srgb, var(--destructive) 8%, var(--card));
    color: var(--card-foreground);
    border-radius: var(--radius);
    padding: 10px 12px;
  }
  .body { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .title { font-size: 13px; font-weight: 700; margin: 0; }
  .msg { font-size: 13px; color: var(--muted-foreground); margin: 0; overflow-wrap: anywhere; }
  .actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
  .retry {
    font-size: 13px;
    font-weight: 600;
    color: var(--card-foreground);
    background: none;
    border: 1px solid var(--border);
    border-radius: calc(var(--radius) - 2px);
    padding: 4px 10px;
    cursor: pointer;
  }
  .retry:hover { background: var(--accent); }
  .dismiss {
    font-size: 16px;
    line-height: 1;
    color: var(--muted-foreground);
    background: none;
    border: none;
    padding: 2px 6px;
    cursor: pointer;
    border-radius: calc(var(--radius) - 2px);
  }
  .dismiss:hover { color: var(--foreground); background: var(--accent); }
</style>
