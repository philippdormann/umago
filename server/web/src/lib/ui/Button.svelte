<script lang="ts">
  import type { ComponentProps, Snippet } from 'svelte'
  import { Button as ShadButton } from '$lib/components/ui/button/index.js'

  type Props = Omit<ComponentProps<typeof ShadButton>, 'variant' | 'size' | 'children'> & {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'md' | 'sm'
    children: Snippet
  }
  let { variant = 'primary', size = 'md', children, ...rest }: Props = $props()

  const mapped = {
    primary: 'default',
    secondary: 'outline',
    ghost: 'ghost',
    danger: 'destructive',
  } as const
</script>

<ShadButton variant={mapped[variant]} size={size === 'sm' ? 'sm' : 'default'} class="text-[13px] font-semibold" {...rest}>{@render children()}</ShadButton>
