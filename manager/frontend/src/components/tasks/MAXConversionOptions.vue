<template>
  <div class="max-options">
    <Model3DSourceUnitSelect
      :model-value="modelValue.source_unit || ''"
      :size="size"
      class="unit-select"
      @update:model-value="updateUnit"
    />
    <el-button :size="size" @click="openTextures">{{ t('manager.maxTextures.button', { count: Object.keys(modelValue.texture_files || {}).length }) }}</el-button>
    <el-dialog v-model="visible" :title="t('manager.maxTextures.title')" width="min(760px, 90vw)" append-to-body>
      <p>{{ t('manager.maxTextures.description') }}</p>
      <div v-for="(row, index) in rows" :key="index" class="texture-row">
        <el-input v-model="row.reference" :placeholder="t('manager.maxTextures.reference')" :aria-label="t('manager.maxTextures.referenceLabel', { index: index + 1 })" />
        <el-input v-model="row.path" :placeholder="t('manager.maxTextures.path')" :aria-label="t('manager.maxTextures.pathLabel', { index: index + 1 })" />
        <el-button @click="rows.splice(index, 1)" :aria-label="t('manager.maxTextures.removeLabel', { index: index + 1 })">{{ t('common.delete') }}</el-button>
      </div>
      <el-button @click="rows.push({ reference: '', path: '' })">{{ t('manager.maxTextures.add') }}</el-button>
      <el-alert v-if="declarations.error" :title="t(`manager.maxTextures.${declarations.error}`)" type="error" :closable="false" class="validation-error" />
      <template #footer>
        <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="Boolean(declarations.error)" @click="applyTextures">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Model3DSourceUnitSelect from './Model3DSourceUnitSelect.vue'
import { maxTextureDeclarations } from '@/utils/maxTextureDeclarations'

const props = defineProps({ modelValue: { type: Object, default: () => ({}) }, size: { type: String, default: 'default' } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const visible = ref(false)
const rows = ref([])
const declarations = computed(() => maxTextureDeclarations(rows.value))

function updateUnit(unit) {
  const options = { ...props.modelValue }
  if (unit) options.source_unit = unit
  else delete options.source_unit
  emit('update:modelValue', options)
}

function openTextures() {
  rows.value = Object.entries(props.modelValue.texture_files || {}).map(([reference, path]) => ({ reference, path }))
  visible.value = true
}

function applyTextures() {
  if (declarations.value.error) return
  const options = { ...props.modelValue }
  if (rows.value.length) options.texture_files = declarations.value.mapping
  else delete options.texture_files
  emit('update:modelValue', options)
  visible.value = false
}
</script>

<style scoped>
.max-options { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.unit-select { width: 190px; }
.texture-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 8px; margin-bottom: 12px; }
.validation-error { margin-top: 12px; }
@media (max-width: 600px) { .texture-row { grid-template-columns: minmax(0, 1fr); } }
</style>
