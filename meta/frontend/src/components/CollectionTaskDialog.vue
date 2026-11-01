<template>
  <el-dialog :model-value="true" :title="t('meta.scan.collection.title')" width="min(640px, 95vw)"
    :close-on-click-modal="!busy" :close-on-press-escape="!busy" :show-close="!busy" @close="emit('close')">
    <el-alert type="info" :closable="false" :title="t('meta.scan.collection.boundary')" />
    <el-alert v-if="error" type="error" :closable="false" :title="error" />
    <el-alert v-if="saved" type="success" :closable="false" :title="t('meta.scan.collection.saved')" />
    <el-form label-position="top" :disabled="busy" class="collection-task-form" @submit.prevent="save">
      <el-form-item :label="t('meta.scan.collection.task')">
        <el-select :model-value="selectedID" @change="selectTask" data-testid="collection-definition-select">
          <el-option :label="t('meta.scan.collection.new')" value="" />
          <el-option v-for="item in engineTasks" :key="item.id" :value="String(item.id)" :label="`${item.name} · #${item.id}`" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('meta.scan.collection.name')" required>
        <el-input v-model="form.name" data-testid="collection-definition-name" />
      </el-form-item>
      <p>{{ engine.name }} · {{ t('meta.scan.collection.scope') }} · {{ t('meta.scan.basicScan') }}</p>
      <el-alert v-if="unsupported" type="warning" :closable="false" :title="t('meta.scan.collection.unsupported')" />
      <el-form-item :label="t('meta.scan.collection.scheduled')">
        <el-switch v-model="form.scheduled" data-testid="collection-definition-scheduled" />
      </el-form-item>
      <ScheduleConfig v-if="form.scheduled" v-model="form.schedule" />
      <template v-if="task">
        <p data-testid="collection-definition-id">{{ t('meta.scan.collection.task') }}：{{ task.id }}</p>
        <p>{{ t('meta.scan.collection.approvalHint') }}</p>
        <div class="collection-actions">
          <el-button v-if="canOpenApproval" :disabled="dirty || unsupported" @click="openApproval" data-testid="collection-definition-approval">{{ t('meta.scan.collection.approval') }}</el-button>
          <el-button v-if="canExecute" :disabled="dirty || unsupported" @click="emit('execute', task)" data-testid="collection-definition-execute">{{ t('meta.scan.collection.execute') }}</el-button>
          <MonitorExecutionsButton module="meta" task-type="scan" />
        </div>
      </template>
    </el-form>
    <template #footer>
      <el-button :disabled="busy" @click="emit('close')">{{ t('meta.scan.cancel') }}</el-button>
      <el-button v-if="task ? canUpdate : canCreate" type="primary" :loading="busy" :disabled="unsupported || !dirty" @click="save" data-testid="collection-definition-save">{{ t('meta.scan.collection.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MonitorExecutionsButton, ScheduleConfig, openConsoleRoute, allowsConsoleRoute } from '@common-ui'
import metaApi from '../api/meta'
import { useAuthStore } from '../store/auth'
import { collectionTaskPayload, isEngineCollectionTask, saveCollectionTask } from '../utils/collectionTask'

const props = defineProps({ engine: { type: Object, required: true }, tasks: { type: Array, required: true }, initialTaskId: { type: String, default: '' } })
const emit = defineEmits(['close', 'saved', 'execute'])
const { t } = useI18n(), auth = useAuthStore()
const canCreate = computed(() => auth.hasPermission('meta.scan_task.create'))
const canUpdate = computed(() => auth.hasPermission('meta.scan_task.update'))
const canExecute = computed(() => auth.hasPermission('meta.scan_task.execute'))
const engineTasks = computed(() => props.tasks.filter(task => isEngineCollectionTask(task, props.engine.id)))
const selectedID = ref(''), task = ref(null), busy = ref(false), error = ref(''), saved = ref(false)
const form = reactive({ name: '', scheduled: false, schedule: '' })
let baseline = ''
const dirty = computed(() => !task.value || JSON.stringify(form) !== baseline)
const unsupported = computed(() => !!task.value && task.value.parameters?.scan_depth !== 'basic')
const approvalPath = computed(() => `/system/engines/${props.engine.id}?tab=collection-qualifications&collection_task_id=${task.value?.id || ''}`)
const canOpenApproval = computed(() => allowsConsoleRoute(approvalPath.value, auth.contextType, auth.permissions) &&
  ['read', 'approve'].some(action => auth.hasPermission(`system.meta_collection_qualification.${action}`)))
function selectTask(id) {
  selectedID.value = id
  task.value = engineTasks.value.find(item => String(item.id) === id) || null
  form.name = task.value?.name || `${props.engine.name} · ${t('meta.scan.collection.defaultName')}`
  form.scheduled = !!task.value?.enabled && !!task.value?.schedule
  form.schedule = task.value?.schedule || ''
  baseline = JSON.stringify(form); saved.value = false; error.value = ''
}
selectTask(props.initialTaskId || (engineTasks.value[0] ? String(engineTasks.value[0].id) : ''))
async function save() {
  if (busy.value || unsupported.value || !(task.value ? canUpdate.value : canCreate.value)) return
  try { collectionTaskPayload(props.engine, form, task.value) }
  catch { error.value = t('meta.scan.collection.invalid'); return }
  busy.value = true; error.value = ''; saved.value = false
  try {
    const result = await saveCollectionTask(metaApi, props.engine, form, task.value)
    task.value = result; selectedID.value = String(result.id); baseline = JSON.stringify(form)
    saved.value = true; emit('saved')
  } catch (e) { error.value = e?.response?.data?.error || t('meta.scan.collection.failed') }
  finally { busy.value = false }
}
async function openApproval() {
  if (!dirty.value && !unsupported.value && canOpenApproval.value) await openConsoleRoute(approvalPath.value)
}
</script>

<style scoped>
.collection-task-form { margin-top: 16px; }
.collection-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.el-select { width: 100%; }
</style>
