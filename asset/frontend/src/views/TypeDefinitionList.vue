<template>
  <div class="type-definition-list">
    <div class="page-header">
      <h2>{{ t('asset.typeDefinition.title') }}</h2>
      <p class="page-desc">{{ t('asset.typeDefinition.desc') }}</p>
    </div>

    <el-table v-loading="loading" :data="types" border stripe>
      <el-table-column :label="t('asset.typeDefinition.typeName')" prop="name" width="140">
        <template #default="{ row }">
          <el-tag :type="typeTagMap[row.code] || 'info'" size="small">{{ row.name }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('asset.typeDefinition.typeCode')" prop="code" width="120">
        <template #default="{ row }">
          <code>{{ row.code }}</code>
        </template>
      </el-table-column>
      <el-table-column :label="t('asset.typeDefinition.authHandler')" prop="auth_handler" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.auth_handler === 'token'" type="warning" size="small">{{ t('asset.typeDefinition.tokenAuth') }}</el-tag>
          <el-tag v-else type="success" size="small">{{ t('asset.typeDefinition.softAuth') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('asset.typeDefinition.entryType')" prop="entry_type" width="100">
        <template #default="{ row }">
          {{ entryTypeLabel(row.entry_type) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('asset.typeDefinition.description')" prop="description" />
      <el-table-column :label="t('asset.typeDefinition.status')" prop="enabled" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'danger'" size="small">
            {{ row.enabled ? t('asset.typeDefinition.enabled') : t('asset.typeDefinition.disabled') }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && types.length === 0" :description="t('asset.typeDefinition.empty')" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { typeDefinitionAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const loading = ref(false)
const types = ref([])

const typeTagMap = {
  dataset: '',
  service: 'success',
  metric: 'warning',
  report: 'info',
  algo_model: 'danger',
  application: ''
}

function entryTypeLabel(entry) {
  const map = {
    preview: t('asset.typeDefinition.entryPreview'),
    token: t('asset.typeDefinition.entryToken'),
    link: t('asset.typeDefinition.entryLink'),
    iframe: t('asset.typeDefinition.entryIframe'),
  }
  return map[entry] || entry
}

async function loadTypes() {
  loading.value = true
  try {
    const res = await typeDefinitionAPI.list()
    types.value = res || []
  } catch (e) {
    ElMessage.error(t('asset.typeDefinition.loadFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(loadTypes)
</script>

<style scoped>
.type-definition-list { padding: 24px; }
.page-header { margin-bottom: 20px; }
.page-header h2 { margin: 0 0 6px; font-size: 18px; font-weight: 600; }
.page-desc { margin: 0; font-size: 13px; color: var(--el-text-color-secondary); }
code { padding: 2px 6px; background: var(--el-fill-color-light); border-radius: 3px; font-size: 12px; font-family: monospace; }
</style>
