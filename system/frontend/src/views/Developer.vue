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
            <p><strong>版本前缀:</strong> /api/v1/</p>
            <p><strong>认证方式:</strong> Bearer Token (JWT)</p>
            <p><strong>Content-Type:</strong> application/json</p>
            <p><strong>说明:</strong> 基于 Swagger/OpenAPI 自动生成，各模块独立提供</p>
          </div>
        </el-popover>
        <el-tag type="info" size="small">平台级文档中心</el-tag>
      </div>
    </div>

    <!-- 主体：左侧模块列表 + 右侧 Swagger UI -->
    <div class="api-docs-body">
      <!-- 左侧模块导航 -->
      <div class="api-nav">
        <div
          v-for="mod in modules"
          :key="mod.name"
          class="module-item"
          :class="{ active: selectedModule?.name === mod.name }"
          @click="selectModule(mod)"
        >
          <div class="module-name">{{ mod.label }}</div>
          <div class="module-url">{{ mod.port }}</div>
        </div>
      </div>

      <!-- 右侧 Swagger UI -->
      <div class="api-content">
        <template v-if="selectedModule">
          <div class="swagger-header">
            <span class="swagger-title">{{ selectedModule.label }}</span>
            <el-button
              type="primary"
              size="small"
              link
              @click="openInNewTab(selectedModule.swaggerUrl)"
            >
              在新标签页打开
            </el-button>
          </div>
          <iframe
            :key="selectedModule.name"
            :src="selectedModule.swaggerUrl"
            class="swagger-iframe"
            @load="onIframeLoad"
            @error="onIframeError"
          />
        </template>
        <template v-else>
          <div class="welcome">
            <el-empty description="请在左侧选择一个模块查看 API 文档">
              <template #image>
                <div class="welcome-icon">📖</div>
              </template>
            </el-empty>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'

// 各模块 Swagger UI 地址
// Go 模块：使用 swaggo/swag，访问 /swagger/index.html
// Python 模块（FastAPI）：访问 /docs
const modules = [
  { name: 'system', label: 'System - 系统模块', port: ':8180', swaggerUrl: 'http://localhost:8180/swagger/index.html' },
  { name: 'manager', label: 'Manager - 数据管理', port: ':8081', swaggerUrl: 'http://localhost:8081/swagger/index.html' },
  { name: 'meta', label: 'Meta - 元数据', port: ':8082', swaggerUrl: 'http://localhost:8082/swagger/index.html' },
  { name: 'develop', label: 'Develop - 数据开发', port: ':8083', swaggerUrl: 'http://localhost:8083/swagger/index.html' },
  { name: 'transfer', label: 'Transfer - 数据传输', port: ':8084', swaggerUrl: 'http://localhost:8084/swagger/index.html' },
  { name: 'service', label: 'Service - 数据服务', port: ':8085', swaggerUrl: 'http://localhost:8085/swagger/index.html' },
  { name: 'orchestrator', label: 'Orchestrator - 工作流编排', port: ':8086', swaggerUrl: 'http://localhost:8086/swagger/index.html' },
  { name: 'monitor', label: 'Monitor - 执行监控', port: ':8087', swaggerUrl: 'http://localhost:8087/swagger/index.html' },
  { name: 'standard', label: 'Standard - 数据标准', port: ':8088', swaggerUrl: 'http://localhost:8088/swagger/index.html' },
  { name: 'model', label: 'Model - 数据建模', port: ':8089', swaggerUrl: 'http://localhost:8089/swagger/index.html' },
  { name: 'agent', label: 'Agent - AI 对话助手', port: ':8190', swaggerUrl: 'http://localhost:8190/docs' },
  { name: 'copilot', label: 'Copilot - AI 辅助', port: ':8191', swaggerUrl: 'http://localhost:8191/docs' },
]

const selectedModule = ref(null)

const selectModule = (mod) => {
  selectedModule.value = mod
}

const openInNewTab = (url) => {
  window.open(url, '_blank')
}

const onIframeLoad = () => {
  // iframe 加载成功
}

const onIframeError = () => {
  // iframe 加载失败，Swagger 可能还未部署
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

/* 左侧模块列表 */
.api-nav {
  width: 220px;
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
  padding: 8px 0;
  flex-shrink: 0;
}

.module-item {
  padding: 10px 16px;
  cursor: pointer;
  border-left: 3px solid transparent;
  transition: all 0.15s;
}

.module-item:hover {
  background: var(--addp-bg-secondary);
}

.module-item.active {
  background: var(--el-color-primary-light-9);
  border-left-color: var(--el-color-primary);
}

.module-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--addp-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.module-url {
  font-size: 11px;
  color: var(--addp-text-secondary);
  margin-top: 2px;
  font-family: monospace;
}

/* 右侧内容 */
.api-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.swagger-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  border-bottom: 1px solid var(--addp-border-color-light);
  flex-shrink: 0;
}

.swagger-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.swagger-iframe {
  flex: 1;
  width: 100%;
  border: none;
  background: #fff;
}

.welcome {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.welcome-icon {
  font-size: 60px;
  line-height: 1;
}
</style>
