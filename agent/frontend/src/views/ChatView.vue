<template>
  <div class="chat-layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <div class="sidebar-title-row">
          <button class="workspace-home" type="button" :aria-label="t('agent.workspace.home')" @click="showHome">{{ t('agent.workspace.moduleTitle') }}</button>
          <el-button v-if="authStore.hasPermission('agent.session.create')" type="primary" :icon="Plus" :disabled="navigationDraftPending" @click="createSession">{{ t('agent.chat.newSession') }}</el-button>
        </div>
        <div class="session-search-row">
          <el-input v-model="sessionSearch" clearable :placeholder="t('agent.workspace.search')" @keyup.enter="filterSessions" @clear="filterSessions" />
          <el-dropdown trigger="click" @command="filterCommand">
            <el-button :icon="Filter" :aria-label="t('agent.workspace.filter')" :type="showArchived ? 'primary' : 'default'" plain />
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="current" :icon="!showArchived ? Check : undefined">{{ t('agent.workspace.unarchived') }}</el-dropdown-item>
              <el-dropdown-item command="archived" :icon="showArchived ? Check : undefined">{{ t('agent.workspace.archived') }}</el-dropdown-item>
              <el-dropdown-item command="refresh" divided :icon="RefreshRight">{{ t('agent.workspace.refresh') }}</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </div>
        <span v-if="showArchived" class="active-filter">{{ t('agent.workspace.archived') }}</span>
      </div>
      <div class="session-list" v-loading="sessionsLoading">
        <el-alert v-if="sessionsError" :title="t('agent.workspace.loadFailed')" type="error" :closable="false" />
        <section v-for="group in sessionGroups" :key="group.key">
          <h3 class="session-group-title">{{ t(`agent.workspace.groups.${group.key}`) }}</h3>
          <div v-for="session in group.items" :key="session.id" class="session-item" :class="{ active: currentSessionId === String(session.id) }">
            <button type="button" class="session-select" :title="session.title || t('agent.chat.newSession')" @click="switchSession(session.id)">
              <el-icon><ChatDotRound /></el-icon><span class="session-title">{{ session.title || t('agent.chat.newSession') }}</span>
              <span v-if="currentSessionId === String(session.id) && attentionStatus" class="session-attention">{{ t(`agent.workspace.status.${attentionStatus}`) }}</span>
            </button>
            <el-dropdown v-if="authStore.hasPermission('agent.session.update') || authStore.hasPermission('agent.session.delete')" trigger="click" @command="command => sessionCommand(session, command)">
              <el-button text :aria-label="t('agent.workspace.actions')"><el-icon><MoreFilled /></el-icon></el-button>
              <template #dropdown><el-dropdown-menu>
                <template v-if="authStore.hasPermission('agent.session.update')">
                  <el-dropdown-item command="rename">{{ t('agent.workspace.rename') }}</el-dropdown-item>
                  <el-dropdown-item command="pin">{{ t(session.pinned ? 'agent.workspace.unpin' : 'agent.workspace.pin') }}</el-dropdown-item>
                  <el-dropdown-item command="archive">{{ t(session.archived ? 'agent.workspace.restore' : 'agent.workspace.archive') }}</el-dropdown-item>
                </template>
                <el-dropdown-item v-if="authStore.hasPermission('agent.session.delete')" command="delete" divided class="agent-session-delete">{{ t('agent.workspace.delete') }}</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </div>
        </section>
        <el-empty v-if="!sessionsError && !sessionsLoading && sessions.length === 0" :description="t('agent.chat.noSessions')" :image-size="60" />
      </div>
      <el-pagination v-if="sessionTotal > 20" small layout="prev, pager, next" :page-size="20" :total="sessionTotal" v-model:current-page="sessionPage" @current-change="loadSessions" />
      <div class="sidebar-footer">
        <el-dropdown trigger="click" @command="settingsCommand">
          <el-button text :icon="Setting">{{ t('agent.workspace.settings') }}</el-button>
          <template #dropdown><el-dropdown-menu>
            <el-dropdown-item command="preferences">{{ t('agent.workspace.preferences') }}</el-dropdown-item>
            <el-dropdown-item v-if="authStore.hasPermission('agent.configuration.read')" command="configuration">{{ t('agent.workspace.configuration') }}</el-dropdown-item>
          </el-dropdown-menu></template>
        </el-dropdown>
      </div>
    </aside>
    <WorkspacePreferences v-model="preferencesVisible" :preferences="preferences" @save="savePreferences" />
    <el-dialog v-model="branchVisible" :title="t('agent.workspace.branch')" width="min(640px, 90vw)">
      <p>{{ t('agent.workspace.branchNotice') }}</p>
      <el-input v-model="branchText" type="textarea" :rows="8" :aria-label="t('agent.workspace.branchText')" />
      <el-alert v-if="branchText.trim() && !validBranchDraft" :title="t('agent.workspace.draftLimit')" type="warning" :closable="false" />
      <template #footer><el-button @click="branchVisible = false">{{ t('agent.workspace.close') }}</el-button><el-button type="primary" :disabled="!validBranchDraft || isLoading" @click="useBranchDraft">{{ t('agent.workspace.useDraft') }}</el-button></template>
    </el-dialog>
    <el-dialog v-model="renameVisible" :title="t('agent.workspace.rename')" width="min(480px, 90vw)" :close-on-click-modal="false">
      <el-input v-model="renameDraft.title" maxlength="255" :aria-label="t('agent.workspace.title')" />
      <p v-if="renameServerTitle !== renameDraft.title">{{ t('agent.workspace.savedTitle') }}：{{ renameServerTitle }}</p>
      <el-alert v-if="renameConflict" :title="t('agent.workspace.conflict')" type="warning" :closable="false" />
      <template #footer>
        <el-button v-if="renameConflict" @click="reloadRenameVersion">{{ t('agent.workspace.reload') }}</el-button>
        <el-button @click="renameVisible = false">{{ t('agent.workspace.close') }}</el-button>
        <el-button type="primary" :disabled="renameConflict || !renameDraft.title.trim()" :loading="organizationSaving" @click="saveRename">{{ t('agent.workspace.save') }}</el-button>
      </template>
    </el-dialog>

    <main class="chat-main">
      <div v-if="currentSession" class="conversation-header">
        <span class="conversation-title">{{ currentSession.title || t('agent.chat.newSession') }}</span>
        <div class="conversation-actions">
          <el-button text :icon="Search" :aria-label="t('agent.workspace.find')" :aria-expanded="findVisible" @click="findVisible = !findVisible" />
          <el-button text :icon="Bottom" :aria-label="t('agent.workspace.latest')" @click="scrollToBottom" />
          <el-button text :icon="Download" :aria-label="t('agent.workspace.export')" :disabled="isLoading || !messages.length" @click="exportConversation" />
          <el-button :aria-expanded="panelVisible" @click="panelVisible = !panelVisible">{{ t(panelVisible ? 'agent.workspace.collapsePanel' : 'agent.workspace.panel') }}</el-button>
        </div>
      </div>
      <div v-if="findVisible && currentSession" class="conversation-find">
        <el-input v-model="findQuery" clearable :maxlength="200" :placeholder="t('agent.workspace.findPlaceholder')" @keyup.enter="moveMatch(1)" />
        <span>{{ findMatches.length ? findIndex + 1 : 0 }}/{{ findMatches.length }}</span>
        <el-button text :aria-label="t('agent.workspace.previousMatch')" :disabled="!findMatches.length" @click="moveMatch(-1)">↑</el-button>
        <el-button text :aria-label="t('agent.workspace.nextMatch')" :disabled="!findMatches.length" @click="moveMatch(1)">↓</el-button>
      </div>
      <div v-if="currentSession?.archived" class="archive-notice">
        <el-alert :title="t('agent.workspace.archiveNotice')" type="info" :closable="false" />
        <el-button v-if="authStore.hasPermission('agent.session.update')" @click="updateOrganization(currentSession, { archived: false })">{{ t('agent.workspace.restore') }}</el-button>
      </div>
      <div ref="messagesAreaRef" class="messages-area">
        <el-empty
          v-if="sessionUnavailable"
          class="empty-hint"
          :description="t('agent.chat.sessionUnavailable')"
        />
        <section v-else-if="messages.length === 0 && !liveMessage" class="capability-home">
          <h1>{{ t('agent.chat.welcome') }}</h1>
          <p>{{ t('agent.workspace.chooseCapability') }}</p>
          <el-alert v-if="skillsError" :title="t('agent.workspace.skillsFailed')" type="error" :closable="false" />
          <div class="capability-grid">
            <button v-for="skill in skills" :key="skill.name" class="capability-card" type="button" :disabled="!canRun" @click="fillExample(skill.name)">
              <strong>{{ t(`agent.skills.${skill.name}.title`) }}</strong>
              <span>{{ t(`agent.skills.${skill.name}.description`) }}</span>
              <small>{{ t(`agent.skills.${skill.name}.example`) }}</small>
            </button>
          </div>
        </section>

        <div
          v-for="message in messages"
          :key="message.protocol_message_id || message.id"
          :data-message-id="String(message.protocol_message_id || message.id)"
          class="message-row"
          :class="[message.role, { 'find-match': findMatches[findIndex] === messageIdentity(message) }]"
        >
          <div class="message-bubble">
            <MessagePartsRenderer
              :message="message"
              :expand-details="preferences.expandDetails"
              :interaction-disabled="isLoading || !canRun"
              @action="handleA2UIAction"
              @error="handlePresentationError"
            />
            <div v-if="messageText(message)" class="message-actions">
              <el-button text size="small" @click="copyMessage(message)">{{ t('agent.workspace.copy') }}</el-button>
              <el-button v-if="canBranch" text size="small" :disabled="isLoading" @click="prepareBranch(message)">{{ t('agent.workspace.branch') }}</el-button>
            </div>
          </div>
        </div>

        <div v-if="liveMessage" class="message-row assistant" :data-message-id="String(liveMessage.id)">
          <div class="message-bubble live-message">
            <details v-if="liveTools.length" class="tool-trace" :open="preferences.expandDetails"><summary>{{ t('agent.workspace.toolCount', { count: liveTools.length }) }}</summary>
              <div v-for="tool in liveTools" :key="tool.id" class="tool-trace-item">
                <el-icon :class="{ spinning: tool.status === 'running' }"><Loading /></el-icon>
                <span>{{ tool.name }}</span>
                <el-tag size="small" :type="tool.status === 'completed' ? 'success' : 'info'">
                  {{ t(`agent.chat.toolStatus.${tool.status}`) }}
                </el-tag>
              </div>
            </details>
            <MessagePartsRenderer
              :message="liveMessage"
              :expand-details="preferences.expandDetails"
              :interaction-disabled="isLoading || !canRun"
              @action="handleA2UIAction"
              @error="handlePresentationError"
            />
            <span v-if="isLoading && liveMessage.parts.length === 0" class="running-indicator">…</span>
          </div>
        </div>
      </div>

      <div class="input-area" :aria-busy="navigationDraftPending">
        <el-input
          v-model="inputText"
          type="textarea"
          :rows="3"
          :placeholder="t(pendingClarifications.length === 1 ? 'agent.chat.clarificationPlaceholder' : preferences.sendShortcut === 'enter' ? 'agent.workspace.enterPlaceholder' : 'agent.chat.inputPlaceholder')"
          :maxlength="pendingClarifications.length ? 2000 : undefined"
          resize="none"
          :disabled="isLoading || !canRun"
          @keydown="handleInputKey"
        />
        <div class="input-actions">
          <el-tooltip v-if="isLoading && authStore.hasPermission('agent.run.cancel')" :content="t('agent.chat.cancel')">
            <el-button
              :icon="CircleClose"
              :loading="isCancelling"
              :aria-label="t('agent.chat.cancel')"
              @click="cancelActiveRun"
            />
          </el-tooltip>
          <el-tooltip v-if="!isLoading && retryRunId && authStore.hasPermission('agent.run.execute')" :content="t('agent.chat.retry')">
            <el-button
              :icon="RefreshRight"
              :aria-label="t('agent.chat.retry')"
              @click="retryFailedRun"
            />
          </el-tooltip>
          <el-button v-if="!isLoading && canRun" type="primary" :icon="Position" @click="handleSend">
            {{ t('agent.chat.send') }}
          </el-button>
        </div>
      </div>
    </main>
    <WorkspacePanel v-if="panelVisible && currentSessionId && !sessionUnavailable" :session-id="currentSessionId" :messages="panelMessages" :can-read-runs="authStore.hasPermission('agent.run.read')" :revision="runRevision" :active-run-id="activeRunId" :expand-details="preferences.expandDetails" @locate="locateMessage" @close="panelVisible = false" />
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Bottom, ChatDotRound, Check, CircleClose, Download, Filter, Loading, MoreFilled, Plus, Position, RefreshRight, Search, Setting } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { openConsoleRoute, resolveTaskOwnerUrl, takeConsoleNavigationDraft } from '@common-ui'
import { saveBlobDownload } from '@common-ui/utils/exportSession'

