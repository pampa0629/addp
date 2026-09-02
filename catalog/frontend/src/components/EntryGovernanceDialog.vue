<template>
  <el-dialog
    :model-value="visible"
    :title="t(`catalog.governance.${mode}.title`)"
    width="min(620px, 92vw)"
    :close-on-click-modal="false"
    @update:model-value="value => emit('update:visible', value)"
  >
    <el-alert
      :type="mode === 'withdraw-certification' ? 'warning' : 'error'"
      :closable="false"
      show-icon
      :title="t(`catalog.governance.${mode}.description`)"
      class="governance-alert"
    />
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('catalog.governance.reason')" required>
        <el-input
          v-model="form.reason"
          type="textarea"
          :rows="3"
          maxlength="500"
          show-word-limit
          :placeholder="t(`catalog.governance.${mode}.reasonPlaceholder`)"
        />
      </el-form-item>
      <el-form-item v-if="showsSuccessor" :label="t('catalog.governance.recommendedSuccessor')">
        <el-select
          v-model="form.recommendedSuccessorEntryId"
          clearable
          filterable
          remote
          reserve-keyword
          :loading="successorLoading"
          :remote-method="searchSuccessors"
          :placeholder="t('catalog.governance.recommendedSuccessorPlaceholder')"
          @visible-change="opened => opened && searchSuccessors('')"
        >
          <el-option
            v-for="option in successorOptions"
            :key="option.id"
            :label="successorLabel(option)"
            :value="option.id"
          />
        </el-select>
        <div class="field-hint">{{ t('catalog.governance.recommendedSuccessorHint') }}</div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:visible', false)">{{ t('catalog.edit.cancel') }}</el-button>
      <el-button
        :type="mode === 'withdraw-certification' ? 'warning' : 'danger'"
        :loading="saving"
        @click="submit"
      >
        {{ t(`catalog.governance.${mode}.confirm`) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { listEntries } from '../api/catalog'
import { isCanonicalUUID } from '../utils/entryEdit'

const props = defineProps({
  entry: { type: Object, required: true },
  visible: { type: Boolean, default: false },
  mode: {
    type: String,
    required: true,
    validator: value => ['withdraw-certification', 'deprecate', 'deprecated'].includes(value)
  },
  saving: { type: Boolean, default: false }
})
const emit = defineEmits(['update:visible', 'submit'])
const { t } = useI18n()
const form = reactive(emptyForm(props.entry))
const successorOptions = ref(initialSuccessorOptions(props.entry))
const successorLoading = ref(false)
let successorRequestVersion = 0
const showsSuccessor = computed(() => ['deprecate', 'deprecated'].includes(props.mode))

watch(() => [props.visible, props.entry, props.mode], ([visible, entry]) => {
  if (!visible) return
  Object.assign(form, emptyForm(entry))
  successorOptions.value = initialSuccessorOptions(entry)
}, { deep: true })

function emptyForm(entry) {
  return {
    reason: '',
    recommendedSuccessorEntryId: entry?.recommended_successor_entry_id || ''
  }
}

function initialSuccessorOptions(entry) {
  if (entry?.recommended_successor) return [entry.recommended_successor]
  if (entry?.recommended_successor_entry_id) {
    return [{ id: entry.recommended_successor_entry_id, display_name: t('catalog.edit.referenceUnavailable') }]
  }
  return []
}

function successorLabel(option) {
  const name = option.display_name || t('catalog.entries.unnamed')
  return option.governance_status ? `${name} · ${t(`catalog.status.governance.${option.governance_status}`)}` : name
}

async function searchSuccessors(search = '') {
  const version = ++successorRequestVersion
  successorLoading.value = true
  try {
    const query = String(search || '').trim()
    const responses = await Promise.all(['curated', 'certified'].map(governanceStatus => listEntries({
      ...(query ? { search: query } : {}),
      source_status: 'active',
      governance_status: governanceStatus,
      page: 1,
      page_size: 20
    })))
    if (version !== successorRequestVersion) return
    const options = new Map(initialSuccessorOptions(props.entry).map(item => [item.id, item]))
    for (const response of responses) {
      for (const candidate of response.data || []) {
        if (candidate.id !== props.entry.id) options.set(candidate.id, candidate)
      }
    }
    successorOptions.value = [...options.values()]
  } catch (error) {
    if (version === successorRequestVersion) {
      ElMessage.error(error?.response?.data?.error || t('catalog.governance.successorSearchFailed'))
    }
  } finally {
    if (version === successorRequestVersion) successorLoading.value = false
  }
}

function submit() {
  if (!form.reason.trim()) {
    ElMessage.error(t('catalog.governance.reasonRequired'))
    return
  }
  if (showsSuccessor.value && form.recommendedSuccessorEntryId && !isCanonicalUUID(form.recommendedSuccessorEntryId)) {
    ElMessage.error(t('catalog.governance.invalidSuccessor'))
    return
  }
  emit('submit', {
    reason: form.reason,
    recommendedSuccessorEntryId: showsSuccessor.value ? form.recommendedSuccessorEntryId : ''
  })
}
</script>

<style scoped>
.governance-alert { margin-bottom: 18px; }
.field-hint { color: var(--addp-text-secondary); font-size: 12px; margin-top: 6px; }
:deep(.el-select) { width: 100%; }
</style>
