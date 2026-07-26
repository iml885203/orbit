<script lang="ts">
  let { state }: { state: string } = $props()
</script>

<span class="badge badge-{state}">{state}</span>