import { createAgentClient, replayAgentRunEvents } from '../agent/createAgentClient'
import { runAPI, sessionAPI } from '../api/index'
import MessagePartsRenderer from '../components/MessagePartsRenderer.vue'
import WorkspacePanel from '../components/WorkspacePanel.vue'
import WorkspacePreferences from '../components/WorkspacePreferences.vue'
import { useWorkspacePreferences } from '../composables/useWorkspacePreferences'
import { createMessageDraft, readMessageDraft, conversationMarkdown, messageIdentity, messageText, pendingSessionInteractions } from '../utils/conversation'
import { useAuthStore } from '../store/auth'
import { navigateAgentRoute } from '../utils/moduleNavigation'
import { createLatestRequestScope } from '@common-ui'
import { groupSessions } from '../utils/workspace'

const { t } = useI18n()
const authStore = useAuthStore()
const { preferences, save: persistPreferences, scopeKey: preferenceScope } = useWorkspacePreferences(authStore)
const preferencesVisible = ref(false), branchVisible = ref(false), branchText = ref('')
const findVisible = ref(false), findQuery = ref(''), findIndex = ref(0)
watch(preferenceScope, () => { preferencesVisible.value = false })
const validBranchDraft = computed(() => { try { createMessageDraft(branchText.value); return true } catch { return false } })
const route = useRoute()
const router = useRouter()
const navigationDraftPending = ref(!route.params.session_id && window.parent !== window)

