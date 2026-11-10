<script lang="ts">
  // Single host for confirmAction(): renders the pending request as a
  // shadcn dialog with explicit Cancel / confirm buttons.
  import { confirmStore, resolveConfirm } from '$lib/confirm.svelte'
  import Modal from '$lib/ui/Modal.svelte'
  import Button from '$lib/ui/Button.svelte'

  const req = $derived(confirmStore.req)
</script>

{#if req}
  <Modal title={req.title} onclose={() => resolveConfirm(false)}>
    <p class="msg">{req.message}</p>
    <div class="actions">
      <Button variant="secondary" size="sm" onclick={() => resolveConfirm(false)}>Cancel</Button>
      <Button variant={req.danger ? 'danger' : 'primary'} size="sm" onclick={() => resolveConfirm(true)}>{req.confirmLabel}</Button>
    </div>
  </Modal>
{/if}

<style>
  .msg { font: var(--up-type-meta); color: var(--up-text-muted); margin: 0 0 16px; }
  .actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
