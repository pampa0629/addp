<template>
  <span
    class="addp-visually-hidden"
    role="status"
    aria-live="polite"
    aria-atomic="true"
    :aria-label="label || undefined"
  >{{ message }}</span>
</template>

<script setup>
defineProps({
  message: {
    type: String,
    default: ''
  },
  label: {
    type: String,
    default: ''
  }
})
</script>
