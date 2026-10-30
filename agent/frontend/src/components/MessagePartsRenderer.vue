<template>
  <div class="message-parts" @click="copyCode">
    <template v-for="(part, index) in displayParts" :key="partKey(part, index)">
      <div
        v-if="part.type === 'text' && part.text"
        class="message-content markdown"
        v-html="renderMarkdown(part.text)"
      />
      <AgentPlan v-else-if="part.type === 'presentation_ref' && part.protocol === 'ag-ui' && part.activity_type === 'addp-plan'" :plan="part.content?.todos || []" :compact="!expandDetails" />
      <A2UISurface
        v-else-if="isVisiblePresentation(part)"
        :operations="part.content?.operations || []"
        :interaction-disabled="interactionDisabled"
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
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { A2UISurface } from '@addp/common-frontend/agent-ui'

import ExecutionResultRef from './ExecutionResultRef.vue'
import AgentPlan from './AgentPlan.vue'

const props = defineProps({
  message: { type: Object, required: true },
  expandDetails: Boolean,
  interactionDisabled: Boolean
})

const { t } = useI18n()
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

function escapeHtml(value) {
  return String(value).replace(/[&<>"]/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[character])
}
function renderMarkdown(content) {
  const renderer = new marked.Renderer()
  renderer.code = ({ text }) => `<div class="agent-code-block"><button type="button" data-agent-copy-code>${escapeHtml(t('agent.workspace.copyCode'))}</button><pre><code>${escapeHtml(text)}</code></pre></div>`
  return DOMPurify.sanitize(marked.parse(content || '', { renderer }))
}
async function copyCode(event) {
  const button = event.target.closest?.('button[data-agent-copy-code]')
  if (!button || !event.currentTarget.contains(button)) return
  const code = button.closest('.agent-code-block')?.querySelector('pre code')
  if (!code) return
  try { await navigator.clipboard.writeText(code.textContent); ElMessage.success(t('agent.workspace.copied')) }
  catch { ElMessage.error(t('agent.workspace.copyFailed')) }
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
.markdown :deep(.agent-code-block) { border: 1px solid var(--addp-border-color); border-radius: 6px; padding: 8px; }
.markdown :deep(.agent-code-block button) { color: var(--el-color-primary); background: transparent; border: 0; padding: 4px 0; cursor: pointer; font: inherit; font-size: 12px; }
</style>
