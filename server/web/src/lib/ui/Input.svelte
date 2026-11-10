<script lang="ts">
  import type { ComponentProps } from 'svelte'
  import { Input as ShadInput } from '$lib/components/ui/input/index.js'

  type Props = Omit<ComponentProps<typeof ShadInput>, 'class' | 'value' | 'files' | 'type'> & {
    value?: string
    type?: string
    mono?: boolean
  }
  let { value = $bindable(''), mono = false, ...rest }: Props = $props()
</script>

<ShadInput bind:value class={mono ? 'font-mono text-xs' : ''} {...rest} />
