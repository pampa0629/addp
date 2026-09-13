<template>
  <el-drawer
    :model-value="modelValue"
    :title="t('security.enrollment.details')"
    size="min(760px, 100vw)"
    @update:model-value="emit('update:modelValue', $event)"
    @close="emit('close')"
    @closed="emit('closed')"
  >
    <template v-if="detail">
      <div class="detail-refresh">
        <span class="refresh-feedback" aria-live="polite">{{ refreshState.feedback }}</span>
        <el-button link type="primary" :icon="Refresh" :loading="refreshState.loading" @click="emit('refresh')">
          {{ t('security.enrollment.refresh') }}
        </el-button>
      </div>
      <section class="detail-resource">
        <div>
          <h3>{{ detail.identity.name }}</h3>
          <p>{{ detail.identity.path }}</p>
        </div>
        <el-tag :type="detail.state.type">{{ detail.state.label }}</el-tag>
      </section>

      <template v-if="detail.releaseAudit">
        <h4>{{ t('security.enrollment.releaseAudit') }}</h4>
        <el-descriptions class="release-audit" :column="2" border>
          <el-descriptions-item :label="t('security.enrollment.releaseBasisLabel')">
            {{ detail.releaseAudit.basis }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.releaseRequestedBy')">
            {{ detail.releaseAudit.requestedBy }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.releaseRequestedAt')">
            {{ detail.releaseAudit.requestedAt }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.releaseAudit.releasedAt" :label="t('security.enrollment.releasedAt')">
            {{ detail.releaseAudit.releasedAt }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('security.enrollment.releaseReasonLabel')" :span="2">
            <span class="release-reason-text">{{ detail.releaseAudit.reason }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </template>

      <h4>{{ t('security.enrollment.discovery') }}</h4>
      <el-alert
        :type="detail.discovery.alertType"
        :closable="false"
        :title="detail.discovery.detailTitle"
        :description="detail.discovery.detailDescription"
        show-icon
      />

      <EnrollmentGovernanceSection
        v-if="governance.visible"
        :enrollment="detail.enrollment"
        :governance="governance"
        :presentation="presentation.governance"
        @update:findings-page="emit('update:findingsPage', $event)"
        @designate="emit('designate')"
        @review="emit('review', $event.finding, $event.decision)"
        @history="emit('history', $event)"
        @revise="emit('revise', $event)"
        @configure-policy="emit('configurePolicy', $event)"
        @restore-policy="emit('restorePolicy', $event)"
        @revoke-assessment="emit('revokeAssessment', $event)"
        @page-change="emit('pageChange', $event)"
      />

      <EnrollmentExemptionSection
        v-if="exemptions.visible"
        ref="exemptionSectionRef"
        :loading="exemptions.loading"
        :exemptions="exemptions.items"
        :focused-exemption-id="exemptions.focusedId"
        :can-revoke="exemptions.canRevoke"
        :presentation="presentation.exemption"
        @revoke="emit('revoke', $event)"
      />

      <h4>{{ t('security.enrollment.ownerProtection') }}</h4>
      <el-alert
        class="owner-protection-hint"
        type="info"
        :closable="false"
        :title="t('security.enrollment.ownerProtectionHint')"
      />
      <div class="owner-detail-list">
        <div v-for="owner in detail.owners" :key="owner.key" class="owner-detail">
          <div>
            <strong>{{ owner.label }}</strong>
            <span>{{ owner.effectDescription }}</span>
          </div>
          <el-tag :type="owner.state.type">{{ owner.state.label }}</el-tag>
        </div>
      </div>

      <el-descriptions class="detail-facts" :column="1" border>
        <el-descriptions-item :label="t('security.enrollment.lastDiscovered')">{{ detail.facts.lastDiscoveredAt }}</el-descriptions-item>
        <el-descriptions-item :label="t('security.enrollment.createdAt')">{{ detail.facts.createdAt }}</el-descriptions-item>
        <el-descriptions-item :label="t('security.enrollment.scope')">{{ t('security.enrollment.wholeResourceScope') }}</el-descriptions-item>
      </el-descriptions>

      <el-collapse class="technical-details">
        <el-collapse-item :title="t('security.enrollment.technicalDetails')" name="technical">
          <el-descriptions :column="1" size="small">
            <el-descriptions-item :label="t('security.enrollment.fingerprint')">
              <span class="technical-value">{{ detail.technical.resourceIdentity }}</span>
            </el-descriptions-item>
            <el-descriptions-item :label="t('security.enrollment.enrollmentId')">
              <span class="technical-value">{{ detail.technical.enrollmentId }}</span>
            </el-descriptions-item>
            <el-descriptions-item v-if="detail.technical.releaseSourceSnapshotHash" :label="t('security.enrollment.releaseSourceSnapshot')">
              <span class="technical-value">{{ detail.technical.releaseSourceSnapshotHash }}</span>
            </el-descriptions-item>
          </el-descriptions>
        </el-collapse-item>
      </el-collapse>

      <div class="detail-actions">
        <el-button v-if="detail.actions.canReEnroll" type="primary" @click="emit('reEnroll', detail.enrollment)">
          {{ t('security.enrollment.reEnroll') }}
        </el-button>
        <el-button v-if="detail.actions.canRediscover" @click="emit('rediscover', detail.enrollment)">
          {{ t('security.enrollment.rediscover') }}
        </el-button>
        <el-button
          v-if="detail.actions.release"
          type="danger"
          plain
          @click="emit('release', detail.enrollment, detail.actions.release.basis)"
        >
          {{ detail.actions.release.label }}
        </el-button>
      </div>
    </template>
  </el-drawer>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Refresh } from '@element-plus/icons-vue'
import EnrollmentExemptionSection from './EnrollmentExemptionSection.vue'
import EnrollmentGovernanceSection from './EnrollmentGovernanceSection.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  enrollment: { type: Object, default: null },
  refreshState: { type: Object, required: true },
  governance: { type: Object, required: true },
  exemptions: { type: Object, required: true },
  lifecycle: { type: Object, required: true },
  presentation: { type: Object, required: true }
})

const emit = defineEmits([
  'update:modelValue',
  'update:findingsPage',
  'close',
  'closed',
  'refresh',
  'designate',
  'review',
  'history',
  'revise',
  'configurePolicy',
  'restorePolicy',
  'revokeAssessment',
  'pageChange',
  'revoke',
  'reEnroll',
  'rediscover',
  'release'
])
const { t } = useI18n()
const exemptionSectionRef = ref(null)
const detail = computed(() => props.enrollment
  ? props.presentation.resource.detail(props.enrollment, props.lifecycle)
  : null)

function focusExemption() {
  return exemptionSectionRef.value?.focusFocused?.()
}

defineExpose({ focusExemption })
</script>

<style scoped>
.refresh-feedback { color: var(--addp-text-tertiary); font-size: 12px; white-space: nowrap; }
.detail-resource { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; padding-bottom: 18px; border-bottom: 1px solid var(--addp-border-color); }
.detail-refresh { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin: -8px 0 8px; }
.detail-resource h3 { margin: 0; font-size: 20px; }
.detail-resource p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.release-audit { margin-bottom: 4px; }
.release-reason-text { white-space: pre-wrap; overflow-wrap: anywhere; }
h4 { margin: 24px 0 12px; }
.owner-protection-hint { margin-bottom: 12px; }
.owner-detail-list { display: flex; flex-direction: column; gap: 10px; }
.owner-detail { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.owner-detail div { display: flex; flex-direction: column; gap: 5px; }
.owner-detail span { color: var(--addp-text-secondary); font-size: 12px; }
.detail-facts { margin-top: 22px; }
.technical-details { margin-top: 18px; }
.technical-value { overflow-wrap: anywhere; font-family: monospace; color: var(--addp-text-secondary); }
.detail-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
:deep(.el-drawer__body) { padding-top: 8px; }
</style>
