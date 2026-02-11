<template>
  <div class="api-docs-container">
    <!-- 头部 -->
    <div class="api-docs-header">
      <span class="title">API 文档</span>
      <div class="header-actions">
        <el-popover placement="bottom-end" :width="400" trigger="click">
          <template #reference>
            <el-button :icon="QuestionFilled" circle size="small" />
          </template>
          <div class="help-content">
            <h4>ADDP 平台 API 文档</h4>
            <p><strong>Base URL:</strong> http://localhost:8000 (Gateway 统一入口)</p>
            <p><strong>认证方式:</strong> Bearer Token (JWT)</p>
            <p><strong>Content-Type:</strong> application/json</p>
            <p><strong>说明:</strong> 涵盖 ADDP 平台所有模块的 REST API 接口</p>
          </div>
        </el-popover>
        <el-tag type="info" size="small">平台级文档中心</el-tag>
      </div>
    </div>

    <!-- 主体：左侧导航 + 右侧内容 -->
    <div class="api-docs-body">
      <!-- 左侧导航树 -->
      <div class="api-nav">
        <el-tree
          ref="treeRef"
          :data="navTree"
          :props="treeProps"
          :expand-on-click-node="false"
          :default-expanded-keys="['System']"
          node-key="id"
          highlight-current
          @node-click="handleNodeClick"
        >
          <template #default="{ node, data }">
            <span class="nav-node">
              <el-tag v-if="data.method" :type="getMethodType(data.method)" size="small" class="method-tag">
                {{ data.method }}
              </el-tag>
              <span class="nav-label" :title="node.label">{{ node.label }}</span>
            </span>
          </template>
        </el-tree>
      </div>

      <!-- 右侧内容面板 -->
      <div class="api-content">
        <!-- API 详情展示 -->
        <template v-if="selectedApi && !selectedApi.isRoute">
          <div class="api-detail">
            <div class="api-header">
              <el-tag :type="getMethodType(selectedApi.api.method)" size="large">
                {{ selectedApi.api.method }}
              </el-tag>
              <span class="api-path">{{ selectedApi.api.path }}</span>
            </div>

            <h3 class="api-name">{{ selectedApi.api.name }}</h3>
            <p class="api-desc">{{ selectedApi.api.description }}</p>

            <el-descriptions :column="1" border size="small" class="api-meta">
              <el-descriptions-item label="是否需要认证">
                <el-tag :type="selectedApi.api.auth ? 'danger' : 'success'" size="small">
                  {{ selectedApi.api.auth ? '需要' : '不需要' }}
                </el-tag>
              </el-descriptions-item>
            </el-descriptions>

            <!-- 路径参数 -->
            <div class="code-block" v-if="selectedApi.api.params">
              <div class="code-title">路径参数</div>
              <pre>{{ selectedApi.api.params }}</pre>
            </div>

            <!-- 查询参数 -->
            <div class="code-block" v-if="selectedApi.api.query">
              <div class="code-title">查询参数</div>
              <pre>{{ selectedApi.api.query }}</pre>
            </div>

            <!-- 请求参数示例 -->
            <div class="code-block" v-if="selectedApi.api.request">
              <div class="code-title">请求参数示例</div>
              <pre>{{ selectedApi.api.request }}</pre>
            </div>

            <!-- 响应示例 -->
            <div class="code-block" v-if="selectedApi.api.response">
              <div class="code-title">响应示例</div>
              <pre>{{ selectedApi.api.response }}</pre>
            </div>
          </div>
        </template>

        <!-- Gateway 路由特殊展示 -->
        <template v-else-if="selectedApi && selectedApi.isRoute">
          <div class="route-detail">
            <h3 class="api-name">{{ selectedApi.api.name }}</h3>
            <el-descriptions :column="1" border>
              <el-descriptions-item label="路由前缀">
                <el-text type="primary">{{ selectedApi.api.path }}</el-text>
              </el-descriptions-item>
              <el-descriptions-item label="目标服务">{{ selectedApi.api.target }}</el-descriptions-item>
              <el-descriptions-item label="端口">{{ selectedApi.api.port }}</el-descriptions-item>
              <el-descriptions-item label="说明">{{ selectedApi.api.description }}</el-descriptions-item>
            </el-descriptions>
          </div>
        </template>

        <!-- 未选择时显示空状态 -->
        <template v-else>
          <el-empty description="请在左侧选择一个 API" />
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'