const sessions = ref([])
const currentSessionId = ref(null)
const currentSession = ref(null)
useConsolePageDescriptor(router, 'agent', {
  title: computed(() => t('agent.chat.recentVisitTitle')),
  subject: computed(() => currentSession.value?.title || ''),
  ready: computed(() => Boolean(currentSession.value?.title))
})
const sessionUnavailable = ref(false)
const canRun = computed(() =>
  !navigationDraftPending.value && !sessionUnavailable.value && !currentSession.value?.archived &&
  ['agent.run.create', 'agent.run.execute'].every(permission => authStore.hasPermission(permission)) &&
  (Boolean(currentSessionId.value) || authStore.hasPermission('agent.session.create'))
)
const sessionsLoaded = ref(false)
const messages = ref([])
const canBranch = computed(() => ['agent.session.create', 'agent.run.create', 'agent.run.execute'].every(permission => authStore.hasPermission(permission)))
const findMatches = computed(() => findQuery.value.trim() ? messages.value.filter(message => messageText(message).toLocaleLowerCase().includes(findQuery.value.trim().toLocaleLowerCase())).map(messageIdentity) : [])
const pendingInteractions = computed(() => pendingSessionInteractions(messages.value))
const attentionStatus = computed(() => isLoading.value ? 'running' : pendingInteractions.value.length ? 'waiting' : retryRunId.value ? 'failed' : null)
const pendingClarifications = computed(() => {
  const interactions = new Map()
  for (const message of messages.value) {
    for (const part of message.parts || []) {
      if (part.type === 'interaction_ref' && part.kind === 'clarification') {
        interactions.set(part.interaction_id, part)
      }
    }
  }
  return [...interactions.values()].filter(part => part.status === 'pending')
})
const inputText = ref('')
const isLoading = ref(false)
const isCancelling = ref(false)
const liveMessage = ref(null)
const liveTools = ref([])
const messagesAreaRef = ref(null)
const panelVisible = ref(false), runRevision = ref(0)
const panelMessages = computed(() => liveMessage.value ? [...messages.value, liveMessage.value] : messages.value)
const activeRunId = ref(null)
const retryRunId = ref(null)

