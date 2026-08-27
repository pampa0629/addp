<template>
  <div class="iframe-wrapper">
    <div class="iframe-container">
      <iframe
        v-if="iframeUrl"
        :src="iframeUrl"
        :key="iframeKey || iframeUrl"
        data-testid="module-iframe"
        frameborder="0"
        allow="clipboard-write; clipboard-read"
        @load="$emit('load')"
        class="module-iframe"
      ></iframe>
      <div v-else class="loading-container">
        <el-icon class="is-loading" :size="32"><Loading /></el-icon>
        <p>等待选择模块...</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { Loading } from '@element-plus/icons-vue'

defineProps({
  iframeUrl: { type: String, default: '' },
  iframeKey: { type: [String, Number], default: '' },
})

defineEmits(['load'])
</script>

<style scoped>
.iframe-wrapper {
  flex: 1;
  display: flex;
  min-height: 0;
  background: var(--addp-bg-secondary);
}

.iframe-container {
  width: 100%;
  flex: 1;
  min-height: 0;
  background: var(--addp-bg-secondary);
}

.module-iframe {
  width: 100%;
  height: 100%;
  border: none;
  background: var(--addp-bg-secondary);
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--addp-text-tertiary);
}
</style>
