<script lang="ts">
  // Centered dialog over a dimmed backdrop. Closes on Escape, on the
  // backdrop, or via the × button.
  import type { Snippet } from 'svelte'
  import * as Dialog from '$lib/components/ui/dialog/index.js'

  let { title, subtitle = '', onclose, children }: { title: string; subtitle?: string; onclose: () => void; children: Snippet } = $props()
</script>

<Dialog.Root open onOpenChange={(o) => !o && onclose()}>
  <Dialog.Content class="max-w-[560px] gap-0 p-0 sm:max-w-[560px]">
    <Dialog.Header class="flex-row items-start justify-between gap-4 p-5 pb-3 text-left">
      <div>
        <Dialog.Title class="text-sm font-bold">{title}</Dialog.Title>
        {#if subtitle}<Dialog.Description class="mt-0.5 text-[13px]">{subtitle}</Dialog.Description>{/if}
      </div>
    </Dialog.Header>
    <div class="max-h-[min(80vh,720px)] overflow-auto px-5 pb-5">{@render children()}</div>
  </Dialog.Content>
</Dialog.Root>
