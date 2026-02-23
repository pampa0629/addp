<template>
  <div>
    <div class="page-header">
      <h2>问题工单</h2>
    </div>

    <el-form :inline="true" style="margin-bottom:16px">
      <el-form-item label="状态">
        <el-select v-model="filter.status" clearable placeholder="全部" @change="fetchList" style="width:120px">
          <el-option label="待处理" value="open" />
          <el-option label="已解决" value="resolved" />
          <el-option label="已忽略" value="ignored" />
        </el-select>
      </el-form-item>
    </el-form>

    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="rule_type" label="规则类型" width="120" />
      <el-table-column prop="table_name" label="表名" width="160" />
      <el-table-column prop="column_name" label="字段" width="130" />
      <el-table-column label="通过率" width="100">
        <template #default="{ row }">{{ row.pass_rate?.toFixed(1) }}%</template>
      </el-table-column>
      <el-table-column prop="failed_count" label="失败行数" width="100" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="时间" width="180">
        <template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template>
      </el-table-column>
      <el-table-column label="操作" width="160">
        <template #default="{ row }">
          <el-button v-if="row.status === 'open'" size="small" type="success" @click="changeStatus(row.id, 'resolved')">标记解决</el-button>
          <el-button v-if="row.status === 'open'" size="small" @click="changeStatus(row.id, 'ignored')">忽略</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { issueAPI } from '../api/quality'

const list = ref([])
const loading = ref(false)
const filter = ref({ status: '' })

const statusTagType = (s) => ({ open: 'danger', resolved: 'success', ignored: 'info' }[s] || 'info')
const statusLabel = (s) => ({ open: '待处理', resolved: '已解决', ignored: '已忽略' }[s] || s)

const fetchList = async () => {
  loading.value = true
  try {
    const params = {}
    if (filter.value.status) params.status = filter.value.status
    const res = await issueAPI.list(params)
    list.value = res.data || []
  } finally {
    loading.value = false
  }
}

const changeStatus = async (id, status) => {
  try {
    await issueAPI.updateStatus(id, status)
    ElMessage.success('已更新')
    await fetchList()
  } catch (e) {
    ElMessage.error('更新失败')
  }
}

onMounted(fetchList)
</script>

<style scoped>
.page-header {
  margin-bottom: 16px;
}
</style>
