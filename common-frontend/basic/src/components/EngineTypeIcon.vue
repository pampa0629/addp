<template>
  <span class="engine-type-icon" :data-engine-type="icon.type" aria-hidden="true">
    <svg v-if="icon.path" viewBox="0 0 24 24" focusable="false">
      <path :d="icon.path" />
    </svg>
    <span v-else class="engine-type-icon__initials">{{ icon.initials }}</span>
  </span>
</template>

<script setup>
import { computed } from 'vue'
import { engineTypeIcon } from '../utils/engineTypeIcons.mjs'

const props = defineProps({ type: { type: String, required: true } })
const icon = computed(() => engineTypeIcon(props.type))
</script>

<style scoped>
.engine-type-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 18px;
  width: 18px;
  height: 18px;
  color: inherit;
}

.engine-type-icon svg {
  display: block;
  width: 100%;
  height: 100%;
  fill: currentColor;
}

.engine-type-icon__initials {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  width: 100%;
  height: 100%;
  border: 1px solid currentColor;
  border-radius: 3px;
  font-size: 8px;
  font-weight: 700;
  line-height: 1;
}
</style>
