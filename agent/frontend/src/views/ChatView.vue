<template>
  <div class="chat-layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <el-button type="primary" size="small" :icon="Plus" @click="createSession">
          {{ t('agent.chat.newSession') }}
        </el-button>
      </div>
      <div class="session-list">
        <button
          v-for="session in sessions"
          :key="session.id"
          type="button"
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
        </button>
        <el-empty
          v-if="sessions.length === 0"
          :description="t('agent.chat.noSessions')"
          :image-size="60"
        />
      </div>
    </aside>

    <main class="chat-main">
      <div ref="messagesAreaRef" class="messages-area">
        <div v-if="messages.length === 0 && !liveMessage" class="empty-hint">
          <el-icon size="48"><ChatDotRound /></el-icon>
          <p>{{ t('agent.chat.welcome') }}</p>
          <div class="quick-actions">
            <el-tag
              v-for="hint in quickHints"
              :key="hint"
              class="quick-tag"
              @click="sendQuickMessage(hint)"
            >
              {{ hint }}
            </el-tag>
          </div>
        </div>

        <div
          v-for="message in messages"
          :key="message.protocol_message_id || message.id"
          class="message-row"
          :class="message.role"
        >
          <div class="message-bubble">
            <MessagePartsRenderer
              :message="message"
              @action="handleA2UIAction"
              @error="handlePresentationError"
            />
          </div>
        </div>

        <div v-if="liveMessage" class="message-row assistant">
          <div class="message-bubble live-message">
            <div v-if="liveTools.length" class="tool-trace">
              <div v-for="tool in liveTools" :key="tool.id" class="tool-trace-item">
                <el-icon :class="{ spinning: tool.status === 'running' }"><Loading /></el-icon>
                <span>{{ tool.name }}</span>
                <el-tag size="small" :type="tool.status === 'completed' ? 'success' : 'info'">
                  {{ t(`agent.chat.toolStatus.${tool.status}`) }}
                </el-tag>
              </div>
            </div>
            <MessagePartsRenderer
              :message="liveMessage"
              @action="handleA2UIAction"
              @error="handlePresentationError"
            />
            <span v-if="isLoading && liveMessage.parts.length === 0" class="running-indicator">…</span>
          </div>
        </div>
      </div>

      <div class="input-area">
        <el-input
          v-model="inputText"
          type="textarea"
          :rows="3"
          :placeholder="t('agent.chat.inputPlaceholder')"
          resize="none"
          :disabled="isLoading"
          @keydown.ctrl.enter="handleSend"
        />
        <div class="input-actions">
          <el-tooltip v-if="isLoading" :content="t('agent.chat.cancel')">
            <el-button
              :icon="CircleClose"
              :loading="isCancelling"
              :aria-label="t('agent.chat.cancel')"
              @click="cancelActiveRun"
            />
          </el-tooltip>
          <el-tooltip v-if="!isLoading && retryRunId" :content="t('agent.chat.retry')">
            <el-button
              :icon="RefreshRight"
              :aria-label="t('agent.chat.retry')"
              @click="retryFailedRun"
            />
          </el-tooltip>
          <el-button v-if="!isLoading" type="primary" :icon="Position" @click="handleSend">
            {{ t('agent.chat.send') }}
          </el-button>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, ref } from 'vue'
import { ChatDotRound, CircleClose, Delete, Loading, Plus, Position, RefreshRight } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { resolveTaskOwnerUrl } from '@common-ui'

import { createAgentClient, replayAgentRunEvents } from '../agent/createAgentClient'
import { runAPI, sessionAPI } from '../api/index'
import MessagePartsRenderer from '../components/MessagePartsRenderer.vue'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const authStore = useAuthStore()

const sessions = ref([])
const currentSessionId = ref(null)
const messages = ref([])
const inputText = ref('')
const isLoading = ref(false)
const isCancelling = ref(false)
const liveMessage = ref(null)
const liveTools = ref([])
const messagesAreaRef = ref(null)
const activeRunId = ref(null)
const retryRunId = ref(null)

const quickHints = computed(() => [
  t('agent.quickHints.viewCatalog'),
  t('agent.quickHints.listSources'),
  t('agent.quickHints.importShapefile'),
  t('agent.quickHints.runSQL')
])

async function loadSessions() {
  try {
    sessions.value = await sessionAPI.list() || []
  } catch (error) {
    console.error('Failed to load sessions', error)
  }
}

async function createSession() {
  try {
    const session = await sessionAPI.create({ title: null })
    sessions.value.unshift(session)
    await switchSession(session.id)
    return session.id
  } catch (error) {
    ElMessage.error(t('agent.chat.createFailed'))
    return null
  }
}

