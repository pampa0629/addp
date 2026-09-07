<template>
  <div ref="popupEl" class="ol-popup">
    <div class="ol-popup-closer" @click="emit('close')"></div>
    <div class="ol-popup-content" v-html="content"></div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

defineProps({
  content: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['close'])

const popupEl = ref(null)

defineExpose({ popupEl })
</script>

<style scoped>
/* Popup 样式 */
.ol-popup {
  position: absolute;
  background-color: white;
  color: #333;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  padding: 0;
  border-radius: 8px;
  border: 1px solid #ccc;
  bottom: 12px;
  left: -50px;
  min-width: 280px;
  max-width: 400px;
  max-height: 300px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.ol-popup:after,
.ol-popup:before {
  top: 100%;
  border: solid transparent;
  content: " ";
  height: 0;
  width: 0;
  position: absolute;
  pointer-events: none;
}

.ol-popup:after {
  border-top-color: white;
  border-width: 10px;
  left: 48px;
  margin-left: -10px;
}

.ol-popup:before {
  border-top-color: #ccc;
  border-width: 11px;
  left: 48px;
  margin-left: -11px;
}

.ol-popup-closer {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 20px;
  height: 20px;
  cursor: pointer;
  z-index: 1;
}

.ol-popup-closer:after {
  content: "✕";
  font-size: 16px;
  color: #666;
  display: block;
  text-align: center;
  line-height: 20px;
}

.ol-popup-closer:hover:after {
  color: #333;
}

.ol-popup-content {
  overflow-y: auto;
  max-height: 320px;
  padding: 0;
}

/* 分层卡片式样式 - 使用 :deep() 让样式穿透到 v-html 动态内容 */
.ol-popup-content :deep(.feature-card) {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
}

.ol-popup-content :deep(.feature-card-header) {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-size: 13px;
}

.ol-popup-content :deep(.feature-id) {
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
}

.ol-popup-content :deep(.id-icon) {
  font-size: 14px;
}

.ol-popup-content :deep(.feature-geom-type) {
  font-size: 11px;
  background: rgba(255, 255, 255, 0.2);
  padding: 2px 8px;
  border-radius: 10px;
}

.ol-popup-content :deep(.feature-primary-field) {
  padding: 16px 12px 12px;
  border-bottom: 2px solid #f0f0f0;
}

.ol-popup-content :deep(.primary-value) {
  font-size: 20px;
  font-weight: 700;
  color: #333;
  margin-bottom: 4px;
  line-height: 1.3;
}

.ol-popup-content :deep(.primary-label) {
  font-size: 11px;
  color: #999;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.ol-popup-content :deep(.feature-attributes) {
  padding: 12px;
}

.ol-popup-content :deep(.attribute-item) {
  padding: 6px 0;
  font-size: 13px;
  line-height: 1.5;
  border-bottom: 1px solid #f5f5f5;
}

.ol-popup-content :deep(.attribute-item:last-child) {
  border-bottom: none;
}

.ol-popup-content :deep(.attr-key) {
  font-weight: 600;
  color: #666;
  margin-right: 4px;
}

.ol-popup-content :deep(.attr-value) {
  color: #000;
  word-break: break-word;
  user-select: text;
  cursor: text;
}

.ol-popup-content :deep(.state-indicator) {
  display: inline-block;
  margin-left: 4px;
  padding: 1px 6px;
  border: 1px solid currentColor;
  border-radius: 999px;
  font-size: 11px;
  line-height: 16px;
}

.ol-popup-content :deep(.state--info) { color: var(--el-color-info); background: var(--el-color-info-light-9); }
.ol-popup-content :deep(.state--success) { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.ol-popup-content :deep(.state--warning) { color: var(--el-color-warning); background: var(--el-color-warning-light-9); }
.ol-popup-content :deep(.state--danger) { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }

.ol-popup-content :deep(.null-value) {
  color: #999;
  font-style: italic;
}
</style>
