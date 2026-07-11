<template>
  <el-form
    ref="formRef"
    :model="formState"
    :rules="computedRules"
    :label-width="labelWidth"
    :validate-on-rule-change="false"
  >
    <el-form-item
      v-if="showTypeSelector && typeOptions && typeOptions.length"
      :label="t('storageEngine.type')"
      prop="engine_type"
    >
      <el-select
        v-model="formState.engine_type"
        :placeholder="t('storageEngine.typePlaceholder')"
        :disabled="isEdit && disableTypeChange"
        :validate-event="false"
        @change="handleTypeChange"
      >
        <el-option
          v-for="option in effectiveTypeOptions"
          :key="option.value"
          :label="option.label"
          :value="option.value"
        />
      </el-select>
    </el-form-item>

    <el-form-item :label="t('storageEngine.name')" prop="name">
      <el-input v-model="formState.name" :placeholder="t('storageEngine.namePlaceholder')" />
    </el-form-item>

    <!-- PostgreSQL -->
    <template v-if="formState.engine_type === 'postgresql'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database')" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" :placeholder="t('storageEngine.databasePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username')" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" :placeholder="t('storageEngine.usernamePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.password')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          :placeholder="t('storageEngine.passwordPlaceholder')"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
      <el-form-item :label="t('storageEngine.sslMode')">
        <el-select v-model="formState.connection_info.sslmode">
          <el-option :label="t('storageEngine.sslDisable')" value="disable" />
          <el-option :label="t('storageEngine.sslRequire')" value="require" />
          <el-option :label="t('storageEngine.sslVerifyCa')" value="verify-ca" />
          <el-option :label="t('storageEngine.sslVerifyFull')" value="verify-full" />
        </el-select>
      </el-form-item>
    </template>

    <!-- Apache Doris -->
    <template v-else-if="formState.engine_type === 'doris'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database')" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" :placeholder="t('storageEngine.databasePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username')" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" :placeholder="t('storageEngine.usernamePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.passwordOptional')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          placeholder="root"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
      <div v-else class="field-hint">
        {{ t('storageEngine.hints.dorisPassword') }}
      </div>
    </template>

    <!-- ClickHouse -->
    <template v-else-if="formState.engine_type === 'clickhouse'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database')" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="default" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username')" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="default" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.passwordOptional')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
      <div v-else class="field-hint">
        {{ t('storageEngine.hints.clickhousePassword') }}
      </div>
    </template>

    <!-- MySQL -->
    <template v-else-if="formState.engine_type === 'mysql'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database') + ' (' + t('common.optional') + ')'">
        <el-input v-model="formState.connection_info.database" :placeholder="t('storageEngine.databasePlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username')" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="root" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.password')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
    </template>

    <!-- MongoDB -->
    <template v-else-if="formState.engine_type === 'mongodb'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database')" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="business" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username') + ' (' + t('common.optional') + ')'">
        <el-input v-model="formState.connection_info.user" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.passwordOptional')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          show-password
        />
      </el-form-item>
      <el-form-item :label="t('storageEngine.authSource')">
        <el-input v-model="formState.connection_info.auth_source" :placeholder="t('storageEngine.authSourcePlaceholder')" />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
      <div v-else class="field-hint">
        {{ t('storageEngine.hints.mongoAuth') }}
      </div>
    </template>

    <!-- MinIO / S3 -->
    <template v-else-if="formState.engine_type === 'minio' || formState.engine_type === 's3'">
      <el-form-item :label="t('storageEngine.endpoint')" prop="connection_info.endpoint">
        <el-input v-model="formState.connection_info.endpoint" :placeholder="t('storageEngine.endpointPlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.accessKey')" prop="connection_info.access_key">
        <el-input v-model="formState.connection_info.access_key" :placeholder="t('storageEngine.accessKey')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.secretKey')" prop="connection_info.secret_key">
        <el-input
          v-model="formState.connection_info.secret_key"
          type="password"
          :placeholder="t('storageEngine.secretKey')"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredSecretKey" class="field-hint">
        {{ t('storageEngine.secretKeyHint') }}
      </div>
      <el-form-item :label="t('storageEngine.bucket')">
        <el-input v-model="formState.connection_info.bucket" :placeholder="t('storageEngine.bucketPlaceholder')" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.useSsl')">
        <el-switch v-model="formState.connection_info.use_ssl" />
      </el-form-item>
    </template>

    <!-- NFS 文件系统 -->
    <template v-else-if="formState.engine_type === 'nfs'">
      <el-form-item :label="t('storageEngine.nfsServer')" prop="connection_info.server">
        <el-input v-model="formState.connection_info.server" placeholder="192.168.1.100" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.nfsExportPath')" prop="connection_info.export_path">
        <el-input v-model="formState.connection_info.export_path" placeholder="/exports/data" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.nfsAccessMode')">
        <el-select v-model="formState.connection_info.access_mode">
          <el-option :label="t('storageEngine.nfsAccessModeRw')" value="rw" />
          <el-option :label="t('storageEngine.nfsAccessModeRo')" value="ro" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('storageEngine.nfsVersion')">
        <el-select v-model="formState.connection_info.nfs_version">
          <el-option label="NFSv3" value="3" />
          <el-option label="NFSv4" value="4" />
        </el-select>
      </el-form-item>
      <div class="field-hint">{{ t('storageEngine.hints.nfs') }}</div>
    </template>

    <!-- Neo4j -->
    <template v-else-if="formState.engine_type === 'neo4j'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username')" prop="connection_info.user">
        <el-input v-model="formState.connection_info.user" placeholder="neo4j" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.password')" prop="connection_info.password">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          :placeholder="t('storageEngine.passwordPlaceholder')"
          show-password
        />
      </el-form-item>
      <div v-if="hasStoredPassword" class="field-hint">
        {{ t('storageEngine.passwordHint') }}
      </div>
      <el-form-item :label="t('storageEngine.database') + ' (' + t('common.optional') + ')'">
        <el-input v-model="formState.connection_info.database" placeholder="neo4j" />
      </el-form-item>
      <div class="field-hint">
        {{ t('storageEngine.hints.neo4j') }}
      </div>
    </template>

    <!-- SpatiaLite / SQLite (file-based) -->
    <template v-else-if="formState.engine_type === 'spatialite' || formState.engine_type === 'sqlite'">
      <el-form-item :label="t('storageEngine.filePath')" prop="connection_info.full_name">
        <el-input v-model="formState.connection_info.full_name" :placeholder="t('storageEngine.filePathPlaceholder')" />
      </el-form-item>
      <div class="field-hint">
        {{ t('storageEngine.hints.spatialite') }}
      </div>
    </template>

    <!-- Apache Spark -->
    <template v-else-if="formState.engine_type === 'spark'">
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="host.docker.internal" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" placeholder="10000" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.database')" prop="connection_info.database">
        <el-input v-model="formState.connection_info.database" placeholder="default" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.username') + ' (' + t('common.optional') + ')'">
        <el-input v-model="formState.connection_info.username" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.passwordOptional')">
        <el-input
          v-model="formState.connection_info.password"
          type="password"
          show-password
        />
      </el-form-item>
      <div class="field-hint">
        {{ t('storageEngine.hints.spark') }}
      </div>
    </template>

    <!-- GeoPython Workflow / Spark Workflow -->
    <template v-else-if="formState.engine_type === 'geopython_workflow' || formState.engine_type === 'spark_workflow'">
      <el-form-item :label="t('storageEngine.protocol')">
        <el-select v-model="formState.connection_info.protocol">
          <el-option label="HTTP" value="http" />
          <el-option label="HTTPS" value="https" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('storageEngine.host')" prop="connection_info.host">
        <el-input v-model="formState.connection_info.host" placeholder="localhost" />
      </el-form-item>
      <el-form-item :label="t('storageEngine.port')" prop="connection_info.port">
        <el-input-number v-model="formState.connection_info.port" :min="1" :max="65535" />
      </el-form-item>
      <div class="field-hint">
        {{ t('storageEngine.hints.workflow') }}
      </div>
    </template>

    <!-- 描述 -->
    <el-form-item :label="t('storageEngine.description')" prop="description">
      <el-input
        v-model="formState.description"
        type="textarea"
        :rows="2"
        :placeholder="t('storageEngine.descPlaceholder')"
      />
    </el-form-item>

    <!-- 激活状态 -->
    <el-form-item v-if="showActiveSwitch" :label="t('storageEngine.activeStatus')">
      <el-switch v-model="formState.is_active" />
      <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
        {{ t('storageEngine.activeHint') }}
      </span>
    </el-form-item>

    <!-- 元数据扫描配置 -->
    <el-divider content-position="left">
      <span style="cursor: pointer; user-select: none;" @click="scanConfigExpanded = !scanConfigExpanded">
        {{ t('storageEngine.scanConfig') }}
        <el-icon style="margin-left: 4px;">
          <component :is="scanConfigExpanded ? 'ArrowDown' : 'ArrowRight'" />
        </el-icon>
      </span>
    </el-divider>

    <!-- 扫描配置内容（可折叠） -->
    <template v-if="scanConfigExpanded">
      <!-- 1. 立即扫描开关 -->
      <el-form-item :label="t('storageEngine.immediateScan')">
        <el-switch v-model="immediateScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          {{ t('storageEngine.immediateScanHint') }}
        </span>
      </el-form-item>

      <!-- 1.1 立即扫描深度配置（仅在立即扫描启用时显示） -->
      <template v-if="immediateScanEnabled">
        <el-form-item :label="t('storageEngine.immediateDepth')" style="margin-left: 30px;">
          <el-radio-group v-model="formState.scan_config.immediate_depth">
            <el-radio value="basic">{{ t('storageEngine.basicScan') }}</el-radio>
            <el-radio value="deep">{{ t('storageEngine.deepScan') }}</el-radio>
          </el-radio-group>
          <div class="field-hint" style="white-space: pre-line;">{{ t('storageEngine.scanDepthHint') }}</div>
        </el-form-item>
      </template>

      <!-- 2. 定时扫描开关 -->
      <el-form-item :label="t('storageEngine.scheduledScan')">
        <el-switch v-model="scheduledScanEnabled" />
        <span style="margin-left: 10px; font-size: 12px; color: var(--el-text-color-secondary)">
          {{ t('storageEngine.scheduledScanHint') }}
        </span>
      </el-form-item>

      <!-- 2.1 定时扫描详细配置（仅在开关打开时显示） -->
      <template v-if="scheduledScanEnabled">
        <el-form-item :label="t('storageEngine.scanFrequency')" style="margin-left: 30px;">
          <ScheduleConfig
            v-model="scheduledScanCron"
            :allow-custom-cron="false"
            compact-mode
          />
        </el-form-item>

        <!-- 2.2 定时扫描深度提示（固定深度扫描） -->
        <el-form-item :label="t('storageEngine.scanDepth')" style="margin-left: 30px;">
          <span style="color: var(--el-text-color-regular)">{{ t('storageEngine.deepScanFixed') }}</span>
          <div class="field-hint">{{ t('storageEngine.deepScanFixedHint') }}</div>
        </el-form-item>
      </template>
    </template>
  </el-form>
