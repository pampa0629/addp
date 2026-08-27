<template>
  <div class="catalog-entry-picker">
    <div class="picker-toolbar">
      <span class="picker-hint">{{ t('asset.catalogPicker.hint') }}</span>
      <el-button type="primary" plain @click="openPicker">{{ t('asset.catalogPicker.add') }}</el-button>
    </div>
    <el-table :data="selected" border empty-text="-">
      <el-table-column :label="t('asset.catalogPicker.role')" width="130">
        <template #default="{ row }">
          <el-radio :model-value="primaryID" :value="row.catalog_entry_id" @change="setPrimary(row.catalog_entry_id)">
            {{ row.role === 'primary' ? t('asset.catalogPicker.primary') : t('asset.catalogPicker.setPrimary') }}
          </el-radio>
        </template>
      </el-table-column>
      <el-table-column :label="t('asset.catalogPicker.entry')" min-width="280">
        <template #default="{ row }">
          <div>{{ row.display_name || row.catalog_entry_id }}</div>
          <code>{{ row.catalog_entry_id }}</code>
        </template>
      </el-table-column>
      <el-table-column width="90" :label="t('asset.catalogPicker.actions')">
        <template #default="{ row }">
          <el-button link type="danger" @click="remove(row.catalog_entry_id)">{{ t('asset.catalogPicker.remove') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="visible" :title="t('asset.catalogPicker.title')" width="760px">
      <div class="search-row">
        <el-input v-model="keyword" clearable :placeholder="t('asset.catalogPicker.searchPlaceholder')" @keyup.enter="loadCandidates" />
        <el-button :loading="loading" @click="loadCandidates">{{ t('asset.catalogPicker.search') }}</el-button>
      </div>
      <el-table v-loading="loading" :data="candidates" @selection-change="candidateSelection = $event">
        <el-table-column type="selection" width="48" />
        <el-table-column prop="display_name" :label="t('asset.catalogPicker.entry')" min-width="220" />
        <el-table-column prop="governance_status" :label="t('asset.catalogPicker.governance')" width="130" />
        <el-table-column prop="source_status" :label="t('asset.catalogPicker.sourceStatus')" width="110" />
      </el-table>
      <template #footer>
        <el-button @click="visible = false">{{ t('asset.catalogPicker.cancel') }}</el-button>
        <el-button type="primary" @click="addCandidates">{{ t('asset.catalogPicker.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { enterpriseCatalogAPI } from '../api/asset'

const props = defineProps({ modelValue: { type: Array, default: () => [] } })
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const selected = ref([])
const visible = ref(false)
const loading = ref(false)
const keyword = ref('')
const candidates = ref([])
const candidateSelection = ref([])
const primaryID = computed(() => selected.value.find(item => item.role === 'primary')?.catalog_entry_id || '')

watch(() => props.modelValue, async value => {
  selected.value = (value || []).map((item, index) => ({ ...item, sort_order: index }))
  await hydrateNames()
}, { immediate: true, deep: true })

async function hydrateNames() {
  await Promise.all(selected.value.filter(item => !item.display_name).map(async item => {
    try {
      const detail = await enterpriseCatalogAPI.get(item.catalog_entry_id)
      item.display_name = detail.display_name
    } catch {
      item.display_name = item.catalog_entry_id
    }
  }))
}

function publish() {
  emit('update:modelValue', selected.value.map((item, index) => ({
    catalog_entry_id: item.catalog_entry_id,
    role: item.role,
    sort_order: index
  })))
}

function setPrimary(id) {
  selected.value = selected.value.map(item => ({ ...item, role: item.catalog_entry_id === id ? 'primary' : 'supporting' }))
  publish()
}

function remove(id) {
  const wasPrimary = primaryID.value === id
  selected.value = selected.value.filter(item => item.catalog_entry_id !== id)
  if (wasPrimary && selected.value.length) selected.value[0].role = 'primary'
  publish()
}

async function openPicker() {
  visible.value = true
  await loadCandidates()
}

async function loadCandidates() {
  loading.value = true
  try {
    const response = await enterpriseCatalogAPI.list({ view: 'inventory', search: keyword.value || undefined, source_status: 'active', page: 1, page_size: 100 })
    candidates.value = (response.data || []).filter(item => item.entry_status === 'active' && item.governance_status !== 'deprecated')
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('asset.catalogPicker.loadFailed'))
  } finally {
    loading.value = false
  }
}

function addCandidates() {
  const existing = new Set(selected.value.map(item => item.catalog_entry_id))
  for (const item of candidateSelection.value) {
    if (!existing.has(item.id)) {
      selected.value.push({
        catalog_entry_id: item.id,
        display_name: item.display_name,
        role: selected.value.length === 0 ? 'primary' : 'supporting',
        sort_order: selected.value.length
      })
      existing.add(item.id)
    }
  }
  publish()
  visible.value = false
}
</script>

<style scoped>
.picker-toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
.picker-hint { color: var(--el-text-color-secondary); font-size: 13px; }
.search-row { display: flex; gap: 10px; margin-bottom: 14px; }
code { color: var(--el-text-color-secondary); font-size: 11px; }
</style>