// 导入所有模块的 API 配置文件
import systemManifest from '../../../docs/api-manifest.json'
import managerManifest from '../../../../manager/docs/api-manifest.json'
import metaManifest from '../../../../meta/docs/api-manifest.json'
import transferManifest from '../../../../transfer/docs/api-manifest.json'
import developManifest from '../../../../develop/docs/api-manifest.json'
import serviceManifest from '../../../../service/docs/api-manifest.json'
import orchestratorManifest from '../../../../orchestrator/docs/api-manifest.json'
import gatewayManifest from '../../../../gateway/docs/api-manifest.json'
// 导入工作流引擎的 API 配置文件
import pythonWorkflowManifest from '../../../../engines/python-workflow/docs/api-manifest.json'
import sparkWorkflowManifest from '../../../../engines/spark-workflow/docs/api-manifest.json'
import mathWorkflowManifest from '../../../../engines/math-workflow/docs/api-manifest.json'

const treeRef = ref(null)
const selectedApi = ref(null)

// 树形组件配置
const treeProps = {
  children: 'children',
  label: 'label'
}

// HTTP 方法颜色映射
const getMethodType = (method) => {
  const types = {
    'GET': 'success',
    'POST': 'primary',
    'PUT': 'warning',
    'DELETE': 'danger'
  }
  return types[method] || 'info'
}

// 构建导航树数据结构
const navTree = computed(() => {
  const modules = [
    { manifest: systemManifest, name: 'System', label: 'System - 系统模块' },
    { manifest: managerManifest, name: 'Manager', label: 'Manager - 数据管理' },
    { manifest: metaManifest, name: 'Meta', label: 'Meta - 元数据' },
    { manifest: transferManifest, name: 'Transfer', label: 'Transfer - 数据传输' },
    { manifest: developManifest, name: 'Develop', label: 'Develop - 数据开发' },
    { manifest: serviceManifest, name: 'Service', label: 'Service - 数据服务' },
    { manifest: orchestratorManifest, name: 'Orchestrator', label: 'Orchestrator - 工作流编排' },
    { manifest: gatewayManifest, name: 'Gateway', label: 'Gateway - 路由规则' },
    // 工作流计算引擎
    { manifest: pythonWorkflowManifest, name: 'PythonWorkflow', label: 'Python Workflow - Python 计算引擎' },
    { manifest: sparkWorkflowManifest, name: 'SparkWorkflow', label: 'Spark Workflow - Spark 计算引擎' },
    { manifest: mathWorkflowManifest, name: 'MathWorkflow', label: 'Math Workflow - 数学计算引擎' }
  ]

  return modules.map(mod => ({
    id: mod.name,
    label: mod.label,
    children: (mod.manifest.categories || []).map(cat => ({
      id: `${mod.name}-${cat.name}`,
      label: cat.name,
      children: (cat.apis || cat.routes || []).map((api, idx) => ({
        id: `${mod.name}-${cat.name}-${idx}`,
        label: api.name,
        method: api.method,
        api: api,
        isRoute: !!cat.routes
      }))
    }))
  }))
})

// 点击节点
const handleNodeClick = (data) => {
  if (data.api) {
    selectedApi.value = data
  }
}
</script>

<style scoped>
.api-docs-container {
  height: calc(100vh - 120px);
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
  border-radius: 4px;
  box-shadow: var(--addp-shadow-card);
  margin: 20px;
}

.api-docs-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--addp-border-color);
  flex-shrink: 0;
}

.api-docs-header .title {
  font-size: 18px;
  font-weight: 600;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.help-content h4 {
  margin: 0 0 12px 0;
  font-size: 16px;
  color: var(--addp-text-primary);
}

.help-content p {
  margin: 8px 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
}

.api-docs-body {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.api-nav {
  width: 300px;
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
  padding: 10px 0;
  flex-shrink: 0;
}

.api-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px;
}

.nav-node {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  overflow: hidden;
}

.method-tag {
  flex-shrink: 0;
  font-size: 10px;
  padding: 0 4px;
  height: 18px;
  line-height: 16px;
}

.nav-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

/* API 详情样式 */
.api-detail,
.route-detail {
  max-width: 900px;
}

.api-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.api-path {
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 15px;
  color: var(--el-color-primary);
}

.api-name {
  margin: 0 0 8px 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.api-desc {
  color: var(--addp-text-secondary);
  margin: 0 0 20px 0;
  font-size: 14px;
  line-height: 1.6;
}

.api-meta {
  margin-bottom: 20px;
}

/* 代码块样式 */
.code-block {
  margin-top: 16px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
  overflow: hidden;
}

.code-title {
  background: var(--addp-border-color);
  padding: 8px 15px;
  font-weight: 600;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.code-block pre {
  margin: 0;
  padding: 15px;
  background: #282c34;
  color: #abb2bf;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.6;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* Element Plus 深度选择器自定义 */
:deep(.el-descriptions__label) {
  width: 120px;
  font-weight: 600;
}

:deep(.el-tree-node__content) {
  height: 32px;
}

:deep(.el-tree-node__expand-icon) {
  font-size: 14px;
}

:deep(.el-empty) {
  padding-top: 100px;
}
</style>