const sessionSearch = ref(''), showArchived = ref(false), sessionPage = ref(1), sessionTotal = ref(0)
const sessionsLoading = ref(false), sessionsError = ref(false), skills = ref([]), skillsError = ref(false)
const renameVisible = ref(false), renameDraft = ref({ id: null, title: '', version: 1 }), renameConflict = ref(false)
const organizationSaving = ref(false), renameServerTitle = ref('')
const listScope = createLatestRequestScope(), detailScope = createLatestRequestScope()
const sessionGroups = computed(() => groupSessions(sessions.value))

async function loadSessions() {
  const ticket = listScope.invalidate()
  sessionsLoading.value = true
  sessionsError.value = false
  try {
    const result = await listScope.request(options => sessionAPI.list({ page: sessionPage.value, page_size: 20, search: sessionSearch.value, archived: showArchived.value }, options))
    if (listScope.current(ticket)) { sessions.value = result.data; sessionTotal.value = result.total }
  } catch {
    if (listScope.current(ticket)) sessionsError.value = true
  } finally {
    if (listScope.current(ticket)) sessionsLoading.value = false
  }
}
function settingsCommand(command) {
  if (command === 'configuration') return openConfiguration()
  preferencesVisible.value = true
}
function savePreferences(value) {
  if (!persistPreferences(value)) { ElMessage.error(t('agent.workspace.preferencesFailed')); return }
  preferencesVisible.value = false
  ElMessage.success(t('agent.workspace.preferencesSaved'))
}
function handleInputKey(event) {
  if (event.key !== 'Enter' || event.isComposing || event.shiftKey || event.altKey || event.repeat) return
  const send = preferences.value.sendShortcut === 'enter' ? !event.ctrlKey && !event.metaKey : event.ctrlKey || event.metaKey
  if (send) { event.preventDefault(); handleSend() }
}
async function copyMessage(message) {
  try { await navigator.clipboard.writeText(messageText(message)); ElMessage.success(t('agent.workspace.copied')) }
  catch { ElMessage.error(t('agent.workspace.copyFailed')) }
}
function exportConversation() {
  const text = conversationMarkdown(currentSession.value.title || t('agent.chat.newSession'), messages.value, t)
  saveBlobDownload(new Blob([text], { type: 'text/markdown;charset=utf-8' }), `agent-session-${currentSessionId.value}.md`)
}
function prepareBranch(message) {
  const selection = window.getSelection()
  const row = [...(messagesAreaRef.value?.querySelectorAll('[data-message-id]') || [])].find(element => element.dataset.messageId === messageIdentity(message))
  const selected = selection && row?.contains(selection.anchorNode) && row.contains(selection.focusNode) ? selection.toString() : ''
  branchText.value = selected.trim() || messageText(message)
  branchVisible.value = true
}
async function useBranchDraft() {
  if (!canBranch.value || isLoading.value || !branchText.value.trim()) return
  if (inputText.value.trim()) {
    try { await ElMessageBox.confirm(t('agent.workspace.replaceDraft'), t('agent.workspace.branch'), { confirmButtonText: t('agent.workspace.useDraft'), cancelButtonText: t('agent.workspace.close'), type: 'warning' }) }
    catch { return }
  }
  try {
    const draft = createMessageDraft(branchText.value)
    if (window.parent !== window) {
      const moved = await openConsoleRoute('/agent', { draft })
      if (moved === false) return
    } else {
      const moved = await navigateAgentRoute(router, { name: 'Chat' })
      if (moved === false || route.params.session_id) return
      inputText.value = readMessageDraft(draft)
    }
    branchVisible.value = false
  } catch { ElMessage.error(t('agent.workspace.draftFailed')) }
}
function moveMatch(direction) {
  if (!findMatches.value.length) return
  findIndex.value = (findIndex.value + direction + findMatches.value.length) % findMatches.value.length
  locateMessage(findMatches.value[findIndex.value])
}
watch([findQuery, currentSessionId], () => { findIndex.value = 0; if (findMatches.value.length) locateMessage(findMatches.value[0]) })
function filterSessions() { sessionPage.value = 1; loadSessions() }
function filterCommand(command) {
  if (command === 'refresh') return loadSessions()
  showArchived.value = command === 'archived'
  filterSessions()
}
async function openConfiguration() {
  if (!authStore.hasPermission('agent.configuration.read')) return
  try { await openConsoleRoute('/configuration/agent') }
  catch { ElMessage.error(t('agent.workspace.navigationFailed')) }
}
async function loadSkills() {
  try { skills.value = await sessionAPI.skills(); skillsError.value = false }
  catch { skillsError.value = true }
}
function fillExample(name) { inputText.value = t(`agent.skills.${name}.example`) }
function showHome() { if (!isLoading.value) navigateAgentRoute(router, { name: 'Chat' }) }

