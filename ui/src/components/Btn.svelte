<script lang="ts">
  let { label, danger = false, disabled = false, onclick }: {
    label: string
    danger?: boolean
    disabled?: boolean
    onclick?: (e: MouseEvent) => void
  } = $props()
</script>

<button
  class:danger
  {disabled}
  onclick={onclick}
>{label}</button>
