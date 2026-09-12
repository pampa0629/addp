<template>
  <section class="exemption-section">
    <div class="exemption-section__header">
      <div>
        <h4>{{ t('security.exemption.title') }}</h4>
        <p>{{ t('security.exemption.hint') }}</p>
      </div>
    </div>
    <el-skeleton v-if="loading" class="exemption-loading" :rows="2" animated />
    <div v-else-if="exemptions.length > 0" class="exemption-list">
      <article
        v-for="exemption in exemptions"
        :key="exemption.id"
        :ref="element => setCardRef(exemption.id, element)"
        class="exemption-card"
        :class="{ 'is-focused': String(exemption.id) === String(focusedExemptionId) }"
      >
        <div class="exemption-card__main">
          <div class="exemption-card__title">
            <strong>{{ assessmentComponent(exemption.assessment_id) }}</strong>
            <el-tag size="small" :type="exemptionStatePresentation(exemption).type">
              {{ exemptionStatePresentation(exemption).label }}
            </el-tag>
          </div>
          <span>{{ t('security.exemption.subject') }}：{{ exemption.subject_id }}</span>
          <span>{{ ownerLabel(exemption.consumer_owner) }} · {{ actionLabel(exemption.action) }}</span>
          <span>{{ t('security.exemption.expiresAt') }}：{{ formatDateTime(exemption.current?.expires_at) }}</span>
          <p>{{ exemption.current?.rationale }}</p>
        </div>
        <div v-if="canRevoke" class="exemption-card__actions">
          <el-button
            v-if="exemption.effective_state === 'active'"
            link
            type="danger"
            @click="emit('revoke', exemption)"
          >
            {{ t('security.exemption.revoke') }}
          </el-button>
        </div>
      </article>
    </div>
    <el-empty v-else :description="t('security.exemption.empty')" :image-size="64" />
  </section>
</template>

<script setup>
import { nextTick, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  loading: { type: Boolean, default: false },
  exemptions: { type: Array, required: true },
  focusedExemptionId: { type: [String, Number], default: '' },
  canRevoke: { type: Boolean, default: false },
  assessmentComponent: { type: Function, required: true },
  exemptionStatePresentation: { type: Function, required: true },
  ownerLabel: { type: Function, required: true },
  actionLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits(['revoke'])
const { t } = useI18n()
const cardRefs = new Map()

function cardKey(exemptionId) {
  return String(exemptionId ?? '')
}

function setCardRef(exemptionId, element) {
  const key = cardKey(exemptionId)
  if (element) cardRefs.set(key, element)
  else cardRefs.delete(key)
}

async function focusFocused() {
  const key = cardKey(props.focusedExemptionId)
  if (!key) return
  await nextTick()
  cardRefs.get(key)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

watch(
  () => [props.focusedExemptionId, props.exemptions],
  focusFocused,
  { flush: 'post' }
)

onMounted(focusFocused)

defineExpose({ focusFocused })
</script>

<style scoped>
.exemption-section { margin-top: 24px; }
.exemption-section__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.exemption-section__header h4 { margin: 0; }
.exemption-section__header p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.exemption-loading { margin-top: 14px; }
.exemption-list { display: flex; flex-direction: column; gap: 8px; margin-top: 12px; }
.exemption-card { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.exemption-card.is-focused { border-color: var(--el-color-primary); box-shadow: 0 0 0 1px var(--el-color-primary); }
.exemption-card__main { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 4px; }
.exemption-card__title { display: flex; align-items: center; gap: 8px; }
.exemption-card__title strong { min-width: 0; overflow-wrap: anywhere; }
.exemption-card__main > span, .exemption-card__main > p { margin: 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; }
.exemption-card__actions { display: flex; flex: 0 0 auto; gap: 4px; }
@media (max-width: 720px) {
  .exemption-section__header, .exemption-card { flex-direction: column; }
}
</style>
