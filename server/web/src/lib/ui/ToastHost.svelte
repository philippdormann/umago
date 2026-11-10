<script lang="ts">
  // Single global slot for error/info banners. Mounted once in App.svelte
  // next to ConfirmHost; producers call pushToast() from anywhere.
  import { dismissToast, toastStore } from '$lib/toast.svelte'

  const items = $derived(toastStore.items)
</script>

{#if items.length > 0}
  <div class="toasts" role="status" aria-live="polite">
    {#each items as t (t.id)}
      <div class="toast">
        <span class="msg">{t.message}</span>
        <button type="button" class="close" aria-label="Dismiss" onclick={() => dismissToast(t.id)}>×</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toasts {
    position: fixed;
    left: 50%;
    bottom: 24px;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 100;
    max-width: min(480px, calc(100vw - 32px));
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--up-surface-dark);
    color: var(--up-text-on-dark);
    border: 1px solid var(--up-divider-on-dark);
    border-radius: var(--up-radius-tooltip);
    box-shadow: var(--up-shadow-tooltip);
    padding: 10px 8px 10px 14px;
    font: var(--up-type-meta);
  }
  .msg { flex: 1; }
  .close {
    background: none;
    border: none;
    padding: 2px 8px;
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    color: var(--up-text-on-dark-muted);
  }
  .close:hover { color: var(--up-text-on-dark); }
</style>
