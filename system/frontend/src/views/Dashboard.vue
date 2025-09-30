<template>
  <Layout>
    <el-row :gutter="20">
      <el-col :span="24">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>欢迎使用全域数据平台</span>
            </div>
          </template>
          <div class="welcome-content">
            <el-icon :size="64" color="#409eff">
              <Platform />
            </el-icon>
            <p class="welcome-text">请从左侧菜单选择功能模块开始使用</p>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px">
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="功能模块" :value="3">
            <template #prefix>
              <el-icon><Grid /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="在线状态">
            <template #prefix>
              <el-icon color="#67c23a"><SuccessFilled /></el-icon>
            </template>
            <template #suffix>
              <span style="font-size: 16px">运行中</span>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card shadow="hover">
          <el-statistic title="系统版本" value="1.0.0">
            <template #prefix>
              <el-icon><Checked /></el-icon>
            </template>
          </el-statistic>
        </el-card>
      </el-col>
    </el-row>
  </Layout>
</template>

<script setup>
import Layout from '../components/Layout.vue'
import { Platform, Grid, SuccessFilled, Checked } from '@element-plus/icons-vue'
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 600;
}

.welcome-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 0;
}

.welcome-text {
  margin-top: 20px;
  font-size: 16px;
  color: #606266;
}
</style>