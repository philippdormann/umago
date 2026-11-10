<script lang="ts">
  import { Switch as ShadSwitch } from '$lib/components/ui/switch/index.js'

  let { checked = $bindable(false), disabled = false, onchange, label }: { checked?: boolean; disabled?: boolean; onchange?: (v: boolean) => void; label?: string } = $props()
</script>

<ShadSwitch bind:checked {disabled} aria-label={label} onCheckedChange={(v) => onchange?.(v)} />
