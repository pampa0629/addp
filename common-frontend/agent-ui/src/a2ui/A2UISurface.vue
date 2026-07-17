<template>
  <div class="addp-a2ui-surfaces">
    <div
      v-for="entry in surfaces"
      :key="entry.id"
      class="addp-a2ui-surface"
      :data-surface-id="entry.id"
    >
      <A2UIRoot :surface="entry.surface" />
    </div>
  </div>
</template>

<script setup>
import { onBeforeUnmount, shallowRef, watch } from 'vue'
import { MessageProcessor } from '@a2ui/web_core/v0_9'

import A2UIRoot from './A2UIRoot.js'
import { createAddpCatalog, validateCatalogComponent } from './catalog.js'

const props = defineProps({
  operations: {
    type: Array,
    required: true
  }
})

const emit = defineEmits(['action', 'error'])
const surfaces = shallowRef([])
let processor = null

function disposeProcessor() {
  if (!processor) return
  for (const entry of surfaces.value) entry.surface.dispose()
  processor = null
  surfaces.value = []
}

function renderOperations(operations) {
  disposeProcessor()
  if (!Array.isArray(operations) || operations.length === 0) return

  processor = new MessageProcessor([createAddpCatalog()], action => emit('action', action))
  try {
    processor.processMessages(operations)
    const surfaceIds = operations
      .map(operation => operation?.createSurface?.surfaceId)
      .filter(Boolean)
    const nextSurfaces = [...new Set(surfaceIds)]
      .map(id => ({ id, surface: processor.model.getSurface(id) }))
      .filter(entry => entry.surface)
    for (const entry of nextSurfaces) {
      for (const [, component] of entry.surface.componentsModel.entries) {
        validateCatalogComponent(entry.surface.catalog, component)
      }
    }
    surfaces.value = nextSurfaces
  } catch (error) {
    emit('error', error)
  }
}

watch(() => props.operations, renderOperations, { deep: true, immediate: true })
onBeforeUnmount(disposeProcessor)
</script>

<style scoped>
.addp-a2ui-surfaces,
.addp-a2ui-surface {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
}
</style>
