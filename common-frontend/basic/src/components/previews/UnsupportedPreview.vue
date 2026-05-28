<template>
  <div class="unsupported-preview">
    <el-empty :description="title">
      <template #description>
        <p class="unsupported-title">{{ title }}</p>
        <p class="unsupported-description">{{ description }}</p>
      </template>
    </el-empty>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const { t } = useI18n()

const title = computed(() => t('unsupportedPreview.title'))

const description = computed(() => {
  const content = props.data?.object?.content || {}
  return content.metadata?.unsupported_reason || content.metadata?.reason || t('unsupportedPreview.description')
})
</script>

<style scoped>
.unsupported-preview {
  min-height: 220px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.unsupported-title {
  margin: 0;
  color: var(--el-text-color-primary);
  font-size: 14px;
}

.unsupported-description {
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.5;
}
</style>