</template>

<script setup>
import { computed, reactive, ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown, ArrowRight } from '@element-plus/icons-vue'
import ScheduleConfig from './ScheduleConfig.vue'
import { buildScheduleFromForm, decodeScheduleToForm } from '../utils/schedule'

const { t } = useI18n()

const SENSITIVE_PLACEHOLDER = '********'

const props = defineProps({
  modelValue: {
    type: Object,
    default: () => ({})
  },
  isEdit: {
    type: Boolean,
    default: false
  },
  disableTypeChange: {
    type: Boolean,
    default: true
  },
  typeOptions: {
    type: Array,
    default: () => ([
      { label: 'PostgreSQL', value: 'postgresql' },
      { label: 'MySQL', value: 'mysql' },
      { label: 'Apache Doris', value: 'doris' },
      { label: 'ClickHouse', value: 'clickhouse' },
      { label: 'MongoDB', value: 'mongodb' },
      { label: 'MinIO', value: 'minio' },
      { label: 'Neo4j', value: 'neo4j' },
      { label: 'NFS 文件系统', value: 'nfs' },
      { label: 'Apache Spark', value: 'spark' }
    ])
  },
  showActiveSwitch: {
    type: Boolean,
    default: true
  },
  showTypeSelector: {
    type: Boolean,
    default: true
  },
  labelWidth: {
    type: String,
    default: '120px'
  }
})

