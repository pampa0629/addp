<template>
  <div class="task-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">
      <el-icon><ArrowLeft /></el-icon>
      {{ t('transfer.taskDetail.back') }}
    </el-button>

    <el-card v-loading="loading">
      <template #header>
        <div class="header">
          <span>{{ t('transfer.taskDetail.taskDetailTitle', { name: task.name }) }}</span>
          <div>
            <template v-if="isContinuousTask">
              <el-tooltip :content="continuousStartDisabledMessage" :disabled="!continuousStartDisabledMessage" placement="top">
                <span class="continuous-start-action">
                  <el-button type="primary" @click="handleStartContinuous" :disabled="!canStartContinuous">
                    {{ task.desired_state === 'paused' ? t('transfer.taskDetail.resume') : t('transfer.taskDetail.start') }}
                  </el-button>
                </span>
              </el-tooltip>
              <el-button type="warning" @click="handlePause" :disabled="task.desired_state !== 'running' || isCDCSchemaBlocked">
                {{ t('transfer.taskDetail.pause') }}
              </el-button>
              <el-button :type="isDatabaseCDC ? 'danger' : undefined" @click="handleStop" :disabled="task.desired_state === 'stopped' && task.capture?.status !== 'cleanup_failed'">
                {{ task.capture?.status === 'cleanup_failed' ? t('transfer.taskDetail.retryCleanup') : t('transfer.taskDetail.stop') }}
              </el-button>
            </template>
            <template v-else-if="isManualTask">
              <el-button type="primary" @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.execute') }}
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="handleResume" :disabled="!canStartSchedule">
                {{ t('transfer.taskDetail.start') }}
              </el-button>
              <el-button type="warning" @click="handlePause" :disabled="!canPauseSchedule">
                {{ t('transfer.taskDetail.pause') }}
              </el-button>
              <el-button @click="handleExecute" :disabled="task.status === 'running'">
                {{ t('transfer.taskDetail.runOnce') }}
              </el-button>
            </template>
            <el-button @click="handleEdit" :disabled="!canEditTask">{{ t('transfer.taskDetail.edit') }}</el-button>
            <el-button v-if="canCreateReplay" type="primary" plain @click="openReplayDialog">
              {{ t('transfer.taskDetail.createReplay') }}
            </el-button>
            <el-button @click="openJsonDialog">{{ t('transfer.taskDetail.viewJsonConfig') }}</el-button>
          </div>
        </div>
      </template>

      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.taskDetail.taskId')">{{ task.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.taskName')">{{ task.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.status')">
          <el-tag :type="taskStatusTagType">{{ taskDisplayStatus }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.batchSize')">{{ task.batch_size }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.schedule')">
          {{ isContinuousTask ? t('transfer.taskDetail.continuousRuntime') : formatSchedule(task.schedule) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.createdAt')">
          {{ formatDate(task.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.description')" :span="2">
          {{ task.description || '-' }}
        </el-descriptions-item>
        <template v-if="isDatabaseCDC && task.capture">
          <el-descriptions-item :label="t('transfer.taskDetail.captureStatus')">
            {{ task.capture.status }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.taskDetail.connectorStatus')">
            {{ task.capture.connector_status || '-' }}
          </el-descriptions-item>
        </template>
      </el-descriptions>

			<div v-if="isCDCSchemaBlocked" class="schema-change-panel">
				<el-alert
					:title="schemaChange?.approvable ? t('transfer.taskDetail.schemaAdditiveTitle') : t('transfer.taskDetail.schemaBlockedTitle')"
					:description="schemaChange?.approvable ? t('transfer.taskDetail.schemaAdditiveDescription') : t('transfer.taskDetail.schemaBlockedDescription')"
					:type="schemaChange?.approvable ? 'warning' : 'error'"
					:closable="false"
					show-icon
				/>
				<el-button
					v-if="schemaChange?.approvable"
					type="primary"
					:loading="schemaChangeLoading"
					@click="openSchemaChangeDialog"
				>
					{{ t('transfer.taskDetail.reviewSchemaChange') }}
				</el-button>
			</div>

			<div v-else-if="schemaChangeScanNotice" class="schema-change-panel">
				<el-alert
					:title="t(`transfer.taskDetail.schemaScan.${schemaChangeScanNotice.state}Title`)"
					:description="t(`transfer.taskDetail.schemaScan.${schemaChangeScanNotice.state}Description`, { attempt: schemaChangeScanNotice.attempt })"
					:type="schemaChangeScanNotice.state === 'failed' ? 'error' : 'warning'"
					:closable="false"
					show-icon
				/>
				<el-button
					v-if="schemaChangeScanNotice.retryable"
					type="primary"
					:loading="schemaChangeSubmitting"
					@click="retrySchemaChangeScan"
				>
					{{ t('transfer.taskDetail.schemaScan.retry') }}
				</el-button>
			</div>

			<el-alert
				v-else-if="captureHealthWarning"
				:title="t('transfer.taskDetail.captureHealthWarningTitle')"
				:description="t('transfer.taskDetail.captureHealthWarningDescription', captureHealthWarning)"
				type="error"
				:closable="false"
				show-icon
				class="schema-blocked-alert"
			/>

      <el-alert
        v-if="continuousRecoveryNotice"
        :title="recoveryStateText(continuousRecoveryNotice)"
        :description="recoveryNoticeDescription"
        :type="continuousRecoveryNotice.state === 'open' ? 'error' : 'warning'"
        :closable="false"
        show-icon
        class="schema-blocked-alert"
      />

      <el-divider content-position="left">{{ t('transfer.taskDetail.sourceDataSource') }}</el-divider>
      <el-descriptions :column="2" border>
        <el-descriptions-item
          v-for="item in sourceDetails"
          :key="`source-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-divider content-position="left">{{ t('transfer.taskDetail.targetDataSource') }}</el-divider>
      <el-descriptions :column="2" border>
        <el-descriptions-item
          v-for="item in targetDetails"
          :key="`target-${item.label}`"
          :span="item.span || 1"
        >
          <template #label>{{ item.label }}</template>
          <span class="detail-text">{{ item.value }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <template v-if="isDeadLetterTask">
        <el-divider content-position="left">{{ t('transfer.taskDetail.deadLetters') }}</el-divider>
        <el-alert
          :title="t('transfer.taskDetail.deadLetterNoticeTitle')"
          :description="t('transfer.taskDetail.deadLetterNoticeDescription')"
          type="warning"
          :closable="false"
          show-icon
          class="dead-letter-notice"
        />
        <el-form :inline="true" class="dead-letter-filters" @submit.prevent="applyDeadLetterFilters">
          <el-form-item :label="t('transfer.taskDetail.sourcePartition')">
            <el-input v-model="deadLetterFilters.source_partition" clearable class="dead-letter-filter-input" />
          </el-form-item>
          <el-form-item :label="t('transfer.taskDetail.errorCategory')">
            <el-input v-model="deadLetterFilters.error_category" clearable class="dead-letter-filter-input" />
          </el-form-item>
          <el-form-item :label="t('transfer.taskDetail.errorCode')">
            <el-input v-model="deadLetterFilters.error_code" clearable class="dead-letter-filter-input" />
          </el-form-item>
          <el-form-item :label="t('transfer.taskDetail.payloadAvailability')">
            <el-select v-model="deadLetterFilters.payload_available" class="dead-letter-filter-input">
              <el-option :label="t('transfer.taskDetail.all')" value="" />
              <el-option :label="t('transfer.taskDetail.payloadAvailable')" :value="true" />
              <el-option :label="t('transfer.taskDetail.payloadUnavailable')" :value="false" />
            </el-select>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" native-type="submit">{{ t('transfer.taskDetail.query') }}</el-button>
            <el-button @click="resetDeadLetterFilters">{{ t('transfer.taskDetail.reset') }}</el-button>
          </el-form-item>
        </el-form>

        <el-table v-loading="deadLettersLoading" :data="deadLetters" stripe empty-text="-">
          <el-table-column :label="t('transfer.taskDetail.sourcePosition')" min-width="180">
            <template #default="{ row }">
              <div>{{ row.source_topic }}</div>
              <div class="secondary-text">{{ row.source_partition }} : {{ row.source_offset }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('transfer.taskDetail.error')" min-width="230" show-overflow-tooltip>
            <template #default="{ row }">
              <div>{{ row.error_category }} / {{ row.error_code }}</div>
              <div class="secondary-text dead-letter-message">{{ row.error_message }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('transfer.taskDetail.payloadAvailability')" width="130">
            <template #default="{ row }">
              <el-tag :type="row.payload_available ? 'success' : 'info'" size="small">
                {{ row.payload_available ? t('transfer.taskDetail.payloadAvailable') : t('transfer.taskDetail.payloadUnavailable') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="occurrence_count" :label="t('transfer.taskDetail.occurrenceCount')" width="100" />
          <el-table-column :label="t('transfer.taskDetail.lastObservedAt')" width="180">
            <template #default="{ row }">{{ formatDate(row.last_observed_at) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.taskDetail.actions')" width="100" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openDeadLetterDetail(row.identity)">
                {{ t('transfer.taskDetail.detail') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <div class="dead-letter-pagination">
          <el-pagination
            v-model:current-page="deadLetterPage"
            v-model:page-size="deadLetterPageSize"
            :total="deadLetterTotal"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next"
            @current-change="loadDeadLetters"
            @size-change="handleDeadLetterPageSizeChange"
          />
        </div>
      </template>

      <el-divider>{{ t('transfer.taskDetail.executionRecords') }}</el-divider>
      <el-table :data="executions" stripe>
        <el-table-column prop="execution_id" :label="t('transfer.taskDetail.executionId')" width="220" show-overflow-tooltip />
        <el-table-column prop="status" :label="t('transfer.taskDetail.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="getExecutionTagType(row.status)">
              {{ getExecutionLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="records_written" :label="t('transfer.taskDetail.recordsWritten')" width="120" />
        <el-table-column v-if="isContinuousTask" :label="t('transfer.recovery.state')" min-width="190">
          <template #default="{ row }">
            <template v-if="executionRecovery(row)">
              <el-tag :type="continuousRecoveryTagType(executionRecovery(row).state)" size="small">
                {{ recoveryStateText(executionRecovery(row)) }}
              </el-tag>
              <div v-if="['waiting', 'open'].includes(executionRecovery(row).state)" class="recovery-next-at">
                {{ t('transfer.recovery.nextAttemptAt') }}：{{ formatDate(executionRecovery(row).notBefore) }}
              </div>
            </template>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column prop="start_time" :label="t('transfer.taskDetail.startTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.start_time) }}
          </template>
        </el-table-column>
        <el-table-column prop="end_time" :label="t('transfer.taskDetail.endTime')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.end_time) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskDetail.actions')" width="150">
          <template #default="{ row }">
            <el-button size="small" @click="viewExecution(row.execution_id)">{{ t('transfer.taskDetail.detail') }}</el-button>
            <el-button size="small" type="primary" @click="retryExecution(row.execution_id)" v-if="row.status === 'failed' && !isContinuousTask">
              {{ t('transfer.taskDetail.retry') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="jsonDialogVisible" width="700px">
      <template #header>
        <div class="json-dialog-header">
          <span>{{ t('transfer.taskDetail.jsonConfig') }}</span>
          <el-button size="small" type="primary" @click="handleCopyJson">{{ t('transfer.taskDetail.copy') }}</el-button>
        </div>
      </template>
      <pre class="json-pre">{{ formattedConfig }}</pre>
    </el-dialog>

		<el-dialog v-model="schemaChangeDialogVisible" :title="t('transfer.taskDetail.schemaChangeDialogTitle')" width="760px">
			<el-alert
				:title="t('transfer.taskDetail.schemaChangeConfirmNotice')"
				type="warning"
				:closable="false"
				show-icon
				class="schema-change-dialog-alert"
			/>
			<el-table :data="schemaChangeFields" stripe>
				<el-table-column prop="source" :label="t('transfer.taskDetail.sourceField')" min-width="160" />
				<el-table-column :label="t('transfer.taskDetail.targetField')" min-width="180">
					<template #default="{ row }">
						<el-input v-model="row.target" />
					</template>
				</el-table-column>
				<el-table-column prop="target_type" :label="t('transfer.taskDetail.fieldType')" min-width="120" />
				<el-table-column :label="t('transfer.taskDetail.nullable')" width="100">
					<template #default><el-tag type="success">true</el-tag></template>
				</el-table-column>
			</el-table>
			<template #footer>
				<el-button @click="schemaChangeDialogVisible = false">{{ t('transfer.taskDetail.cancel') }}</el-button>
				<el-button type="primary" :loading="schemaChangeSubmitting" @click="submitSchemaChange">
					{{ t('transfer.taskDetail.applySchemaChange') }}
				</el-button>
			</template>
		</el-dialog>

    <el-dialog v-model="deadLetterDetailVisible" :title="t('transfer.taskDetail.deadLetterDetail')" width="760px">
      <el-descriptions v-if="deadLetterDetail" :column="2" border>
        <el-descriptions-item :label="t('transfer.taskDetail.deadLetterIdentity')" :span="2">
          <span class="detail-text">{{ deadLetterDetail.identity }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.sourceTopic')">{{ deadLetterDetail.source_topic }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.sourcePosition')">
          {{ deadLetterDetail.source_partition }} : {{ deadLetterDetail.source_offset }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.sourceTimestamp')">{{ formatDate(deadLetterDetail.source_timestamp) }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.payloadAvailability')">
          {{ deadLetterDetail.payload_available ? t('transfer.taskDetail.payloadAvailable') : t('transfer.taskDetail.payloadUnavailable') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.errorCategory')">{{ deadLetterDetail.error_category }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.errorCode')">{{ deadLetterDetail.error_code }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.errorMessage')" :span="2">
          <span class="detail-text">{{ deadLetterDetail.error_message }}</span>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.firstExecutionId')">{{ deadLetterDetail.first_execution_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.lastExecutionId')">{{ deadLetterDetail.last_execution_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.firstObservedAt')">{{ formatDate(deadLetterDetail.first_observed_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.lastObservedAt')">{{ formatDate(deadLetterDetail.last_observed_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.taskDetail.occurrenceCount')">{{ deadLetterDetail.occurrence_count }}</el-descriptions-item>
      </el-descriptions>
    </el-dialog>

    <el-dialog v-model="replayDialogVisible" :title="t('transfer.taskDetail.createReplay')" width="760px">
      <el-alert
        :title="t('transfer.taskDetail.replayNoticeTitle')"
        :description="t('transfer.taskDetail.replayNoticeDescription')"
        type="warning"
        :closable="false"
        show-icon
        class="replay-notice"
      />
      <el-form label-width="150px" class="replay-form">
        <el-form-item :label="t('transfer.taskDetail.replayParentLocator')" required>
          <el-input v-model="replayForm.target.parent_locator" />
        </el-form-item>
        <el-form-item :label="t('transfer.taskDetail.replayTargetName')" required>
          <el-input v-model="replayForm.target.name" />
        </el-form-item>
        <el-form-item :label="t('transfer.taskDetail.replayRanges')" required>
          <div class="replay-ranges">
            <div v-for="(range, index) in replayForm.ranges" :key="index" class="replay-range-row">
              <el-input v-model="range.partition" :placeholder="t('transfer.taskDetail.partition')" />
              <el-input-number v-model="range.start_offset" :min="0" :controls="false" :placeholder="t('transfer.taskDetail.startOffset')" />
              <span class="range-separator">→</span>
              <el-input-number v-model="range.end_offset" :min="0" :controls="false" :placeholder="t('transfer.taskDetail.endOffset')" />
              <el-button link type="danger" :disabled="replayForm.ranges.length === 1" @click="removeReplayRange(index)">
                {{ t('transfer.taskDetail.remove') }}
              </el-button>
            </div>
            <el-button link type="primary" @click="addReplayRange">{{ t('transfer.taskDetail.addRange') }}</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="replayDialogVisible = false">{{ t('transfer.taskDetail.cancel') }}</el-button>
        <el-button type="primary" :loading="replaySubmitting" @click="submitReplay">
          {{ t('transfer.taskDetail.createReplay') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { continuousRecoveryTagType, formatLocatorDisplayPath, getContinuousRecovery } from '@addp/common-frontend'
import { taskAPI, executionAPI } from '@/api/tasks'
import { formatDate } from '@common-ui'
import { formatSchedule, getTaskStatusLabel, getTaskStatusTagType, getExecutionTagType, getExecutionLabel } from '@/utils/formatters'
import { buildCDCStopRequest, continuousStartDisabledReason, getCDCCaptureHealthWarning, isCDCSchemaBlocked as isCDCSchemaBlockedTask, isDatabaseCDCTask } from '@/utils/cdcTask.mjs'
import { parseTransferLocator } from '@/utils/resourceLocator'
import { buildSchemaChangeApproval, buildSchemaChangeScanRetry, getSchemaChangeScanNotice } from '@/utils/schemaChange.mjs'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const task = ref({})
const executions = ref([])
const jsonDialogVisible = ref(false)
const deadLetters = ref([])
const deadLettersLoading = ref(false)
const deadLetterPage = ref(1)
const deadLetterPageSize = ref(20)
const deadLetterTotal = ref(0)
const deadLetterDetailVisible = ref(false)
const deadLetterDetail = ref(null)
const replayDialogVisible = ref(false)
const replaySubmitting = ref(false)
const schemaChange = ref(null)
const schemaChangeLoading = ref(false)
const schemaChangeDialogVisible = ref(false)
const schemaChangeSubmitting = ref(false)
const schemaChangeFields = ref([])
const deadLetterFilters = ref({
  source_partition: '',
  error_category: '',
  error_code: '',
  payload_available: ''
})
const replayForm = ref(newReplayForm())

const isContinuousTask = computed(() => task.value?.config?.runtime?.boundary === 'continuous')
const isBusinessKafkaRecordTask = computed(() => isContinuousTask.value &&
  task.value?.config?.load?.change_detection?.type === 'kafka' &&
  task.value?.config?.source?.change_stream?.envelope === 'record' &&
  task.value?.config?.source?.change_stream?.encoding === 'json')
const recordFailureMode = computed(() => task.value?.config?.runtime?.record_failure?.mode)
const isDeadLetterTask = computed(() => isBusinessKafkaRecordTask.value && recordFailureMode.value === 'dead_letter')
const canCreateReplay = computed(() => isBusinessKafkaRecordTask.value && recordFailureMode.value === 'block')
const isDatabaseCDC = computed(() => isDatabaseCDCTask(task.value))
const isCDCSchemaBlocked = computed(() => isCDCSchemaBlockedTask(task.value))
const schemaChangeScanNotice = computed(() => getSchemaChangeScanNotice(schemaChange.value))
const captureHealthWarning = computed(() => getCDCCaptureHealthWarning(task.value))
const isManualTask = computed(() => !isContinuousTask.value && !task.value?.schedule)
const canStartSchedule = computed(() => !task.value?.enabled)
const canPauseSchedule = computed(() => task.value?.enabled)
const continuousStartDisabledReasonCode = computed(() => continuousStartDisabledReason(task.value))
const continuousStartDisabledMessage = computed(() => continuousStartDisabledReasonCode.value
	? t(`transfer.continuousStartDisabled.${continuousStartDisabledReasonCode.value}`)
	: '')
const canStartContinuous = computed(() => continuousStartDisabledReasonCode.value === null)
const canEditTask = computed(() => task.value?.status !== 'running' && (!isContinuousTask.value || task.value?.desired_state === 'stopped'))
const latestExecution = computed(() => executions.value[0] || null)
const latestRecovery = computed(() => latestExecution.value
  ? getContinuousRecovery(latestExecution.value.metadata, latestExecution.value.status)
  : null)
const continuousRecoveryNotice = computed(() => {
  if (!isContinuousTask.value || task.value?.desired_state !== 'running') return null
  return ['waiting', 'open', 'half_open', 'ready'].includes(latestRecovery.value?.state) ? latestRecovery.value : null
})
const taskStatusTagType = computed(() => continuousRecoveryNotice.value
  ? continuousRecoveryTagType(continuousRecoveryNotice.value.state)
  : getTaskStatusTagType(task.value))
const taskDisplayStatus = computed(() => {
  if (!isContinuousTask.value) return getTaskStatusLabel(task.value)
	if (isCDCSchemaBlocked.value) return t('transfer.taskDetail.schemaBlocked')
  if (task.value?.desired_state === 'paused') return t('transfer.taskDetail.continuousPaused')
	if (task.value?.desired_state === 'stopped') {
		if (isDatabaseCDC.value && !task.value?.capture) return t('transfer.taskDetail.continuousNotStarted')
		return t('transfer.taskDetail.continuousStopped')
	}
  if (continuousRecoveryNotice.value) return recoveryStateText(continuousRecoveryNotice.value)
  return t('transfer.taskDetail.continuousRunning')
})
const recoveryNoticeDescription = computed(() => continuousRecoveryNotice.value
  ? t('transfer.recovery.noticeDescription', {
      reason: recoveryReasonText(continuousRecoveryNotice.value),
      failures: continuousRecoveryNotice.value.consecutiveFailures,
      nextAttempt: formatDate(continuousRecoveryNotice.value.notBefore)
    })
  : '')

let refreshTimer = null

const isTaskRunning = (taskData) => !isCDCSchemaBlockedTask(taskData) && (taskData?.status === 'running' || (taskData?.config?.runtime?.boundary === 'continuous' && taskData?.desired_state === 'running'))

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

const syncAutoRefresh = () => {
  if (isTaskRunning(task.value) || schemaChangeScanNotice.value?.state === 'running') {
    if (!refreshTimer) {
      refreshTimer = setInterval(loadTask, 5000)
    }
    return
  }

  stopAutoRefresh()
}

const loadTask = async () => {
  if (!route.params.id) return
  loading.value = true
  try {
    const taskData = await taskAPI.get(route.params.id)
    const executionsRes = await taskAPI.executions(route.params.id, { page: 1, page_size: 10 }).catch(() => ({ data: [] }))

    task.value = taskData || {}
    executions.value = executionsRes?.data || []
    if (isDeadLetterTask.value) {
      await loadDeadLetters()
    } else {
      deadLetters.value = []
      deadLetterTotal.value = 0
    }
		if (isDatabaseCDC.value) {
			await loadSchemaChange()
		} else {
			schemaChange.value = null
		}
    syncAutoRefresh()
  } finally {
    loading.value = false
  }
}

const loadSchemaChange = async () => {
	if (!route.params.id || !isDatabaseCDC.value) return
	schemaChangeLoading.value = true
	try {
		schemaChange.value = await taskAPI.schemaChange(route.params.id)
	} catch (error) {
		if (error?.response?.status !== 404) console.error('加载结构变更请求失败:', error)
		schemaChange.value = null
	} finally {
		schemaChangeLoading.value = false
	}
}

const openSchemaChangeDialog = () => {
	schemaChangeFields.value = (schemaChange.value?.suggested_fields || []).map((field) => ({ ...field }))
	schemaChangeDialogVisible.value = true
}

const submitSchemaChange = async () => {
	const approval = buildSchemaChangeApproval(schemaChangeFields.value)
	if (!approval) {
		ElMessage.warning(t('transfer.taskDetail.schemaChangeInvalid'))
		return
	}
	try {
		await ElMessageBox.confirm(
			t('transfer.taskDetail.schemaChangeConfirm'),
			t('transfer.taskDetail.schemaChangeDialogTitle'),
			{
				confirmButtonText: t('transfer.taskDetail.applySchemaChange'),
				cancelButtonText: t('transfer.taskDetail.cancel'),
				type: 'warning'
			}
		)
		schemaChangeSubmitting.value = true
		const result = await taskAPI.approveSchemaChange(route.params.id, approval)
		schemaChangeDialogVisible.value = false
		ElMessage.success(result?.metadata_scan_status === 'failed'
			? t('transfer.taskDetail.schemaChangeAppliedScanFailed')
			: ['pending', 'running'].includes(result?.metadata_scan_status)
				? t('transfer.taskDetail.schemaChangeAppliedScanRunning')
				: t('transfer.taskDetail.schemaChangeApplied'))
		await loadTask()
	} catch (error) {
		if (error !== 'cancel') console.error('审批结构变更失败:', error)
	} finally {
		schemaChangeSubmitting.value = false
	}
}

const retrySchemaChangeScan = async () => {
	const approval = buildSchemaChangeScanRetry(schemaChange.value)
	if (!approval) return
	schemaChangeSubmitting.value = true
	try {
		const result = await taskAPI.approveSchemaChange(route.params.id, approval)
		schemaChange.value = result
		ElMessage.success(result?.metadata_scan_status === 'failed'
			? t('transfer.taskDetail.schemaChangeAppliedScanFailed')
			: ['pending', 'running'].includes(result?.metadata_scan_status)
				? t('transfer.taskDetail.schemaChangeAppliedScanRunning')
				: t('transfer.taskDetail.schemaScan.retrySubmitted'))
		await loadTask()
	} finally {
		schemaChangeSubmitting.value = false
	}
}

const handleExecute = async () => {
  await taskAPI.start(route.params.id)
  const message = isManualTask.value ? t('transfer.taskDetail.executeSubmitted') : t('transfer.taskDetail.runOnceSubmitted')
  ElMessage.success(message)
  await loadTask()
}

const handlePause = async () => {
  try {
    const message = isDatabaseCDC.value
      ? t('transfer.taskDetail.cdcPauseConfirm')
      : t('transfer.taskDetail.pauseConfirm')
    await ElMessageBox.confirm(message, t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.pause(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskPaused'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('暂停任务失败:', error)
    }
  }
}

const handleResume = async () => {
  await taskAPI.resume(route.params.id)
  ElMessage.success(t('transfer.taskDetail.taskResumed'))
  await loadTask()
}

const handleStartContinuous = async () => {
  if (task.value?.desired_state === 'paused') {
    await taskAPI.resume(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskResumed'))
  } else {
    await taskAPI.start(route.params.id)
    ElMessage.success(t('transfer.taskDetail.executeSubmitted'))
  }
  await loadTask()
}

const handleStop = async () => {
  try {
    if (isDatabaseCDC.value) {
      const { value } = await ElMessageBox.prompt(
        t('transfer.taskDetail.cdcStopConfirm', { name: task.value.name }),
        t('transfer.taskDetail.cdcStopTitle'),
        {
          confirmButtonText: t('transfer.taskDetail.cdcStopButton'),
          cancelButtonText: t('transfer.taskDetail.cancel'),
          type: 'error',
          confirmButtonClass: 'el-button--danger',
          inputPlaceholder: t('transfer.taskDetail.cdcStopInputPlaceholder'),
          inputValidator: (input) => input === task.value.name || t('transfer.taskDetail.cdcStopNameMismatch')
        }
      )
      await taskAPI.stop(route.params.id, buildCDCStopRequest(task.value.name, value))
      ElMessage.success(t('transfer.taskDetail.taskStopped'))
      await loadTask()
      return
    }
    await ElMessageBox.confirm(t('transfer.taskDetail.stopConfirm'), t('transfer.taskDetail.hint'), {
      confirmButtonText: t('transfer.taskDetail.confirm'),
      cancelButtonText: t('transfer.taskDetail.cancel'),
      type: 'warning'
    })
    await taskAPI.stop(route.params.id)
    ElMessage.success(t('transfer.taskDetail.taskStopped'))
    await loadTask()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('停止持续同步任务失败:', error)
    }
  }
}

const handleEdit = () => {
  if (!canEditTask.value) {
    ElMessage.warning(t('transfer.taskDetail.taskRunning'))
    return
  }
  router.push(`/tasks/${route.params.id}/edit`)
}

const viewExecution = (executionId) => {
  router.push(`/executions/${executionId}`)
}

const retryExecution = async (executionId) => {
  await executionAPI.retry(executionId)
  ElMessage.success(t('transfer.taskDetail.retrySubmitted'))
  loadTask()
}

const loadDeadLetters = async () => {
  if (!route.params.id || !isDeadLetterTask.value) return
  deadLettersLoading.value = true
  try {
    const params = {
      page: deadLetterPage.value,
      page_size: deadLetterPageSize.value
    }
    for (const [key, value] of Object.entries(deadLetterFilters.value)) {
      if (value !== '') params[key] = value
    }
    const response = await taskAPI.deadLetters(route.params.id, params)
    deadLetters.value = response?.data || []
    deadLetterTotal.value = response?.total || 0
  } finally {
    deadLettersLoading.value = false
  }
}

const applyDeadLetterFilters = () => {
  deadLetterPage.value = 1
  loadDeadLetters()
}

const resetDeadLetterFilters = () => {
  deadLetterFilters.value = { source_partition: '', error_category: '', error_code: '', payload_available: '' }
  deadLetterPage.value = 1
  loadDeadLetters()
}

const handleDeadLetterPageSizeChange = () => {
  deadLetterPage.value = 1
  loadDeadLetters()
}

const openDeadLetterDetail = async (identity) => {
  deadLetterDetail.value = await taskAPI.deadLetter(route.params.id, identity)
  deadLetterDetailVisible.value = true
}

function newReplayForm() {
  return {
    ranges: [{ partition: '', start_offset: 0, end_offset: 1 }],
    target: { parent_locator: '', name: '' }
  }
}

const openReplayDialog = () => {
  replayForm.value = newReplayForm()
  replayForm.value.target.parent_locator = task.value?.config?.target?.parent_locator || ''
  replayDialogVisible.value = true
}

const addReplayRange = () => {
  replayForm.value.ranges.push({ partition: '', start_offset: 0, end_offset: 1 })
}

const removeReplayRange = (index) => {
  replayForm.value.ranges.splice(index, 1)
}

const submitReplay = async () => {
  const target = replayForm.value.target
  const ranges = replayForm.value.ranges
  if (!target.parent_locator.trim() || !target.name.trim() || ranges.some((range) =>
    !String(range.partition).trim() || range.start_offset < 0 || range.end_offset <= range.start_offset)) {
    ElMessage.warning(t('transfer.taskDetail.replayInvalid'))
    return
  }
  const partitions = ranges.map((range) => String(range.partition).trim())
  if (new Set(partitions).size !== partitions.length) {
    ElMessage.warning(t('transfer.taskDetail.replayDuplicatePartition'))
    return
  }
  replaySubmitting.value = true
  try {
    const execution = await taskAPI.replay(route.params.id, {
      ranges: ranges.map((range) => ({
        partition: String(range.partition).trim(),
        start_offset: range.start_offset,
        end_offset: range.end_offset
      })),
      target: {
        parent_locator: target.parent_locator.trim(),
        name: target.name.trim()
      }
    })
    replayDialogVisible.value = false
    ElMessage.success(t('transfer.taskDetail.replaySubmitted', { id: execution?.execution_id || '-' }))
    await loadTask()
  } finally {
    replaySubmitting.value = false
  }
}

const executionRecovery = (execution) => getContinuousRecovery(execution?.metadata, execution?.status)

const recoveryStateText = (recovery) => recovery
  ? t(`transfer.recovery.states.${recovery.state}`)
  : '-'

const recoveryReasonText = (recovery) => {
  const reason = recovery?.reason || 'unknown'
  const key = `transfer.recovery.reasons.${reason}`
  const translated = t(key)
  return translated === key ? reason : translated
}

const sourceDetails = computed(() => buildEndpointDetails(task.value?.config?.source, 'source'))
const targetDetails = computed(() => buildEndpointDetails(task.value?.config?.target, 'target'))

function buildEndpointDetails(endpoint, role) {
  if (!endpoint || typeof endpoint !== 'object') {
    return [{ label: t('transfer.taskDetail.dataSource'), value: t('transfer.taskDetail.notConfigured'), span: 2 }]
  }

  const items = []
  const loc = parseTransferLocator(role === 'target' ? endpoint.parent_locator : endpoint.locator)
  addItem(items, t('transfer.taskDetail.reviewEngineId'), loc.engineID)
  addItem(items, t('transfer.taskDetail.connectionType'), loc.type)
  addItem(items, t('transfer.taskDetail.dataType'), endpoint.data_type)
  addItem(items, t('transfer.taskDetail.representation'), endpoint.representation)
  addItem(items, t('transfer.taskDetail.path'), formatEndpointPath(endpoint, role), 2)

  if (role === 'source' && endpoint.change_stream) {
    addItem(items, t('transfer.taskDetail.messageFormat'), `${endpoint.change_stream.envelope || '-'} / ${endpoint.change_stream.encoding || '-'}`)
    addItem(items, t('transfer.taskDetail.keyFields'), endpoint.change_stream.key?.fields)
    addItem(items, t('transfer.taskDetail.initialPosition'), endpoint.change_stream.start?.initial)
    addItem(items, t('transfer.taskDetail.pollBatchSize'), endpoint.change_stream.poll_batch_size)
  }

  if (role === 'target') {
    addItem(items, t('transfer.taskDetail.format'), endpoint.format)
    addItem(items, t('transfer.taskDetail.writeMode'), endpoint.policy?.apply_mode)
    addItem(items, t('transfer.taskDetail.options'), endpoint.options, 2)
  }

  return items
}

function addItem(items, label, value, span) {
  items.push({ label, value: formatValue(value), span })
}

function formatEndpointPath(endpoint, role) {
  if (role !== 'target') {
    return formatLocatorDisplayPath(endpoint?.locator, endpoint?.representation)
  }
  const parent = parseTransferLocator(endpoint?.parent_locator)
  const name = String(endpoint?.name || '').trim()
  if (endpoint?.representation === 'native') {
    return [parent.path[parent.path.length - 1], name].filter(Boolean).join('.')
  }
  return [...parent.path, name].filter(Boolean).join('/')
}

function formatValue(value) {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'boolean') return value ? t('transfer.taskDetail.boolYes') : t('transfer.taskDetail.boolNo')
  if (typeof value === 'object') {
    try {
      const json = JSON.stringify(value)
      return json && json !== '{}' ? json : '-'
    } catch {
      return '-'
    }
  }
  return String(value)
}

const formattedConfig = computed(() => {
  try {
    const value = {
      task_id: task.value.id,
      name: task.value.name,
      description: task.value.description,
      task_type: task.value.task_type,
      schedule: task.value.schedule || '',
      batch_size: task.value.batch_size,
      config: task.value.config || {}
    }
    return JSON.stringify(value, null, 2)
  } catch (error) {
    console.error('格式化配置失败:', error)
    return '{}'
  }
})

const openJsonDialog = () => {
  jsonDialogVisible.value = true
}

const handleCopyJson = () => {
  try {
    copyToClipboard(formattedConfig.value)
    ElMessage.success(t('transfer.taskDetail.copiedToClipboard'))
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error(t('transfer.taskDetail.copyFailed'))
  }
}

const copyToClipboard = (text) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '2em'
  textarea.style.height = '2em'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.outline = 'none'
  textarea.style.boxShadow = 'none'
  textarea.style.background = 'transparent'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()

  try {
    const successful = document.execCommand('copy')
    document.body.removeChild(textarea)
    if (!successful) {
      throw new Error(t('transfer.taskDetail.copyFailed'))
    }
  } catch (err) {
    document.body.removeChild(textarea)
    throw err
  }
}

onMounted(() => {
  loadTask()
})

onBeforeUnmount(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.task-detail {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.detail-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.schema-blocked-alert {
	margin-top: 16px;
}

.schema-change-panel {
	display: flex;
	flex-wrap: wrap;
	gap: 12px;
	align-items: center;
	margin-top: 16px;
}

.schema-change-panel .el-alert {
	flex: 1 1 520px;
}

.schema-change-dialog-alert {
	margin-bottom: 16px;
}

.dead-letter-notice,
.replay-notice {
  margin-bottom: 16px;
}

.dead-letter-filters {
  margin-bottom: 4px;
}

.dead-letter-filter-input {
  width: 180px;
}

.dead-letter-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.secondary-text {
  margin-top: 4px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
}

.dead-letter-message {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.replay-form {
  margin-top: 20px;
}

.replay-ranges {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 10px;
}

.replay-range-row {
  display: grid;
  grid-template-columns: minmax(90px, 0.8fr) minmax(130px, 1fr) auto minmax(130px, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.range-separator {
  color: var(--addp-text-tertiary);
}

.continuous-start-action {
  display: inline-flex;
}

.continuous-start-action + .el-button {
  margin-left: 12px;
}

.recovery-next-at {
  margin-top: 4px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
  line-height: 1.4;
}

.json-dialog-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.json-pre {
  background-color: var(--addp-bg-secondary);
  border-radius: 6px;
  padding: 16px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 420px;
  overflow: auto;
  color: var(--addp-text-primary);
}
</style>
