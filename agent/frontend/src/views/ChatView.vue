<template>
  <div class="chat-layout">
    <!-- 左侧会话列表 -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <el-button type="primary" size="small" @click="createSession" :icon="Plus">{{ t('agent.chat.newSession') }}</el-button>
      </div>
      <div class="session-list">
        <div
          v-for="session in sessions"
          :key="session.id"
          class="session-item"
          :class="{ active: currentSessionId === session.id }"
          @click="switchSession(session.id)"
        >
          <el-icon><ChatDotRound /></el-icon>
          <span class="session-title">{{ session.title }}</span>
          <el-button
            class="delete-btn"
            type="danger"
            link
            size="small"
            :icon="Delete"
            @click.stop="deleteSession(session.id)"
          />
        </div>
        <el-empty v-if="sessions.length === 0" :description="t('agent.chat.noSessions')" :image-size="60" />
      </div>
    </aside>

    <!-- 主聊天区域 -->
    <main class="chat-main">
      <div class="messages-area" ref="messagesAreaRef">
        <div v-if="messages.length === 0" class="empty-hint">
          <el-icon size="48" color="var(--el-text-color-secondary)"><ChatDotRound /></el-icon>
          <p>{{ t('agent.chat.welcome') }}</p>
          <div class="quick-actions">
            <el-tag
              v-for="hint in quickHints"
              :key="hint"
              class="quick-tag"
              @click="sendQuickMessage(hint)"
            >{{ hint }}</el-tag>
          </div>
        </div>

        <div v-for="msg in messages" :key="msg.id" class="message-row" :class="msg.role">
          <div class="message-bubble">
            <div v-if="msg.role === 'assistant'" class="message-content markdown" v-html="renderMarkdown(msg.content)" />
            <div v-else class="message-content">{{ msg.content }}</div>
            <div v-if="msg.dagData" class="dag-container">
              <DAGViewer :dag-data="msg.dagData" :height="400" />
            </div>
          </div>
        </div>

        <!-- 流式输出中的 AI 回复 -->
        <div v-if="isLoading" class="message-row assistant">
          <div class="message-bubble">
            <div class="message-content markdown" v-html="renderMarkdown(streamContent || '...')" />
            <div v-if="streamDagData" class="dag-container">
              <DAGViewer :dag-data="streamDagData" :height="400" />
            </div>
          </div>
        </div>
      </div>

      <!-- 输入区域 -->
      <div class="input-area">
        <el-input
          v-model="inputText"
          type="textarea"
          :rows="3"
          :placeholder="t('agent.chat.inputPlaceholder')"
          resize="none"
          @keydown.ctrl.enter="handleSend"
        />
        <el-button
          type="primary"
          :loading="isLoading"
          @click="handleSend"
          :icon="Position"
        >
          {{ t('agent.chat.send') }}
        </el-button>
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { Plus, Delete, ChatDotRound, Position } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import { useAuthStore } from '../store/auth'
import { sessionAPI } from '../api/index'
import { DAGViewer } from '@addp/common-frontend/dag'

const { t } = useI18n()
const authStore = useAuthStore()

const sessions = ref([])
const currentSessionId = ref(null)
const messages = ref([])
const inputText = ref('')
const isLoading = ref(false)
const streamContent = ref('')
const streamDagData = ref(null)
const messagesAreaRef = ref(null)

const quickHints = computed(() => [
  t('agent.quickHints.viewCatalog'),
  t('agent.quickHints.listSources'),
  t('agent.quickHints.importShapefile'),
  t('agent.quickHints.runSQL'),
])

function renderMarkdown(content) {
  return marked.parse(content || '')
}

async function loadSessions() {
  try {
    const list = await sessionAPI.list()
    sessions.value = list || []
  } catch (e) {
    console.error('加载会话列表失败', e)
  }
}

async function createSession() {
  try {
    const session = await sessionAPI.create({ title: null })
    sessions.value.unshift(session)
    await switchSession(session.id)
  } catch (e) {
    ElMessage.error(t('agent.chat.createFailed'))
  }
}

async function switchSession(sessionId) {
  currentSessionId.value = sessionId
  messages.value = []
  try {
    const list = await sessionAPI.getMessages(sessionId)
    messages.value = (list || []).map(m => ({
      ...m,
      dagData: m.result_type === 'dag' ? m.result_data : null
    }))
    scrollToBottom()
  } catch (e) {
    console.error('加载消息历史失败', e)
  }
}

async function deleteSession(sessionId) {
  try {
    await sessionAPI.delete(sessionId)
    sessions.value = sessions.value.filter(s => s.id !== sessionId)
    if (currentSessionId.value === sessionId) {
      currentSessionId.value = null
      messages.value = []
    }
  } catch (e) {
    ElMessage.error(t('agent.chat.deleteFailed'))
  }
}