async function sessionCommand(session, command) {
  if (command === 'rename') {
    renameDraft.value = { id: session.id, title: session.title || '', version: session.version }
    renameServerTitle.value = session.title || ''; renameConflict.value = false; renameVisible.value = true
  } else if (command === 'pin') await updateOrganization(session, { pinned: !session.pinned })
  else if (command === 'archive') await updateOrganization(session, { archived: !session.archived })
  else if (command === 'delete') await deleteSession(session)
}
async function updateOrganization(session, changes) {
  if (organizationSaving.value) return false
  organizationSaving.value = true
  try {
    const updated = await sessionAPI.update(session.id, { version: session.version, ...changes })
    if (String(updated.id) === currentSessionId.value) currentSession.value = updated
    await loadSessions()
    return true
  } catch (error) {
    const conflict = error.response?.data?.error_code === 'resource_version_conflict'
    if (renameVisible.value && conflict) renameConflict.value = true
    ElMessage.error(t(conflict ? 'agent.workspace.conflict' : 'agent.workspace.updateFailed'))
    return false
  } finally { organizationSaving.value = false }
}
async function saveRename() {
  if (await updateOrganization(renameDraft.value, { title: renameDraft.value.title.trim() })) renameVisible.value = false
}
async function reloadRenameVersion() {
  try {
    const latest = await sessionAPI.get(renameDraft.value.id)
    renameDraft.value.version = latest.version
    renameServerTitle.value = latest.title || ''
    renameConflict.value = false
  } catch { ElMessage.error(t('agent.workspace.loadFailed')) }
}

