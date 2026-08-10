<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.issue.title') }}</h2>
    </div>

    <el-form :inline="true" style="margin-bottom:16px">
      <el-form-item :label="t('quality.issue.statusFilter')">
        <el-select v-model="filter.status" clearable :placeholder="t('quality.issue.allStatus')" @change="applyFilters" style="width:120px">
          <el-option :label="t('quality.issue.open')" value="open" />
          <el-option :label="t('quality.issue.resolved')" value="resolved" />
          <el-option :label="t('quality.issue.ignored')" value="ignored" />
        </el-select>
      </el-form-item>
    </el-form>

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="id" :label="t('quality.issue.id')" width="80" />
      <el-table-column prop="type" :label="t('quality.issue.ruleType')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.issue.tableName')" width="160" />
      <el-table-column prop="column_name" :label="t('quality.issue.column')" width="130" />
      <el-table-column :label="t('quality.issue.passRate')" width="100">
        <template #default="{ row }">{{ row.pass_rate?.toFixed(1) }}%</template>
      </el-table-column>
      <el-table-column prop="failed_count" :label="t('quality.issue.failedCount')" width="100" />
      <el-table-column prop="status" :label="t('quality.issue.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" :label="t('quality.issue.createdAt')" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.issue.actions')" width="160">
        <template #default="{ row }">
          <el-button v-if="row.status === 'open'" size="small" type="success" @click="changeStatus(row.id, 'resolved')">{{ t('quality.issue.markResolved') }}</el-button>
          <el-button v-if="row.status === 'open'" size="small" @click="changeStatus(row.id, 'ignored')">{{ t('quality.issue.ignore') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.page_size"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @size-change="fetchList"
      @current-change="fetchList"
    />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { issueAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const list = ref([])
const loading = ref(false)
const filter = ref({ status: '' })
const pagination = ref({ page: 1, page_size: 20, total: 0 })

const statusTagType = (s) => ({ open: 'danger', resolved: 'success', ignored: 'info' }[s] || 'info')
const statusLabel = (s) => ({
  open: t('quality.issue.open'),
  resolved: t('quality.issue.resolved'),
  ignored: t('quality.issue.ignored')
}[s] || s)

const fetchList = async () => {
  loading.value = true
  try {
    const params = { page: pagination.value.page, page_size: pagination.value.page_size }
    if (filter.value.status) params.status = filter.value.status
    const res = await issueAPI.list(params)
    list.value = res?.data || []
    pagination.value.total = res?.total || 0
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.issue.loadFailed'))
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {
  pagination.value.page = 1
  fetchList()
}

const changeStatus = async (id, status) => {
  try {
    const { value: note } = await ElMessageBox.prompt(t('quality.issue.notePrompt'), t('quality.issue.noteTitle'), {
      inputPattern: /\S+/,
      inputErrorMessage: t('quality.issue.noteRequired'),
      confirmButtonText: t('quality.issue.confirm'),
      cancelButtonText: t('quality.issue.cancel')
    })
    await issueAPI.updateStatus(id, status, note)
    ElMessage.success(t('quality.issue.updateSuccess'))
    await fetchList()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.issue.updateFailed'))
  }
}

onMounted(fetchList)
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