async function switchSession(sessionId) {
  if (isLoading.value) return
  currentSessionId.value = sessionId
  try {
    messages.value = await sessionAPI.getMessages(sessionId) || []
    scrollToBottom()
  } catch (error) {
    console.error('Failed to load messages', error)
  }
}

async function deleteSession(sessionId) {
  if (isLoading.value) return
  try {
    await sessionAPI.delete(sessionId)
    sessions.value = sessions.value.filter(session => session.id !== sessionId)
    if (currentSessionId.value === sessionId) {
      currentSessionId.value = null
      messages.value = []
    }
  } catch (error) {
    ElMessage.error(t('agent.chat.deleteFailed'))
  }
}

async function sendQuickMessage(text) {
  inputText.value = text
  await handleSend()
}

function startLiveMessage() {
  liveMessage.value = {
    id: `live:${crypto.randomUUID()}`,
    role: 'assistant',
    content: '',
    parts: []
  }
  liveTools.value = []
}

function ensureLiveTextPart() {
  let part = liveMessage.value.parts.find(item => item.type === 'text')
  if (!part) {
    part = { type: 'text', text: '' }
    liveMessage.value.parts.unshift(part)
  }
  return part
}

function applyToolStart(event) {
  if (!liveTools.value.some(item => item.id === event.toolCallId)) {
    liveTools.value.push({ id: event.toolCallId, name: event.toolCallName, status: 'running' })
  }
  scrollToBottom()
}

function applyToolResult(event) {
  const tool = liveTools.value.find(item => item.id === event.toolCallId)
  if (tool) tool.status = 'completed'
}

function applyActivity(event) {
  liveMessage.value.parts.push({
    type: 'presentation_ref',
    protocol: 'a2ui',
    activity_type: event.activityType,
    surface_id: event.messageId,
    content: event.content
  })
  scrollToBottom()
}

function applyStateSnapshot(event) {
  if (event.snapshot?.agentRunId) activeRunId.value = event.snapshot.agentRunId
}

function applyRunError(event, { notify = true } = {}) {
  if (event.code !== 'cancelled' && activeRunId.value) retryRunId.value = activeRunId.value
  if (notify) ElMessage.error(event.message || t('agent.chat.errorReply'))
}

function applyReplayEvent(event) {
  switch (event.type) {
    case 'TEXT_MESSAGE_START':
      ensureLiveTextPart()
      break
    case 'TEXT_MESSAGE_CONTENT':
      ensureLiveTextPart().text += event.delta || ''
      break
    case 'TOOL_CALL_START':
      applyToolStart(event)
      break
    case 'TOOL_CALL_RESULT':
      applyToolResult(event)
      break
    case 'ACTIVITY_SNAPSHOT':
      applyActivity(event)
      break
    case 'STATE_SNAPSHOT':
      applyStateSnapshot(event)
      break
    case 'RUN_ERROR':
      applyRunError(event)
      break
  }
}

function createSubscriber() {
  return {
    onTextMessageContentEvent({ event }) {
      ensureLiveTextPart().text += event.delta
      scrollToBottom()
    },
    onToolCallStartEvent({ event }) {
      applyToolStart(event)
    },
    onToolCallResultEvent({ event }) {
      applyToolResult(event)
    },
    onActivitySnapshotEvent({ event }) {
      applyActivity(event)
    },
    onStateSnapshotEvent({ event }) {
      applyStateSnapshot(event)
    },
    onRunErrorEvent({ event }) {
      applyRunError(event)
    }
  }
}

async function replayActiveRun() {
  if (!activeRunId.value) return false
  startLiveMessage()
  await replayAgentRunEvents({
    agentRunId: activeRunId.value,
    getAuthStore: () => authStore,
    onEvent: ({ event }) => applyReplayEvent(event)
  })
  return true
}

async function runAgent({ userMessage = null, resume = null } = {}) {
  if (!currentSessionId.value || isLoading.value) return
  isLoading.value = true
  retryRunId.value = null
  startLiveMessage()

  const agent = createAgentClient({
    sessionId: currentSessionId.value,
    getAuthStore: () => authStore
  })
  if (userMessage) agent.addMessage(userMessage)

  let replayed = false
  try {
    await agent.runAgent(resume ? { resume } : {}, createSubscriber())
  } catch (error) {
    try {
      replayed = await replayActiveRun()
    } catch {
      replayed = false
    }
    if (!replayed) {
      ElMessage.error(t('agent.chat.sendFailed', { msg: error.message }))
    }
  } finally {
    if (!replayed) {
      liveMessage.value = null
      liveTools.value = []
    }
    isLoading.value = false
    isCancelling.value = false
    activeRunId.value = null
    await Promise.all([loadSessions(), switchSession(currentSessionId.value)])
  }
}

