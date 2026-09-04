<template>
  <el-dialog
    v-model="visible"
    :title="title || t('common.export.title')"
    width="min(480px, calc(100vw - 24px))"
    :close-on-click-modal="!exporting"
    :close-on-press-escape="!exporting"
    :show-close="!exporting"
    @open="resetForm"
  >
    <el-form :model="form" label-width="96px">
      <el-form-item :label="t('common.export.format')" required>
        <el-select
          v-model="form.format"
          :placeholder="t('common.export.formatPlaceholder')"
          :disabled="exporting"
          class="export-field"
        >
          <el-option
            v-for="formatName in formats"
            :key="formatName"
            :label="formatDisplayName(formatName)"
            :value="formatName"
          />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('common.export.fileName')" required>
        <el-input
          v-model="form.fileName"
          :placeholder="t('common.export.fileNamePlaceholder')"
          :disabled="exporting"
          maxlength="180"
          clearable
        />
      </el-form-item>
      <el-alert
        :title="hint || t('common.export.hint')"
        type="info"
        :closable="false"
        show-icon
      />
    </el-form>
    <template #footer>
      <el-button :disabled="exporting" @click="visible = false">
        {{ t('common.cancel') }}
      </el-button>
      <el-button type="primary" :loading="exporting" :disabled="!canConfirm" @click="handleConfirm">
        {{ t('common.export.start') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  formats: { type: Array, default: () => [] },
  defaultFileName: { type: String, default: '' },
  exporting: { type: Boolean, default: false },
  title: { type: String, default: '' },
  hint: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'confirm'])
const visible = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value)
})
const form = reactive({ format: '', fileName: '' })
const formatDisplayNames = {
  csv: 'CSV',
  tsv: 'TSV',
  geojson: 'GeoJSON',
  json: 'JSON',
  jsonl: 'JSONL',
  mongodb_extended_jsonl: 'MongoDB Extended JSON Lines',
  parquet: 'Parquet',
  shapefile: 'Shapefile'
}
const formatDisplayName = formatName => {
  const normalized = String(formatName || '').trim().toLowerCase()
  if (!normalized) return ''
  return formatDisplayNames[normalized] || normalized
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
const resetForm = () => {
  form.format = props.formats[0] || ''
  form.fileName = props.defaultFileName || ''
}
watch(() => [props.defaultFileName, props.formats], () => {
  if (visible.value && !props.exporting) resetForm()
})
const canConfirm = computed(() => Boolean(
  String(form.format || '').trim() && String(form.fileName || '').trim() && !props.exporting
))
const handleConfirm = () => {
  const format = String(form.format || '').trim()
  const fileName = String(form.fileName || '').trim()
  if (!format) {
    ElMessage.warning(t('common.export.formatRequired'))
    return
  }
  if (!fileName) {
    ElMessage.warning(t('common.export.fileNameRequired'))
    return
  }
  emit('confirm', { format, fileName })
}
</script>

<style scoped>
.export-field {
  width: 100%;
}
</style>
