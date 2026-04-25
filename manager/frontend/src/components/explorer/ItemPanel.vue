<template>
  <div class="item-panel">
    <el-card v-if="itemMeta" shadow="never" class="meta-card">
      <template #header>
        <div class="meta-header" @click="metaExpanded = !metaExpanded">
          <span>{{ t('meta.additionalAttributes') }}</span>
          <el-button text>{{ metaExpanded ? '收起' : '展开' }}</el-button>
        </div>
      </template>
      <el-descriptions v-show="metaExpanded" :column="1" border size="small">
        <el-descriptions-item :label="t('meta.itemType')">{{ itemTypeLabel }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.fullName')">{{ itemMeta.full_name || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.scannedAt')">{{ itemMeta.scanned_at || '-' }}</el-descriptions-item>
        <el-descriptions-item v-for="attr in itemMeta.attributes || []" :key="attr.key" :label="attr.key">
          {{ formatValue(attr.value) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <div class="preview-wrapper">
      <PreviewPanel
        :selected-node="selectedNode"
        :preview-data="previewData"
        :loading="loading"
        @page-change="$emit('page-change', $event)"
        @navigate="$emit('navigate', $event)"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'

const { t } = useI18n()

const props = defineProps({
  selectedNode: {
    type: Object,
    default: null
  },
  previewData: {
    type: Object,
    default: null
  },
  loading: {
    type: Boolean,
    default: false
  }
})

defineEmits(['page-change', 'navigate'])

const metaExpanded = ref(true)

watch(() => props.selectedNode?.locator, () => {
  metaExpanded.value = true
})

const itemMeta = computed(() => props.previewData?.item_meta)

const itemTypeLabel = computed(() => {
  const key = itemMeta.value?.item_type_i18n_key || `engine.term.${itemMeta.value?.item_type || ''}`
  const translated = t(key)
  return translated === key ? (itemMeta.value?.item_type || '-') : translated
})

const formatValue = (v) => {
  if (v === null || v === undefined) return '-'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
</script>

<style scoped>
.item-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.meta-card {
  border: none;
}

.meta-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
}

.preview-wrapper {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
</style>
