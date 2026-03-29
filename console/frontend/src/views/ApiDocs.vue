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
          <div class="module-meta">
            <span class="module-port">{{ mod.port }}</span>
            <span
              class="status-dot"
              :class="moduleStatus[mod.name] === 'online' ? 'online' : moduleStatus[mod.name] === 'offline' ? 'offline' : 'checking'"
              :title="moduleStatus[mod.name] === 'online' ? '在线' : moduleStatus[mod.name] === 'offline' ? '离线' : '检测中'"
            />
          </div>
        </div>
      </div>

      <!-- 右侧 Swagger UI -->
      <div class="api-content">
        <template v-if="selectedModule">
          <div class="swagger-header">
            <span class="swagger-title">{{ selectedModule.label }}</span>
            <div class="swagger-actions">
              <el-tag
                v-if="moduleStatus[selectedModule.name] === 'online'"
                type="success"
                size="small"
              >在线</el-tag>
              <el-tag
                v-else-if="moduleStatus[selectedModule.name] === 'offline'"
                type="danger"
                size="small"
              >离线</el-tag>
              <el-tag v-else size="small">检测中</el-tag>
              <el-button
                type="primary"
                size="small"
                link
                @click="openInNewTab(selectedModule.swaggerUrl)"
              >
                在新标签页打开
              </el-button>
            </div>
          </div>
          <template v-if="moduleStatus[selectedModule.name] === 'offline'">
            <div class="error-state">
              <el-result icon="warning" title="模块离线" sub-title="该模块服务未运行，无法加载 API 文档">
                <template #extra>
                  <el-button size="small" @click="checkModuleStatus(selectedModule)">重新检测</el-button>
                </template>
              </el-result>
            </div>
          </template>
          <template v-else-if="iframeError">
            <div class="error-state">
              <el-result icon="info" title="暂无 Swagger 文档" sub-title="该模块尚未配置 Swagger，服务在线但文档不可用">
                <template #extra>
                  <el-button size="small" @click="openInNewTab(selectedModule.swaggerUrl)">尝试直接访问</el-button>
                </template>
              </el-result>
            </div>
          </template>
          <template v-else>
            <iframe
              :key="selectedModule.name"
              :src="selectedModule.swaggerUrl"
              class="swagger-iframe"
              @load="onIframeLoad"
              @error="onIframeError"
            />
          </template>
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
import { ref, onMounted } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'

const isDev = import.meta.env.DEV
const protocol = window.location.protocol
const hostname = window.location.hostname

// 各模块配置
// swaggerUrl: 开发环境直连各模块端口，生产环境通过 nginx 路由
// healthUrl: 开发环境通过 Vite proxy 避免 CORS，生产环境不检测
const viewer = (specUrl) => `/swagger-viewer.html?url=${encodeURIComponent(specUrl)}`