async function createSession() {
  if (navigationDraftPending.value) return null
  try {
    const session = await sessionAPI.create({ title: null })
    currentSession.value = session
    await loadSessions()
    await navigateAgentRoute(router, {
      name: 'ChatSession',
      params: { session_id: String(session.id) }
    })
    return session.id
  } catch (error) {
    ElMessage.error(t('agent.chat.createFailed'))
    return null
  }
}

async function loadSessionMessages(sessionId) {
  if (isLoading.value || !sessionId) return
  const ticket = detailScope.invalidate()
  if (currentSessionId.value !== String(sessionId)) {
    currentSession.value = null; messages.value = []; retryRunId.value = null
  }
  currentSessionId.value = String(sessionId)
  sessionUnavailable.value = false
  try {
    const [session, history] = await detailScope.request(options => Promise.all([sessionAPI.get(sessionId, options), sessionAPI.getMessages(sessionId, options)]))
    if (!detailScope.current(ticket)) return
    if (!currentSession.value || currentSession.value.version <= session.version) currentSession.value = session
    messages.value = history
    scrollToBottom()
  } catch {
    if (detailScope.current(ticket)) { currentSession.value = null; messages.value = []; sessionUnavailable.value = true }
  }
}
async function switchSession(sessionId) {
  if (isLoading.value || navigationDraftPending.value || String(sessionId) === currentSessionId.value) return
  await navigateAgentRoute(router, { name: 'ChatSession', params: { session_id: String(sessionId) } })
}
async function restoreRouteSession(sessionId) {
  if (!sessionsLoaded.value) return
  if (sessionId) return loadSessionMessages(String(sessionId))
  detailScope.invalidate()
  currentSessionId.value = null; currentSession.value = null; messages.value = []; sessionUnavailable.value = false; retryRunId.value = null
}
async function deleteSession(session) {
  if (isLoading.value) return
  try {
    await sessionAPI.delete(session.id, session.version)
    await loadSessions()
    if (currentSessionId.value === String(session.id)) await navigateAgentRoute(router, { name: 'Chat' }, { history: 'replace' })
  } catch (error) { ElMessage.error(t(error.response?.status === 409 ? 'agent.workspace.conflict' : 'agent.chat.deleteFailed')) }
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
  let part = liveMessage.value.parts.at(-1)
  if (part?.type !== 'text') {
    part = { type: 'text', text: '' }
    liveMessage.value.parts.push(part)
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
  runRevision.value++
  const tool = liveTools.value.find(item => item.id === event.toolCallId)
  if (tool) tool.status = 'completed'
}

function applyActivity(event) {
  const part = {
    type: 'presentation_ref',
    protocol: event.activityType === 'addp-plan' ? 'ag-ui' : 'a2ui',
    activity_type: event.activityType,
    surface_id: event.messageId,
    content: event.content
  }
  const planIndex = event.activityType === 'addp-plan'
    ? liveMessage.value.parts.findIndex(item => item.protocol === 'ag-ui' && item.activity_type === 'addp-plan' && item.surface_id === event.messageId)
    : -1
  if (planIndex < 0) liveMessage.value.parts.push(part)
  else liveMessage.value.parts.splice(planIndex, 1, part)
  scrollToBottom()
}

function applyStateSnapshot(event) {
  if (event.snapshot?.agentRunId) activeRunId.value = event.snapshot.agentRunId
  runRevision.value++
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
  if (!activeRunId.value || !authStore.hasPermission('agent.run.read')) return false
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
  let succeeded = false
  try {
    await agent.runAgent(resume ? { resume } : { forwardedProps: { replyDetail: preferences.value.replyDetail } }, createSubscriber())
    succeeded = true
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
    runRevision.value++
    await Promise.all([loadSessions(), loadSessionMessages(currentSessionId.value)])
  }
  return succeeded
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
  if (!retryRunId.value || isLoading.value || !canRun.value) return
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
    runRevision.value++
    await Promise.all([loadSessions(), loadSessionMessages(currentSessionId.value)])
  }
}

