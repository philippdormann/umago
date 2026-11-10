// Promise-based confirm dialog to replace window.confirm().
// Usage: `if (!(await confirmAction({ title, message }))) return`
// A single <ConfirmHost /> in App.svelte renders the shadcn dialog.
export interface ConfirmRequest {
  title: string
  message: string
  confirmLabel: string
  danger: boolean
  resolve: (v: boolean) => void
}

export const confirmStore = $state<{ req: ConfirmRequest | null }>({ req: null })

export function confirmAction(opts: { title: string; message: string; confirmLabel?: string; danger?: boolean }): Promise<boolean> {
  return new Promise((resolve) => {
    confirmStore.req = {
      title: opts.title,
      message: opts.message,
      confirmLabel: opts.confirmLabel ?? 'Confirm',
      danger: opts.danger ?? true,
      resolve,
    }
  })
}

export function resolveConfirm(v: boolean) {
  confirmStore.req?.resolve(v)
  confirmStore.req = null
}
