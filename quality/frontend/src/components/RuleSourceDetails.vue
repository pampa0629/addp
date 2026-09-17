<template>
  <section class="source-details" :aria-label="t('quality.rule.source')">
    <el-tag>{{ t(preview ? 'quality.rule.sourcePreview' : source ? 'quality.rule.standardSource' : 'quality.rule.manualSource') }}</el-tag>
    <template v-if="source">
      <p>{{ t('quality.rule.sourceIdentity', { element: source.element_id, revision: source.element_revision_id }) }}</p>
      <p v-if="!canRead">{{ t('quality.rule.sourcePermission') }}</p>
      <template v-else>
        <p v-if="loading" role="status">{{ t('quality.rule.sourceLoading') }}</p>
        <el-alert v-if="error" :title="error" type="error" :closable="false" />
        <el-button v-if="error" link @click="load">{{ t('quality.rule.retry') }}</el-button>
        <template v-if="detail">
          <p>{{ t('quality.rule.sourceElement') }}：{{ detail.name }} · {{ detail.element_code }} · R{{ detail.revision_no }}</p>
          <p v-if="detail.code_set_revision">{{ t('quality.rule.sourceCodeSet') }}：{{ detail.code_set_revision.name }} · {{ detail.code_set_revision.code }} · R{{ detail.code_set_revision.revision_no }}</p>
          <el-button link @click="expanded = !expanded">{{ t(expanded ? 'quality.rule.hideSource' : 'quality.rule.viewSource') }}</el-button>
          <div v-if="expanded" class="source-evidence">
            <p>{{ detail.definition }}</p>
            <p v-if="source.rule_key">{{ t('quality.rule.sourceRuleKey') }}：{{ source.rule_key }}</p>
            <template v-if="detail.code_set_revision">
              <p>{{ detail.code_set_revision.description }}</p>
              <el-table :data="detail.code_set_revision.items" border max-height="240">
                <el-table-column prop="code" :label="t('quality.rule.valueCode')" min-width="130" />
                <el-table-column prop="label" :label="t('quality.rule.valueLabel')" min-width="130" />
                <el-table-column prop="definition" :label="t('quality.plan.description')" min-width="170" />
              </el-table>
            </template>
          </div>
        </template>
      </template>
    </template>
    <p v-else>{{ t('quality.rule.manualSourceHint') }}</p>
  </section>
</template>
<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { standardSourceAPI } from '../api/quality'
const props = defineProps({ source: { type: Object, default: null }, canRead: Boolean, preview: Boolean })
const emit = defineEmits(['loaded'])
const { t } = useI18n()
const detail = ref(null), loading = ref(false), error = ref(''), expanded = ref(false)
let sequence = 0
async function load() {
  const current = ++sequence
  detail.value = null
  error.value = ''
  expanded.value = false
  loading.value = false
  emit('loaded', null)
  if (!props.source || !props.canRead) return
  const { element_id, element_revision_id } = props.source
  loading.value = true
  try {
    const result = await standardSourceAPI.getRevision(element_id, element_revision_id)
    if (current !== sequence) return
    if (String(result.id) !== String(element_revision_id) || String(result.element_id) !== String(element_id)) throw new Error('source identity mismatch')
    detail.value = result
    emit('loaded', result)
  } catch (failure) {
    if (current === sequence) error.value = t(failure.response?.status === 403 ? 'quality.rule.sourcePermission' : 'quality.rule.sourceFailed')
  } finally {
    if (current === sequence) loading.value = false
  }
}
watch(() => [props.source?.element_id, props.source?.element_revision_id, props.source?.rule_key, props.canRead], load, { immediate: true })
onBeforeUnmount(() => { sequence++ })
</script>
<style scoped>
.source-details { margin: 16px 0; padding: 16px; background: var(--addp-bg-secondary); border: 1px solid var(--addp-border-color); border-radius: 4px; overflow-wrap: anywhere; }
.source-details p { margin: 8px 0; color: var(--addp-text-secondary); }
</style>
