<template>
  <div class="floating-input" :class="{ expanded: isExpanded, minimized: isMinimized }">
    <!-- Minimized state -->
    <div v-if="isMinimized" class="minimized-bar" @click="restore">
      <div class="minimized-content">
        <span class="icon">🤖</span>
        <span class="text">AI 快速构建</span>
      </div>
    </div>

    <!-- Expanded state -->
    <div v-else class="input-container">
      <div class="header">
        <span class="title">🤖 AI 快速构建</span>
        <div class="controls">
          <button @click="minimize" class="btn-control" title="最小化">−</button>
          <button @click="toggleExpand" class="btn-control" title="切换大小">⬜</button>
        </div>
      </div>

      <div class="input-area">
        <textarea
          v-model="prompt"
          placeholder="描述你想构建的内容…（例如：创建带有用户接口的 REST API 服务）"
          @keydown.ctrl.enter="submit"
          @keydown.meta.enter="submit"
          :disabled="loading"
        ></textarea>
      </div>

      <div class="actions">
        <button @click="submit" :disabled="!prompt.trim() || loading" class="btn-submit">
          {{ loading ? '生成中…' : '生成并部署' }}
        </button>
        <button @click="clear" :disabled="loading" class="btn-clear">清空</button>
      </div>

      <div v-if="error" class="error-message">
        {{ error }}
      </div>

      <div v-if="currentTask" class="task-status">
        <div class="status-header">
          <span class="status-badge" :class="currentTask.status">
            {{ getStatusLabel(currentTask.status) }}
          </span>
          <span class="task-id">{{ currentTask.id.substring(0, 8) }}</span>
        </div>

        <div v-if="currentTask.logs && currentTask.logs.length > 0" class="logs">
          <div v-for="(log, index) in currentTask.logs" :key="index" class="log-entry">
            {{ log }}
          </div>
        </div>

        <div v-if="currentTask.status === 'completed'" class="success-message">
          ✅ 代码生成并部署成功！
        </div>

        <div v-if="currentTask.status === 'failed' && currentTask.error" class="error-details">
          <div class="error-title">❌ 任务失败</div>
          <div class="error-content">{{ currentTask.error }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useTaskStore } from '../store/task'
import { getStatusLabel } from '../utils/status'

const taskStore = useTaskStore()

const prompt = ref('')
const isExpanded = ref(false)
const isMinimized = ref(false)

const loading = computed(() => taskStore.loading)
const error = computed(() => taskStore.error)
const currentTask = computed(() => taskStore.currentTask)

const submit = async () => {
  if (!prompt.value.trim() || loading.value) return

  try {
    await taskStore.createTask(prompt.value)
    // Don't clear prompt on success - let user see what they submitted
  } catch (error) {
    console.error('Failed to create task:', error)
  }
}

const clear = () => {
  prompt.value = ''
  taskStore.error = null
}

const toggleExpand = () => {
  isExpanded.value = !isExpanded.value
}

const minimize = () => {
  isMinimized.value = true
  isExpanded.value = false
}

const restore = () => {
  isMinimized.value = false
}
</script>

<style scoped>
.floating-input {
  position: fixed;
  bottom: 20px;
  right: 20px;
  background: white;
  border-radius: 12px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
  z-index: 9999;
  transition: all 0.3s ease;
  width: 400px;
}

.floating-input.expanded {
  width: 600px;
  max-height: 70vh;
}

.floating-input.minimized {
  width: auto;
  bottom: 0;
  right: 0;
  border-radius: 12px 12px 0 0;
}

.minimized-bar {
  padding: 12px 24px;
  cursor: pointer;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border-radius: 12px 12px 0 0;
  user-select: none;
}

.minimized-content {
  display: flex;
  align-items: center;
  gap: 8px;
  color: white;
  font-weight: 600;
}

.minimized-content .icon {
  font-size: 20px;
}

.input-container {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-bottom: 12px;
  border-bottom: 2px solid #f0f0f0;
}

.title {
  font-size: 18px;
  font-weight: 700;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.controls {
  display: flex;
  gap: 8px;
}

.btn-control {
  width: 28px;
  height: 28px;
  border: none;
  background: #f5f5f5;
  border-radius: 6px;
  cursor: pointer;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.btn-control:hover {
  background: #e0e0e0;
}

.input-area textarea {
  width: 100%;
  min-height: 100px;
  padding: 12px;
  border: 2px solid #e0e0e0;
  border-radius: 8px;
  font-size: 14px;
  font-family: inherit;
  resize: vertical;
  transition: border-color 0.2s;
}

.expanded .input-area textarea {
  min-height: 150px;
}

.input-area textarea:focus {
  outline: none;
  border-color: #667eea;
}

.input-area textarea:disabled {
  background: #f5f5f5;
  cursor: not-allowed;
}

.actions {
  display: flex;
  gap: 8px;
}

.btn-submit,
.btn-clear {
  padding: 10px 20px;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
}

.btn-submit {
  flex: 1;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

.btn-submit:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
}

.btn-submit:disabled {
  opacity: 0.6;
  cursor: not-allowed;
  transform: none;
}

.btn-clear {
  background: #f5f5f5;
  color: #666;
}

.btn-clear:hover:not(:disabled) {
  background: #e0e0e0;
}

.error-message {
  padding: 12px;
  background: #fee;
  border: 1px solid #fcc;
  border-radius: 8px;
  color: #c33;
  font-size: 14px;
}

.task-status {
  padding: 12px;
  background: #f9f9f9;
  border-radius: 8px;
  font-size: 13px;
}

.status-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.status-badge {
  padding: 4px 12px;
  border-radius: 12px;
  font-weight: 600;
  text-transform: uppercase;
  font-size: 11px;
}

.status-badge.pending {
  background: #fff3cd;
  color: #856404;
}

.status-badge.processing {
  background: #cce5ff;
  color: #004085;
}

.status-badge.compiling {
  background: #d1ecf1;
  color: #0c5460;
}

.status-badge.deploying {
  background: #d4edda;
  color: #155724;
}

.status-badge.completed {
  background: #d4edda;
  color: #155724;
}

.status-badge.failed {
  background: #f8d7da;
  color: #721c24;
}

.task-id {
  font-family: monospace;
  color: #666;
  font-size: 12px;
}

.logs {
  max-height: 150px;
  overflow-y: auto;
  background: #fff;
  padding: 8px;
  border-radius: 6px;
  margin-top: 8px;
}

.log-entry {
  padding: 4px 0;
  font-family: monospace;
  font-size: 12px;
  color: #333;
  border-bottom: 1px solid #f0f0f0;
}

.log-entry:last-child {
  border-bottom: none;
}

.success-message {
  margin-top: 8px;
  padding: 8px;
  background: #d4edda;
  color: #155724;
  border-radius: 6px;
  font-weight: 600;
}

.error-details {
  margin-top: 8px;
  padding: 12px;
  background: #f8d7da;
  border: 2px solid #f5c6cb;
  border-radius: 6px;
  font-size: 13px;
}

.error-title {
  font-weight: 700;
  color: #721c24;
  margin-bottom: 8px;
}

.error-content {
  color: #721c24;
  line-height: 1.6;
  word-break: break-word;
  white-space: pre-wrap;
  max-height: 200px;
  overflow-y: auto;
}
</style>
