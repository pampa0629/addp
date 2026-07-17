<template>
  <div class="workflow-dag-presentation">
    <DAGViewer :dag-data="properties.workflow" :height="properties.height || 400" />
  </div>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import { DAGViewer } from '@addp/common-frontend/dag'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const properties = ref({ ...props.context.componentModel.properties })
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
.workflow-dag-presentation {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
}
</style>