async function cancelActiveRun() {
  if (!activeRunId.value || isCancelling.value) return
  isCancelling.value = true
  try {
    await runAPI.cancel(activeRunId.value)
  } catch (error) {
    ElMessage.error(t('agent.chat.cancelFailed', { msg: error.message }))
    isCancelling.value = false
  }
}

async function retryFailedRun() {
  if (!retryRunId.value || isLoading.value) return
  const agentRunId = retryRunId.value
  isLoading.value = true
  retryRunId.value = null
  startLiveMessage()
  const agent = createAgentClient({
    sessionId: currentSessionId.value,
    endpoint: `/api/v1/agent/runs/${agentRunId}/retry`,
    getAuthStore: () => authStore
  })
  try {
    await agent.runAgent({}, createSubscriber())
  } catch (error) {
    retryRunId.value = agentRunId
    ElMessage.error(t('agent.chat.sendFailed', { msg: error.message }))
  } finally {
    liveMessage.value = null
    liveTools.value = []
    isLoading.value = false
    activeRunId.value = null
    await Promise.all([loadSessions(), switchSession(currentSessionId.value)])
  }
}

async function handleSend() {
  const content = inputText.value.trim()
  if (!content || isLoading.value) return

  if (!currentSessionId.value && !await createSession()) return

  retryRunId.value = null

  const protocolMessageId = crypto.randomUUID()
  messages.value.push({
    id: `local:${protocolMessageId}`,
    protocol_message_id: protocolMessageId,
    role: 'user',
    content,
    parts: [{ type: 'text', text: content }]
  })
  inputText.value = ''
  scrollToBottom()

  await runAgent({
    userMessage: { id: protocolMessageId, role: 'user', content }
  })
}

async function handleA2UIAction(action) {
  if (action.name === 'owner.open') {
    const openUrl = action.context?.openUrl
    const resolved = resolveTaskOwnerUrl(openUrl)
    if (resolved) window.open(resolved, '_blank', 'noopener,noreferrer')
    return
  }
  if (action.name !== 'interaction.submit' || isLoading.value) return
  const interactionId = action.context?.interactionId
  if (!interactionId) return
  await runAgent({
    resume: [{
      interruptId: interactionId,
      status: 'resolved',
      payload: action.context.answer
    }]
  })
}

function handlePresentationError(error) {
  console.error('A2UI rendering failed', error)
  ElMessage.error(t('agent.chat.presentationFailed'))
}

function scrollToBottom() {
  nextTick(() => {
    const element = messagesAreaRef.value
    if (element) element.scrollTop = element.scrollHeight
  })
}

onMounted(async () => {
  await loadSessions()
  if (sessions.value.length > 0) await switchSession(sessions.value[0].id)
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
  border-right: 1px solid var(--addp-border-color);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-header {
  padding: 16px;
  border-bottom: 1px solid var(--addp-border-color);
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
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 0;
  border-radius: 8px;
  color: var(--addp-text-primary);
  background: transparent;
  cursor: pointer;
  text-align: left;
}

.session-item:hover {
  background: var(--addp-bg-secondary);
}

.session-item.active {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

.session-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}

.delete-btn {
  opacity: 0;
}

.session-item:hover .delete-btn {
  opacity: 1;
}

.chat-main {
  flex: 1;
  min-width: 0;
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
  margin: auto;
  text-align: center;
  color: var(--addp-text-secondary);
}

.empty-hint p {
  margin: 16px 0;
}

.quick-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
}

.quick-tag {
  cursor: pointer;
}

.message-row {
  display: flex;
  min-width: 0;
}

.message-row.user {
  justify-content: flex-end;
}

.message-row.assistant {
  justify-content: flex-start;
}

.message-bubble {
  max-width: min(860px, 82%);
  min-width: 0;
  padding: 12px 16px;
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.6;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color-light);
}

.message-row.user .message-bubble {
  background: var(--el-color-primary);
  color: var(--el-color-white);
  border-color: var(--el-color-primary);
}

.live-message {
  width: min(860px, 82%);
}

.tool-trace {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 10px;
}

.tool-trace-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.tool-trace-item span {
  flex: 1;
}

.spinning {
  animation: spin 1s linear infinite;
}

.running-indicator {
  color: var(--addp-text-tertiary);
}

.input-area {
  display: flex;
  gap: 12px;
  align-items: flex-end;
  padding: 16px 20px;
  border-top: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
}

.input-actions {
  display: flex;
  align-items: flex-end;
}

.input-area .el-input {
  flex: 1;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 900px) {
  .sidebar {
    width: 200px;
  }

  .message-bubble,
  .live-message {
    max-width: 94%;
    width: auto;
  }
}
</style>