const emit = defineEmits(['update:modelValue', 'type-change'])

const effectiveTypeOptions = computed(() =>
  props.typeOptions.map(opt =>
    opt.value === 'nfs' ? { ...opt, label: t('storageEngine.typeNfs') } : opt
  )
)

const formRef = ref(null)
const hasStoredPassword = ref(false)
const hasStoredSecretKey = ref(false)
const immediateScanEnabled = ref(true)  // 默认启用立即扫描
const scheduledScanEnabled = ref(false) // 默认不启用定时扫描
const scanConfigExpanded = ref(false)   // 扫描配置折叠状态（默认折叠）

const defaultScheduledScanCron = '0 0 * * *'

const scanPolicyToCron = (scanConfig = {}) => {
  if (!scanConfig.scheduled_scan) {
    return ''
  }
  if (scanConfig.schedule_mode === 'cron') {
    return scanConfig.cron_expression || ''
  }

  const form = {
    mode: scanConfig.schedule_mode || 'daily',
    time: scanConfig.schedule_time || '00:00',
    weekDays: (scanConfig.schedule_value || []).map(value => String(value)),
    dayOfMonth: scanConfig.schedule_value?.[0] || 1
  }
  return buildScheduleFromForm(form)?.cron || defaultScheduledScanCron
}

