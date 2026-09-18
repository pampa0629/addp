<template>
  <el-dialog :model-value="modelValue" :title="t(status === 'accepted' ? 'quality.issue.acceptCurrent' : 'quality.issue.markResolved')" width="600px" :show-close="!loading" :close-on-press-escape="!loading" :close-on-click-modal="false" @update:model-value="$emit('update:modelValue', $event)">
    <template v-if="current">
      <p>{{ t('quality.issue.acceptanceHelp') }}</p>
      <p v-if="current.type !== 'row_count'">{{ t('quality.issue.currentCounts', { failed: current.failed_count, accepted: current.accepted_count, pending: current.pending_count }) }}</p>
      <el-alert v-if="error" :title="error" type="error" :closable="false" />
      <el-button v-if="conflict" :loading="loading" @click="reload">{{ t('quality.issue.reloadObservation') }}</el-button>
      <el-input v-model="note" type="textarea" :rows="4" maxlength="4000" :placeholder="t('quality.issue.notePrompt')" :aria-label="t('quality.issue.resolutionNote')" />
    </template>
    <template #footer>
      <el-button :disabled="loading" @click="$emit('update:modelValue', false)">{{ t('quality.issue.cancel') }}</el-button>
      <el-button type="primary" :loading="loading" :disabled="!canSubmit" @click="save">{{ t('quality.issue.confirm') }}</el-button>
    </template>
  </el-dialog>
</template>
<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { issueAPI } from '../api/quality'
const props = defineProps({ modelValue: Boolean, issue: Object, status: String })
const emit = defineEmits(['update:modelValue', 'updated'])
const { t } = useI18n()
const current = ref(null)
const note = ref('')
const loading = ref(false)
const conflict = ref(false)
const error = ref('')
const canSubmit = computed(() => current.value?.status === 'open' && current.value?.version > 0 && note.value.trim() && !conflict.value && !loading.value && (props.status !== 'accepted' || current.value.evidence_reason === ''))
watch(() => props.modelValue, open => {
  if (!open) return
  current.value = props.issue
  note.value = ''
  conflict.value = false
  error.value = ''
})
async function reload() {
  loading.value = true
  try {
    current.value = await issueAPI.get(current.value.id)
    conflict.value = false
    error.value = ''
  } catch (e) { error.value = e.response?.data?.error || t('quality.issue.detailLoadFailed') }
  finally { loading.value = false }
}
async function save() {
  if (!canSubmit.value) return
  loading.value = true
  try {
    const updated = await issueAPI.updateStatus(current.value.id, current.value.version, props.status, note.value.trim())
    emit('updated', updated)
    emit('update:modelValue', false)
  } catch (e) {
    error.value = e.response?.data?.error || t('quality.issue.updateFailed')
    conflict.value = e.response?.status === 409
  } finally { loading.value = false }
}
</script>
