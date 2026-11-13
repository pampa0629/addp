<template>
  <el-dialog v-model="open" :title="t('manager.derivedTasks.types.imageSegmentation')" width="760px" destroy-on-close @closed="emit('closed')">
    <el-form label-position="top">
      <el-form-item :label="t('manager.derivedTasks.columns.name')"><el-input v-model="form.name" /></el-form-item>
      <el-form-item :label="t('manager.segmentationTask.text')"><el-input v-model="form.text" maxlength="256" /></el-form-item>
      <el-form-item :label="t('manager.derivedTasks.sourceItem')">
        <ResourceTreePicker v-model="source" mode="item" :initial-locator="initialSource" :engine-filter="isBusinessMinIO" :selectable-filter="isGeoTIFF" tree-height="220px" />
      </el-form-item>
      <el-form-item :label="t('manager.segmentationTask.target')">
        <ResourceTreePicker v-model="target" mode="node" :initial-locator="initialTarget" :engine-filter="isBusinessMinIO" :selectable-filter="isObjectParent" tree-height="220px" />
      </el-form-item>
      <el-form-item :label="t('manager.segmentationTask.folder')"><el-input v-model="form.folder" /></el-form-item>
      <el-form-item :label="t('manager.derivedTasks.resultName')"><el-input v-model="form.fileName"><template #append>.geojson</template></el-input></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="open = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ResourceTreePicker, buildLocator, parseLocatorSafe } from '@addp/common-frontend'
import { createDerivedTask, updateDerivedTask } from '../../api/derivedTasks'

const props = defineProps({ modelValue: Boolean, task: Object, locator: String })
const emit = defineEmits(['update:modelValue', 'saved', 'closed'])
const open = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const { t } = useI18n()
const form = reactive({ name: '', text: '', fileName: '', folder: '' })
const initialSource = ref('')
const initialTarget = ref('')
const source = ref(null)
const target = ref(null)
const saving = ref(false)
const locatorOf = selection => selection?.identity?.locator || ''
const isBusinessMinIO = engine => engine?.engine_type === 'minio' && engine?.is_builtin !== true
const isGeoTIFF = node => node?.metadata?.format === 'tiff'
const isObjectParent = (_, { locator }) => ['bucket', 'prefix'].includes(locator.type)

watch(() => props.modelValue, value => {
  if (!value) return
  const config = props.task?.config
  const output = parseLocatorSafe(config?.target?.locator)
  const parts = output?.path || []
  Object.assign(form, {
    name: props.task?.name || '',
    text: config?.text || '',
    fileName: (parts.at(-1) || '').replace(/\.geojson$/i, ''),
    folder: parts.slice(1, -1).join('/')
  })
  source.value = target.value = null
  initialSource.value = config?.source?.locator || props.locator || ''
  initialTarget.value = parts.length ? buildLocator({ engineId: output.engineId, path: [parts[0]], type: 'bucket' }) : ''
}, { immediate: true })

async function save() {
  const sourceLocator = locatorOf(source.value) || initialSource.value
  const parentLocator = locatorOf(target.value) || initialTarget.value
  const parent = parseLocatorSafe(parentLocator)
  const folder = form.folder.trim() ? form.folder.trim().split('/') : []
  const fileName = form.fileName.trim()
  if (!form.name.trim() || !form.text.trim() || !sourceLocator || !parent || !fileName ||
      /[/\\]/.test(fileName) || folder.some(part => !part || part === '.' || part === '..' || part.includes('\\'))) {
    ElMessage.warning(t('manager.segmentationTask.invalid'))
    return
  }
  const request = {
    name: form.name.trim(), description: props.task?.description || '', enabled: props.task?.enabled ?? true,
    config: {
      source_locator: sourceLocator,
      target_parent_locator: folder.length ? buildLocator({ engineId: parent.engineId, path: [...parent.path, ...folder], type: 'prefix' }) : parentLocator,
      target_name: fileName + '.geojson',
      text: form.text.trim()
    }
  }
  saving.value = true
  try {
    if (props.task) await updateDerivedTask('image_segmentation', props.task.id, { ...request, version: props.task.version })
    else await createDerivedTask('image_segmentation', request)
    open.value = false
    emit('saved')
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('manager.segmentationTask.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>
