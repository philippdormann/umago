<script lang="ts">
  // Dismissible error banner with a Retry action for the Sites index.
  let {
    message,
    onretry,
    ondismiss,
  }: {
    message: string
    onretry: () => void
    ondismiss: () => void
  } = $props()
</script>

<div class="banner" role="alert">
  <span class="msg">{message}</span>
  <span class="actions">
    <button type="button" class="retry" onclick={onretry}>Retry</button>
    <button type="button" class="dismiss" aria-label="Dismiss error" title="Dismiss" onclick={ondismiss}>×</button>
  </span>
</div>

<style>
  .banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 12px;
    padding: 10px 8px 10px 14px;
    border: 1px solid var(--up-border-control);
    border-left: 3px solid var(--up-degraded-strong);
    border-radius: var(--up-radius-control);
    background: var(--up-bg);
  }
  .msg { font: var(--up-type-meta); color: var(--up-degraded-strong); min-width: 0; }
  .actions { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .retry {
    background: none;
    border: 1px solid var(--up-border-control);
    border-radius: var(--up-radius-control);
    padding: 4px 12px;
    cursor: pointer;
    font: var(--up-type-ui);
    color: var(--up-ink);
  }
  .retry:hover { border-color: var(--up-accent); color: var(--up-accent-hover); }
  .dismiss {
    background: none;
    border: none;
    padding: 2px 8px;
    cursor: pointer;
    font-size: 16px;
    font-weight: 500;
    line-height: 1;
    color: var(--up-text-faint);
  }
  .dismiss:hover { color: var(--up-ink); }
</style>
