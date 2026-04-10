<template>
  <div class="dashboard">
    <el-row :gutter="20">
      <el-col :span="4">
        <el-card>
          <el-statistic :title="t('transfer.dashboard.totalTasks')" :value="stats.total_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="5">
        <el-card>
          <el-statistic :title="t('transfer.dashboard.notExecuted')" :value="stats.not_executed_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="5">
        <el-card>
          <el-statistic :title="t('transfer.dashboard.running')" :value="stats.last_running_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="5">
        <el-card>
          <el-statistic :title="t('transfer.dashboard.success')" :value="stats.last_success_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="5">
        <el-card>
          <el-statistic :title="t('transfer.dashboard.failed')" :value="stats.last_failed_tasks || 0" />
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px;">
      <template #header>{{ t('transfer.dashboard.recentExecutions') }}</template>
      <el-table :data="recentExecutions">
        <el-table-column prop="id" :label="t('transfer.dashboard.id')" width="80" />
        <el-table-column prop="task_id" :label="t('transfer.dashboard.taskId')" />
        <el-table-column prop="status" :label="t('transfer.dashboard.status')" />
        <el-table-column prop="start_time" :label="t('transfer.dashboard.startTime')" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { taskAPI, executionAPI } from '@/api/tasks'

const { t } = useI18n()
const stats = ref({})
const recentExecutions = ref([])

const loadData = async () => {
  stats.value = await taskAPI.statistics()
  const res = await executionAPI.list({ page: 1, page_size: 10 })
  recentExecutions.value = res.data || []
}

onMounted(() => {
  loadData()
  setInterval(loadData, 10000)
})
</script>

<style scoped>
.dashboard { padding: 20px; }
</style>
