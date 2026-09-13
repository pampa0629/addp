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
      <template v-if="presentation">
        <div class="policy-target">
          <strong>{{ presentation.componentKey }}</strong>
          <span>{{ t('security.assessment.historyHint') }}</span>
        </div>
        <el-timeline v-if="presentation.items.length">
          <el-timeline-item
            v-for="row in presentation.items"
            :key="row.id"
            :timestamp="row.timestamp"
            :type="row.current ? 'primary' : undefined"
            placement="top"
          >
            <article class="assessment-history__item">
              <div class="assessment-history__header">
                <strong>{{ row.revisionLabel }}</strong>
                <el-tag v-if="row.current" size="small" type="primary">
                  {{ t('security.assessment.currentRevision') }}
                </el-tag>
                <el-tag size="small" :type="row.conclusion.type">
                  {{ row.conclusion.label }}
                </el-tag>
                <el-tag size="small" effect="plain">
                  {{ row.sourceLabel }}
                </el-tag>
              </div>
              <p class="assessment-history__summary">{{ row.summary }}</p>
              <p>{{ row.rationale }}</p>
              <small>{{ row.metadata }}</small>
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

defineProps({
  modelValue: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  presentation: { type: Object, default: null }
})

const emit = defineEmits(['update:modelValue', 'close', 'closed'])
const { t } = useI18n()
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
