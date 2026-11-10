<script lang="ts">
  let { title = 'UP', crumb = '' }: { title?: string; crumb?: string } = $props()
</script>

<div class="logo">
  <!-- Black mark on light backgrounds, white mark when .dark is set (follows the OS). -->
  <img class="mark mark-light" src="/umago-transparent-black-font.png" alt="" width="28" height="28" />
  <img class="mark mark-dark" src="/umago-transparent-white-font.png" alt="" width="28" height="28" />
  <span class="wordmark">{title}</span>
  {#if crumb}<span class="crumb">/ {crumb}</span>{/if}
</div>

<style>
  .logo { display: flex; align-items: center; gap: 10px; color: var(--up-ink); }
  .mark { width: 28px; height: 28px; flex-shrink: 0; }
  .mark-dark { display: none; }
  :global(.dark) .mark-light { display: none; }
  :global(.dark) .mark-dark { display: block; }
  .wordmark { font: var(--up-type-wordmark); letter-spacing: 0.02em; }
  .crumb { font: var(--up-type-status-line); color: var(--up-text-faint); }
  @media (max-width: 600px) {
    .crumb { display: none; }
  }
</style>
