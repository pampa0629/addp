<template>
  <section class="approval-request">
    <div class="approval-header">
      <div>
        <p class="approval-title">{{ t('agent.chat.approval.title') }}</p>
        <p class="approval-owner">{{ properties.owner }} · {{ properties.ownerInteractionId }}</p>
      </div>
      <el-tag type="warning">{{ t('agent.chat.approval.pending') }}</el-tag>
    </div>

    <dl class="approval-summary">
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
      <el-button :disabled="submitting" @click="openOwner">
        <el-icon><TopRight /></el-icon>
        {{ t('agent.chat.approval.openOwner') }}
      </el-button>
      <el-button type="primary" :loading="submitting" @click="checkStatus">
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

const props = defineProps({
  context: { type: Object, required: true },
  buildChild: { type: Function, required: false, default: null }
})

const { t } = useI18n()
const properties = ref({ ...props.context.componentModel.properties })
const submitting = ref(false)
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
  if (submitting.value) return
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

onUnmounted(() => subscription.unsubscribe())
</script>

<style scoped>
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
