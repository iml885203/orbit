import { toast } from './stores.svelte'

// Best-effort clipboard write. The Clipboard API can fail in non-secure
// contexts or when the user denies permission; we surface that as a toast
// rather than throwing so callers don't have to wrap every call.
export async function copyToClipboard(text: string, success = 'Copied'): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    toast(success)
  } catch {
    toast("Couldn't copy — check browser permissions")
  }
}
