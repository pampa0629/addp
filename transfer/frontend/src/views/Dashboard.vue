<template>
  <div class="dashboard">
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card>
          <el-statistic title="总任务数" :value="stats.total_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="运行中" :value="stats.running_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="成功" :value="stats.success_tasks || 0" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic title="失败" :value="stats.failed_tasks || 0" />
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px;">
      <template #header>最近执行</template>
      <el-table :data="recentExecutions">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="task_id" label="任务ID" />
        <el-table-column prop="status" label="状态" />
        <el-table-column prop="start_time" label="开始时间" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { taskAPI, executionAPI } from '@/api/tasks'

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
