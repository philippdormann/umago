<!--
  ReorderButtons.svelte — keyboard-accessible move up/down pair for list items.

  No page wiring: pages own the list state and just handle the events.

  Usage (ReorderButtons in a list row at index i):
    disabledUp={i === 0} disabledDown={i === items.length - 1}
    onmoveup={() => move(i, -1)} onmovedown={() => move(i, 1)}
    custom labels: upLabel="Move up" downLabel="Move down"

  Props:
    disabledUp?: boolean, disabledDown?: boolean
    upLabel?: string (default "Move up"), downLabel?: string (default "Move down")
    onmoveup: () => void, onmovedown: () => void
-->
<script lang="ts">
  let {
    disabledUp = false,
    disabledDown = false,
    upLabel = 'Move up',
    downLabel = 'Move down',
    onmoveup,
    onmovedown,
  }: {
    disabledUp?: boolean
    disabledDown?: boolean
    upLabel?: string
    downLabel?: string
    onmoveup: () => void
    onmovedown: () => void
  } = $props()
</script>

<div class="reorder" role="group" aria-label="Reorder item">
  <button type="button" aria-label={upLabel} title={upLabel} disabled={disabledUp} onclick={onmoveup}>↑</button>
  <button type="button" aria-label={downLabel} title={downLabel} disabled={disabledDown} onclick={onmovedown}>↓</button>
</div>

<style>
  .reorder { display: inline-flex; gap: 4px; }
  button {
    font-size: 13px;
    line-height: 1;
    color: var(--muted-foreground);
    background: none;
    border: 1px solid var(--border);
    border-radius: calc(var(--radius) - 2px);
    padding: 4px 8px;
    cursor: pointer;
  }
  button:hover:not(:disabled) { color: var(--foreground); background: var(--accent); }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  button:focus-visible { outline: 2px solid var(--ring); outline-offset: 1px; }
</style>