const modules = [
  {
    name: 'agent',
    label: 'Agent - AI智能体',
    port: ':8190',
    swaggerUrl: isDev ? viewer('/swagger-spec/agent') : viewer('/agent/openapi.json'),
    healthUrl: isDev ? '/module-health/agent' : null,
  },
  {
    name: 'copilot',
    label: 'Copilot - AI助手',
    port: ':8087',
    swaggerUrl: isDev ? viewer('/swagger-spec/copilot') : viewer('/copilot/openapi.json'),
    healthUrl: isDev ? '/module-health/copilot' : null,
  },
  {
    name: 'develop',
    label: 'Develop - 数据开发',
    port: ':8085',
    swaggerUrl: isDev ? viewer('/swagger-spec/develop') : viewer('/develop/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/develop' : null,
  },
  {
    name: 'manager',
    label: 'Manager - 数据管理',
    port: ':8081',
    swaggerUrl: isDev ? viewer('/swagger-spec/manager') : viewer('/manager/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/manager' : null,
  },
  {
    name: 'meta',
    label: 'Meta - 元数据',
    port: ':8082',
    swaggerUrl: isDev ? viewer('/swagger-spec/meta') : viewer('/meta/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/meta' : null,
  },
  {
    name: 'model',
    label: 'Model - 数据建模',
    port: ':8181',
    swaggerUrl: isDev ? viewer('/swagger-spec/model') : viewer('/model/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/model' : null,
  },
  {
    name: 'monitor',
    label: 'Monitor - 执行监控',
    port: ':8100',
    swaggerUrl: isDev ? viewer('/swagger-spec/monitor') : viewer('/monitor/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/monitor' : null,
  },
  {
    name: 'orchestrator',
    label: 'Orchestrator - 工作流编排',
    port: ':8084',
    swaggerUrl: isDev ? viewer('/swagger-spec/orchestrator') : viewer('/orchestrator/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/orchestrator' : null,
  },
  {
    name: 'portal',
    label: 'Portal - 资产门户',
    port: ':8184',
    swaggerUrl: isDev ? viewer('/swagger-spec/portal') : viewer('/portal/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/portal' : null,
  },
  {
    name: 'quality',
    label: 'Quality - 数据质量',
    port: ':8182',
    swaggerUrl: isDev ? viewer('/swagger-spec/quality') : viewer('/quality/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/quality' : null,
  },
  {
    name: 'service',
    label: 'Service - 数据服务',
    port: ':8086',
    swaggerUrl: isDev ? viewer('/swagger-spec/service') : viewer('/service/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/service' : null,
  },
  {
    name: 'standard',
    label: 'Standard - 数据标准',
    port: ':8110',
    swaggerUrl: isDev ? viewer('/swagger-spec/standard') : viewer('/standard/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/standard' : null,
  },
  {
    name: 'system',
    label: 'System - 系统模块',
    port: ':8180',
    swaggerUrl: isDev ? viewer('/swagger-spec/system') : viewer('/system/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/system' : null,
  },
  {
    name: 'transfer',
    label: 'Transfer - 数据传输',
    port: ':8083',
    swaggerUrl: isDev ? viewer('/swagger-spec/transfer') : viewer('/transfer/swagger/doc.json'),
    healthUrl: isDev ? '/module-health/transfer' : null,
  },
]

const selectedModule = ref(null)
const moduleStatus = ref({})
const iframeError = ref(false)

const checkModuleStatus = async (mod) => {
  moduleStatus.value[mod.name] = 'checking'
  if (!mod.healthUrl) {
    moduleStatus.value[mod.name] = 'unknown'
    return
  }
  try {
    const resp = await fetch(mod.healthUrl, { signal: AbortSignal.timeout(3000) })
    moduleStatus.value[mod.name] = resp.ok ? 'online' : 'offline'
  } catch {
    moduleStatus.value[mod.name] = 'offline'
  }
}

const checkAllModules = () => {
  modules.forEach(mod => checkModuleStatus(mod))
}

onMounted(() => {
  checkAllModules()
})

const selectModule = (mod) => {
  selectedModule.value = mod
  iframeError.value = false
}

const openInNewTab = (url) => {
  window.open(url, '_blank')
}

const onIframeLoad = () => {
  iframeError.value = false
}

const onIframeError = () => {
  iframeError.value = true
}
</script>

<style scoped>
.api-docs-container {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
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
  color: var(--addp-text-primary);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.api-docs-body {
  flex: 1;
  display: flex;
  min-height: 0;
  overflow: hidden;
}

/* 左侧导航 */
.api-nav {
  width: 220px;
  flex-shrink: 0;
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
  background: var(--addp-bg-sidebar);
}

.module-item {
  padding: 12px 16px;
  cursor: pointer;
  border-left: 3px solid transparent;
  transition: all 0.2s;
  border-bottom: 1px solid var(--addp-border-color);
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
  margin-bottom: 4px;
}

.module-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.module-port {
  font-size: 11px;
  color: var(--addp-text-tertiary);
  font-family: monospace;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-dot.online {
  background: #67c23a;
}

.status-dot.offline {
  background: #f56c6c;
}

.status-dot.checking {
  background: #909399;
  animation: pulse 1s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

/* 右侧内容 */
.api-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
}

.swagger-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--addp-border-color);
  flex-shrink: 0;
  background: var(--addp-bg-primary);
}

.swagger-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--addp-text-primary);
}

.swagger-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.swagger-iframe {
  width: 100%;
  flex: 1;
  border: none;
  background: #fff;
}

/* 深色模式和蓝色模式下的 iframe 滤镜 */
html.dark .swagger-iframe,
html.blue .swagger-iframe {
  filter: invert(0.9) hue-rotate(180deg);
}

/* 修正图片和某些元素的反转 */
html.dark .swagger-iframe img,
html.blue .swagger-iframe img {
  filter: invert(1) hue-rotate(-180deg);
}

.error-state,
.welcome {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.welcome-icon {
  font-size: 64px;
  line-height: 1;
}

.help-content h4 {
  margin: 0 0 12px;
  color: var(--addp-text-primary);
}

.help-content p {
  margin: 6px 0;
  font-size: 13px;
  color: var(--addp-text-secondary);
}
</style>
