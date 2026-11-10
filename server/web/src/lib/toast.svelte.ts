// Toasts: fire-and-forget error/info banners.
// Usage: `pushToast('Could not load data.')`
// A single <ToastHost /> in App.svelte renders them.
export interface Toast {
  id: number
  message: string
}

export const toastStore = $state<{ items: Toast[] }>({ items: [] })

let next = 1

export function pushToast(message: string, timeout = 6000) {
  const id = next++
  toastStore.items = [...toastStore.items, { id, message }]
  if (timeout > 0) {
    setTimeout(() => dismissToast(id), timeout)
  }
  return id
}

export function dismissToast(id: number) {
  toastStore.items = toastStore.items.filter((t) => t.id !== id)
}
