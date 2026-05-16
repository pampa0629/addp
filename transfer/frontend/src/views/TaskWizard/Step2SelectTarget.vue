<template>
  <div class="step2-select-target">
    <h3>{{ t('transfer.taskWizard.selectTargetPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.selectTargetPageDesc') }}</p>

    <el-form :model="formData" label-width="120px">
      <el-form-item :label="t('transfer.taskWizard.targetTypeLabel')">
        <el-radio-group v-model="targetType" @change="handleTargetTypeChange">
          <el-radio-button value="nfs">NFS 文件系统</el-radio-button>
          <el-radio-button value="s3">{{ t('transfer.taskWizard.objectStorageOption') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.targetEngineLabel')">
        <el-select
          v-model="formData.engineID"
          :placeholder="t('transfer.taskWizard.targetEnginePlaceholder')"
          filterable
          :loading="loadingEngines"
          @change="syncTarget"
        >
          <el-option
            v-for="engine in filteredEngines"
            :key="engine.id"
            :label="`${engine.name} (${engine.engine_type})`"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.outputFormatLabel')">
        <el-select v-model="outputFormat" :placeholder="t('transfer.taskWizard.outputFormatLabel')" @change="syncTarget">
          <el-option label="CSV" value="csv" />
          <el-option label="TSV" value="tsv" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.outputPathLabel')">
        <el-input
          v-model="outputPath"
          :placeholder="targetType === 'nfs' ? '例如：exports/roads' : t('transfer.taskWizard.storagePathPlaceholder')"
          :readonly="targetType === 's3'"
          @click="targetType === 's3' && (showOutputPathPicker = true)"
          @input="syncTarget"
        >
          <template v-if="targetType === 's3'" #append>
            <el-button :disabled="!formData.engineID" @click="showOutputPathPicker = true">{{ t('transfer.taskWizard.browse') }}</el-button>
          </template>
        </el-input>
      </el-form-item>

      <el-form-item :label="t('transfer.taskWizard.outputFileNameLabel')">
        <el-input
          v-model="outputFileName"
          :placeholder="outputFormat === 'tsv' ? 'example.tsv' : 'example.csv'"
          @input="syncTarget"
        />
      </el-form-item>

      <el-form-item v-if="outputFormat === 'csv'" :label="t('transfer.taskWizard.csvOptionsLabel')">
        <div class="csv-options">
          <el-checkbox v-model="csvHeaders" @change="syncTarget">{{ t('transfer.taskWizard.csvHeadersLabel') }}</el-checkbox>
          <div class="delimiter-control">
            <span>{{ t('transfer.taskWizard.csvDelimiterLabel') }}：</span>
            <el-input v-model="csvDelimiter" placeholder="," @input="syncTarget" />
          </div>
        </div>
      </el-form-item>
    </el-form>

    <ObjectStoragePathPicker
      v-if="targetType === 's3'"
      v-model:visible="showOutputPathPicker"
      scope="system"
      :resource-id="formData.engineID"
      :initial-prefix="outputPath"
      @selected="handleOutputPathSelected"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { systemEnginesAPI } from '@/api/systemEngines'
import ObjectStoragePathPicker from '@/components/ObjectStoragePathPicker.vue'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const formData = reactive({
  engineID: null
})

const targetType = ref('nfs')
const outputFormat = ref('csv')
const outputPath = ref('')
const outputFileName = ref('')
const csvHeaders = ref(true)
const csvDelimiter = ref(',')
const showOutputPathPicker = ref(false)

const engines = ref([])
const loadingEngines = ref(false)

const selectedEngine = computed(() => {
  return engines.value.find(engine => engine.id === formData.engineID) || null
})

const filteredEngines = computed(() => {
  return engines.value.filter(engine => matchesEngineType(engine.engine_type, targetType.value))
})

const canProceed = computed(() => {
  if (targetType.value === 's3') {
    return !!(formData.engineID && outputPath.value.trim() && outputFileName.value.trim())
  }
  return !!(formData.engineID && outputFileName.value.trim())
})

watch(canProceed, (ready) => {
  if (ready) {
    syncTarget()
  }
})

function matchesEngineType(engineType, expected) {
  const type = (engineType || '').toLowerCase()
  if (expected === 'nfs') return type === 'nfs'
  if (expected === 's3') return type.includes('s3') || type.includes('minio') || type.includes('oss')
  return false
}

function syncTarget() {
  if (!canProceed.value || !selectedEngine.value) return

  props.wizardState.updateTarget({
    engineID: formData.engineID,
    engineType: selectedEngine.value.engine_type,
    scope: 'system',
    targetType: targetType.value,
    extra: {
      format: outputFormat.value,
      resourcePath: outputPath.value,
      resourceFile: outputFileName.value,
      includeHeader: csvHeaders.value,
      delimiter: csvDelimiter.value,
      writeMode: 'overwrite'
    }
  })
}

function handleTargetTypeChange() {
  formData.engineID = null
  outputPath.value = ''
  outputFileName.value = ''
}

function handleOutputPathSelected(path) {
  outputPath.value = path
  syncTarget()
}

async function loadEngines() {
  loadingEngines.value = true
  try {
    const data = await systemEnginesAPI.list()
    engines.value = (data || []).filter(engine =>
      engine?.id !== undefined &&
      engine?.id !== null &&
      (matchesEngineType(engine.engine_type, 'nfs') || matchesEngineType(engine.engine_type, 's3'))
    )
  } catch (error) {
    ElMessage.error(t('transfer.taskWizard.loadTargetEngineFailedMsg'))
  } finally {
    loadingEngines.value = false
  }
}

async function restoreState() {
  const state = props.wizardState
  targetType.value = state.targetType.value || 'nfs'

  const config = state.targetConfig.value || {}
  outputFormat.value = config.format || 'csv'
  outputPath.value = config.resourcePath || ''
  outputFileName.value = config.resourceFile || ''
  csvHeaders.value = config.includeHeader !== false
  csvDelimiter.value = config.delimiter || ','

  if (state.targetEngineID.value) {
    formData.engineID = state.targetEngineID.value
    await nextTick()
    syncTarget()
  }
}

onMounted(async () => {
  await loadEngines()
  await restoreState()
})
</script>

<style scoped>
.step2-select-target {
  max-width: 800px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 30px;
}

.csv-options {
  display: flex;
  align-items: center;
  gap: 20px;
}

.delimiter-control {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--addp-text-secondary);
}

.delimiter-control .el-input {
  width: 80px;
}
</style>
