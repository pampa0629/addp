<template>
  <div class="image-preview">
    <div v-if="imageSrc" class="image-wrapper">
      <img :src="imageSrc" :alt="fileName" />
    </div>
    <div v-else class="placeholder">
      图片超出预览限制，无法展示
    </div>
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

const imageData = computed(() => {
  return props.data?.object?.content?.image_data || props.data?.object?.content?.imageData || null
})

const contentType = computed(() => {
  return props.data?.object?.content_type || props.data?.object?.contentType || 'image/png'
})

const fileName = computed(() => {
  return props.data?.object?.path || props.data?.object?.name || 'image'
})

const imageSrc = computed(() => {
  if (!imageData.value) return ''
  return `data:${contentType.value};base64,${imageData.value}`
})
</script>

<style scoped>
.image-preview {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  padding: 20px;
  background: var(--el-fill-color);
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
}

.image-wrapper {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
}

.image-wrapper img {
  max-width: 100%;
  max-height: 360px;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.placeholder {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  text-align: center;
  padding: 24px;
}
</style>
