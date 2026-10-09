<template>
  <section class="clarification-choice">
    <template v-for="(part, index) in promptParts" :key="index">
      <details v-if="part.type === 'code'" class="clarification-details">
        <summary>{{ t('agentUi.clarification.details') }}</summary>
        <pre>{{ part.text }}</pre>
      </details>
      <p v-else class="clarification-prompt">{{ part.text }}</p>
    </template>
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
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const properties = ref({ ...props.context.componentModel.properties })
const { t } = useI18n()
const promptParts = computed(() => {
  const prompt = properties.value.prompt || ''
  const parts = []
  const fences = /^```[^\n\r]*\r?\n([\s\S]*?)\r?\n```[ \t]*(?:\r?\n|$)/gm
  let offset = 0
  for (const match of prompt.matchAll(fences)) {
    const text = prompt.slice(offset, match.index).trim()
    if (text) parts.push({ type: 'text', text })
    parts.push({ type: 'code', text: match[1] })
    offset = match.index + match[0].length
  }
  const text = prompt.slice(offset).trim()
  if (text) parts.push({ type: 'text', text })
  return parts
})
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
  box-sizing: border-box;
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
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.clarification-details {
  min-width: 0;
  margin-top: 12px;
  color: var(--addp-text-secondary);
}

.clarification-details summary {
  cursor: pointer;
}

.clarification-details pre {
  max-height: 320px;
  overflow: auto;
  padding: 12px;
  border-radius: 6px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
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
