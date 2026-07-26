<script lang="ts">
  import { store } from '$lib/stores.svelte'
</script>

<div class="toast" class:show={store.ui.toastVisible}>{store.ui.toastMessage}</div>
