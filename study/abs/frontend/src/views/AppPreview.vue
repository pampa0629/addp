<template>
  <div class="preview-layout">
    <header class="preview-header">
      <div>
        <p class="preview-breadcrumb">应用中心 / {{ app?.name || '加载中' }}</p>
        <h1>{{ app?.name || '加载应用中…' }}</h1>
        <p class="preview-subtitle">
          {{ app?.description || 'AI 生成的应用入口，可在此运行、查看输出并继续迭代' }}
        </p>
      </div>
      <button class="btn-outline" @click="goBack">返回控制台</button>
    </header>

    <section v-if="loading" class="preview-loading">
      正在加载应用数据…
    </section>

    <section v-else-if="error" class="preview-error">
      {{ error }}
    </section>

    <section v-else-if="!app" class="preview-error">
      未找到指定应用，请返回控制台重试。
    </section>

    <section v-else class="preview-body">
      <!-- HTML应用：直接嵌入iframe -->
      <div v-if="isHTMLApp" class="game-frame-container">
        <h3>🎮 游戏运行中</h3>
        <iframe
          :src="`/api/workspace/${app.workspace_path}/index.html`"
          class="game-iframe"
          frameborder="0"
        ></iframe>
        <div class="game-controls">
          <button class="btn-outline" @click="reloadGame">重新加载游戏</button>
          <button class="btn-submit" @click="openInNewTab">在新标签页打开</button>
        </div>
      </div>

      <!-- 普通应用：显示运行状态 -->
      <div v-else class="preview-card">
        <div class="preview-card-header">
          <div>
            <p class="preview-label">状态</p>
            <span class="preview-status" :class="app.status">{{ getAppStatusLabel(app.status) }}</span>
          </div>
          <div class="preview-actions">
            <button class="btn-outline" :disabled="launching" @click="launchCurrentApp">
              {{ launching ? '启动中…' : '重新运行' }}
            </button>
            <button
              v-if="hasExternalEntry"
              class="btn-submit"
              @click="openExternalEntry"
            >
              打开真实入口
            </button>
          </div>
        </div>

        <div v-if="app.last_start_result" class="preview-log">
          <div class="preview-log-header">
            <h3>最新输出</h3>
            <span>{{ app.last_started_at ? formatDate(app.last_started_at) : '刚刚' }}</span>
          </div>
          <pre>{{ app.last_start_result }}</pre>
        </div>

        <div v-else class="preview-log empty">
          暂无运行输出，点击"重新运行"查看最新结果
        </div>

        <div class="preview-meta">
          <div>
            <p class="preview-label">启动命令</p>
            <code v-if="app.start_command?.length">{{ app.start_command.join(' ') }}</code>
            <span v-else>未配置启动命令</span>
          </div>
          <div>
            <p class="preview-label">Workspace</p>
            <code>{{ app.workspace_path || 'N/A' }}</code>
          </div>
          <div>
            <p class="preview-label">应用入口</p>
            <span v-if="hasExternalEntry">{{ app.entry_url }}</span>
            <span v-else>由 ABS 托管（当 AI 提供真实入口后会自动替换）</span>
          </div>
        </div>
      </div>

      <div class="preview-note">
        <h3>如何继续迭代？</h3>
        <div class="modify-section">
          <p class="modify-hint">
            在下方输入修改需求，AI 会基于当前代码进行增量修改并自动部署：
          </p>
          <div class="modify-input-group">
            <textarea
              v-model="modifyPrompt"
              placeholder="例如：添加一个显示时间的功能，或者：把背景颜色改成蓝色"
              class="modify-textarea"
              rows="3"
              :disabled="modifying"
            ></textarea>
            <button
              class="btn-submit modify-btn"
              @click="modifyApp"
              :disabled="!modifyPrompt.trim() || modifying"
            >
              {{ modifying ? '修改中...' : '🤖 AI 增量修改' }}
            </button>
          </div>
          <div v-if="modifyError" class="modify-error">
            {{ modifyError }}
          </div>
          <div v-if="modifySuccess" class="modify-success">
            ✅ 修改任务已提交，请在控制台查看进度或等待自动刷新
          </div>
        </div>

        <div v-if="app.modification_history && app.modification_history.length > 0" class="modification-history">
          <h4>修改历史</h4>
          <ul>
            <li v-for="(record, index) in app.modification_history" :key="index">
              <span class="history-time">{{ formatDate(record.timestamp) }}</span>
              <span class="history-prompt">{{ record.prompt }}</span>
              <span class="history-status" :class="{ success: record.success, failed: !record.success }">
                {{ record.success ? '✅' : '❌' }}
              </span>
            </li>
          </ul>
        </div>

        <h3>其他方式</h3>
        <ol>
          <li>回到控制台使用 AI 快速构建，描述你想要的改动。</li>
          <li>若 AI 生成 Web 服务，请在提示词中说明监听端口，并在 app_manifest.json 里写明 entry_url。</li>
          <li>新的构建会再次出现在应用中心，你可以在此查看输出或启动服务。</li>
        </ol>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useAppCenterStore } from '../store/appCenter'
