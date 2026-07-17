<template>
  <section class="agent-resource-picker">
    <p class="picker-prompt">{{ properties.prompt }}</p>
    <div class="resource-options">
      <button
        v-for="option in properties.options"
        :key="option.value"
        type="button"
        class="resource-option"
        :disabled="submitting"
        @click="submit(option)"
      >
        <span class="resource-label">{{ option.label }}</span>
        <span class="resource-locator">{{ option.value }}</span>
      </button>
    </div>
  </section>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const properties = ref({ ...props.context.componentModel.properties })
const submitting = ref(false)
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})

async function submit(option) {
  if (submitting.value) return
  submitting.value = true
  try {
    await props.context.dispatchAction({
      event: {
        name: 'interaction.submit',
        context: {
          interactionId: properties.value.interactionId,
          answer: option
        }
      }
    })
  } finally {
    submitting.value = false
  }
}

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
.agent-resource-picker {
  box-sizing: border-box;
  width: 100%;
  padding: 14px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
}

.picker-prompt {
  margin: 0 0 10px;
  color: var(--addp-text-primary);
}

.resource-options {
  display: grid;
  gap: 8px;
}

.resource-option {
  width: 100%;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  text-align: left;
  cursor: pointer;
}

.resource-option:hover:not(:disabled),
.resource-option:focus-visible:not(:disabled) {
  border-color: var(--el-color-primary);
}

.resource-option:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.resource-label,
.resource-locator {
  display: block;
  overflow-wrap: anywhere;
}

.resource-label {
  font-weight: 600;
}

.resource-locator {
  margin-top: 4px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}
</style>
