<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t('security.assessment.historyTitle')"
    width="min(720px, calc(100vw - 24px))"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <div v-loading="loading" class="assessment-history">
      <template v-if="assessment">
        <div class="policy-target">
          <strong>{{ assessment.component_key }}</strong>
          <span>{{ t('security.assessment.historyHint') }}</span>
        </div>
        <el-timeline v-if="assessment.history?.length">
          <el-timeline-item
            v-for="revision in assessment.history"
            :key="revision.id"
            :timestamp="formatDateTime(revision.created_at)"
            :type="isCurrentRevision(revision) ? 'primary' : undefined"
            placement="top"
          >
            <article class="assessment-history__item">
              <div class="assessment-history__header">
                <strong>{{ t('security.assessment.revisionLabel', { revision: revision.revision }) }}</strong>
                <el-tag v-if="isCurrentRevision(revision)" size="small" type="primary">
                  {{ t('security.assessment.currentRevision') }}
                </el-tag>
                <el-tag size="small" :type="revision.conclusion === 'sensitive' ? 'success' : 'info'">
                  {{ assessmentConclusionLabel(revision.conclusion) }}
                </el-tag>
                <el-tag size="small" effect="plain">
                  {{ assessmentRevisionSourceLabel(revision.source_kind) }}
                </el-tag>
              </div>
              <p class="assessment-history__summary">{{ assessmentRevisionSummary(revision) }}</p>
              <p>{{ revision.rationale }}</p>
              <small>{{ t('security.assessment.historyMeta', { actor: assessmentActorLabel(revision.created_by), time: formatDateTime(revision.created_at) }) }}</small>
            </article>
          </el-timeline-item>
        </el-timeline>
        <el-empty v-else :description="t('security.assessment.emptyHistory')" :image-size="64" />
      </template>
    </div>
    <template #footer>
      <el-button @click="emit('close')">{{ t('security.common.close') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  assessment: { type: Object, default: null },
  formatDateTime: { type: Function, required: true },
  assessmentConclusionLabel: { type: Function, required: true },
  assessmentRevisionSourceLabel: { type: Function, required: true },
  assessmentRevisionSummary: { type: Function, required: true },
  assessmentActorLabel: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'close', 'closed'])
const { t } = useI18n()

function isCurrentRevision(revision) {
  return Number(revision?.revision) === Number(props.assessment?.current_revision)
}
</script>

<style scoped>
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.assessment-history { min-height: 120px; }
.assessment-history :deep(.el-timeline) { padding-left: 6px; }
.assessment-history__item { padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.assessment-history__header { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; }
.assessment-history__item p { margin: 7px 0 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.assessment-history__item .assessment-history__summary { color: var(--addp-text-primary); font-weight: 600; }
.assessment-history__item small { display: block; margin-top: 8px; color: var(--addp-text-tertiary); }
</style>
