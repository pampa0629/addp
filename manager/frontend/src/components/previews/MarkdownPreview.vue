<template>
  <div class="markdown-preview-container">
    <div class="markdown-body" v-html="sanitizedHtml"></div>
    <div v-if="truncated" class="truncate-tip">内容较大，仅展示部分</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

marked.setOptions({
  breaks: true,
  gfm: true,
  mangle: false,
  headerIds: false
})

const rawText = computed(() => {
  const content = props.data?.object?.content || {}
  if ((content.kind || '').toLowerCase() === 'unsupported') {
    return ''
  }
  return content.text || ''
})

const truncated = computed(() => {
  return props.data?.object?.content?.truncated || props.data?.object?.truncated || false
})

const sanitizedHtml = computed(() => {
  const source = rawText.value
  if (!source) {
    return '<p class="markdown-empty">暂无可用内容</p>'
  }
  const html = marked.parse(source || '')
  return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
})
</script>

<style scoped>
.markdown-preview-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.markdown-body {
  padding: 18px 20px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  color: var(--el-text-color-primary);
  line-height: 1.7;
  font-size: 14px;
  overflow: auto;
  max-height: 540px;
}

.markdown-body :deep(pre) {
  background: rgba(0, 0, 0, 0.05);
  padding: 12px;
  border-radius: 6px;
  overflow: auto;
}

.markdown-body :deep(code) {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3),
.markdown-body :deep(h4),
.markdown-body :deep(h5),
.markdown-body :deep(h6) {
  margin-top: 1.2em;
  margin-bottom: 0.6em;
  font-weight: 600;
}

.markdown-body :deep(p) {
  margin: 0.6em 0;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-left: 24px;
  margin: 0.6em 0;
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--el-border-color-lighter);
  padding: 8px;
  text-align: left;
}

.markdown-body :deep(.markdown-empty) {
  color: var(--el-text-color-secondary);
  text-align: center;
  margin: 0;
}

.truncate-tip {
  font-size: 12px;
  color: var(--el-color-primary);
  text-align: center;
}
</style>
