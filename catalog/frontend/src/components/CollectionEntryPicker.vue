<template>
  <el-select
    :model-value="modelValue"
    multiple
    filterable
    remote
    reserve-keyword
    :remote-method="searchEntries"
    :loading="searching"
    :placeholder="t('catalog.collections.searchEntries')"
    style="width: 100%"
    @update:model-value="emit('update:modelValue', $event)"
    @visible-change="visible => { if (visible && options.length === 0) searchEntries('') }"
  >
    <el-option v-for="entry in options" :key="entry.id" :label="entry.display_name || t('catalog.entries.unnamed')" :value="entry.id" />
  </el-select>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { listEntries } from '../api/catalog'

const props = defineProps({
  modelValue: { type: Array, required: true },
  initialEntries: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const searching = ref(false)
const options = ref([])
let searchVersion = 0

watch(() => props.initialEntries, entries => {
  options.value = [...entries]
  searchVersion++
  searching.value = false
}, { immediate: true })

async function searchEntries(search) {
  const version = ++searchVersion
  searching.value = true
  try {
    const response = await listEntries({ search: String(search || '').trim(), page: 1, page_size: 50 })
    if (version !== searchVersion) return
    const selected = new Map(options.value.filter(entry => props.modelValue.includes(entry.id)).map(entry => [entry.id, entry]))
    for (const entry of response.data || []) selected.set(entry.id, entry)
    options.value = [...selected.values()]
  } catch (error) {
    if (version === searchVersion) ElMessage.error(error?.response?.data?.error || t('catalog.entries.loadFailed'))
  } finally {
    if (version === searchVersion) searching.value = false
  }
}
</script>