const applyCronToScanPolicy = (cron) => {
  const normalized = (cron || '').trim().replace(/\s+/g, ' ')
  if (!normalized) {
    scheduledScanEnabled.value = false
    formState.scan_config.scheduled_scan = false
    formState.scan_config.schedule_mode = 'cron'
    formState.scan_config.cron_expression = ''
    formState.scan_config.schedule_time = '00:00'
    formState.scan_config.schedule_value = []
    return
  }

  const decoded = decodeScheduleToForm(normalized)
  scheduledScanEnabled.value = true
  formState.scan_config.scheduled_scan = true
  formState.scan_config.schedule_mode = 'cron'
  formState.scan_config.cron_expression = normalized
  formState.scan_config.schedule_time = decoded?.time || '00:00'
  if (decoded?.mode === 'weekly') {
    formState.scan_config.schedule_value = decoded.weekDays.map(value => Number(value))
  } else if (decoded?.mode === 'monthly') {
    formState.scan_config.schedule_value = [decoded.dayOfMonth]
  } else {
    formState.scan_config.schedule_value = []
  }
}

const ensureConnectionDefaults = (form) => {
  if (!form.connection_info || typeof form.connection_info !== 'object') {
    form.connection_info = {}
  }

  const original = { ...(form.connection_info || {}) }
  const hadPassword = original._has_password === true
  const hadSecret = original._has_secret_key === true

  if (form.engine_type === 'postgresql') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 5432,
      database: original.database ?? '',
      user: original.user ?? '',
      password: original.password ?? '',
      sslmode: original.sslmode ?? 'disable'
    }
  } else if (form.engine_type === 'mysql') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 3306,
      database: original.database ?? '',
      user: original.user ?? '',
      password: original.password ?? ''
    }
  } else if (form.engine_type === 'doris') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 9030,
      database: original.database ?? '',
      user: original.user ?? 'root',
      password: original.password ?? ''
    }
  } else if (form.engine_type === 'clickhouse') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 9000,
      database: original.database ?? 'default',
      user: original.user ?? 'default',
      password: original.password ?? ''
    }
  } else if (form.engine_type === 'mongodb') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 27017,
      database: original.database ?? '',
      user: original.user ?? '',
      password: original.password ?? '',
      auth_source: original.auth_source ?? ''
    }
  } else if (form.engine_type === 'neo4j') {
    form.connection_info = {
      host: original.host ?? 'localhost',
      port: original.port ?? 7687,
      user: original.user ?? 'neo4j',
      password: original.password ?? '',
      database: original.database ?? 'neo4j'
    }
  } else if (form.engine_type === 'minio' || form.engine_type === 's3') {
    form.connection_info = {
      endpoint: original.endpoint ?? 'localhost:9002',
      access_key: original.access_key ?? '',
      secret_key: original.secret_key ?? '',
      bucket: original.bucket ?? '',
      use_ssl: original.use_ssl ?? false
    }
  } else if (form.engine_type === 'nfs') {
    form.connection_info = {
      server: original.server ?? '',
      export_path: original.export_path ?? '/exports/data',
      access_mode: original.access_mode ?? 'rw',
      nfs_version: original.nfs_version ?? '3'
    }
  } else if (form.engine_type === 'spatialite' || form.engine_type === 'sqlite') {
    form.connection_info = {
      full_name: original.full_name ?? ''
    }
  } else if (form.engine_type === 'spark') {
    form.connection_info = {
      host: original.host ?? 'host.docker.internal',
      port: original.port ?? 10000,
      database: original.database ?? 'default',
      username: original.username ?? '',
      password: original.password ?? ''
    }
  } else if (form.engine_type === 'geopython_workflow' || form.engine_type === 'spark_workflow') {
    form.connection_info = {
      protocol: original.protocol ?? 'http',
      host: original.host ?? 'localhost',
      port: original.port ?? (form.engine_type === 'geopython_workflow' ? 8099 : 8100)
    }
  } else {
    form.connection_info = { ...original }
  }

  if (hadPassword) {
    form.connection_info._has_password = true
  } else {
    delete form.connection_info._has_password
  }

  if (hadSecret) {
    form.connection_info._has_secret_key = true
  } else {
    delete form.connection_info._has_secret_key
  }
}

