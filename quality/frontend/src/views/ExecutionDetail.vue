<template>
  <div v-loading="loading">
    <el-page-header @back="$router.back()" :content="`执行详情 - ${execution?.execution_id || ''}`" />

    <el-descriptions v-if="execution" :column="2" border style="margin-top:20px">
      <el-descriptions-item label="状态">
        <el-tag :type="statusType(execution.status)">{{ execution.status }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item label="质量评分">
        <span v-if="execution.result?.quality_score != null" style="font-size:18px;font-weight:bold">
          {{ execution.result.quality_score.toFixed(1) }}%
        </span>
        <span v-else>-</span>
      </el-descriptions-item>
      <el-descriptions-item label="总规则数">{{ execution.result?.total_rules ?? '-' }}</el-descriptions-item>
      <el-descriptions-item label="通过/失败">
        {{ execution.result?.passed_rules ?? '-' }} / {{ execution.result?.failed_rules ?? '-' }}
      </el-descriptions-item>
      <el-descriptions-item label="执行耗时">{{ execution.execution_time_ms ? execution.execution_time_ms + ' ms' : '-' }}</el-descriptions-item>
      <el-descriptions-item label="创建时间">{{ execution.created_at ? new Date(execution.created_at).toLocaleString() : '-' }}</el-descriptions-item>
    </el-descriptions>

    <template v-if="execution?.result?.field_scores?.length">
      <h3 style="margin-top:24px">字段质量评分</h3>
      <el-table :data="execution.result.field_scores" border size="small">
        <el-table-column prop="column" label="字段" />
        <el-table-column label="评分" width="120">
          <template #default="{ row }">{{ row.score.toFixed(1) }}%</template>
        </el-table-column>
        <el-table-column prop="passed" label="通过规则" width="100" />
        <el-table-column prop="failed" label="失败规则" width="100" />
      </el-table>
    </template>

    <template v-if="execution?.result?.rule_details?.length">
      <h3 style="margin-top:24px">规则执行明细</h3>
      <el-table :data="execution.result.rule_details" border size="small">
        <el-table-column prop="rule_type" label="规则类型" width="120" />
        <el-table-column prop="column" label="字段" width="150" />
        <el-table-column prop="table" label="表名" width="150" />
        <el-table-column label="通过率" width="100">
          <template #default="{ row }">{{ row.pass_rate?.toFixed(1) ?? '-' }}%</template>
        </el-table-column>
        <el-table-column label="结果" width="80">
          <template #default="{ row }">
            <el-tag :type="row.passed ? 'success' : 'danger'">{{ row.passed ? '通过' : '失败' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="failed_count" label="失败行数" width="100" />
        <el-table-column prop="error" label="错误信息" show-overflow-tooltip />
      </el-table>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { executionAPI } from '../api/quality'

const route = useRoute()
const execution = ref(null)
const loading = ref(false)

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
}

onMounted(async () => {
  loading.value = true
  try {
    const res = await executionAPI.get(route.params.id)
    execution.value = res
  } finally {
    loading.value = false
  }
})
</script>
