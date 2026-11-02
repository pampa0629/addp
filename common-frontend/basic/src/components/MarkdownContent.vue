<template>
  <div ref="body" class="markdown-content" v-html="html" @click="handleCopy" />
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

const props = defineProps({
  text: { type: String, default: '' },
  copyCode: Boolean,
  breaks: Boolean
})
const { t } = useI18n()
const body = ref(null)
const html = computed(() => DOMPurify.sanitize(
  marked.parse(props.text, { gfm: true, breaks: props.breaks }),
  { USE_PROFILES: { html: true }, ALLOW_DATA_ATTR: false, ADD_ATTR: ['data-mermaid-id'] }
))
const copyLabel = computed(() => t('common.markdown.copyCode'))
let controls = new WeakMap()
let decorations = []

function decorateCode() {
  const root = body.value
  if (!root) return
  for (const { wrapper, pre } of decorations) {
    if (root.contains(wrapper)) wrapper.replaceWith(pre)
  }
  decorations = []
  controls = new WeakMap()
  if (!props.copyCode) return
  for (const code of root.querySelectorAll('pre > code')) {
    const pre = code.parentElement
    const wrapper = document.createElement('div')
    wrapper.className = 'markdown-code-block'
    const button = document.createElement('button')
    button.type = 'button'
    button.textContent = copyLabel.value
    pre.replaceWith(wrapper)
    wrapper.append(button, pre)
    controls.set(button, code)
    decorations.push({ wrapper, pre })
  }
}

async function handleCopy(event) {
  const button = event.target.closest?.('button')
  const code = button && controls.get(button)
  if (!code || !body.value?.contains(button)) return
  try {
    // Marked's standard code renderer appends a display newline.
    await navigator.clipboard.writeText(code.textContent.replace(/\n$/, ''))
    ElMessage.success(t('common.markdown.copied'))
  } catch {
    ElMessage.error(t('common.markdown.copyFailed'))
  }
}

onMounted(decorateCode)
watch([html, copyLabel, () => props.copyCode], decorateCode, { flush: 'post' })
</script>

<style scoped>
.markdown-content { min-width: 0; overflow-wrap: anywhere; }
.markdown-content :deep(pre) { max-width: 100%; overflow-x: auto; }
.markdown-content :deep(.markdown-code-block) { border: 1px solid var(--addp-border-color); border-radius: 6px; padding: 8px; }
.markdown-content :deep(.markdown-code-block button) { color: var(--el-color-primary); background: transparent; border: 0; padding: 4px 0; cursor: pointer; font: inherit; font-size: 12px; }
</style>