const applySensitiveHints = () => {
  hasStoredPassword.value = formState.connection_info?._has_password === true
  if (hasStoredPassword.value && (!formState.connection_info.password || formState.connection_info.password === '')) {
    formState.connection_info.password = SENSITIVE_PLACEHOLDER
  }

  hasStoredSecretKey.value = formState.connection_info?._has_secret_key === true
  if (hasStoredSecretKey.value && (!formState.connection_info.secret_key || formState.connection_info.secret_key === '')) {
    formState.connection_info.secret_key = SENSITIVE_PLACEHOLDER
  }
}

const formState = reactive({
  engine_type: '',
  name: '',
  description: '',
  is_active: true,
  connection_info: {},
  scan_config: {
    enabled: true,
    immediate_scan: true,  // 默认启用立即扫描
    immediate_depth: 'basic',  // 立即扫描默认基础
    scheduled_scan: false,  // 默认只立即扫描一次
    schedule_mode: 'cron',
    cron_expression: '',
    schedule_time: '00:00',  // 凌晨执行
    schedule_value: []
  }
})

const syncFromProps = (value) => {
  formState.engine_type = value.engine_type || ''
  formState.name = value.name || ''
  formState.description = value.description || ''
  formState.is_active = value.is_active !== undefined ? value.is_active : true
  formState.connection_info = { ...(value.connection_info || {}) }

  // 同步扫描配置
  if (value.scan_config) {
    formState.scan_config = {
      enabled: value.scan_config.enabled || false,
      immediate_scan: value.scan_config.immediate_scan !== undefined ? value.scan_config.immediate_scan : true,
      immediate_depth: value.scan_config.immediate_depth || 'basic',
      scheduled_scan: value.scan_config.scheduled_scan !== undefined ? value.scan_config.scheduled_scan : true,
      schedule_mode: 'cron',
      cron_expression: scanPolicyToCron(value.scan_config),
      schedule_time: value.scan_config.schedule_time || '00:00',
      schedule_value: value.scan_config.schedule_value || []
    }
    immediateScanEnabled.value = formState.scan_config.immediate_scan
    scheduledScanEnabled.value = formState.scan_config.scheduled_scan
  } else {
    // 没有既有 Meta 调度时，默认只在保存后触发一次基础扫描。
    immediateScanEnabled.value = true
    scheduledScanEnabled.value = false
    formState.scan_config = {
      enabled: true,
      immediate_scan: true,
      immediate_depth: 'basic',
      scheduled_scan: false,
      schedule_mode: 'cron',
      cron_expression: '',
      schedule_time: '00:00',
      schedule_value: []
    }
  }

  ensureConnectionDefaults(formState)
  applySensitiveHints()
}

