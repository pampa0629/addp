<template>
  <el-dialog :model-value="true" :title="t('meta.scan.manual.title')" width="min(760px, 94vw)" @close="emit('close')">
    <el-form label-position="top">
      <el-form-item :label="t('meta.scan.engineInfo')">
        <strong>{{ context.engine.name }}</strong>
      </el-form-item>
      <el-form-item :label="t('meta.scan.manual.target')">
        <template v-if="scope">
          <el-alert :closable="false" type="info" :title="scope.kind === 'engine' ? t('meta.scan.manual.wholeEngine') : scope.paths.join('；')" />
          <el-button text @click="scope = null">{{ t('meta.scan.manual.reselect') }}</el-button>
        </template>
        <ResourceTreePicker v-else v-model="selection" :engine-id="context.engine.id" :adapter="catalogAdapter" mode="any"
          :show-engine-selector="false" :title="t('meta.scan.manual.target')" tree-height="260px"
          @error="pickerError = true" @select="pickerError = false" />
      </el-form-item>
      <el-form-item :label="t('meta.scan.scanDepth')">
        <el-radio-group v-model="scanDepth">
          <el-radio value="basic">{{ t('meta.scan.basicScan') }}</el-radio>
          <el-radio value="deep">{{ t('meta.scan.deepScan') }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-alert :closable="false" type="info" :title="t(`meta.scan.manual.${scanDepth === 'basic' ? 'basicHint' : 'deepHint'}`)" />
      <el-form-item>
        <el-checkbox v-model="force">{{ t('meta.scan.manual.force') }}</el-checkbox>
      </el-form-item>
      <el-alert v-if="pickerError" :closable="false" type="error" :title="t('meta.scan.manual.loadFailed')" />
      <el-descriptions v-if="prepared" :column="1" border>
        <el-descriptions-item :label="t('meta.scan.manual.target')">{{ targetLabel }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.scan.scanDepth')">{{ t(`meta.scan.${scanDepth === 'basic' ? 'basicScan' : 'deepScan'}`) }}</el-descriptions-item>
      </el-descriptions>
      <p v-if="prepared">{{ t(`meta.scan.manual.${scope || selection?.raw?.node?.metadata?.catalog_entry?.role === 'branch' ? 'branchScope' : 'leafScope'}`) }}</p>
    </el-form>
    <template #footer>
      <el-button @click="emit('close')">{{ t('meta.scan.cancel') }}</el-button>
      <el-button type="primary" :disabled="!prepared || pickerError || busy" @click="confirm">{{ t('meta.scan.manual.confirm') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ResourceTreePicker, createEngineCatalogTreeAdapter } from '@common-ui'
import metaApi from '../api/meta'
import { prepareManualScan } from '../utils/manualScan.js'

const props = defineProps({ context: { type: Object, required: true }, busy: Boolean })
const emit = defineEmits(['close', 'submit'])
const { t } = useI18n()
const catalogAdapter = createEngineCatalogTreeAdapter(props.context.engine, (id, path) => metaApi.listCatalogChildren(id, path))
const scope = ref(props.context.scope)
const selection = ref(null)
const scanDepth = ref('basic')
const force = ref(props.context.force === true)
const pickerError = ref(false)
const prepared = computed(() => {
  try {
    return prepareManualScan({ engine: props.context.engine, scope: scope.value, selection: selection.value, scanDepth: scanDepth.value, force: force.value })
  } catch {
    return null
  }
})
const targetLabel = computed(() => scope.value?.kind === 'engine'
  ? t('meta.scan.manual.wholeEngine')
  : scope.value?.paths?.join('；') || selection.value?.display?.path || selection.value?.display?.label)
const confirm = () => {
  if (!props.busy && !pickerError.value && prepared.value) emit('submit', prepared.value)
}
</script>