async function handleSend() {
  const content = inputText.value.trim()
  if (!content || isLoading.value || !canRun.value) return

  if (pendingClarifications.value.length > 1) {
    ElMessage.warning(t('agent.chat.multipleClarifications'))
    return
  }
  if (pendingClarifications.value.length === 1) {
    const interactionId = pendingClarifications.value[0].interaction_id
    inputText.value = ''
    const succeeded = await resumeInteraction(interactionId, { text: content })
    if (!succeeded) inputText.value = content
    return
  }

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

  const succeeded = await runAgent({
    userMessage: { id: protocolMessageId, role: 'user', content }
  })
  if (!succeeded && currentSession.value?.archived) inputText.value = content
}

async function handleA2UIAction(action) {
  if (action.name === 'owner.open') {
    const openUrl = action.context?.openUrl
    const resolved = resolveTaskOwnerUrl(openUrl)
    if (resolved) window.open(resolved, '_blank', 'noopener,noreferrer')
    return
  }
  if (action.name !== 'interaction.submit' || isLoading.value || !canRun.value) return
  const interactionId = action.context?.interactionId
  if (!interactionId) return
  await resumeInteraction(interactionId, action.context.answer)
}

function resumeInteraction(interactionId, answer) {
  return runAgent({
    resume: [{
      interruptId: interactionId,
      status: 'resolved',
      payload: answer
    }]
  })
}

function handlePresentationError(error) {
  console.error('A2UI rendering failed', error)
  ElMessage.error(t('agent.chat.presentationFailed'))
}

function locateMessage(source) {
  nextTick(() => {
    const element = [...(messagesAreaRef.value?.querySelectorAll('[data-message-id]') || [])].find(item => item.dataset.messageId === source)
    element?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    if (element) { element.setAttribute('tabindex', '-1'); element.focus({ preventScroll: true }) }
  })
}

function scrollToBottom() {
  nextTick(() => {
    const element = messagesAreaRef.value
    if (element) element.scrollTop = element.scrollHeight
  })
}