// Avoid infinite update loop between props -> local state -> emit -> props
let syncingFromProps = false
watch(
  () => props.modelValue,
  async (value) => {
    syncingFromProps = true
    try {
      syncFromProps(value || {})
    } finally {
      // ensure local watchers run while the flag is set
      await nextTick()
      syncingFromProps = false
    }
  },
  { immediate: true, deep: true }
)

// 计算属性：判断是否启用了任何扫描配置
const scanConfigEnabled = computed(() => {
  return immediateScanEnabled.value || scheduledScanEnabled.value
})

const scheduledScanCron = computed({
  get() {
    return scanPolicyToCron(formState.scan_config)
  },
  set(value) {
    applyCronToScanPolicy(value)
  }
})

// 监听立即扫描开关，同步到 formState
watch(immediateScanEnabled, (value) => {
  formState.scan_config.immediate_scan = value
})

// 监听定时扫描开关，同步到 formState
watch(scheduledScanEnabled, (value) => {
  formState.scan_config.scheduled_scan = value
  if (!value) {
    // 禁用定时扫描时，重置相关配置
    formState.scan_config.schedule_mode = 'cron'
    formState.scan_config.cron_expression = ''
    formState.scan_config.schedule_time = '00:00'
    formState.scan_config.schedule_value = []
  } else if (!formState.scan_config.cron_expression) {
    applyCronToScanPolicy(defaultScheduledScanCron)
  }
})

watch(
  formState,
  (value) => {
    // Skip emitting while we are syncing from props to prevent recursion
    if (syncingFromProps) return
    const payload = {
      engine_type: value.engine_type,
      name: value.name,
      description: value.description,
      is_active: value.is_active,
      connection_info: { ...value.connection_info }
    }
    payload.scan_config = { ...value.scan_config, enabled: scanConfigEnabled.value }
    emit('update:modelValue', payload)
  },
  { deep: true }
)

