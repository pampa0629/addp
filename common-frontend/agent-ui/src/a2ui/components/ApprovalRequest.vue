<template>
  <section class="approval-request">
    <div class="approval-header">
      <div>
        <p class="approval-title">{{ t(properties.inlineDecision ? 'agent.chat.approval.runTitle' : 'agent.chat.approval.title') }}</p>
        <p class="approval-owner">{{ properties.owner }} · {{ properties.ownerInteractionId }}</p>
      </div>
      <el-tag type="warning">{{ t('agent.chat.approval.pending') }}</el-tag>
    </div>

    <template v-if="properties.inlineDecision">
      <el-alert v-if="properties.requestSummary?.config?.target?.policy?.apply_mode === 'replace'" :closable="false" type="warning" :title="t('agent.chat.approval.replaceWarning')" />
      <dl class="approval-summary">
        <div><dt>{{ t('agent.chat.approval.taskName') }}</dt><dd>{{ properties.requestSummary?.name }}</dd></div>
        <div><dt>{{ t('agent.chat.approval.source') }}</dt><dd>{{ properties.requestSummary?.config?.source?.locator }}</dd></div>
        <div><dt>{{ t('agent.chat.approval.target') }}</dt><dd>{{ properties.requestSummary?.config?.target?.parent_locator }} / {{ properties.requestSummary?.config?.target?.name }}</dd></div>
        <div><dt>{{ t('agent.chat.approval.writePolicy') }}</dt><dd>{{ properties.requestSummary?.config?.target?.policy?.apply_mode }}</dd></div>
        <div><dt>{{ t('agent.chat.approval.expiresAt') }}</dt><dd>{{ formatTime(properties.expiresAt) }}</dd></div>
      </dl>
      <details><summary>{{ t('agent.chat.approval.exactConfiguration') }}</summary><pre class="approval-config">{{ JSON.stringify(properties.requestSummary, null, 2) }}</pre></details>
    </template>
    <dl v-else class="approval-summary">
      <div>
        <dt>{{ t('agent.chat.approval.workflowEngine') }}</dt>
        <dd>{{ properties.requestSummary?.workflow_engine_id || '-' }}</dd>
      </div>
      <div>
        <dt>{{ t('agent.chat.approval.taskCount') }}</dt>
        <dd>{{ properties.requestSummary?.task_count ?? '-' }}</dd>
      </div>
      <div>
        <dt>{{ t('agent.chat.approval.expiresAt') }}</dt>
        <dd>{{ formatTime(properties.expiresAt) }}</dd>
      </div>
    </dl>

    <div class="approval-actions">
      <template v-if="properties.inlineDecision">
        <el-button type="primary" :disabled="interactionDisabled || submitting" :loading="submitting" @click="decide('approved')">{{ t('agent.chat.approval.approveRun') }}</el-button>
        <el-button :disabled="interactionDisabled || submitting" @click="decide('rejected')">{{ t('agent.chat.approval.rejectRun') }}</el-button>
      </template>
      <el-button v-else :disabled="submitting" @click="openOwner">
        <el-icon><TopRight /></el-icon>
        {{ t('agent.chat.approval.openOwner') }}
      </el-button>
      <el-button type="primary" :disabled="interactionDisabled" :loading="submitting" @click="checkStatus">
        <el-icon><Refresh /></el-icon>
        {{ t('agent.chat.approval.checkStatus') }}
      </el-button>
    </div>
  </section>
</template>

<script setup>
import { onUnmounted, ref } from 'vue'
import { Refresh, TopRight } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useInteractionDisabled } from '../interactionState.js'

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const submitting = ref(false)
const interactionDisabled = useInteractionDisabled()
const subscription = props.context.componentModel.onUpdated.subscribe(component => {
  properties.value = { ...component.properties }
})

const formatTime = value => value ? new Date(value).toLocaleString() : '-'

async function openOwner() {
  await props.context.dispatchAction({
    event: {
      name: 'owner.open',
      context: { openUrl: properties.value.openUrl }
    }
  })
}

async function checkStatus() {
  if (submitting.value || interactionDisabled.value) return
  submitting.value = true
  try {
    await props.context.dispatchAction({
      event: {
        name: 'interaction.submit',
        context: {
          interactionId: properties.value.interactionId,
          answer: { action: 'check' }
        }
      }
    })
  } finally {
    submitting.value = false
  }
}

async function decide(decision) {
  if (submitting.value || interactionDisabled.value) return
  submitting.value = true
  try {
    await props.context.dispatchAction({ event: {
      name: 'owner.decide',
      context: { owner: properties.value.owner, interactionId: properties.value.interactionId,
        ownerInteractionId: properties.value.ownerInteractionId, requestFingerprint: properties.value.requestFingerprint, decision }
    } })
  } finally { submitting.value = false }
}

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
.approval-config { max-height: 24rem; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
.approval-request {
  box-sizing: border-box;
  width: 100%;
  padding: 14px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
}

.approval-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.approval-title {
  margin: 0;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.approval-owner {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.approval-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 14px 0;
}

.approval-summary div {
  min-width: 0;
}

.approval-summary dt {
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.approval-summary dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
  color: var(--addp-text-primary);
}

.approval-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 640px) {
  .approval-summary {
    grid-template-columns: 1fr;
  }

  .approval-actions {
    flex-wrap: wrap;
  }
}
</style>