onMounted(async () => {
  try {
    await Promise.all([loadSessions(), loadSkills()])
    sessionsLoaded.value = true
    await restoreRouteSession(route.params.session_id)
    if (navigationDraftPending.value) {
      try {
        const draft = await takeConsoleNavigationDraft()
        if (draft) inputText.value = readMessageDraft(draft)
      } catch { ElMessage.error(t('agent.workspace.draftFailed')) }
    }
  } finally { navigationDraftPending.value = false }
})

onBeforeUnmount(() => { listScope.dispose(); detailScope.dispose() })

watch(() => route.params.session_id, sessionId => {
  restoreRouteSession(sessionId)
})

onBeforeRouteUpdate((to, from) => {
  if ((isLoading.value || navigationDraftPending.value) && String(to.params.session_id || '') !== String(from.params.session_id || '')) {
    return false
  }
  return true
})
</script>

<style scoped>
.chat-layout {
  position: relative;
  display: flex;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.sidebar {
  width: 280px;
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

.sidebar-title-row, .session-search-row { display: flex; align-items: center; gap: 8px; }
.sidebar-title-row { justify-content: space-between; margin-bottom: 12px; }
.workspace-home { padding: 0; border: 0; background: transparent; color: var(--addp-text-primary); font: inherit; font-weight: 600; cursor: pointer; }
.session-search-row .el-input { min-width: 0; flex: 1; }
.sidebar-footer { padding: 8px 12px; border-top: 1px solid var(--addp-border-color); }
.active-filter { display: inline-block; margin-top: 8px; color: var(--el-color-primary); font-size: 12px; }
:global(.agent-session-delete) { color: var(--el-color-danger); }

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
  width: min(100%, 760px);
  margin: auto;
  text-align: center;
  color: var(--addp-text-secondary);
}

.empty-hint p {
  margin: 16px 0;
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
  margin-bottom: 10px;
}

.tool-trace summary { cursor: pointer; color: var(--addp-text-secondary); font-size: 12px; }
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
.session-group-title { font-size: 12px; font-weight: 500; color: var(--addp-text-secondary); padding: 8px; margin: 0; }
.session-select { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; border: 0; background: transparent; color: inherit; cursor: pointer; text-align: left; }
.session-item { padding: 4px 8px; }
.archive-notice { display: flex; gap: 8px; padding: 8px 16px; }
.capability-home { width: 100%; max-width: 900px; margin: 32px auto; }
.capability-home h1 { font-size: 24px; color: var(--addp-text-primary); }
.capability-home p { color: var(--addp-text-secondary); }
.capability-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; }
.capability-card { display: flex; flex-direction: column; gap: 12px; text-align: left; padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); color: var(--addp-text-primary); cursor: pointer; }
.capability-card:hover { border-color: var(--el-color-primary); }
.capability-card span, .capability-card small { color: var(--addp-text-secondary); line-height: 1.6; }
.capability-card:disabled { cursor: default; opacity: .65; }
@media (max-width: 760px) { .sidebar { width: 200px; } .capability-grid { grid-template-columns: 1fr; } .capability-home { margin: 16px auto; } }
@media (max-width: 540px) { .chat-layout { flex-direction: column; } .sidebar { width: 100%; max-height: 220px; border-right: 0; border-bottom: 1px solid var(--addp-border-color); } .sidebar-header { padding: 8px; } .session-list { max-height: 80px; } }
.conversation-header { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 8px 16px; border-bottom: 1px solid var(--addp-border-color); color: var(--addp-text-primary); }
.conversation-header .conversation-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.conversation-actions, .conversation-find, .message-actions { display: flex; align-items: center; gap: 4px; }
.conversation-actions { flex-shrink: 0; flex-wrap: wrap; justify-content: flex-end; }
.conversation-find { padding: 8px 16px; border-bottom: 1px solid var(--addp-border-color); color: var(--addp-text-secondary); }
.conversation-find .el-input { flex: 1; min-width: 0; }
.message-actions { margin-top: 8px; }
.message-row.user .message-actions .el-button { color: var(--el-color-white); }
.find-match .message-bubble { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
.session-attention { flex-shrink: 0; font-size: 11px; color: var(--el-color-warning); }
@media (max-width: 760px) { .conversation-header { align-items: flex-start; gap: 4px; flex-direction: column; } .conversation-actions { width: 100%; } }
</style>
