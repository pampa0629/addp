<template>
  <section class="clarification-choice">
    <p class="clarification-prompt">{{ properties.prompt }}</p>
    <div v-if="properties.options?.length" class="clarification-options">
      <el-button
        v-for="option in properties.options"
        :key="String(option.value)"
        :disabled="submitting"
        plain
        @click="submit(option)"
      >
        {{ option.label }}
      </el-button>
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
.clarification-choice {
  width: 100%;
  padding: 14px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
}

.clarification-prompt {
  margin: 0;
  color: var(--addp-text-primary);
  line-height: 1.6;
}

.clarification-options {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.clarification-options :deep(.el-button) {
  margin-left: 0;
}
</style>