async function sendQuickMessage(text) {
  inputText.value = text
  await handleSend()
}

async function handleSend() {
  const content = inputText.value.trim()
  if (!content || isLoading.value) return

  // 确保有会话
  if (!currentSessionId.value) {
    await createSession()
  }

  // 添加用户消息到界面
  const userMsg = { id: Date.now(), role: 'user', content }
  messages.value.push(userMsg)
  inputText.value = ''
  isLoading.value = true
  streamContent.value = ''
  streamDagData.value = null

  await nextTick()
  scrollToBottom()

  try {
    const response = await fetch('/api/v1/agent/chat', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${authStore.token}`,
      },
      body: JSON.stringify({
        session_id: currentSessionId.value,
        message: content,
      }),
    })

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}`)
    }

    // 读取流式响应
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let fullContent = ''
    let dagData = null

    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      const text = decoder.decode(value)
      const lines = text.split('\n').filter(l => l.trim())
      for (const line of lines) {
        try {
          if (line.startsWith('dag:')) {
            // DAG 事件
            dagData = JSON.parse(line.slice(4))
            streamDagData.value = dagData
            scrollToBottom()
          } else if (line.startsWith('0:')) {
            // 文本事件
            const chunk = JSON.parse(line.slice(2))
            fullContent += chunk
            streamContent.value = fullContent
            scrollToBottom()
          }
        } catch (e) {
          // 忽略解析错误
        }
      }
    }

    // 流结束，添加完整 AI 消息
    messages.value.push({
      id: Date.now() + 1,
      role: 'assistant',
      content: fullContent || t('agent.chat.workflowGenerated'),
      result_type: dagData ? 'dag' : 'text',
      dagData: dagData,
    })
    streamContent.value = ''
    streamDagData.value = null

    // 更新会话标题
    const session = sessions.value.find(s => s.id === currentSessionId.value)
    if (session && !session.title) {
      session.title = content.slice(0, 30)
    }
  } catch (e) {
    ElMessage.error(t('agent.chat.sendFailed', { msg: e.message }))
    messages.value.push({
      id: Date.now() + 1,
      role: 'assistant',
      content: t('agent.chat.errorReply'),
      result_type: 'error',
    })
  } finally {
    isLoading.value = false
    scrollToBottom()
  }
}

function scrollToBottom() {
  nextTick(() => {
    const el = messagesAreaRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

onMounted(async () => {
  await loadSessions()
  if (sessions.value.length > 0) {
    await switchSession(sessions.value[0].id)
  }
})
</script>

<style scoped>
.chat-layout {
  display: flex;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.sidebar {
  width: 240px;
  background: var(--addp-bg-primary);
  border-right: 1px solid var(--el-border-color);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid var(--el-border-color);
}

.sidebar-header .el-button {
  width: 100%;
}

.session-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.session-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  position: relative;
  transition: background 0.2s;
}

.session-item:hover {
  background: var(--el-fill-color-light);
}

.session-item.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.session-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.delete-btn {
  opacity: 0;
  transition: opacity 0.2s;
}

.session-item:hover .delete-btn {
  opacity: 1;
}

.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.messages-area {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.empty-hint {
  text-align: center;
  margin: auto;
  color: var(--el-text-color-secondary);
}

.empty-hint p {
  margin: 16px 0;
  font-size: 15px;
}

.quick-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  margin-top: 16px;
}

.quick-tag {
  cursor: pointer;
}

.message-row {
  display: flex;
}

.message-row.user {
  justify-content: flex-end;
}

.message-row.assistant {
  justify-content: flex-start;
}

.message-bubble {
  max-width: 72%;
  padding: 12px 16px;
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.6;
}

.message-row.user .message-bubble {
  background: var(--el-color-primary);
  color: #fff;
  border-bottom-right-radius: 4px;
}

.message-row.assistant .message-bubble {
  background: var(--addp-bg-primary);
  border: 1px solid var(--el-border-color);
  border-bottom-left-radius: 4px;
}

.message-content.markdown :deep(p) {
  margin: 0 0 8px;
}

.message-content.markdown :deep(p:last-child) {
  margin-bottom: 0;
}

.message-content.markdown :deep(code) {
  background: var(--el-fill-color);
  padding: 2px 4px;
  border-radius: 3px;
  font-family: monospace;
}

.message-content.markdown :deep(pre) {
  background: var(--el-fill-color-darker);
  padding: 12px;
  border-radius: 6px;
  overflow-x: auto;
}

.dag-container {
  margin-top: 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.input-area {
  padding: 16px 20px;
  background: var(--addp-bg-primary);
  border-top: 1px solid var(--el-border-color);
  display: flex;
  gap: 12px;
  align-items: flex-end;
}

.input-area .el-textarea {
  flex: 1;
}

.input-area .el-button {
  align-self: flex-end;
  height: 64px;
}
</style>