import { appAPI } from '../api/client'

const props = defineProps({
  appId: {
    type: String,
    required: true
  }
})

const appCenterStore = useAppCenterStore()
const loading = ref(true)
const error = ref(null)
const modifyPrompt = ref('')
const modifying = ref(false)
const modifyError = ref(null)
const modifySuccess = ref(false)

// 支持通过 app.id 或 task_id 查找应用
const app = computed(() => {
  return appCenterStore.apps.find(app =>
    app.id === props.appId || app.task_id === props.appId
  )
})

const launching = computed(() => {
  const foundApp = app.value
  if (!foundApp) return false
  return appCenterStore.isLaunching(foundApp.id)
})

const hasExternalEntry = computed(() => {
  if (!app.value?.entry_url) return false
  return !isInternalPreviewLink(app.value.entry_url)
})

// 检测是否为 HTML 应用
const isHTMLApp = computed(() => {
  if (!app.value) return false
  // 检测标签或启动命令中包含 HTML 相关关键词
  const tags = (app.value.tags || []).join(' ').toLowerCase()
  const cmd = (app.value.start_command || []).join(' ').toLowerCase()
  return tags.includes('html') || cmd.includes('html app')
})

const iframeKey = ref(0)

const reloadGame = () => {
  iframeKey.value++
}

const openInNewTab = () => {
  if (!app.value?.workspace_path) return
  const url = `/api/workspace/${app.value.workspace_path}/index.html`
  window.open(url, '_blank', 'noopener,noreferrer')
}

const loadApp = async () => {
  loading.value = true
  error.value = null
  try {
    // 先加载应用列表，以便通过 task_id 或 app.id 查找
    await appCenterStore.fetchApps()

    // 如果仍然找不到应用，尝试通过 ID 直接获取
    if (!app.value) {
      try {
        await appCenterStore.fetchAppById(props.appId)
      } catch {
        // 如果直接获取也失败，显示错误
        error.value = `app ${props.appId} not found`
      }
    }
  } catch (err) {
    error.value = err.response?.data?.error || err.message
  } finally {
    loading.value = false
  }
}

const launchCurrentApp = async () => {
  error.value = null
  if (!app.value) {
    error.value = '应用未找到'
    return
  }
  try {
    await appCenterStore.launchApp(app.value.id)
  } catch (err) {
    error.value = err.response?.data?.error || err.message
  }
}

const modifyApp = async () => {
  if (!modifyPrompt.value.trim()) return
  if (!app.value) {
    modifyError.value = '应用未找到'
    return
  }

  modifying.value = true
  modifyError.value = null
  modifySuccess.value = false

  try {
    const response = await appAPI.modify(app.value.id, modifyPrompt.value.trim())
    modifySuccess.value = true
    modifyPrompt.value = ''

    // 等待 2 秒后开始轮询应用状态
    setTimeout(() => {
      const pollInterval = setInterval(async () => {
        await appCenterStore.fetchApps()
        // 可以添加更多逻辑：检测任务完成后停止轮询
      }, 3000)

      // 30秒后停止轮询
      setTimeout(() => clearInterval(pollInterval), 30000)
    }, 2000)
  } catch (err) {
    modifyError.value = err.response?.data?.error || err.message
  } finally {
    modifying.value = false
  }
}

const openExternalEntry = () => {
  if (!app.value?.entry_url) return
  window.open(app.value.entry_url, '_blank', 'noopener,noreferrer')
}

const goBack = () => {
  window.location.href = window.location.origin
}

const formatDate = (value) => {
  if (!value) return ''
  const date = new Date(value)
  return date.toLocaleString()
}

const isInternalPreviewLink = (url) => {
  if (!url) return false
  try {
    const parsed = new URL(url, window.location.origin)
    return parsed.origin === window.location.origin && parsed.searchParams.has('app')
  } catch {
    return false
  }
}

const getAppStatusLabel = (status) => {
  switch (status) {
    case 'running':
      return '运行中'
    case 'starting':
      return '启动中'
    case 'failed':
      return '启动失败'
    default:
      return '已注册'
  }
}

onMounted(loadApp)
</script>

