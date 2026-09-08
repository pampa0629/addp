<template>
  <el-dialog
    :model-value="modelValue"
    :title="t('manager.quickViewCreator.title')"
    width="860px"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
    @closed="emit('closed')"
  >
    <el-alert
      class="creator-hint"
      type="info"
      :closable="false"
      show-icon
      :title="t('manager.quickViewCreator.hint')"
    />

    <el-form label-position="top">
      <el-form-item :label="t('manager.quickViewCreator.source')">
        <div class="source-picker" v-loading="detecting">
          <ResourceTreePicker
            v-model="sourceSelection"
            mode="item"
            :initial-locator="locator"
            tree-height="360px"
          />
        </div>
      </el-form-item>

      <el-form-item v-if="sourceSelection" :label="t('manager.quickViewCreator.generationType')">
        <el-radio-group v-if="options.length" v-model="selectedAction" class="generation-options">
          <el-radio v-for="option in options" :key="option.action" :value="option.action">
            {{ t(option.labelKey) }}
          </el-radio>
        </el-radio-group>
        <el-alert
          v-else-if="!detecting"
          type="warning"
          :closable="false"
          show-icon
          :title="capabilityError || t(`manager.quickViewCreator.${emptyReason}`)"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">
        {{ t('manager.quickViewCreator.generate') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTreePicker } from '@addp/common-frontend'
import { quickViewAPI } from '@/api/quickView'
import { useCurrentResultConfirmation } from '@/composables/useCurrentResultConfirmation'
import { toQuickViewExistingResultPayload } from '@/utils/currentResultConfirmation'
import {
  generationOptionsForCapability,
  isPPTXGenerationSource,
  PPTX_PDF_GENERATION_ACTION,
  pptxGenerationOptions,
  quickViewCreationEmptyReason,
  quickViewTaskTypeForAction
} from '@/utils/quickViewTaskCreation'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  taskType: { type: String, default: '' },
  locator: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'created', 'closed'])
const { t } = useI18n()
const confirmCurrentResult = useCurrentResultConfirmation()
const sourceSelection = ref(null)
const options = ref([])
const selectedAction = ref('')
const detecting = ref(false)
const submitting = ref(false)
const capabilityError = ref('')
const emptyReason = ref('unsupported')
let detectionSequence = 0

const sourceLocator = computed(() => String(sourceSelection.value?.identity?.locator || '').trim())
const canSubmit = computed(() => Boolean(sourceLocator.value && selectedAction.value && !detecting.value))

function reset() {
  detectionSequence += 1
  sourceSelection.value = null
  options.value = []
  selectedAction.value = ''
  detecting.value = false
  submitting.value = false
  capabilityError.value = ''
  emptyReason.value = 'unsupported'
}

function applyOptions(nextOptions) {
  options.value = nextOptions
  if (!nextOptions.some(option => option.action === selectedAction.value)) {
    selectedAction.value = nextOptions.length === 1 ? nextOptions[0].action : ''
  }
}

watch(sourceSelection, async selection => {
  const sequence = ++detectionSequence
  applyOptions([])
  capabilityError.value = ''
  emptyReason.value = 'unsupported'
  if (!selection) return

  if (isPPTXGenerationSource(selection)) {
    applyOptions(pptxGenerationOptions(props.taskType))
    return
  }
  if (props.taskType === 'pptx_pdf_generation') return

  detecting.value = true
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(sourceLocator.value)
    if (sequence !== detectionSequence) return
    applyOptions(generationOptionsForCapability(capability, props.taskType))
    emptyReason.value = quickViewCreationEmptyReason(capability, props.taskType)
  } catch (error) {
    if (sequence !== detectionSequence) return
    capabilityError.value = error?.response?.data?.error || t('manager.quickViewCreator.detectFailed')
  } finally {
    if (sequence === detectionSequence) detecting.value = false
  }
})

watch(() => [props.modelValue, props.taskType, props.locator], ([visible]) => {
  if (visible) reset()
}, { immediate: true })

async function submit() {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    let response
    if (selectedAction.value === PPTX_PDF_GENERATION_ACTION) {
      response = await quickViewAPI.ensurePPTXPDFPreview(sourceLocator.value, { retry: true })
    } else {
      response = await confirmCurrentResult(payload => quickViewAPI.executeQuickViewAction(
        sourceLocator.value,
        selectedAction.value,
        toQuickViewExistingResultPayload(payload)
      ))
    }
    const result = response?.data ?? response
    const taskType = result?.task_type || quickViewTaskTypeForAction(selectedAction.value)
    ElMessage.success(result?.status === 'ready'
      ? t('manager.quickViewCreator.ready')
      : t('manager.quickViewCreator.queued'))
    emit('created', {
      taskType,
      taskId: Number(result?.task_id || 0),
      executionId: String(result?.execution_id || ''),
      status: String(result?.status || '')
    })
    emit('update:modelValue', false)
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.error || error?.message || t('manager.quickViewCreator.generateFailed'))
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.creator-hint { margin-bottom: 18px; }
.source-picker { width: 100%; }
.generation-options { display: flex; flex-direction: column; align-items: flex-start; gap: 10px; }
</style>
