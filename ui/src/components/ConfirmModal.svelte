<script lang="ts">
  import { X } from '@lucide/svelte'
  import { fly, fade } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { MediaQuery } from 'svelte/reactivity'

  let {
    open,
    title,
    message,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    danger = false,
    onConfirm,
    onCancel,
  }: {
    open: boolean
    title: string
    message: string
    confirmLabel?: string
    cancelLabel?: string
    danger?: boolean
    onConfirm: () => void
    onCancel: () => void
  } = $props()

  const reducedMotionQuery = new MediaQuery('prefers-reduced-motion: reduce')
  const reducedMotion = $derived(reducedMotionQuery.current)
  let modal = $state<HTMLDivElement>()
  let returnFocus: HTMLElement | null = null

  $effect(() => {
    if (!open) return
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    queueMicrotask(() => modal?.querySelector<HTMLElement>('button')?.focus())
    return () => returnFocus?.focus()
  })

  function handleKey(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') onCancel()
    if (e.key === 'Enter' && !danger) onConfirm()
    if (e.key === 'Tab') {
      if (!modal) return
      const controls = [...modal.querySelectorAll<HTMLElement>('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')]
      if (controls.length === 0) return
      const first = controls[0]
      const last = controls[controls.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
    }
  }

  type Segment = { code: boolean; text: string }
  const messageParts = $derived<Segment[]>(
    message.split('`').map((text, i) => ({ code: i % 2 === 1, text }))
  )
</script>

<svelte:window onkeydown={handleKey} />

{#if open}
  <div
    class="backdrop"
    role="presentation"
    onclick={onCancel}
    transition:fade={{ duration: reducedMotion ? 0 : 120 }}
  ></div>
  <div
    bind:this={modal}
    class="modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="confirm-title"
    aria-describedby="confirm-msg"
    transition:fly={{ y: reducedMotion ? 0 : 12, duration: reducedMotion ? 0 : 180, easing: cubicOut, opacity: reducedMotion ? 1 : 0 }}
  >
    <header>
      <h2 id="confirm-title">{title}</h2>
      <button type="button" class="close" aria-label="Cancel" onclick={onCancel}><X size={18} /></button>
    </header>
    <p id="confirm-msg" class="message">{#each messageParts as part, i (i)}{#if part.code}<code>{part.text}</code>{:else}{part.text}{/if}{/each}</p>
    <footer>
      <button type="button" class="btn cancel" onclick={onCancel}>{cancelLabel}</button>
      <button type="button" class="btn primary" class:danger={danger} onclick={onConfirm}>{confirmLabel}</button>
    </footer>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed; inset: 0;
    background: rgba(13, 17, 23, 0.6);
    z-index: 200;
  }
  .modal {
    position: fixed;
    top: 30%;
    left: 50%;
    transform: translate(-50%, -30%);
    width: min(440px, calc(100vw - var(--space-5) * 2));
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.5);
    z-index: 201;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: var(--space-4);
    border-bottom: 1px solid var(--border);
  }
  h2 { margin: 0; font-size: var(--text-lg); font-weight: 600; }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 0;
    color: var(--dim);
    cursor: pointer;
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    line-height: 0;
    min-width: var(--hit-target);
    min-height: var(--hit-target);
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .close:hover { color: var(--fg); background: rgba(255,255,255,0.06); }
  .message code {
    font-family: monospace;
    font-size: 0.92em;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px var(--space-1);
    color: var(--blue);
  }
  .message {
    padding: var(--space-4);
    color: var(--fg);
    font-size: var(--text-base);
    line-height: 1.5;
    margin: 0;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4) var(--space-4);
  }
  .btn {
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-sm);
    font-size: var(--text-base);
    font-family: inherit;
    cursor: pointer;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--fg);
    transition: background var(--transition-fast), border-color var(--transition-fast);
  }
  .btn:hover { background: rgba(255,255,255,0.06); }
  .btn.primary {
    background: var(--blue);
    border-color: var(--blue);
    color: white;
  }
  .btn.primary:hover { background: color-mix(in srgb, var(--blue) 85%, white); }
  .btn.primary.danger {
    background: var(--red);
    border-color: var(--red);
  }
  .btn.primary.danger:hover { background: color-mix(in srgb, var(--red) 85%, white); }
</style>
