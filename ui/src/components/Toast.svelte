<script lang="ts">
  import { store } from '$lib/stores.svelte'
</script>

<!-- The toast is the only feedback channel for every start/stop/restart,
     env toggle, publish and reset failure, so it has to reach screen readers
     too. The message is rendered only while visible: the container fades with
     opacity and never leaves the DOM, so a live region wrapping permanent text
     would announce stale results long after the toast faded. -->
<div class="toast" class:show={store.ui.toastVisible} role="status" aria-live="polite" aria-atomic="true">
  {#if store.ui.toastVisible}{store.ui.toastMessage}{/if}
</div>
