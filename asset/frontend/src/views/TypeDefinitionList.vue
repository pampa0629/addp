<template>
  <div class="type-definition-list">
    <div class="page-header">
      <h2>资产类型</h2>
      <p class="page-desc">系统内置6种资产类型，配置驱动，不硬编码</p>
    </div>

    <el-table v-loading="loading" :data="types" border stripe>
      <el-table-column label="类型名称" prop="name" width="140">
        <template #default="{ row }">
          <el-tag :type="typeTagMap[row.code] || 'info'" size="small">{{ row.name }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="类型代码" prop="code" width="120">
        <template #default="{ row }">
          <code>{{ row.code }}</code>
        </template>
      </el-table-column>
      <el-table-column label="元数据来源" prop="source_module" width="130">
        <template #default="{ row }">
          {{ sourceModuleMap[row.source_module] || row.source_module }}
        </template>
      </el-table-column>
      <el-table-column label="授权方式" prop="auth_handler" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.auth_handler === 'token'" type="warning" size="small">Token 授权</el-tag>
          <el-tag v-else type="success" size="small">软性授权</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="使用入口" prop="entry_type" width="100">
        <template #default="{ row }">
          {{ entryTypeMap[row.entry_type] || row.entry_type }}
        </template>
      </el-table-column>
      <el-table-column label="说明" prop="description" />
      <el-table-column label="状态" prop="enabled" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'danger'" size="small">
            {{ row.enabled ? '启用' : '禁用' }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && types.length === 0" description="暂无资产类型" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { typeDefinitionAPI } from '../api/asset'

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

const sourceModuleMap = {
  meta: 'Meta 模块',
  service: 'Service 模块',
  standard: 'Standard 模块',
  develop: 'Develop 模块',
  manual: '手动录入'
}

const entryTypeMap = {
  preview: '在线预览',
  token: 'API Token',
  link: '跳转链接',
  iframe: '嵌入展示'
}

async function loadTypes() {
  loading.value = true
  try {
    const res = await typeDefinitionAPI.list()
    types.value = res.data || []
  } catch (e) {
    ElMessage.error('加载资产类型失败')
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
