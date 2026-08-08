<script lang="ts">
  import { store } from '$lib/stores.svelte'
</script>

<!-- The toast is the only feedback channel for every start/stop/restart, env
     toggle, publish and reset failure, so it has to reach screen readers too.
     The message stays mounted while the container fades with opacity: a live
     region announces on content change, and toastMessage only ever changes
     inside toast(), which is also what makes the toast visible. Unmounting the
     text on hide instead would drop it at t=0 and leave the card fading out
     empty for the length of the transition. -->
<div class="toast" class:show={store.ui.toastVisible} role="status" aria-live="polite" aria-atomic="true">
  {store.ui.toastMessage}
</div>
