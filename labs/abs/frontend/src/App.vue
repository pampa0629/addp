<template>
  <AppPreview v-if="previewAppId" :app-id="previewAppId" />

  <div v-else id="app">
    <div class="container">
      <header class="header">
        <h1>🤖 AI 启动系统</h1>
        <p class="subtitle">描述你的需求，AI 即刻为你构建</p>
      </header>

      <main class="main">
        <div class="welcome-section">
          <div class="feature-card">
            <div class="feature-icon">💡</div>
            <h3>自然语言输入</h3>
            <p>只需用自然语言描述你想构建的内容</p>
          </div>

          <div class="feature-card">
            <div class="feature-icon">🤖</div>
            <h3>AI 代码生成</h3>
            <p>Codex AI 生成可直接上线的代码</p>
          </div>

          <div class="feature-card">
            <div class="feature-icon">🚀</div>
            <h3>自动部署</h3>
            <p>自动编译并部署你的应用程序</p>
          </div>
        </div>

        <div v-if="tasks.length > 0" class="tasks-section">
          <h2>最近任务</h2>
          <div class="task-list">
            <div
              v-for="task in tasks"
              :key="task.id"
              class="task-card"
              @click="selectTask(task)"
              :class="{ active: currentTask?.id === task.id }"
            >
              <div class="task-header">
                <span class="status-dot" :class="task.status"></span>
                <span class="task-time">{{ formatTime(task.created_at) }}</span>
              </div>
              <div class="task-prompt">{{ task.prompt }}</div>

              <!-- Error display for failed tasks -->
              <div v-if="task.status === 'failed' && task.error" class="task-error">
                ❌ {{ task.error }}
              </div>

              <div class="task-meta">
                <span class="task-status" :class="task.status">{{ getStatusLabel(task.status) }}</span>
                <span class="task-id">{{ task.id.substring(0, 8) }}</span>
              </div>
            </div>
          </div>
        </div>

        <div v-else class="empty-state">
          <div class="empty-icon">📝</div>
          <h3>暂时还没有任务</h3>
          <p>使用下方的浮动输入框创建你的第一个 AI 应用</p>
        </div>

        <section class="app-center-section">
          <div class="section-header">
            <div>
              <h2>应用中心</h2>
              <p class="section-subtitle">注册 AI 生成的应用入口，统一调度启动并直接访问</p>
            </div>
            <button class="btn-primary" @click="toggleAppForm">
              {{ showAppForm ? '收起表单' : '注册应用' }}
            </button>
          </div>

          <div v-if="appsLoading" class="app-loading">正在加载应用列表...</div>

          <div v-else-if="apps.length > 0" class="app-grid">
            <div v-for="app in apps" :key="app.id" class="app-card">
              <div class="app-card-header">
                <div class="app-icon" v-if="app.icon">{{ app.icon }}</div>
                <div class="app-meta">
                  <h3>{{ app.name }}</h3>
                  <p>{{ app.description || '暂无描述' }}</p>
                </div>
                <div class="app-actions">
                  <span class="app-status" :class="app.status">
                    {{ getAppStatusLabel(app.status) }}
                  </span>
                  <button
                    class="btn-delete"
                    @click="deleteApp(app.id)"
                    title="删除应用及其工作目录"
                  >
                    🗑️
                  </button>
                </div>
              </div>

              <div class="app-tags" v-if="app.tags?.length">
                <span v-for="tag in app.tags" :key="`${app.id}-${tag}`">
                  {{ tag }}
                </span>
              </div>

              <div class="app-footer">
                <div class="app-links">
                  <button
                    class="btn-outline"
                    :disabled="isLaunchingApp(app.id)"
                    @click="launchApp(app.id)"
                  >
                    {{ isLaunchingApp(app.id) ? '启动中…' : '启动服务' }}
                  </button>
                  <button
                    class="btn-submit"
                    @click="openApp(app)"
                  >
                    进入应用
                  </button>
                </div>
                <div class="app-log" v-if="app.last_start_result">
                  {{ app.last_start_result }}
                </div>
              </div>
            </div>
          </div>

          <div v-else class="empty-state secondary">
            <div class="empty-icon">🚀</div>
            <h3>还没有注册任何应用</h3>
            <p>使用“注册应用”按钮，填写入口 URL 与工作目录，即可在此管理</p>
          </div>

          <div v-if="showAppForm" class="app-form-card">
            <h3>注册新的应用入口</h3>
            <form class="app-form" @submit.prevent="submitNewApp">
              <div class="form-grid">
                <label>
                  <span>应用名称 *</span>
                  <input v-model="newAppForm.name" type="text" placeholder="例如：打飞机游戏" required />
                </label>

                <label>
                  <span>入口 URL *</span>
                  <input v-model="newAppForm.entryUrl" type="url" placeholder="http://localhost:9000" required />
                </label>

                <label>
                  <span>分类</span>
                  <input v-model="newAppForm.category" type="text" placeholder="游戏 / 实用工具" />
                </label>

                <label>
                  <span>图标（可输入 emoji）</span>
                  <input v-model="newAppForm.icon" type="text" placeholder="✈️" maxlength="4" />
                </label>

                <label>
                  <span>关联 Task ID</span>
                  <input v-model="newAppForm.taskId" type="text" placeholder="可选，用于回溯生成记录" />
                </label>

                <label>
                  <span>工作目录 / workspace</span>
                  <input v-model="newAppForm.workspacePath" type="text" placeholder="task-id 或相对路径" />
                </label>
              </div>

              <label>
                <span>应用描述</span>
                <textarea v-model="newAppForm.description" rows="3" placeholder="简要介绍应用能力"></textarea>
              </label>

              <label>
                <span>启动命令（以空格分隔，可使用引号包裹参数）</span>
                <input v-model="newAppForm.startCommand" type="text" placeholder='例如：npm run dev 或 "python3" "main.py"' />
              </label>

              <label>
                <span>标签（逗号分隔）</span>
                <input v-model="newAppForm.tags" type="text" placeholder="demo, shooter, game" />
              </label>

              <div class="form-actions">
                <button type="submit" class="btn-submit" :disabled="creatingApp">
                  {{ creatingApp ? '保存中…' : '保存应用' }}
                </button>
                <button type="button" class="btn-clear" @click="resetAppForm">取消</button>
              </div>
            </form>

            <div v-if="appStoreError" class="error-message">
              {{ appStoreError }}
            </div>
          </div>
        </section>
      </main>
    </div>

    <FloatingInput />

    <!-- App Modification Dialog -->
    <div v-if="showModifyDialog" class="modal-overlay" @click="closeModifyDialog">
      <div class="modal-content" @click.stop>
        <div class="modal-header">
          <h2>🤖 AI 修改应用</h2>
          <button class="btn-close" @click="closeModifyDialog">✕</button>
        </div>

        <div class="modal-body">
          <div class="app-info">
            <span class="app-icon" v-if="modifyingApp?.icon">{{ modifyingApp.icon }}</span>
            <div>
              <h3>{{ modifyingApp?.name }}</h3>
              <p>{{ modifyingApp?.description || '暂无描述' }}</p>
            </div>
          </div>

          <div class="modify-input-section">
            <label>
              <span class="label-text">描述你想要的修改：</span>
              <textarea
                v-model="modifyPrompt"
                placeholder="例如：把背景颜色改成蓝色，或者：添加一个显示时间的功能"
                rows="5"
                :disabled="modifying"
                class="modify-textarea"
              ></textarea>
            </label>
          </div>

          <div v-if="modifyError" class="error-message">
            {{ modifyError }}
          </div>

          <div v-if="modifySuccess" class="success-message">
            ✅ 修改任务已提交！正在处理中，请稍后查看应用中心...
          </div>
        </div>

        <div class="modal-footer">
          <button class="btn-clear" @click="closeModifyDialog" :disabled="modifying">
            取消
          </button>
          <button
            class="btn-submit"
            @click="submitModification"
            :disabled="!modifyPrompt.trim() || modifying"
          >
            {{ modifying ? '提交中...' : '🚀 提交修改' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useTaskStore } from './store/task'
import { useAppCenterStore } from './store/appCenter'
import FloatingInput from './components/FloatingInput.vue'
import wsService from './api/websocket'
import { getStatusLabel } from './utils/status'
import AppPreview from './views/AppPreview.vue'
import { appAPI } from './api/client'

const taskStore = useTaskStore()
const appCenterStore = useAppCenterStore()
let refreshAppsTimer = null
const urlParams = new URLSearchParams(window.location.search)
const previewAppId = urlParams.get('app')

const tasks = computed(() => taskStore.tasks)
const currentTask = computed(() => taskStore.currentTask)
const apps = computed(() => appCenterStore.apps)
const appsLoading = computed(() => appCenterStore.loading)
const creatingApp = computed(() => appCenterStore.creating)
const appStoreError = computed(() => appCenterStore.error)

const selectTask = (task) => {
  taskStore.setCurrentTask(task)
}

const showAppForm = ref(false)
const newAppForm = reactive({
  name: '',
  description: '',
  category: '',
  entryUrl: '',
  icon: '',
  taskId: '',
  workspacePath: '',
  startCommand: '',
  tags: ''
})

// App modification state
const showModifyDialog = ref(false)
const modifyingApp = ref(null)
const modifyPrompt = ref('')
const modifying = ref(false)
const modifyError = ref('')
const modifySuccess = ref(false)

const toggleAppForm = () => {
  showAppForm.value = !showAppForm.value
}

const resetAppForm = () => {
  newAppForm.name = ''
  newAppForm.description = ''
  newAppForm.category = ''
  newAppForm.entryUrl = ''
  newAppForm.icon = ''
  newAppForm.taskId = ''
  newAppForm.workspacePath = ''
  newAppForm.startCommand = ''
  newAppForm.tags = ''
  appCenterStore.error = null
  showAppForm.value = false
}

const parseCommand = (value) => {
  if (!value) return []
  const matches = value.match(/"[^"]+"|'[^']+'|\S+/g) || []
  return matches.map((token) => token.replace(/^['"]|['"]$/g, ''))
}

const submitNewApp = async () => {
  if (!newAppForm.name.trim() || !newAppForm.entryUrl.trim()) {
    return
  }
  try {
    await appCenterStore.registerApp({
      name: newAppForm.name.trim(),
      description: newAppForm.description.trim(),
      category: newAppForm.category.trim(),
      entry_url: newAppForm.entryUrl.trim(),
      icon: newAppForm.icon.trim(),
      task_id: newAppForm.taskId.trim(),
      workspace_path: newAppForm.workspacePath.trim(),
      start_command: parseCommand(newAppForm.startCommand),
      tags: newAppForm.tags
        .split(',')
        .map((tag) => tag.trim())
        .filter(Boolean)
    })
    resetAppForm()
  } catch (error) {
    console.error('注册应用失败：', error)
  }
}

const isLaunchingApp = (id) => appCenterStore.isLaunching(id)

const launchApp = async (id) => {
  try {
    await appCenterStore.launchApp(id)
  } catch (error) {
    console.error('启动应用失败：', error)
  }
}

const openApp = (app) => {
  if (!app?.id) return
  // Open preview page in a new tab with app query parameter
  const url = `${window.location.origin}/?app=${app.id}`
  window.open(url, '_blank')
}

const closeModifyDialog = () => {
  showModifyDialog.value = false
  modifyingApp.value = null
  modifyPrompt.value = ''
  modifyError.value = ''
  modifySuccess.value = false
}

const submitModification = async () => {
  if (!modifyPrompt.value.trim() || !modifyingApp.value) return

  modifying.value = true
  modifyError.value = ''
  modifySuccess.value = false

  try {
    await appAPI.modify(modifyingApp.value.id, modifyPrompt.value.trim())
    modifySuccess.value = true
    modifyPrompt.value = ''

    // Auto-close after 2 seconds and refresh apps
    setTimeout(() => {
      closeModifyDialog()
      appCenterStore.fetchApps()
    }, 2000)
  } catch (error) {
    modifyError.value = error.response?.data?.error || error.message || '修改失败'
  } finally {
    modifying.value = false
  }
}

const deleteApp = async (id) => {
  if (!confirm('确定要删除此应用吗？此操作将删除应用及其工作目录，无法恢复！')) {
    return
  }

  try {
    await appCenterStore.deleteApp(id)
    // 可选：显示成功提示
    console.log('应用已成功删除')
  } catch (error) {
    console.error('删除应用失败：', error)
    alert(`删除失败: ${error.message || error}`)
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

const formatTime = (timestamp) => {
  const date = new Date(timestamp)
  const now = new Date()
  const diffMs = now - date
  const diffMins = Math.floor(diffMs / 60000)

  if (diffMins < 1) return '刚刚'
  if (diffMins < 60) return `${diffMins} 分钟前`

  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours} 小时前`

  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays} 天前`
}

const handleWebSocketMessage = (data) => {
  taskStore.handleProgressUpdate(data)
  if (data?.status === 'completed') {
    if (refreshAppsTimer) {
      clearTimeout(refreshAppsTimer)
    }
    refreshAppsTimer = setTimeout(() => {
      appCenterStore.fetchApps()
    }, 600)
  }
}

onMounted(() => {
  // Fetch initial tasks
  taskStore.fetchTasks()
  appCenterStore.fetchApps()

  // Connect WebSocket
  wsService.connect()
  wsService.addListener(handleWebSocketMessage)
})

onUnmounted(() => {
  if (refreshAppsTimer) {
    clearTimeout(refreshAppsTimer)
  }
  wsService.removeListener(handleWebSocketMessage)
  wsService.disconnect()
})
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  min-height: 100vh;
}

#app {
  min-height: 100vh;
  padding: 20px;
}

.container {
  max-width: 1200px;
  margin: 0 auto;
}

.header {
  text-align: center;
  color: white;
  margin-bottom: 40px;
}

.header h1 {
  font-size: 48px;
  font-weight: 800;
  margin-bottom: 12px;
}

.subtitle {
  font-size: 20px;
  opacity: 0.95;
}

.main {
  background: white;
  border-radius: 16px;
  padding: 40px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.1);
  min-height: 500px;
}

.welcome-section {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 24px;
  margin-bottom: 40px;
}

.feature-card {
  text-align: center;
  padding: 32px 24px;
  background: linear-gradient(135deg, #f5f7fa 0%, #c3cfe2 100%);
  border-radius: 12px;
  transition: transform 0.2s;
}

.feature-card:hover {
  transform: translateY(-4px);
}

.feature-icon {
  font-size: 48px;
  margin-bottom: 16px;
}

.feature-card h3 {
  font-size: 20px;
  margin-bottom: 8px;
  color: #333;
}

.feature-card p {
  color: #666;
  font-size: 14px;
}

.tasks-section {
  margin-top: 40px;
}

.tasks-section h2 {
  font-size: 24px;
  margin-bottom: 20px;
  color: #333;
}

.task-list {
  display: grid;
  gap: 16px;
}

.task-card {
  padding: 20px;
  border: 2px solid #e0e0e0;
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s;
}

.task-card:hover {
  border-color: #667eea;
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.2);
}

.task-card.active {
  border-color: #667eea;
  background: #f9f9ff;
}

.task-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.status-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  display: inline-block;
}

.status-dot.pending { background: #ffc107; }
.status-dot.processing { background: #2196f3; }
.status-dot.compiling { background: #00bcd4; }
.status-dot.deploying { background: #4caf50; }
.status-dot.completed { background: #4caf50; }
.status-dot.failed { background: #f44336; }

.task-time {
  font-size: 12px;
  color: #999;
}

.task-prompt {
  font-size: 16px;
  color: #333;
  margin-bottom: 12px;
  line-height: 1.5;
}

.task-error {
  background: #fee;
  border: 2px solid #fcc;
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 12px;
  color: #c33;
  font-size: 14px;
  line-height: 1.6;
  word-break: break-word;
  max-height: 200px;
  overflow-y: auto;
}

.task-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.task-status {
  padding: 4px 12px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
}

.task-status.pending { background: #fff3cd; color: #856404; }
.task-status.processing { background: #cce5ff; color: #004085; }
.task-status.compiling { background: #d1ecf1; color: #0c5460; }
.task-status.deploying { background: #d4edda; color: #155724; }
.task-status.completed { background: #d4edda; color: #155724; }
.task-status.failed { background: #f8d7da; color: #721c24; }

.task-id {
  font-family: monospace;
  font-size: 12px;
  color: #999;
}

.empty-state {
  text-align: center;
  padding: 80px 20px;
}

.empty-icon {
  font-size: 80px;
  margin-bottom: 20px;
}

.empty-state h3 {
  font-size: 24px;
  color: #333;
  margin-bottom: 12px;
}

.empty-state p {
  font-size: 16px;
  color: #666;
}

.empty-state.secondary {
  border: 2px dashed #d6daff;
  border-radius: 16px;
}

.app-center-section {
  margin-top: 48px;
  padding-top: 32px;
  border-top: 1px solid #f0f0f0;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.section-subtitle {
  color: #666;
  margin-top: 4px;
}

.btn-primary {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  border: none;
  padding: 12px 20px;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 600;
  transition: opacity 0.2s;
}

.btn-primary:hover {
  opacity: 0.9;
}

.btn-submit {
  border: none;
  background: linear-gradient(135deg, #7f7fd5 0%, #86a8e7 50%, #91eae4 100%);
  color: white;
  padding: 10px 18px;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 600;
  transition: opacity 0.2s;
}

.btn-submit:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn-clear {
  border: none;
  background: transparent;
  color: #888;
  font-weight: 600;
  cursor: pointer;
}

.btn-outline {
  background: transparent;
  border: 2px solid #764ba2;
  color: #764ba2;
  padding: 10px 18px;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 600;
  transition: all 0.2s;
}

.btn-outline:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-modify {
  background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
  color: white;
  border: none;
  padding: 10px 18px;
  border-radius: 8px;
  cursor: pointer;
  font-weight: 600;
  transition: all 0.2s;
}

.btn-modify:hover {
  opacity: 0.9;
  transform: translateY(-1px);
}

.btn-modify:active {
  transform: translateY(0);
}

.app-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 20px;
}

.app-card {
  border: 2px solid #f0f0f0;
  border-radius: 16px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: #fff;
}

.app-card-header {
  display: flex;
  gap: 12px;
  align-items: center;
}

.app-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: #f5f5ff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
}

.app-meta {
  flex: 1;
}

.app-meta h3 {
  margin-bottom: 4px;
  color: #333;
}

.app-meta p {
  margin: 0;
  color: #555;
  font-size: 14px;
}

.app-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

.app-status {
  font-size: 12px;
  font-weight: 700;
  padding: 6px 12px;
  border-radius: 999px;
  text-transform: uppercase;
}

.btn-delete {
  background: none;
  border: none;
  font-size: 20px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: all 0.2s;
  opacity: 0.6;
}

.btn-delete:hover {
  opacity: 1;
  background: #fee;
  transform: scale(1.1);
}

.btn-delete:active {
  transform: scale(0.95);
}

.app-status.registered {
  background: #e0e7ff;
  color: #3f51b5;
}

.app-status.starting {
  background: #fff3cd;
  color: #856404;
}

.app-status.running {
  background: #d4edda;
  color: #155724;
}

.app-status.failed {
  background: #f8d7da;
  color: #721c24;
}

.app-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.app-tags span {
  background: #f0f0f5;
  color: #555;
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 999px;
}

.app-footer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.app-links {
  display: flex;
  gap: 8px;
}

.app-links .btn-submit {
  flex: 1;
}

.app-links .btn-outline {
  flex: 1;
}

.app-log {
  font-size: 12px;
  color: #666;
  line-height: 1.4;
}

.app-loading {
  padding: 40px;
  text-align: center;
  color: #666;
}

.app-form-card {
  margin-top: 32px;
  background: #f9f9ff;
  border-radius: 16px;
  padding: 24px;
  border: 2px dashed #d6daff;
}

.app-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
}

.app-form label {
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 14px;
  color: #444;
}

.app-form input,
.app-form textarea {
  border: 1px solid #dcdcdc;
  border-radius: 8px;
  padding: 10px 12px;
  font-size: 14px;
  font-family: inherit;
}

.app-form textarea {
  resize: vertical;
}

.form-actions {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
}

/* Modal Styles */
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  padding: 20px;
}

.modal-content {
  background: white;
  border-radius: 16px;
  max-width: 600px;
  width: 100%;
  max-height: 85vh;
  overflow-y: auto;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 24px;
  border-bottom: 2px solid #f0f0f0;
}

.modal-header h2 {
  margin: 0;
  font-size: 24px;
  color: #333;
}

.btn-close {
  background: none;
  border: none;
  font-size: 28px;
  cursor: pointer;
  color: #999;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.btn-close:hover {
  background: #f5f5f5;
  color: #333;
}

.modal-body {
  padding: 24px;
}

.app-info {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  background: linear-gradient(135deg, #f5f7fa 0%, #c3cfe2 100%);
  border-radius: 12px;
  margin-bottom: 24px;
}

.app-info .app-icon {
  width: 56px;
  height: 56px;
  font-size: 32px;
}

.app-info h3 {
  margin: 0 0 4px 0;
  font-size: 18px;
  color: #333;
}

.app-info p {
  margin: 0;
  font-size: 14px;
  color: #666;
}

.modify-input-section {
  margin-bottom: 16px;
}

.label-text {
  display: block;
  margin-bottom: 8px;
  font-weight: 600;
  color: #333;
  font-size: 15px;
}

.modify-textarea {
  width: 100%;
  padding: 12px;
  border: 2px solid #e0e0e0;
  border-radius: 8px;
  font-size: 14px;
  font-family: inherit;
  resize: vertical;
  transition: border-color 0.2s;
  line-height: 1.6;
}

.modify-textarea:focus {
  outline: none;
  border-color: #667eea;
}

.modify-textarea:disabled {
  background: #f5f5f5;
  cursor: not-allowed;
}

.modal-footer {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  padding: 20px 24px;
  border-top: 2px solid #f0f0f0;
}

.error-message {
  padding: 12px;
  background: #fee;
  border: 1px solid #fcc;
  border-radius: 8px;
  color: #c33;
  font-size: 14px;
  margin-bottom: 12px;
}

.success-message {
  padding: 12px;
  background: #d4edda;
  border: 1px solid #c3e6cb;
  border-radius: 8px;
  color: #155724;
  font-size: 14px;
  margin-bottom: 12px;
}
</style>
