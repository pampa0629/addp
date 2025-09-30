<template>
  <el-card>
    <template #header>
      <span>系统概览</span>
    </template>
    <el-row :gutter="20">
      <el-col :span="8">
        <el-statistic title="用户总数" :value="0">
          <template #prefix>
            <el-icon><User /></el-icon>
          </template>
        </el-statistic>
      </el-col>
      <el-col :span="8">
        <el-statistic title="资源总数" :value="0">
          <template #prefix>
            <el-icon><Files /></el-icon>
          </template>
        </el-statistic>
      </el-col>
      <el-col :span="8">
        <el-statistic title="今日日志" :value="0">
          <template #prefix>
            <el-icon><Document /></el-icon>
          </template>
        </el-statistic>
      </el-col>
    </el-row>
  </el-card>
</template>

<script setup>
</script>

<style scoped>
.el-statistic {
  text-align: center;
}
</style>