// 表单验证规则（响应式，支持语言切换）
const computedRules = computed(() => {
  const rules = {
    engine_type: [{ required: true, message: t('storageEngine.valid.selectType'), trigger: 'change' }],
    name: [{ required: true, message: t('storageEngine.valid.inputName'), trigger: 'blur' }],
    'connection_info.host': [{ required: true, message: t('storageEngine.valid.inputHost'), trigger: 'blur' }],
    'connection_info.port': [{ required: true, message: t('storageEngine.valid.inputPort'), trigger: 'change' }],
    'connection_info.database': [{ required: true, message: t('storageEngine.valid.inputDatabase'), trigger: 'blur' }],
    'connection_info.user': [{ required: true, message: t('storageEngine.valid.inputUsername'), trigger: 'blur' }],
    'connection_info.password': [{ required: true, message: t('storageEngine.valid.inputPassword'), trigger: 'blur' }],
    'connection_info.endpoint': [{ required: true, message: t('storageEngine.valid.inputEndpoint'), trigger: 'blur' }],
    'connection_info.access_key': [{ required: true, message: t('storageEngine.valid.inputAccessKey'), trigger: 'blur' }],
    'connection_info.secret_key': [{ required: true, message: t('storageEngine.valid.inputSecretKey'), trigger: 'blur' }],
    'connection_info.full_name': [{ required: true, message: t('storageEngine.valid.inputFilePath'), trigger: 'blur' }]
  }

  if (formState.engine_type === 'postgresql') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user'],
      'connection_info.password': rules['connection_info.password']
    }
  }

  if (formState.engine_type === 'mysql') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.user': rules['connection_info.user'],
      'connection_info.password': rules['connection_info.password']
    }
  }

  if (formState.engine_type === 'doris') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user']
    }
  }

  if (formState.engine_type === 'clickhouse') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database'],
      'connection_info.user': rules['connection_info.user']
    }
  }

  if (formState.engine_type === 'mongodb') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database']
    }
  }

  if (formState.engine_type === 'minio' || formState.engine_type === 's3') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.endpoint': rules['connection_info.endpoint'],
      'connection_info.access_key': rules['connection_info.access_key'],
      'connection_info.secret_key': rules['connection_info.secret_key']
    }
  }

  if (formState.engine_type === 'nfs') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.server': [{ required: true, message: t('storageEngine.valid.inputNfsServer'), trigger: 'blur' }],
      'connection_info.export_path': [{ required: true, message: t('storageEngine.valid.inputNfsExportPath'), trigger: 'blur' }]
    }
  }

  if (formState.engine_type === 'spatialite' || formState.engine_type === 'sqlite') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.full_name': rules['connection_info.full_name']
    }
  }

  if (formState.engine_type === 'neo4j') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.user': rules['connection_info.user'],
      'connection_info.password': rules['connection_info.password']
    }
  }

  if (formState.engine_type === 'spark') {
    return {
      engine_type: rules.engine_type,
      name: rules.name,
      'connection_info.host': rules['connection_info.host'],
      'connection_info.port': rules['connection_info.port'],
      'connection_info.database': rules['connection_info.database']
    }
  }

  return {
    engine_type: rules.engine_type,
    name: rules.name
  }
})

const handleTypeChange = (type) => {
  ensureConnectionDefaults(formState)
  applySensitiveHints()
  nextTick(() => formRef.value?.clearValidate())
  emit('type-change', type)
}

const validate = async () => {
  if (!formRef.value) return true
  try {
    await formRef.value.validate()
    return true
  } catch {
    return false
  }
}

const reset = () => {
  syncFromProps({})
  formRef.value?.clearValidate()
}

defineExpose({
  validate,
  reset,
  formRef,
  formState
})

watch(
  () => formState.connection_info.password,
  (value) => {
    // 跳过从 props 同步时的处理,避免循环
    if (syncingFromProps) {
      return
    }

    const metaFlag = formState.connection_info?._has_password === true
    if (!metaFlag && value === SENSITIVE_PLACEHOLDER) {
      formState.connection_info.password = ''
      return
    }

    if (value === SENSITIVE_PLACEHOLDER) {
      hasStoredPassword.value = true
      return
    }

    const hasValue = !!value
    formState.connection_info._has_password = hasValue
    hasStoredPassword.value = hasValue
    if (!hasValue) {
      delete formState.connection_info._has_password
    }
  }
)

watch(
  () => formState.connection_info.secret_key,
  (value) => {
    // 跳过从 props 同步时的处理,避免循环
    if (syncingFromProps) {
      return
    }

    const metaFlag = formState.connection_info?._has_secret_key === true
    if (!metaFlag && value === SENSITIVE_PLACEHOLDER) {
      formState.connection_info.secret_key = ''
      return
    }

    if (value === SENSITIVE_PLACEHOLDER) {
      hasStoredSecretKey.value = true
      return
    }

    const hasValue = !!value
    formState.connection_info._has_secret_key = hasValue
    hasStoredSecretKey.value = hasValue
    if (!hasValue) {
      delete formState.connection_info._has_secret_key
    }
  }
)
</script>

<style scoped>
.field-hint {
  margin: -8px 0 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>

<style>
.el-input__inner::placeholder,
.el-textarea__inner::placeholder {
  color: var(--el-text-color-placeholder) !important;
  opacity: 0.6;
}
</style>
