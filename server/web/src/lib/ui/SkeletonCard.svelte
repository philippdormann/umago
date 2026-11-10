<!--
  SkeletonCard.svelte — loading placeholder card built on shadcn Skeleton.

  Usage:
    loading placeholder: if loading render SkeletonCard, else render content
    lines={4} showTitle={false}

  Props:
    lines?: number (default 3) — body placeholder rows
    showTitle?: boolean (default true)
    class?: string — extra classes on the wrapper
-->
<script lang="ts">
  import { Skeleton } from '$lib/components/ui/skeleton/index.js'

  let { lines = 3, showTitle = true, class: className = '' }: { lines?: number; showTitle?: boolean; class?: string } = $props()
  const rows = $derived(Math.max(0, Math.floor(lines)))
</script>

<div class="sk-card {className}" aria-hidden="true">
  {#if showTitle}<Skeleton class="h-4 w-2/5" />{/if}
  {#each Array(rows) as _, i (i)}
    <Skeleton class={i === rows - 1 ? 'h-3 w-3/5' : 'h-3 w-full'} />
  {/each}
</div>

<style>
  .sk-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--card);
    padding: 16px;
  }
</style>
