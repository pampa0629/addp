<template>
  <div class="finding-list">
    <article v-for="{ finding, assessment } in rows" :key="finding.id" class="finding-card">
      <div class="finding-card__header">
        <div>
          <strong>{{ finding.component_key }}</strong>
          <span>{{ presentation.typeName(finding.sensitive_data_type_id) }}</span>
        </div>
        <el-tag size="small" :type="presentation.state(finding).type">
          {{ presentation.state(finding).label }}
        </el-tag>
      </div>
      <div class="finding-explanation">
        <section class="explanation-stage">
          <div class="explanation-stage__title">
            <span>1</span>
            <strong>{{ t('security.finding.explanationStages.detection') }}</strong>
          </div>
          <p class="explanation-primary">{{ presentation.capabilityName(finding) }}</p>
          <p>{{ presentation.evidenceDescription(finding) }}</p>
          <dl class="detection-rule-audit">
            <div>
              <dt>{{ t('security.finding.ruleAudit.actualEvidence') }}</dt>
              <dd>{{ presentation.evidenceAuditDescription(finding) }}</dd>
            </div>
            <div class="detection-rule-audit__details">
              <dt>{{ t('security.finding.ruleAudit.details') }}</dt>
              <dd>
                <el-popover placement="top-start" trigger="click" :width="420" popper-class="security-rule-popover">
                  <template #reference>
                    <el-button class="rule-help-button" link type="primary" :icon="QuestionFilled" :aria-label="t('security.finding.ruleAudit.viewDetails')" />
                  </template>
                  <dl class="recognition-rule-details">
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.method') }}</dt>
                      <dd>{{ presentation.capabilityText(finding, 'method_i18n_key') }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.scope') }}</dt>
                      <dd>{{ presentation.capabilityScope(finding) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.privacy') }}</dt>
                      <dd>{{ presentation.capabilityText(finding, 'privacy_i18n_key') }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.limitations') }}</dt>
                      <dd>{{ presentation.capabilityText(finding, 'limitations_i18n_key') }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('security.finding.ruleAudit.version') }}</dt>
                      <dd class="technical-value">{{ finding.explanation?.capability?.key || finding.detector_version }}</dd>
                    </div>
                  </dl>
                </el-popover>
              </dd>
            </div>
          </dl>
          <div class="explanation-tags">
            <el-tag size="small" effect="plain">{{ t('security.finding.confidenceValue', { value: presentation.confidenceLabel(finding.confidence) }) }}</el-tag>
            <el-tag
              v-if="finding.explanation?.automatic_adoption_threshold != null"
              size="small"
              :type="finding.explanation.meets_automatic_threshold ? 'success' : 'warning'"
            >
              {{ t('security.finding.thresholdValue', { value: presentation.confidenceLabel(finding.explanation.automatic_adoption_threshold) }) }}
            </el-tag>
          </div>
        </section>

        <section class="explanation-stage">
          <div class="explanation-stage__title">
            <span>2</span>
            <strong>{{ t('security.finding.explanationStages.governance') }}</strong>
          </div>
          <el-tag size="small" :type="presentation.decision(finding).type">
            {{ presentation.decision(finding).label }}
          </el-tag>
          <p class="explanation-primary">{{ presentation.effectiveDefinitionSummary(finding) }}</p>
          <p>{{ presentation.baselineDescription(finding) }}</p>
          <p v-if="assessment.active" class="resource-policy-summary">
            {{ assessment.protectionSummary }}
          </p>
        </section>

        <section class="explanation-stage">
          <div class="explanation-stage__title">
            <span>3</span>
            <strong>{{ t('security.finding.explanationStages.execution') }}</strong>
          </div>
          <div class="finding-outlets">
            <div v-for="outlet in finding.explanation.outlets" :key="outlet.consumer_owner" class="finding-outlet">
              <span>{{ presentation.ownerLabel(outlet.consumer_owner) }}</span>
              <strong>{{ presentation.outletRuleDescription(finding, outlet.consumer_owner) }}</strong>
              <el-tag size="small" :type="presentation.outletAcknowledgement(outlet).type">
                {{ presentation.outletAcknowledgement(outlet).label }}
              </el-tag>
            </div>
          </div>
        </section>
      </div>
      <p class="finding-observed-at">{{ t('security.finding.observedAt') }}：{{ presentation.formatDateTime(finding.observed_at) }}</p>
      <div v-if="finding.review" class="review-result">
        <span>{{ t('security.finding.reviewRationale') }}</span>
        <p>{{ finding.review.rationale }}</p>
      </div>
      <div v-if="!finding.review && canReview" class="finding-card__actions">
        <el-button type="danger" plain @click="emit('review', finding, 'reject')">{{ t('security.finding.markFalsePositive') }}</el-button>
        <el-button type="primary" plain @click="emit('review', finding, 'confirm')">{{ t('security.finding.review') }}</el-button>
      </div>
      <div v-else-if="assessment.current" class="finding-card__actions">
        <el-button plain @click="emit('history', assessment.current)">{{ t('security.assessment.history') }}</el-button>
        <el-button v-if="canUpdateAssessments" plain @click="emit('revise', assessment.current)">
          {{ t('security.assessment.reviseConclusion') }}
        </el-button>
        <el-button
          v-if="assessment.active && assessment.canConfigurePolicy"
          type="primary"
          plain
          @click="emit('configurePolicy', assessment.active)"
        >
          {{ assessment.policy?.state === 'active' ? t('security.policy.adjust') : t('security.policy.tighten') }}
        </el-button>
        <el-button
          v-if="assessment.policy?.state === 'active' && canRevokePolicies"
          plain
          @click="emit('restorePolicy', assessment.active)"
        >
          {{ t('security.policy.restoreDefault') }}
        </el-button>
        <el-button
          v-if="assessment.active && canUpdateAssessments"
          type="danger"
          plain
          @click="emit('revokeAssessment', assessment.active)"
        >
          {{ t('security.assessment.revokeConclusion') }}
        </el-button>
      </div>
    </article>
  </div>
  <el-pagination
    v-if="total > pageSize"
    :current-page="page"
    class="finding-pagination"
    small
    background
    layout="total, prev, pager, next"
    :page-size="pageSize"
    :total="total"
    @current-change="handlePageChange"
  />
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { QuestionFilled } from '@element-plus/icons-vue'

const props = defineProps({
  findings: { type: Array, required: true },
  total: { type: Number, required: true },
  page: { type: Number, required: true },
  pageSize: { type: Number, required: true },
  canReview: { type: Boolean, default: false },
  canUpdateAssessments: { type: Boolean, default: false },
  canRevokePolicies: { type: Boolean, default: false },
  presentation: { type: Object, required: true }
})

const emit = defineEmits(['review', 'history', 'revise', 'configurePolicy', 'restorePolicy', 'revokeAssessment', 'update:page', 'pageChange'])
const { t } = useI18n()
const rows = computed(() => props.findings.map(finding => ({
  finding,
  assessment: props.presentation.assessmentView(finding)
})))

function handlePageChange(page) {
  emit('update:page', page)
  emit('pageChange', page)
}
</script>

<style scoped>
.finding-list { display: flex; flex-direction: column; gap: 12px; }
.finding-card { container: finding-card / inline-size; padding: 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.finding-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-card__header > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.finding-card__header strong { overflow-wrap: anywhere; font-size: 15px; }
.finding-card__header span { color: var(--addp-text-secondary); font-size: 12px; }
.finding-explanation { display: grid; grid-template-columns: minmax(0, .9fr) minmax(0, 1fr) minmax(0, 1.25fr); overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-primary); }
.explanation-stage { min-width: 0; padding: 12px; }
.explanation-stage + .explanation-stage { border-left: 1px solid var(--addp-border-color); }
.explanation-stage__title { display: flex; align-items: center; gap: 7px; margin-bottom: 10px; color: var(--addp-text-primary); }
.explanation-stage__title > span { display: inline-flex; width: 20px; height: 20px; align-items: center; justify-content: center; flex: 0 0 auto; color: var(--el-color-primary); font-size: 12px; font-weight: 700; border: 1px solid var(--el-color-primary); border-radius: 50%; }
.explanation-stage__title strong { font-size: 13px; }
.explanation-stage p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.explanation-stage .explanation-primary, .resource-policy-summary { color: var(--addp-text-primary); font-weight: 600; }
.explanation-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.detection-rule-audit { display: flex; flex-direction: column; gap: 7px; margin: 12px 0 0; padding-top: 10px; border-top: 1px dashed var(--addp-border-color); }
.detection-rule-audit > div { display: grid; grid-template-columns: 88px minmax(0, 1fr); gap: 8px; font-size: 12px; line-height: 1.5; }
.detection-rule-audit dt { color: var(--addp-text-tertiary); }
.detection-rule-audit dd { margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.detection-rule-audit__details { align-items: center; }
.detection-rule-audit__details dd { display: flex; align-items: center; }
.rule-help-button { min-height: 24px; padding: 0 2px; }
.recognition-rule-details { display: flex; flex-direction: column; gap: 10px; margin: 0; }
.recognition-rule-details > div { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 10px; font-size: 12px; line-height: 1.6; }
.recognition-rule-details dt { color: var(--addp-text-tertiary); }
.recognition-rule-details dd { margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.technical-value { overflow-wrap: anywhere; font-family: monospace; color: var(--addp-text-secondary); }
:global(.security-rule-popover) { max-width: calc(100vw - 32px); }
.finding-outlets { display: flex; flex-direction: column; gap: 7px; }
.finding-outlet { display: grid; grid-template-columns: minmax(78px, auto) minmax(0, 1fr) auto; align-items: center; gap: 7px; font-size: 12px; }
.finding-outlet > span { color: var(--addp-text-secondary); }
.finding-outlet > strong { min-width: 0; color: var(--addp-text-primary); font-weight: 500; line-height: 1.4; overflow-wrap: anywhere; }
.finding-observed-at { margin: 8px 0 0; color: var(--addp-text-tertiary); font-size: 12px; text-align: right; }
.review-result { margin-top: 12px; padding: 10px 12px; border-left: 3px solid var(--el-color-primary); background: var(--addp-bg-primary); }
.review-result span { color: var(--addp-text-tertiary); font-size: 12px; }
.review-result p { margin: 4px 0 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.finding-card__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.finding-pagination { justify-content: flex-end; margin-top: 14px; }
@container finding-card (max-width: 920px) {
  .finding-explanation { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .explanation-stage:nth-child(3) { grid-column: 1 / -1; border-top: 1px solid var(--addp-border-color); border-left: 0; }
}
@container finding-card (max-width: 560px) {
  .finding-explanation { grid-template-columns: 1fr; }
  .explanation-stage:nth-child(3) { grid-column: auto; }
  .explanation-stage + .explanation-stage { border-top: 1px solid var(--addp-border-color); border-left: 0; }
}
</style>