<style scoped>
.preview-layout {
  min-height: 100vh;
  padding: 32px;
  background: linear-gradient(120deg, #f5f7ff 0%, #fafbff 100%);
  color: #1f1f2e;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 32px;
  gap: 16px;
}

.preview-header h1 {
  font-size: 36px;
  margin-bottom: 8px;
}

.preview-subtitle {
  color: #5a5a78;
}

.preview-breadcrumb {
  font-size: 13px;
  color: #8a8aa5;
  margin-bottom: 6px;
}

.preview-body {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.preview-card {
  background: white;
  border-radius: 16px;
  padding: 28px;
  box-shadow: 0 12px 40px rgba(15, 15, 50, 0.08);
}

.preview-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.preview-label {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: #7a7a92;
  margin-bottom: 4px;
}

.preview-status {
  font-weight: 700;
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 13px;
  text-transform: uppercase;
}

.preview-status.registered {
  background: #e8ecff;
  color: #3440a3;
}
.preview-status.running {
  background: #d4f7e6;
  color: #0f843f;
}
.preview-status.failed {
  background: #ffe8e8;
  color: #c84a4a;
}

.preview-actions {
  display: flex;
  gap: 12px;
}

.preview-log {
  background: #0f172a;
  color: #e2e8f0;
  border-radius: 12px;
  padding: 20px;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  line-height: 1.6;
  margin-bottom: 24px;
}

.preview-log.empty {
  background: #f5f7ff;
  color: #6b6b82;
  font-family: inherit;
}

.preview-log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-size: 14px;
  color: #a8b0d0;
}

.preview-meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
  font-size: 14px;
  color: #3a3a58;
}

.preview-meta code {
  background: #f1f3ff;
  padding: 6px 10px;
  border-radius: 6px;
  display: inline-block;
}

.preview-note {
  background: white;
  border-radius: 16px;
  padding: 24px;
  box-shadow: 0 10px 32px rgba(15, 15, 50, 0.06);
}

.preview-note ol {
  margin: 12px 0 0;
  padding-left: 18px;
  color: #4d4d6b;
  line-height: 1.8;
}

.preview-loading,
.preview-error {
  background: white;
  border-radius: 16px;
  padding: 32px;
  text-align: center;
  box-shadow: 0 12px 30px rgba(15, 15, 50, 0.08);
  color: #4f4f73;
}

/* HTML 应用/游戏的 iframe 样式 */
.game-frame-container {
  background: white;
  border-radius: 16px;
  padding: 24px;
  box-shadow: 0 12px 40px rgba(15, 15, 50, 0.08);
  margin-bottom: 24px;
}

.game-frame-container h3 {
  margin-bottom: 16px;
  color: #333;
}

.game-iframe {
  width: 100%;
  height: 700px;
  border-radius: 12px;
  border: 2px solid #e8ecff;
  background: #000;
  margin-bottom: 16px;
}

.game-controls {
  display: flex;
  gap: 12px;
  justify-content: center;
}

/* 修改功能样式 */
.modify-section {
  background: #f8f9ff;
  border-radius: 12px;
  padding: 20px;
  margin-bottom: 24px;
}

.modify-hint {
  color: #4d4d6b;
  margin-bottom: 12px;
  font-size: 14px;
}

.modify-input-group {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.modify-textarea {
  width: 100%;
  padding: 12px;
  border: 2px solid #e0e4f5;
  border-radius: 8px;
  font-family: inherit;
  font-size: 14px;
  resize: vertical;
  transition: border-color 0.2s;
}

.modify-textarea:focus {
  outline: none;
  border-color: #5a67d8;
}

.modify-textarea:disabled {
  background: #f5f5f5;
  cursor: not-allowed;
}

.modify-btn {
  align-self: flex-end;
  min-width: 180px;
}

.modify-error {
  background: #ffe8e8;
  color: #c84a4a;
  padding: 12px;
  border-radius: 8px;
  margin-top: 12px;
  font-size: 14px;
}

.modify-success {
  background: #d4f7e6;
  color: #0f843f;
  padding: 12px;
  border-radius: 8px;
  margin-top: 12px;
  font-size: 14px;
}

/* 修改历史样式 */
.modification-history {
  margin-top: 24px;
  padding-top: 24px;
  border-top: 1px solid #e8ecff;
}

.modification-history h4 {
  margin-bottom: 12px;
  color: #333;
}

.modification-history ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.modification-history li {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px;
  background: white;
  border-radius: 8px;
  margin-bottom: 8px;
  font-size: 13px;
}

.history-time {
  color: #8a8aa5;
  min-width: 150px;
  font-size: 12px;
}

.history-prompt {
  flex: 1;
  color: #4d4d6b;
}

.history-status {
  font-size: 16px;
}

.history-status.success {
  color: #0f843f;
}

.history-status.failed {
  color: #c84a4a;
}
</style>
