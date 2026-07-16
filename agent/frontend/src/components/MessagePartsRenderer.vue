<template>
  <div class="message-parts">
    <template v-for="(part, index) in displayParts" :key="partKey(part, index)">
      <div
        v-if="part.type === 'text' && part.text"
        class="message-content markdown"
        v-html="renderMarkdown(part.text)"
      />
      <A2UISurface
        v-else-if="isVisiblePresentation(part)"
        :operations="part.content?.operations || []"
        @action="emit('action', $event)"
        @error="emit('error', $event)"
      />
      <ExecutionResultRef
        v-else-if="part.type === 'result_ref' && part.kind === 'execution' && part.owner_module === 'develop'"
        :result-ref="part"
      />
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { A2UISurface } from '@addp/common-frontend/agent-ui'

import ExecutionResultRef from './ExecutionResultRef.vue'

const props = defineProps({
  message: { type: Object, required: true }
})

const emit = defineEmits(['action', 'error'])

const displayParts = computed(() => {
  if (Array.isArray(props.message.parts) && props.message.parts.length > 0) {
    return props.message.parts
  }
  return props.message.content
    ? [{ type: 'text', text: props.message.content }]
    : []
})

const interactionStatus = computed(() => {
  const statuses = new Map()
  for (const part of displayParts.value) {
    if (part.type === 'interaction_ref') {
      statuses.set(part.interaction_id, part.status)
    }
  }
  return statuses
})

function isVisiblePresentation(part) {
  if (part.type !== 'presentation_ref' || part.protocol !== 'a2ui') return false
  if (!part.interaction_id) return true
  return interactionStatus.value.get(part.interaction_id) === 'pending'
}

function renderMarkdown(content) {
  return DOMPurify.sanitize(marked.parse(content || ''))
}

function partKey(part, index) {
  return part.surface_id || part.interaction_id || `${part.type}:${index}`
}
</script>

<style scoped>
.message-parts {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

.message-content {
  min-width: 0;
  overflow-wrap: anywhere;
}

.markdown :deep(p) {
  margin: 0 0 8px;
}

.markdown :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown :deep(pre) {
  max-width: 100%;
  overflow-x: auto;
}
</style>
