<template>
  <div class="text-preview-container">
    <pre class="text-content">{{ textContent }}</pre>
    <div v-if="truncated" class="truncate-tip">内容较大，仅展示部分</div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const textContent = computed(() => {
  return props.data?.object?.content?.text || '暂无可用内容'
})

const truncated = computed(() => {
  return props.data?.object?.content?.truncated || props.data?.object?.truncated || false
})
</script>

<style scoped>
.text-preview-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.text-content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  padding: 12px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: auto;
  max-height: 500px;
}

.truncate-tip {
  font-size: 12px;
  color: var(--el-color-primary);
  text-align: center;
}
</style>
