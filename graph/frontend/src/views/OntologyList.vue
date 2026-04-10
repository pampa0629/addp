<template>
  <div class="page-container">
    <div class="page-header">
      <h2>{{ t('graph.ontology.management') }}</h2>
      <el-button type="primary" @click="$router.push('/ontologies/create')">
        <el-icon><Plus /></el-icon> {{ t('graph.ontology.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="ontologies" border style="width:100%">
      <el-table-column prop="name" :label="t('graph.common.name')" min-width="150">
        <template #default="{ row }">
          <el-link type="primary" @click="$router.push(`/ontologies/${row.id}`)">{{ row.name }}</el-link>
        </template>
      </el-table-column>
      <el-table-column prop="description" :label="t('graph.common.description')" min-width="200" show-overflow-tooltip />
      <el-table-column prop="status" :label="t('graph.common.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'info'" size="small">
            {{ row.status === 'active' ? t('graph.common.active') : t('graph.common.archived') }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" :label="t('graph.common.createdAt')" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('graph.common.actions')" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="$router.push(`/ontologies/${row.id}`)">{{ t('graph.common.view') }}</el-button>
          <el-button link size="small" @click="$router.push(`/ontologies/${row.id}/edit`)">{{ t('graph.common.edit') }}</el-button>
          <el-button link type="danger" size="small" @click="handleDelete(row)">{{ t('graph.common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const loading = ref(false)
const ontologies = ref([])

const load = async () => {
  loading.value = true
  try {
    const res = await ontologyAPI.list()
    ontologies.value = res || []
  } catch (e) {
    ElMessage.error(t('graph.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleDelete = async (row) => {
  await ElMessageBox.confirm(t('graph.ontology.confirmDelete', { name: row.name }), t('graph.common.confirmDelete'), { type: 'warning' })
  try {
    await ontologyAPI.delete(row.id)
    ElMessage.success(t('graph.common.deleteSuccess'))
    load()
  } catch (e) {
    ElMessage.error(t('graph.common.deleteFailed'))
  }
}

const formatDate = (str) => str ? new Date(str).toLocaleString() : '-'

onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-header h2 { margin: 0; font-size: 18px; }
</style>
