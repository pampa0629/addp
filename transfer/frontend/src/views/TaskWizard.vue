<template>
  <div class="task-wizard">
    <el-card v-loading="loadingTask">
      <template #header>
        <div class="card-header">
          <el-button @click="handleBack">
            <el-icon><ArrowLeft /></el-icon>
            {{ t('transfer.taskWizard.back') }}
          </el-button>
          <span>{{ isEdit ? t('transfer.taskWizard.editTask') : t('transfer.taskWizard.createTask') }}</span>
        </div>
      </template>

      <el-steps :active="currentStep" finish-status="success" align-center>
        <el-step :title="t('transfer.taskWizard.stepBasicInfo')" />
        <el-step :title="t('transfer.taskWizard.stepSourceConfig')" />
        <el-step :title="t('transfer.taskWizard.stepTargetConfig')" />
        <el-step :title="t('transfer.taskWizard.stepFieldMapping')" />
        <el-step :title="t('transfer.taskWizard.stepComplete')" />
      </el-steps>

      <div class="step-content">
        <!-- 步骤 1: 基本信息 -->
        <div v-show="currentStep === 0" class="step-panel">
          <el-form :model="taskForm" ref="basicFormRef" label-width="120px">
            <el-form-item :label="t('transfer.taskWizard.taskName')" prop="name" :rules="[{ required: true, message: t('transfer.taskWizard.taskNameRequired') }]">
              <el-input v-model="taskForm.name" :placeholder="t('transfer.taskWizard.taskNamePlaceholder')" />
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.taskDescription')">
              <el-input v-model="taskForm.description" type="textarea" :rows="3"
                :placeholder="t('transfer.taskWizard.taskDescriptionPlaceholder')" />
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.executionMode')">
              <el-radio-group v-model="taskForm.mode">
                <el-radio-button value="batch">{{ t('transfer.taskWizard.batchMode') }}</el-radio-button>
                <el-radio-button value="stream">{{ t('transfer.taskWizard.streamMode') }}</el-radio-button>
                <el-radio-button value="micro-batch">{{ t('transfer.taskWizard.microBatchMode') }}</el-radio-button>
              </el-radio-group>
              <div class="hint">
                <p>• {{ t('transfer.taskWizard.batchModeHint') }}</p>
                <p>• {{ t('transfer.taskWizard.streamModeHint') }}</p>
                <p>• {{ t('transfer.taskWizard.microBatchModeHint') }}</p>
              </div>
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.batchSize')">
              <el-input-number v-model="taskForm.batch_size" :min="100" :max="10000" :step="100" />
              <div class="hint">{{ t('transfer.taskWizard.batchSizeHint') }}</div>
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.maxParallelism')">
              <el-input-number v-model="taskForm.max_parallelism" :min="1" :max="32" />
              <div class="hint">{{ t('transfer.taskWizard.maxParallelismHint') }}</div>
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.schedule')">
              <ScheduleConfig v-model="taskForm.schedule" :allow-custom-cron="true" />
            </el-form-item>
          </el-form>
        </div>

        <!-- 步骤 2: 选择源数据源 -->
        <div v-show="currentStep === 1" class="step-panel">
          <div class="step-section">
            <h3 class="step-section__title">{{ t('transfer.taskWizard.selectSourceDataSource') }}</h3>
            <el-form label-width="120px">
            <el-form-item :label="t('transfer.taskWizard.sourceDataType')">
              <el-radio-group v-model="sourceConnectorType" @change="handleSourceTypeChange">
                <el-radio-button value="postgresql">PostgreSQL</el-radio-button>
                <el-radio-button value="mysql">MySQL</el-radio-button>
                <el-radio-button value="spatialite">SpatiaLite/SQLite</el-radio-button>
                <el-radio-button value="s3">S3/MinIO</el-radio-button>
              </el-radio-group>
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.sourceEngine')">
              <el-select
                v-model="selectedSourceValue"
                :placeholder="t('transfer.taskWizard.selectSourceEngine')"
                style="width: 100%"
                filterable
                :loading="loadingSystemEngines || loadingLocalEngines"
              >
                <el-option
                  v-for="option in sourceOptions"
                  :key="option.value"
                  :label="`${option.name} (${option.engine_type})`"
                  :value="option.value"
                />
              </el-select>
              <div class="hint">
                <p>
                  {{ t('transfer.taskWizard.systemEngineGlobal') }}
                  <el-link type="primary" @click="openSystemEngines">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                </p>
                <p>
                  {{ t('transfer.taskWizard.localEngineTransfer') }}
                  <el-link type="primary" @click="openLocalEngineDialog('source')">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                  <template v-if="selectedSourceLocalResource">
                    <el-link type="primary" @click="openLocalEngineDialog('source', selectedSourceLocalResource)">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                    <el-link
                      type="success"
                      :loading="syncingLocalResource"
                      @click="handleSyncLocalResource('source')"
                    >{{ t('transfer.taskWizard.syncToSystem') }}</el-link>
                  </template>
                </p>
              </div>
            </el-form-item>

            <el-alert
              v-if="selectedSourceResource"
              type="info"
              :closable="false"
              style="margin-top: 20px"
            >
              <template #title>
                {{ t('transfer.taskWizard.selectSourceDataSource') }}：{{ selectedSourceResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedSourceResource.connection_info.host">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedSourceResource.connection_info.host }}:{{ selectedSourceResource.connection_info.port }}
                </p>
                <p v-if="selectedSourceResource.connection_info.database">
                  {{ t('transfer.taskDetail.database') }}：{{ selectedSourceResource.connection_info.database }}
                </p>
                <p v-if="selectedSourceResource.connection_info.bucket">
                  {{ t('transfer.taskDetail.bucket') }}：{{ selectedSourceResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>

            <el-alert
              v-if="selectedSourceLocalResource"
              type="info"
              :closable="false"
              style="margin-top: 20px"
            >
              <template #title>
                {{ t('transfer.taskWizard.localEngineTransfer') }}：{{ selectedSourceLocalResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.host">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedSourceLocalResource.connection_info.host }}:{{ selectedSourceLocalResource.connection_info.port }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.endpoint">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedSourceLocalResource.connection_info.endpoint }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.database">
                  {{ t('transfer.taskDetail.database') }}：{{ selectedSourceLocalResource.connection_info.database }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.bucket">
                  {{ t('transfer.taskDetail.bucket') }}：{{ selectedSourceLocalResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>
            </el-form>
          </div>

          <div class="step-section">
            <h3 class="step-section__title">{{ t('transfer.taskWizard.configReadParams') }}</h3>
            <el-alert
              v-if="selectedSourceLocalResource && !['spatialite','sqlite'].includes(sourceConnectorType)"
              type="warning"
              :closable="false"
              style="margin-bottom: 12px"
            >
              {{ t('transfer.taskWizard.localEngineMetaHint') }}
            </el-alert>
            <el-alert type="info" :closable="false" style="margin-bottom: 20px">
              {{ t('transfer.taskWizard.readParamsHint') }}
            </el-alert>

            <!-- PostgreSQL/MySQL 源配置 -->
            <div v-if="['postgresql', 'mysql'].includes(sourceConnectorType)">
              <el-form label-width="120px">
                <el-form-item :label="t('transfer.taskWizard.queryMode')">
                  <el-radio-group v-model="sourceConfig.queryType">
                    <el-radio-button value="table">{{ t('transfer.taskWizard.selectTable') }}</el-radio-button>
                    <el-radio-button value="sql">{{ t('transfer.taskWizard.customSQL') }}</el-radio-button>
                  </el-radio-group>
                </el-form-item>

                <el-form-item v-if="sourceConfig.queryType === 'table'" :label="t('transfer.taskWizard.dataTable')">
                  <el-select
                    v-model="sourceConfig.table"
                    :placeholder="t('transfer.taskWizard.selectDataTable')"
                    filterable
                    style="width: 100%"
                    :loading="loadingSourceTables"
                    @focus="handleLoadSourceTables"
                  >
                    <el-option
                      v-for="table in availableSourceTables"
                      :key="table"
                      :label="table"
                      :value="table"
                    />
                  </el-select>
                  <div class="hint">
                    {{ t('transfer.taskWizard.tableModeMetaHint') }}
                    <el-button type="primary" link size="small" @click="handleLoadSourceTables">
                      {{ t('transfer.taskWizard.refreshList') }}
                    </el-button>
                  </div>
                </el-form-item>

                <el-form-item v-if="sourceConfig.queryType === 'sql'" :label="t('transfer.taskWizard.sqlQuery')">
                  <el-input v-model="sourceConfig.query" type="textarea" :rows="5"
                    :placeholder="t('transfer.taskWizard.sqlQueryPlaceholder')" />
                  <div class="hint">{{ t('transfer.taskWizard.sqlQueryHint') }}</div>
                </el-form-item>

                <el-form-item v-if="sourceConfig.queryType === 'sql' && sourceConfig.query?.includes('?')"
                  :label="t('transfer.taskWizard.queryParameters')">
                  <el-input v-model="sourceConfig.parameters" :placeholder="t('transfer.taskWizard.queryParametersPlaceholder')" />
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.incrementalField')">
                  <el-input v-model="sourceConfig.incremental_field"
                    :placeholder="t('transfer.taskWizard.incrementalFieldPlaceholder')" />
                  <div class="hint">{{ t('transfer.taskWizard.incrementalFieldHint') }}</div>
                </el-form-item>

                <el-form-item v-if="sourceConfig.incremental_field" :label="t('transfer.taskWizard.incrementalType')">
                  <el-select v-model="sourceConfig.incremental_type">
                    <el-option :label="t('transfer.taskWizard.timestampType')" value="timestamp" />
                    <el-option :label="t('transfer.taskWizard.integerType')" value="integer" />
                  </el-select>
                </el-form-item>
              </el-form>
            </div>

            <!-- SpatiaLite/SQLite 源配置 -->
            <div v-if="['spatialite', 'sqlite'].includes(sourceConnectorType)">
              <el-form label-width="120px">
                <el-form-item :label="t('transfer.taskWizard.queryMode')">
                  <el-radio-group v-model="sourceConfig.queryType">
                    <el-radio-button value="table">{{ t('transfer.taskWizard.selectTable') }}</el-radio-button>
                    <el-radio-button value="sql">{{ t('transfer.taskWizard.customSQL') }}</el-radio-button>
                  </el-radio-group>
                </el-form-item>

                <el-form-item v-if="sourceConfig.queryType === 'table'" :label="t('transfer.taskWizard.dataTable')">
                  <el-select
                    v-model="sourceConfig.table"
                    :placeholder="t('transfer.taskWizard.selectDataTable')"
                    filterable
                    style="width: 100%"
                    :loading="loadingSourceTables"
                    @focus="handleLoadSourceTables"
                  >
                    <el-option
                      v-for="table in availableSourceTables"
                      :key="table"
                      :label="table"
                      :value="table"
                    />
                  </el-select>
                  <div class="hint">
                    {{ t('transfer.taskWizard.spatialiteTableHint') }}
                    <el-button type="primary" link size="small" @click="handleLoadSourceTables">
                      {{ t('transfer.taskWizard.refreshList') }}
                    </el-button>
                  </div>
                </el-form-item>

                <el-form-item v-if="sourceConfig.queryType === 'sql'" :label="t('transfer.taskWizard.sqlQuery')">
                  <el-input v-model="sourceConfig.query" type="textarea" :rows="5"
                    placeholder="SELECT id, ST_AsBinary(geom) AS geom, name FROM pois" />
                  <div class="hint">{{ t('transfer.taskWizard.sqlCustomHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.whereClause')">
                  <el-input v-model="sourceConfig.where_clause" :placeholder="t('transfer.taskWizard.whereClausePlaceholder')" />
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.spatialFields')">
                  <el-input v-model="sourceConfig.geometry_fields" :placeholder="t('transfer.taskWizard.spatialFieldsPlaceholder')" />
                  <div class="hint">{{ t('transfer.taskWizard.spatialFieldsHint') }}</div>
                </el-form-item>
              </el-form>
            </div>

            <!-- CSV/JSON 文件源配置 -->
            <div v-if="['csv', 'json'].includes(sourceConnectorType)">
              <el-form label-width="120px">
                <el-form-item :label="t('transfer.taskWizard.filePath')">
                  <el-input v-model="sourceConfig.path"
                    placeholder="imports/users.csv" />
                </el-form-item>

                <el-form-item v-if="sourceConnectorType === 'csv'" :label="t('transfer.taskWizard.csvOptions')">
                  <el-checkbox v-model="sourceConfig.has_header">{{ t('transfer.taskWizard.includeHeader') }}</el-checkbox>
                  <el-input v-model="sourceConfig.delimiter" :placeholder="t('transfer.taskWizard.delimiter')"
                    style="width: 100px; margin-left: 10px" />
                  <div class="hint">{{ t('transfer.taskWizard.delimiterDefaultHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.encoding')">
                  <el-select v-model="sourceConfig.encoding">
                    <el-option label="UTF-8" value="utf-8" />
                    <el-option label="GBK" value="gbk" />
                    <el-option label="GB2312" value="gb2312" />
                  </el-select>
                </el-form-item>
              </el-form>
            </div>

            <!-- S3/MinIO 源配置 -->
            <div v-if="sourceConnectorType === 's3'">
              <el-form label-width="120px">
                <el-form-item :label="t('transfer.taskWizard.objectPrefix')">
                  <el-input v-model="sourceConfig.prefix"
                    :placeholder="t('transfer.taskWizard.objectPrefixPlaceholder')" />
                  <div class="hint">{{ t('transfer.taskWizard.objectPrefixHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.recursiveRead')">
                  <el-switch v-model="sourceConfig.recursive" />
                  <div class="hint">{{ t('transfer.taskWizard.recursiveHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.includePatterns')">
                  <el-input v-model="sourceConfig.include_patterns"
                    placeholder='["*.json", "*.csv"]' />
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.excludePatterns')">
                  <el-input v-model="sourceConfig.exclude_patterns"
                    placeholder='["*.tmp", "*.log"]' />
                </el-form-item>
              </el-form>
            </div>
          </div>
        </div>

        <!-- 步骤 3: 目标设置 -->
        <div v-show="currentStep === 2" class="step-panel">
          <div class="step-section">
            <h3 class="step-section__title">{{ t('transfer.taskWizard.selectTargetDataSource') }}</h3>
            <el-form label-width="120px">
            <el-form-item :label="t('transfer.taskWizard.targetType')">
              <el-radio-group v-model="targetConnectorType" @change="handleTargetTypeChange">
                <el-radio-button value="postgresql">PostgreSQL</el-radio-button>
                <el-radio-button value="mysql">MySQL</el-radio-button>
                <el-radio-button value="s3">{{ t('transfer.taskWizard.objectStorage') }}</el-radio-button>
              </el-radio-group>
            </el-form-item>

            <el-form-item :label="t('transfer.taskWizard.selectTargetDataSource')">
              <el-select
                v-model="selectedTargetValue"
                :placeholder="t('transfer.taskWizard.selectTargetEngine')"
                style="width: 100%"
                filterable
                :loading="loadingSystemEngines || loadingLocalEngines"
              >
                <el-option
                  v-for="option in targetOptions"
                  :key="option.value"
                  :label="`${option.name} (${option.engine_type})`"
                  :value="option.value"
                />
              </el-select>
              <div class="hint">
                <p>
                  {{ t('transfer.taskWizard.systemEngineGlobal') }}
                  <el-link type="primary" @click="openSystemEngines">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                </p>
                <p>
                  {{ t('transfer.taskWizard.localEngineTransfer') }}
                  <el-link type="primary" @click="openLocalEngineDialog('target')">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                  <template v-if="selectedTargetLocalResource">
                    <el-link type="primary" @click="openLocalEngineDialog('target', selectedTargetLocalResource)">{{ t('transfer.taskWizard.editCurrent') }}</el-link>
                    <el-link
                      type="success"
                      :loading="syncingLocalResource"
                      @click="handleSyncLocalResource('target')"
                    >{{ t('transfer.taskWizard.syncToSystem') }}</el-link>
                  </template>
                </p>
              </div>
            </el-form-item>

            <el-alert
              v-if="selectedTargetResource"
              type="info"
              :closable="false"
              style="margin-top: 20px"
            >
              <template #title>
                {{ t('transfer.taskWizard.selectTargetDataSource') }}：{{ selectedTargetResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedTargetResource.connection_info.host">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedTargetResource.connection_info.host }}:{{ selectedTargetResource.connection_info.port }}
                </p>
                <p v-if="selectedTargetResource.connection_info.database">
                  {{ t('transfer.taskDetail.database') }}：{{ selectedTargetResource.connection_info.database }}
                </p>
                <p v-if="selectedTargetResource.connection_info.bucket">
                  {{ t('transfer.taskDetail.bucket') }}：{{ selectedTargetResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>

            <el-alert
              v-if="selectedTargetLocalResource"
              type="info"
              :closable="false"
              style="margin-top: 20px"
            >
              <template #title>
                {{ t('transfer.taskWizard.localEngineTransfer') }}：{{ selectedTargetLocalResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.host">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedTargetLocalResource.connection_info.host }}:{{ selectedTargetLocalResource.connection_info.port }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.endpoint">
                  {{ t('transfer.taskDetail.host') }}：{{ selectedTargetLocalResource.connection_info.endpoint }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.database">
                  {{ t('transfer.taskDetail.database') }}：{{ selectedTargetLocalResource.connection_info.database }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.bucket">
                  {{ t('transfer.taskDetail.bucket') }}：{{ selectedTargetLocalResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>
            </el-form>
          </div>

          <div class="step-section">
            <h3 class="step-section__title">{{ t('transfer.taskWizard.configWriteParams') }}</h3>
            <el-alert type="info" :closable="false" style="margin-bottom: 20px">
              {{ t('transfer.taskWizard.writeParamsHint') }}
            </el-alert>

            <!-- PostgreSQL/MySQL 目标配置 -->
            <div v-if="['postgresql', 'mysql'].includes(targetConnectorType)">
              <el-form label-width="140px">
                <el-form-item :label="t('transfer.taskWizard.targetTableName')">
                  <el-select
                    v-model="targetConfig.table"
                    :placeholder="t('transfer.taskWizard.selectTableOrInput')"
                    filterable
                    allow-create
                    default-first-option
                    style="width: 100%"
                    :loading="loadingTargetTables"
                    @focus="handleLoadTargetTables"
                    @change="handleTargetTableChange"
                  >
                    <el-option
                      v-for="table in availableTargetTables"
                      :key="table"
                      :label="table"
                      :value="table"
                    />
                  </el-select>
                  <div class="hint">
                    {{ t('transfer.taskWizard.targetTableMetaHint') }}
                    <el-button type="primary" link size="small" @click="handleLoadTargetTables">
                      {{ t('transfer.taskWizard.refreshList') }}
                    </el-button>
                  </div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.writeMode')">
                  <el-radio-group v-model="targetConfig.mode">
                    <el-radio-button value="insert">{{ t('transfer.taskWizard.insert') }}</el-radio-button>
                    <el-radio-button value="upsert">{{ t('transfer.taskWizard.upsert') }}</el-radio-button>
                    <el-radio-button value="replace">{{ t('transfer.taskWizard.replace') }}</el-radio-button>
                  </el-radio-group>
                  <div class="hint">
                    <p>• {{ t('transfer.taskWizard.insertHint') }}</p>
                    <p>• {{ t('transfer.taskWizard.upsertHint') }}</p>
                    <p>• {{ t('transfer.taskWizard.replaceHint') }}</p>
                  </div>
                </el-form-item>

                <el-form-item v-if="targetConfig.mode !== 'insert'" :label="t('transfer.taskWizard.conflictKeys')">
                  <el-input v-model="targetConfig.conflict_keys"
                    placeholder='["id"]' />
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.conflictStrategy')">
                  <el-select v-model="targetConfig.conflict_strategy">
                    <el-option :label="t('transfer.taskWizard.skip')" value="skip" />
                    <el-option :label="t('transfer.taskWizard.update')" value="update" />
                    <el-option :label="t('transfer.taskWizard.error')" value="error" />
                  </el-select>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.autoScanMetadata')">
                  <el-switch v-model="taskForm.auto_scan_metadata" />
                  <div class="hint">{{ t('transfer.taskWizard.autoScanHint') }}</div>
                </el-form-item>
              </el-form>
            </div>

            <!-- S3/MinIO 目标配置 -->
            <div v-if="targetConnectorType === 's3'">
              <el-form label-width="140px">
                <el-form-item :label="t('transfer.taskWizard.outputFormat')">
                  <el-radio-group v-model="selectedTargetFormat">
                    <el-radio-button value="csv">CSV</el-radio-button>
                    <el-radio-button value="csv-wkt" :disabled="!hasSpatialSource">CSV（WKT）</el-radio-button>
                    <el-radio-button value="json">JSON</el-radio-button>
                    <el-radio-button value="jsonl">JSONL</el-radio-button>
                    <el-radio-button value="parquet">Parquet</el-radio-button>
                    <el-radio-button value="geojson" :disabled="!hasSpatialSource">GeoJSON</el-radio-button>
                    <el-radio-button value="shapefile" :disabled="!hasSpatialSource">Shapefile</el-radio-button>
                  </el-radio-group>
                  <div class="hint">{{ t('transfer.taskWizard.outputFormatHint') }}</div>
                </el-form-item>

                <el-alert
                  v-if="selectedTargetFormat === 'geojson'"
                  type="info"
                  :closable="false"
                  style="margin-bottom: 12px"
                >
                  {{ t('transfer.taskWizard.geojsonHint') }}
                </el-alert>

                <el-alert
                  v-if="selectedTargetFormat === 'shapefile'"
                  type="info"
                  :closable="false"
                  style="margin-bottom: 12px"
                >
                  {{ t('transfer.taskWizard.shapefileHint') }}
                </el-alert>

                <el-form-item v-if="needsGeometrySelection" :label="t('transfer.taskWizard.spatialField')">
                  <template v-if="selectedTargetFormat === 'csv-wkt'">
                    <el-select
                      v-model="selectedGeometryFieldsMulti"
                      multiple
                      filterable
                      :placeholder="t('transfer.taskWizard.geometryFieldPlaceholder')"
                      style="width: 100%"
                    >
                      <el-option
                        v-for="field in spatialSourceFields"
                        :key="field"
                        :label="field"
                        :value="field"
                      />
                    </el-select>
                    <div class="hint">{{ t('transfer.taskWizard.wktMultiSelectHint') }}</div>
                  </template>
                  <template v-else>
                    <el-select
                      v-model="selectedGeometryField"
                      filterable
                      :placeholder="t('transfer.taskWizard.geometryFieldPlaceholder')"
                      style="width: 100%"
                    >
                      <el-option
                        v-for="field in spatialSourceFields"
                        :key="field"
                        :label="field"
                        :value="field"
                      />
                    </el-select>
                    <div class="hint">{{ t('transfer.taskWizard.primaryGeometryHint') }}</div>
                  </template>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.outputPath')">
                  <el-input
                    v-model="targetConfig.path"
                    :placeholder="pathPlaceholder"
                  >
                    <template #append>
                      <el-button
                        type="primary"
                        plain
                        @click="openObjectStoragePicker"
                        :disabled="!canOpenObjectStoragePicker"
                      >
                        {{ t('transfer.taskWizard.selectDirectory') }}
                      </el-button>
                    </template>
                  </el-input>
                  <div class="hint">
                    {{ t('transfer.taskWizard.outputPathHint') }} {{ objectStorageDirectoryHint }}
                  </div>
                </el-form-item>

                <el-form-item v-if="showCSVOptions" :label="t('transfer.taskWizard.csvHeaderOption')">
                  <el-checkbox v-model="targetConfig.headers">{{ t('transfer.taskWizard.includeHeader') }}</el-checkbox>
                  <el-input v-model="targetConfig.delimiter" :placeholder="t('transfer.taskWizard.delimiter')"
                    style="width: 100px; margin-left: 10px" />
                  <div class="hint">{{ t('transfer.taskWizard.delimiterDefaultHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.compressionMode')">
                  <el-select v-model="targetConfig.compression">
                    <el-option :label="t('transfer.taskWizard.noCompression')" value="none" />
                    <el-option label="Gzip" value="gzip" />
                    <el-option label="Zip" value="zip" />
                  </el-select>
                  <div class="hint">{{ t('transfer.taskWizard.compressionHint') }}</div>
                </el-form-item>

                <el-form-item :label="t('transfer.taskWizard.overwrite')">
                  <el-switch v-model="targetConfig.overwrite" />
                  <div class="hint">{{ t('transfer.taskWizard.overwriteHint') }}</div>
                </el-form-item>
              </el-form>
            </div>
          </div>
        </div>

        <!-- 步骤 4: 字段映射 -->
        <div v-show="currentStep === 3" class="step-panel">
          <FieldMappingEditor
            :source-fields="sourceFields"
            :target-fields="targetFields"
            :source-field-details="sourceFieldDetails"
            :target-field-details="targetFieldDetails"
            v-model:mappings="fieldMappings"
            :auto-create-mode="targetConnectorType === 's3' || (!targetIsSystem && selectedTargetLocalResource && ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.engine_type || '').toLowerCase()))"
            @fetch-fields="handleFetchFields"
          />
        </div>

        <!-- 步骤 5: 确认 -->
        <div v-show="currentStep === 4" class="step-panel">
          <el-result icon="success" :title="t('transfer.taskWizard.taskBasicInfo')" :sub-title="t('transfer.taskWizard.confirmCreateDesc')">
            <template #extra>
              <el-descriptions :column="2" border>
                <el-descriptions-item :label="t('transfer.taskWizard.taskNameLabel')">{{ taskForm.name }}</el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskWizard.executionMode')">{{ taskForm.mode }}</el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskWizard.batchSize')">{{ taskForm.batch_size }}</el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskWizard.scheduleLabel')" :span="2">
                  {{ taskForm.schedule ? describeCron(taskForm.schedule) : t('transfer.taskWizard.noSchedule') }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskDetail.sourceDataSource')">
                  {{ sourceResourceDisplayName }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskDetail.targetDataSource')">
                  {{ targetResourceDisplayName }}
                </el-descriptions-item>
                <el-descriptions-item :label="t('transfer.taskWizard.stepFieldMapping')" :span="2">
                  {{ t('transfer.taskWizard.fieldMappingCount', { count: fieldMappings.length }) }}
                </el-descriptions-item>
              </el-descriptions>

              <div style="margin-top: 20px;">
                <el-checkbox v-model="startImmediately">{{ t('transfer.taskWizard.taskStarted') }}</el-checkbox>
              </div>
            </template>
          </el-result>
        </div>
      </div>

      <div class="step-actions">
        <el-button v-if="currentStep > 0" @click="prevStep">{{ t('transfer.taskWizard.previousStep') }}</el-button>
        <el-button v-if="currentStep < 4" type="primary" @click="nextStep">{{ t('transfer.taskWizard.nextStep') }}</el-button>
        <el-button v-if="currentStep === 4" type="success" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? t('transfer.taskWizard.updateTask') : t('transfer.taskWizard.createTask2') }}
        </el-button>
      </div>
    </el-card>

    <el-dialog
      v-model="localResourceDialogVisible"
      :title="localResourceDialogMode === 'edit' ? t('transfer.taskWizard.editLocalEngine') : t('transfer.taskWizard.createLocalEngine')"
      width="600px"
    >
      <StorageEngineForm
        ref="localResourceFormRef"
        v-model="localResourceForm"
        :is-edit="localResourceDialogMode === 'edit'"
      />
      <template #footer>
        <el-button @click="localResourceDialogVisible = false">{{ t('transfer.taskWizard.cancel') }}</el-button>
        <el-button type="warning" :loading="testingLocalResource" @click="handleTestLocalResource">{{ t('transfer.taskWizard.testConnection') }}</el-button>
        <el-button type="primary" :loading="savingLocalResource" @click="handleSaveLocalResource">
          {{ localResourceDialogMode === 'edit' ? t('transfer.taskWizard.save') : t('transfer.taskWizard.create') }}
        </el-button>
      </template>
    </el-dialog>

    <ObjectStoragePathPicker
      v-model:visible="objectStoragePickerVisible"
      :scope="objectStoragePickerScope"
      :resource-id="objectStoragePickerResourceId"
      :initial-prefix="objectStoragePickerInitialPrefix"
      @selected="handleObjectStorageDirectorySelected"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { taskAPI } from '@/api/tasks'
import { localEnginesAPI } from '@/api/localEngines'
import { systemEnginesAPI } from '@/api/systemEngines'
import { getTables, getTableFields } from '@/api/meta'
import FieldMappingEditor from '@/components/FieldMappingEditor.vue'
import ObjectStoragePathPicker from '@/components/ObjectStoragePathPicker.vue'
import { StorageEngineForm, ScheduleConfig, describeCron } from '@common-ui'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const isEdit = computed(() => !!route.params.id)

const currentStep = ref(0)
const submitting = ref(false)
const startImmediately = ref(false)
const loadingTask = ref(false)
const isInitializingFromTask = ref(false)  // 防止 watch 在加载任务时重置配置


const basicFormRef = ref(null)
const taskForm = ref({
  name: '',
  description: '',
  type: 'sync',
  mode: 'batch',
  batch_size: 1000,
  max_parallelism: 4,
  schedule: '',
  auto_scan_metadata: true
})

const sourceConnectorType = ref('postgresql')
const targetConnectorType = ref('postgresql')
const sourceConfig = ref({})
const targetConfig = ref({})

const systemResources = ref([])
const localResources = ref([])
const loadingSystemEngines = ref(false)
const loadingLocalEngines = ref(false)

const selectedSourceValue = ref(null)
const selectedTargetValue = ref(null)

const localResourceDialogVisible = ref(false)
const localResourceDialogMode = ref('create')
const localResourceDialogScope = ref('source')
const localResourceFormRef = ref(null)
const localResourceForm = ref({
  engine_type: 'postgresql',
  name: '',
  description: '',
  is_active: true,
  connection_info: {}
})
const savingLocalResource = ref(false)
const testingLocalResource = ref(false)
const syncingLocalResource = ref(false)
const editingLocalResourceId = ref(null)

const sourceFields = ref([])
const targetFields = ref([])
const fieldMappings = ref([])
const sourceFieldDetails = ref([])
const targetFieldDetails = ref([])
const sourceFieldsLoaded = ref(false)
const loadingSourceFields = ref(false)
const targetFieldsLoaded = ref(false)
const loadingTargetFields = ref(false)
const selectedTargetFormat = ref('csv')
const selectedGeometryField = ref('')
const selectedGeometryFieldsMulti = ref([])

// 几何类型关键词列表（用于后端未返回 standard_type 时的备用判断）
const spatialTypeKeywords = ['geometry', 'geography', 'point', 'linestring', 'polygon', 'multipoint', 'multilinestring', 'multipolygon', 'geometrycollection']

// 判断字段是否为空间几何类型
// 优先使用后端返回的 standard_type（标准化类型），这是最准确的判断方式
const isSpatialField = (field) => {
  if (!field) return false

  // 1. 优先使用后端返回的 standard_type（最准确）
  const standardType = (field.standard_type || field.StandardType || '').toLowerCase()
  if (standardType) {
    return spatialTypeKeywords.includes(standardType)
  }

  // 2. 备用方案：基于 data_type 判断（不再使用字段名！）
  const dataType = (field.data_type || field.DataType || '').toLowerCase()
  const columnType = (field.column_type || field.ColumnType || '').toLowerCase()
  const typeString = `${dataType} ${columnType}`

  return spatialTypeKeywords.some(keyword => typeString.includes(keyword))
}

const spatialSourceFields = computed(() => {
  if (!Array.isArray(sourceFieldDetails.value)) {
    return []
  }
  return sourceFieldDetails.value
    .filter(isSpatialField)
    .map(field => field.name)
    .filter(Boolean)
})

const hasSpatialSource = computed(() => spatialSourceFields.value.length > 0)

// Debounce timers for auto-fetching fields when table selection changes
let sourceFieldsAutoFetchDebounce = null
let targetFieldsAutoFetchDebounce = null

// Auto-fetch SOURCE fields after selecting a table (system or local), when using table mode
watch(
  () => sourceConfig.value.table,
  async (newTable, oldTable) => {
    // 只在表名真正改变时触发(避免初始化时重复触发)
    if (!newTable || newTable === oldTable) return

    // SQL 模式不需要自动获取(用户自定义 SQL)
    if (sourceConfig.value.queryType === 'sql') return

    // 必须已选择数据源
    const hasResource = sourceIsSystem.value
      ? !!selectedSourceOption.value
      : !!selectedSourceLocalResource.value

    if (!hasResource) {
      console.log('自动获取源字段: 尚未选择数据源,跳过')
      return
    }

    // 清除之前的防抖定时器
    if (sourceFieldsAutoFetchDebounce) clearTimeout(sourceFieldsAutoFetchDebounce)

    // 300ms 防抖,避免快速切换表时多次请求
    sourceFieldsAutoFetchDebounce = setTimeout(async () => {
      console.log(`自动获取源字段: ${newTable}`)
      try {
        await handleFetchFields('source')

        // 如果已经在字段映射步骤,且目标字段也已准备好,尝试自动匹配
        if (currentStep.value === 3 && targetFields.value.length > 0) {
          performAutoMatch()
        }
      } catch (e) {
        console.error('自动加载源字段失败:', e)
        // 只在非初始化阶段显示错误提示
        if (!loadingTask.value && !isInitializingFromTask.value) {
          ElMessage.warning(t('transfer.taskWizard.autoFetchSourceFailed', { error: e.message || '' }))
        }
      }
    }, 300)
  }
)

// Auto-fetch TARGET fields after selecting a table (system only), when using table mode
watch(
  () => targetConfig.value.table,
  async (newTable, oldTable) => {
    // 只在表名真正改变时触发
    if (!newTable || newTable === oldTable) return

    // SQL 模式不需要自动获取
    if (targetConfig.value.queryType === 'sql') return

    // 目前只支持系统资源自动获取目标字段
    if (!targetIsSystem.value) {
      console.log('自动获取目标字段: 本地资源暂不支持,跳过')
      return
    }

    // 只支持数据库类型
    if (!['postgresql', 'mysql'].includes(targetConnectorType.value)) {
      console.log('自动获取目标字段: 目标类型不支持,跳过')
      return
    }

    // 必须已选择数据源
    if (!selectedTargetOption.value) {
      console.log('自动获取目标字段: 尚未选择数据源,跳过')
      return
    }

    // 清除之前的防抖定时器
    if (targetFieldsAutoFetchDebounce) clearTimeout(targetFieldsAutoFetchDebounce)

    // 300ms 防抖
    targetFieldsAutoFetchDebounce = setTimeout(async () => {
      console.log(`自动获取目标字段: ${newTable}`)
      try {
        await handleFetchFields('target')

        // 如果已经在字段映射步骤,且源字段也已准备好,尝试自动匹配
        if (currentStep.value === 3 && sourceFields.value.length > 0) {
          performAutoMatch()
        }
      } catch (e) {
        console.error('自动加载目标字段失败:', e)

        // 对于目标字段，所有错误都静默处理（可能是新表或表未扫描）
        // 用户可以在字段映射步骤手动配置或让系统自动使用源字段
        console.log(`表「${newTable}」字段获取失败，可能是新表或未扫描。将在字段映射步骤自动处理。`)
      }
    }, 300)
  }
)

const effectiveGeometryFields = computed(() => {
  if (!hasSpatialSource.value) {
    return []
  }
  if (selectedTargetFormat.value === 'csv-wkt') {
    return (selectedGeometryFieldsMulti.value || []).filter(Boolean)
  }
  return selectedGeometryField.value ? [selectedGeometryField.value] : []
})

const formatExtensionMap = {
  csv: 'csv',
  'csv-wkt': 'csv',
  json: 'json',
  jsonl: 'jsonl',
  parquet: 'parquet',
  geojson: 'geojson',
  shapefile: 'zip'
}

const pathPlaceholder = computed(() => {
  const extension = formatExtensionMap[selectedTargetFormat.value] || 'csv'
  return `输入文件路径，如：exports/data.${extension}`
})

const objectStorageDirectoryHint = computed(() => {
  if (!targetConfig.value.path) {
    return '建议使用“目录/文件名”的格式，例如：exports/users.csv'
  }
  const directory = extractDirectoryFromPath(targetConfig.value.path)
  return directory ? `当前目录：/${directory}` : '当前目录：/'
})

const showCSVOptions = computed(() => ['csv', 'csv-wkt'].includes(selectedTargetFormat.value))

const needsGeometrySelection = computed(() =>
  hasSpatialSource.value && ['geojson', 'shapefile', 'csv-wkt'].includes(selectedTargetFormat.value)
)

const availableSourceTables = ref([])
const availableTargetTables = ref([])
const loadingSourceTables = ref(false)
const loadingTargetTables = ref(false)

const objectStoragePickerVisible = ref(false)
const objectStoragePickerScope = ref('system')
const objectStoragePickerResourceId = ref(null)
const objectStoragePickerInitialPrefix = ref('')

const matchesConnectorType = (resourceType, connectorType) => {
  const resource = (resourceType || '').toLowerCase()
  const type = (connectorType || '').toLowerCase()
  if (!type) return true
  if (type === 's3') {
    return ['s3', 'minio', 'oss'].includes(resource)
  }
  if (type === 'spatialite' || type === 'sqlite') {
    return resource.includes('spatialite') || resource.includes('sqlite')
  }
  return resource.includes(type)
}

const filteredSourceSystemResources = computed(() =>
  systemResources.value.filter(r => matchesConnectorType(r.engine_type, sourceConnectorType.value))
)
const filteredSourceLocalResources = computed(() =>
  localResources.value.filter(r => matchesConnectorType(r.engine_type, sourceConnectorType.value))
)
const filteredTargetSystemResources = computed(() =>
  systemResources.value.filter(r => matchesConnectorType(r.engine_type, targetConnectorType.value))
)
const filteredTargetLocalResources = computed(() =>
  localResources.value.filter(r => matchesConnectorType(r.engine_type, targetConnectorType.value))
)

const buildOptions = (systemList, localList) => {
  const options = []
  systemList.forEach(res => {
    options.push({
      value: `system:${res.id}`,
      origin: 'system',
      originLabel: '系统管理',
      resource: res,
      name: res.name,
      engine_type: res.engine_type
    })
  })
  localList.forEach(res => {
    options.push({
      value: `local:${res.id}`,
      origin: 'local',
      originLabel: '数据传输',
      resource: res,
      name: res.name,
      engine_type: res.engine_type
    })
  })
  return options
}


const sourceOptions = computed(() =>
  buildOptions(filteredSourceSystemResources.value, filteredSourceLocalResources.value)
)
const targetOptions = computed(() =>
  buildOptions(filteredTargetSystemResources.value, filteredTargetLocalResources.value)
)

const findOption = (options, value) => options.find(opt => opt.value === value) || null

const selectedSourceOption = computed(() => findOption(sourceOptions.value, selectedSourceValue.value))
const selectedTargetOption = computed(() => findOption(targetOptions.value, selectedTargetValue.value))

const selectedSourceResource = computed(() =>
  selectedSourceOption.value?.origin === 'system' ? selectedSourceOption.value.resource : null
)
const selectedSourceLocalResource = computed(() =>
  selectedSourceOption.value?.origin === 'local' ? selectedSourceOption.value.resource : null
)
const selectedTargetResource = computed(() =>
  selectedTargetOption.value?.origin === 'system' ? selectedTargetOption.value.resource : null
)
const selectedTargetLocalResource = computed(() =>
  selectedTargetOption.value?.origin === 'local' ? selectedTargetOption.value.resource : null
)

const currentTargetResourceType = computed(() => {
  const option = selectedTargetOption.value
  if (option?.resource?.engine_type) {
    return (option.resource.engine_type || '').toLowerCase()
  }
  return ''
})

const canOpenObjectStoragePicker = computed(() => {
  if (targetConnectorType.value !== 's3') {
    return false
  }
  const option = selectedTargetOption.value
  if (!option?.resource?.id) {
    return false
  }
  const resourceType = currentTargetResourceType.value || (targetConnectorType.value || '').toLowerCase()
  return ['s3', 'minio', 'oss'].includes(resourceType)
})

const sourceResourceDisplayName = computed(() => {
  return selectedSourceOption.value?.name || '未选择'
})

const targetResourceDisplayName = computed(() => {
  return selectedTargetOption.value?.name || '未选择'
})

const sourceIsSystem = computed(() => selectedSourceOption.value?.origin === 'system')
const targetIsSystem = computed(() => selectedTargetOption.value?.origin === 'system')

const resetLocalResourceForm = () => {
  localResourceForm.value = {
    engine_type: 'postgresql',
    name: '',
    description: '',
    is_active: true,
    connection_info: {}
  }
  nextTick(() => {
    localResourceFormRef.value?.reset()
  })
}

const loadSystemResources = async () => {
  loadingSystemEngines.value = true
  try {
    const data = await systemEnginesAPI.list()
    systemResources.value = data || []
  } catch (error) {
    console.error('加载 System 资源失败:', error)
    if (!error.response || error.response.status === 503) {
      ElMessage.closeAll()
      ElMessage.warning(
        error.response?.status === 503
          ? t('transfer.taskWizard.systemUnavailable503')
          : t('transfer.taskWizard.cannotConnectSystem')
      )
    }
  } finally {
    loadingSystemEngines.value = false
  }
}

const loadLocalResources = async () => {
  loadingLocalEngines.value = true
  try {
    const data = await localEnginesAPI.list()
    localResources.value = data || []
  } catch (error) {
    console.error('加载本地存储引擎失败:', error)
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.loadLocalEngineFailed', { error: '' }))
  } finally {
    loadingLocalEngines.value = false
  }
}

const openSystemEngines = () => {
  const baseUrl = (import.meta.env.VITE_SYSTEM_URL || 'http://localhost:5173').replace(/\/$/, '')
  const token = localStorage.getItem('token')
  const url = `${baseUrl}/engines${token ? `?token=${encodeURIComponent(token)}` : ''}`
  window.open(url, '_blank', 'noopener,noreferrer')
}

const removeConnectionFields = (config) => {
  const keys = [
    // 数据库连接字段
    'type',
    'driver',
    'host',
    'port',
    'user',
    'password',
    'database',
    'sslmode',
    // 对象存储连接字段
    'endpoint',
    'access_key',
    'secret_key',
    'bucket',
    'use_ssl',
    'path',           // 对象存储路径
    'format',         // 文件格式
    'headers',        // CSV headers
    'delimiter',      // CSV 分隔符
    'compression',    // 压缩格式
    'overwrite',      // 是否覆盖
    // 文件系统字段
    'full_name',      // 文件全路径（应该只在 connection_info 里）
    // 引擎相关字段
    'engine_type',
    'connection_info',
    'local_resource_name'
  ]
  keys.forEach((key) => {
    if (key in config) {
      delete config[key]
    }
  })
}

const sanitizeConfigForDisplay = (config) => {
  const sanitized = { ...(config || {}) }
  removeConnectionFields(sanitized)
  if ('scope' in sanitized) {
    delete sanitized.scope
  }
  return sanitized
}

const stringifyIfNeeded = (value) => {
  if (Array.isArray(value) || (value && typeof value === 'object')) {
    try {
      return JSON.stringify(value)
    } catch {
      return ''
    }
  }
  return value ?? ''
}

const normalizeId = (value) => {
  if (value === undefined || value === null || value === '') {
    return null
  }
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

const normalizeConnectorType = (resourceType, config = {}) => {
  const type = (resourceType || '').toLowerCase()
  if (['postgresql', 'mysql', 's3', 'csv', 'json', 'spatialite', 'sqlite'].includes(type)) {
    return type
  }
  if (['minio', 'oss'].includes(type)) {
    return 's3'
  }

  const driver = (config.driver || '').toLowerCase()
  if (['postgresql', 'mysql', 'spatialite', 'sqlite'].includes(driver)) {
    return driver
  }

  const inferred = (config.type || '').toLowerCase()
  if (['postgresql', 'mysql', 'spatialite', 'sqlite'].includes(inferred)) {
    return inferred
  }
  if (['s3', 'minio', 'oss'].includes(inferred)) {
    return 's3'
  }

  return 'postgresql'
}

const prepareSourceConfigForDisplay = (rawConfig, connectorType) => {
  const sanitized = sanitizeConfigForDisplay(rawConfig)
  if (sanitized.parameters !== undefined && typeof sanitized.parameters !== 'string') {
    sanitized.parameters = stringifyIfNeeded(sanitized.parameters)
  }
  if (sanitized.include_patterns !== undefined && typeof sanitized.include_patterns !== 'string') {
    sanitized.include_patterns = stringifyIfNeeded(sanitized.include_patterns)
  }
  if (sanitized.exclude_patterns !== undefined && typeof sanitized.exclude_patterns !== 'string') {
    sanitized.exclude_patterns = stringifyIfNeeded(sanitized.exclude_patterns)
  }
  if (['postgresql', 'mysql'].includes(connectorType)) {
    sanitized.queryType = sanitized.queryType || (sanitized.query ? 'sql' : 'table')
  }
  return sanitized
}

const prepareTargetConfigForDisplay = (rawConfig, connectorType) => {
  const sanitized = sanitizeConfigForDisplay(rawConfig)

  if (!('path' in sanitized) && typeof sanitized.file_name === 'string') {
    sanitized.path = sanitized.file_name
  }
  if ('file_name' in sanitized) {
    delete sanitized.file_name
  }
  if (sanitized.conflict_keys !== undefined && typeof sanitized.conflict_keys !== 'string') {
    sanitized.conflict_keys = stringifyIfNeeded(sanitized.conflict_keys)
  }
  if (connectorType === 's3') {
    sanitized.format = sanitized.format || 'csv'
    sanitized.headers = sanitized.headers ?? true
    sanitized.delimiter = sanitized.delimiter || ','
    sanitized.compression = sanitized.compression || 'none'
    sanitized.overwrite = sanitized.overwrite ?? false
    sanitized.path = sanitized.path || ''
  }

  return sanitized
}

const normalizeDirectoryPath = (dir) => {
  if (!dir) return ''
  let value = String(dir).trim()
  value = value.replace(/^\/+/, '')
  if (value && !value.endsWith('/')) {
    value += '/'
  }
  return value
}

const extractDirectoryFromPath = (path) => {
  if (!path) return ''
  const index = path.lastIndexOf('/')
  if (index === -1) {
    return ''
  }
  return normalizeDirectoryPath(path.slice(0, index + 1))
}

const extractFileNameFromPath = (path) => {
  if (!path) return ''
  const index = path.lastIndexOf('/')
  return index === -1 ? path : path.slice(index + 1)
}

const toArray = (value) => {
  if (Array.isArray(value)) {
    return value.map(item => (item == null ? '' : String(item))).filter(Boolean)
  }
  if (value === undefined || value === null || value === '') {
    return []
  }
  return [String(value)]
}

const deriveSelectedFormat = (config = {}) => {
  const format = (config.format || '').toLowerCase()
  const spatialFormat = (config.spatial_format || '').toLowerCase()
  if (format === 'geojson') return 'geojson'
  if (format === 'shapefile') return 'shapefile'
  if (format === 'csv' && spatialFormat === 'wkt') return 'csv-wkt'
  if (format) return format
  return 'csv'
}

let syncingFormat = false
const syncSelectedFormatFromConfig = () => {
  syncingFormat = true
  selectedTargetFormat.value = deriveSelectedFormat(targetConfig.value || {})
  updatePathExtensionForFormat()
  nextTick(() => {
    syncingFormat = false
  })
}

const syncGeometrySelectionFromConfig = () => {
  const fields = toArray(targetConfig.value.geometry_fields ?? targetConfig.value.geometry_field)
  if (fields.length > 0) {
    selectedGeometryField.value = fields[0]
    selectedGeometryFieldsMulti.value = [...fields]
  } else {
    selectedGeometryField.value = ''
    selectedGeometryFieldsMulti.value = []
  }
}

const ensureGeometrySelectionDefaults = (fields) => {
  if (fields.length === 0) {
    selectedGeometryField.value = ''
    selectedGeometryFieldsMulti.value = []
    return
  }

  if (!selectedGeometryField.value) {
    selectedGeometryField.value = fields[0]
  }
  if (selectedGeometryFieldsMulti.value.length === 0) {
    selectedGeometryFieldsMulti.value = [fields[0]]
  }
}

const syncGeometryFieldsToConfig = () => {
  if (targetConnectorType.value !== 's3') {
    if (targetConfig.value) {
      delete targetConfig.value.geometry_field
      delete targetConfig.value.geometry_fields
      delete targetConfig.value.spatial_format
    }
    return
  }

  if (!targetConfig.value || typeof targetConfig.value !== 'object') {
    targetConfig.value = {}
  }

  const format = selectedTargetFormat.value

  if (format === 'csv-wkt') {
    const fields = (selectedGeometryFieldsMulti.value || []).filter(Boolean)
    if (fields.length > 0) {
      targetConfig.value.geometry_fields = fields
    } else {
      delete targetConfig.value.geometry_fields
    }
    delete targetConfig.value.geometry_field
    targetConfig.value.spatial_format = 'wkt'
  } else if (['geojson', 'shapefile'].includes(format)) {
    const field = (selectedGeometryField.value || '').trim()
    if (field) {
      targetConfig.value.geometry_field = field
      targetConfig.value.geometry_fields = [field]
    } else {
      delete targetConfig.value.geometry_field
      delete targetConfig.value.geometry_fields
    }
    if (format === 'geojson') {
      targetConfig.value.spatial_format = 'geojson'
    } else if (format === 'shapefile') {
      targetConfig.value.spatial_format = targetConfig.value.spatial_format || 'wkb'
    }
  } else {
    delete targetConfig.value.geometry_field
    delete targetConfig.value.geometry_fields
    if (format !== 'csv') {
      delete targetConfig.value.spatial_format
    } else if (!targetConfig.value.spatial_format || targetConfig.value.spatial_format === 'wkt') {
      delete targetConfig.value.spatial_format
    }
  }
}

const updatePathExtensionForFormat = () => {
  if (!targetConfig.value || typeof targetConfig.value !== 'object') {
    return
  }

  const extension = formatExtensionMap[selectedTargetFormat.value] || 'csv'
  const currentPath = targetConfig.value.path || ''

  if (!currentPath) {
    targetConfig.value.path = `exports/output.${extension}`
    return
  }

  const dir = extractDirectoryFromPath(currentPath)
  const rawName = extractFileNameFromPath(currentPath) || `output.${extension}`
  const nameWithoutExt = rawName.replace(/\.[^.]+$/, '')
  targetConfig.value.path = dir ? `${dir}${nameWithoutExt}.${extension}` : `${nameWithoutExt}.${extension}`
}

const applySelectedFormat = (formatValue) => {
  if (targetConnectorType.value !== 's3') {
    return
  }

  if (!targetConfig.value || typeof targetConfig.value !== 'object') {
    targetConfig.value = {}
  }

  switch (formatValue) {
    case 'csv':
      targetConfig.value.format = 'csv'
      targetConfig.value.spatial_format = ''
      targetConfig.value.headers = targetConfig.value.headers ?? true
      targetConfig.value.delimiter = targetConfig.value.delimiter || ','
      targetConfig.value.compression = targetConfig.value.compression || 'none'
      break
    case 'csv-wkt':
      targetConfig.value.format = 'csv'
      targetConfig.value.spatial_format = 'wkt'
      targetConfig.value.headers = targetConfig.value.headers ?? true
      targetConfig.value.delimiter = targetConfig.value.delimiter || ','
      targetConfig.value.compression = targetConfig.value.compression || 'none'
      ensureGeometrySelectionDefaults(spatialSourceFields.value)
      break
    case 'geojson':
      targetConfig.value.format = 'geojson'
      targetConfig.value.spatial_format = 'geojson'
      delete targetConfig.value.headers
      delete targetConfig.value.delimiter
      delete targetConfig.value.compression
      ensureGeometrySelectionDefaults(spatialSourceFields.value)
      break
    case 'shapefile':
      targetConfig.value.format = 'shapefile'
      targetConfig.value.spatial_format = 'wkb'
      delete targetConfig.value.headers
      delete targetConfig.value.delimiter
      targetConfig.value.compression = 'none'
      ensureGeometrySelectionDefaults(spatialSourceFields.value)
      break
    case 'json':
    case 'jsonl':
    case 'parquet':
      targetConfig.value.format = formatValue
      targetConfig.value.spatial_format = ''
      delete targetConfig.value.delimiter
      targetConfig.value.compression = targetConfig.value.compression || 'none'
      break
    default:
      targetConfig.value.format = formatValue
      targetConfig.value.spatial_format = ''
  }

  if (!targetConfig.value.path) {
    const extension = formatExtensionMap[formatValue] || 'csv'
    targetConfig.value.path = `exports/data.${extension}`
  }

  syncGeometryFieldsToConfig()
}

watch(spatialSourceFields, (fields) => {
  if (fields.length === 0) {
    selectedGeometryField.value = ''
    selectedGeometryFieldsMulti.value = []
    return
  }
  ensureGeometrySelectionDefaults(fields)
  syncGeometryFieldsToConfig()
})

watch(selectedTargetFormat, (newVal, oldVal) => {
  if (syncingFormat) {
    return
  }
  applySelectedFormat(newVal)
  if (newVal !== oldVal) {
    updatePathExtensionForFormat()
  }

  if (newVal === 'csv-wkt') {
    if (selectedGeometryFieldsMulti.value.length === 0 && selectedGeometryField.value) {
      selectedGeometryFieldsMulti.value = [selectedGeometryField.value]
    }
  } else {
    if (selectedGeometryField.value === '' && selectedGeometryFieldsMulti.value.length > 0) {
      selectedGeometryField.value = selectedGeometryFieldsMulti.value[0]
    }
  }

  syncGeometryFieldsToConfig()
})

watch(selectedGeometryField, () => {
  if (['geojson', 'shapefile'].includes(selectedTargetFormat.value)) {
    syncGeometryFieldsToConfig()
  }
})

watch(selectedGeometryFieldsMulti, () => {
  if (selectedTargetFormat.value === 'csv-wkt') {
    syncGeometryFieldsToConfig()
  }
}, { deep: true })

watch(hasSpatialSource, (available) => {
  if (!available && ['csv-wkt', 'geojson', 'shapefile'].includes(selectedTargetFormat.value)) {
    selectedTargetFormat.value = 'csv'
  } else if (available && ['csv-wkt', 'geojson', 'shapefile'].includes(selectedTargetFormat.value)) {
    ensureGeometrySelectionDefaults(spatialSourceFields.value)
    syncGeometryFieldsToConfig()
  }
})

watch(
  [currentStep, () => sourceConfig.value.table, () => targetConnectorType.value, () => sourceIsSystem.value],
  async ([step, table, targetType, isSystem]) => {
    if (
      step >= 2 &&
      targetType === 's3' &&
      isSystem &&
      table &&
      !sourceFieldsLoaded.value &&
      !loadingSourceFields.value
    ) {
      try {
        await handleFetchFields('source')
      } catch (error) {
        console.error('自动加载源字段失败:', error)
      }
    }
  }
)

watch(selectedSourceOption, (option) => {
  availableSourceTables.value = []
  if (option?.origin === 'system') {
    removeConnectionFields(sourceConfig.value)
  }
  sourceFieldDetails.value = []
  sourceFields.value = []
  fieldMappings.value = []
  selectedGeometryField.value = ''
  selectedGeometryFieldsMulti.value = []
  sourceFieldsLoaded.value = false
  syncGeometryFieldsToConfig()
})

watch(selectedTargetOption, (option) => {
  availableTargetTables.value = []
  if (option?.origin === 'system') {
    removeConnectionFields(targetConfig.value)
  }

  const type = (option?.resource?.engine_type || '').toLowerCase()
  if (option?.origin === 'local' && ['s3', 'minio', 'oss'].includes(type)) {
    targetConfig.value = {
      ...targetConfig.value,
      format: targetConfig.value.format || 'csv',
      headers: targetConfig.value.headers ?? true,
      delimiter: targetConfig.value.delimiter || ',',
      compression: targetConfig.value.compression || 'none',
      overwrite: targetConfig.value.overwrite ?? false
    }
  }

  syncSelectedFormatFromConfig()
  syncGeometrySelectionFromConfig()
  syncGeometryFieldsToConfig()
  targetFieldDetails.value = []
  targetFields.value = []
  fieldMappings.value = []
  targetFieldsLoaded.value = false
})

watch(sourceConnectorType, (newType) => {
  if (loadingTask.value) {
    return
  }
  if (selectedSourceOption.value && !matchesConnectorType(selectedSourceOption.value.resource.engine_type, newType)) {
    selectedSourceValue.value = null
  }
  sourceConfig.value = {}
  availableSourceTables.value = []
  sourceFields.value = []
  fieldMappings.value = []
  sourceFieldDetails.value = []
  selectedGeometryField.value = ''
  selectedGeometryFieldsMulti.value = []
  sourceFieldsLoaded.value = false
  syncGeometryFieldsToConfig()
})

watch(targetConnectorType, (newType, oldType) => {
  if (loadingTask.value || isInitializingFromTask.value) {
    return
  }

  if (selectedTargetOption.value && !matchesConnectorType(selectedTargetOption.value.resource.engine_type, newType)) {
    selectedTargetValue.value = null
  }
  targetConfig.value = {}
  availableTargetTables.value = []
  targetFields.value = []
  fieldMappings.value = []
  targetFieldDetails.value = []
  selectedGeometryField.value = ''
  selectedGeometryFieldsMulti.value = []
  targetFieldsLoaded.value = false

  if (newType === 's3') {
    targetConfig.value = {
      format: 'csv',
      headers: true,
      delimiter: ',',
      compression: 'none',
      overwrite: false
    }
    selectedTargetFormat.value = 'csv'
  } else {
    selectedTargetFormat.value = 'csv'
  }

  syncSelectedFormatFromConfig()
  syncGeometrySelectionFromConfig()
  syncGeometryFieldsToConfig()
})

const handleSourceTypeChange = () => {
  selectedSourceValue.value = null
  sourceConfig.value = {}
  availableSourceTables.value = []

  if (sourceConnectorType.value === 's3') {
    sourceConfig.value = {
      prefix: '',
      recursive: false
    }
  }
}

const handleTargetTypeChange = () => {
  selectedTargetValue.value = null
  targetConfig.value = {}
  targetFieldDetails.value = []
  targetFields.value = []
  selectedGeometryField.value = ''
  selectedGeometryFieldsMulti.value = []
  selectedTargetFormat.value = 'csv'

  if (targetConnectorType.value === 's3') {
    targetConfig.value = {
      format: 'csv',
      headers: true,
      delimiter: ',',
      compression: 'none',
      overwrite: false
    }
  }

  syncSelectedFormatFromConfig()
  syncGeometrySelectionFromConfig()
  syncGeometryFieldsToConfig()
}

const openObjectStoragePicker = () => {
  if (!canOpenObjectStoragePicker.value) {
    ElMessage.warning(t('transfer.taskWizard.selectObjectStorageFirst'))
    return
  }

  const option = selectedTargetOption.value
  const resourceId = option?.resource?.id
  if (!resourceId) {
    ElMessage.warning(t('transfer.taskWizard.resourceInfoIncomplete'))
    return
  }

  objectStoragePickerScope.value = option.origin === 'system' ? 'system' : 'local'
  objectStoragePickerResourceId.value = Number(resourceId)
  const directory = extractDirectoryFromPath(targetConfig.value.path)
  objectStoragePickerInitialPrefix.value = directory
  objectStoragePickerVisible.value = true
}

const handleObjectStorageDirectorySelected = (directory) => {
  const normalizedDir = normalizeDirectoryPath(directory)
  const currentFileName = extractFileNameFromPath(targetConfig.value.path)
  const fallbackFileName = `output.${targetConfig.value.format || 'csv'}`
  const fileName = currentFileName || fallbackFileName
  targetConfig.value.path = normalizedDir ? `${normalizedDir}${fileName}` : fileName
  objectStoragePickerInitialPrefix.value = normalizedDir
}

const openLocalEngineDialog = (scope, resource = null) => {
  localResourceDialogScope.value = scope
  if (resource) {
    localResourceDialogMode.value = 'edit'
    editingLocalResourceId.value = resource.id
    localResourceForm.value = {
      engine_type: resource.engine_type,
      name: resource.name,
      description: resource.description,
      is_active: resource.is_active,
      connection_info: { ...(resource.connection_info || {}) }
    }
    nextTick(() => {
      localResourceFormRef.value?.reset()
    })
  } else {
    localResourceDialogMode.value = 'create'
    editingLocalResourceId.value = null
    resetLocalResourceForm()
  }
  localResourceDialogVisible.value = true
}

watch(localResourceDialogVisible, (visible) => {
  if (!visible) {
    editingLocalResourceId.value = null
    resetLocalResourceForm()
  }
})

const handleTestLocalResource = async () => {
  const valid = await localResourceFormRef.value?.validate()
  if (!valid) {
    ElMessage.warning(t('transfer.taskWizard.completeEngineInfo'))
    return
  }

  testingLocalResource.value = true
  try {
    const result = await localEnginesAPI.testConnection(localResourceForm.value)
    if (result.success) {
      ElMessage.success(t('transfer.taskWizard.connectionTestSuccess'))
    } else {
      ElMessage.error(result.error || t('transfer.taskWizard.connectionTestFailed'))
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.connectionTestFailed'))
  } finally {
    testingLocalResource.value = false
  }
}

const handleSaveLocalResource = async () => {
  const valid = await localResourceFormRef.value?.validate()
  if (!valid) {
    ElMessage.warning(t('transfer.taskWizard.completeEngineConfig'))
    return
  }

  savingLocalResource.value = true
  try {
    let saved
    if (editingLocalResourceId.value) {
      saved = await localEnginesAPI.update(editingLocalResourceId.value, {
        name: localResourceForm.value.name,
        description: localResourceForm.value.description,
        is_active: localResourceForm.value.is_active,
        connection_info: localResourceForm.value.connection_info
      })
      ElMessage.success(t('transfer.taskWizard.engineUpdated'))
    } else {
      saved = await localEnginesAPI.create(localResourceForm.value)
      ElMessage.success(t('transfer.taskWizard.engineCreated'))
    }

    await loadLocalResources()

    if (localResourceDialogScope.value === 'source') {
      selectedSourceValue.value = `local:${saved.id}`
    } else {
      selectedTargetValue.value = `local:${saved.id}`
    }

    localResourceDialogVisible.value = false
  } catch (error) {
    console.error('保存存储引擎失败:', error)
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.saveEngineFailed', { error: '' }))
  } finally {
    savingLocalResource.value = false
  }
}

const handleSyncLocalResource = async (scope) => {
  const resource = scope === 'source' ? selectedSourceLocalResource.value : selectedTargetLocalResource.value
  if (!resource) {
    ElMessage.warning(t('transfer.taskWizard.selectSyncEngine'))
    return
  }

  try {
    await ElMessageBox.confirm(
      t('transfer.taskWizard.syncConfirm', { name: resource.name }),
      t('transfer.taskWizard.syncConfirmTitle'),
      { type: 'info' }
    )
  } catch {
    return
  }

  syncingLocalResource.value = true
  try {
    await localEnginesAPI.syncToSystem(resource.id)
    ElMessage.success(t('transfer.taskWizard.syncedToSystem'))
    await loadSystemResources()
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.syncFailed'))
  } finally {
    syncingLocalResource.value = false
  }
}

const loadTaskForEdit = async () => {
  if (!route.params.id) {
    return
  }

  loadingTask.value = true
  isInitializingFromTask.value = true

  try {
    const [taskData, mappingsData = []] = await Promise.all([
      taskAPI.get(route.params.id),
      taskAPI.getMappings(route.params.id).catch(() => [])
    ])

    taskForm.value = {
      ...taskForm.value,
      name: taskData.name || '',
      description: taskData.description || '',
      type: taskData.type || 'sync',
      mode: taskData.mode || 'batch',
      batch_size: taskData.batch_size || 1000,
      max_parallelism: taskData.max_parallelism || 1,
      schedule: taskData.schedule || ''
    }

    const taskConfig = taskData.config || {}
    const rawSourceConfig = { ...(taskConfig.source || {}) }
    const rawTargetConfig = { ...(taskConfig.target || {}) }

    const sourceScope = (rawSourceConfig.scope || '').toLowerCase()
    const sourceEngineId = normalizeId(rawSourceConfig.engine_id)

    if (sourceScope === 'system' || (sourceEngineId && !sourceScope)) {
      const systemResource = systemResources.value.find(res => res.id === sourceEngineId) || null
      if (sourceEngineId && !systemResource) {
        ElMessage.warning(t('transfer.taskWizard.sourceNotFound'))
      }
      const resolvedType = normalizeConnectorType(systemResource?.engine_type, rawSourceConfig)
      sourceConnectorType.value = resolvedType
      sourceConfig.value = prepareSourceConfigForDisplay(rawSourceConfig, resolvedType)
      selectedSourceValue.value = sourceEngineId ? `system:${sourceEngineId}` : null
    } else if (sourceScope === 'local') {
      const localResource = localResources.value.find(res => res.id === sourceEngineId) || null
      if (sourceEngineId && !localResource) {
        ElMessage.warning(t('transfer.taskWizard.localSourceNotFound'))
      }
      const resolvedType = normalizeConnectorType(localResource?.engine_type, rawSourceConfig)
      sourceConnectorType.value = resolvedType
      sourceConfig.value = prepareSourceConfigForDisplay(rawSourceConfig, resolvedType)
      selectedSourceValue.value = sourceEngineId ? `local:${sourceEngineId}` : null
    } else {
      const resolvedType = normalizeConnectorType(rawSourceConfig.engine_type, rawSourceConfig)
      sourceConnectorType.value = resolvedType
      sourceConfig.value = prepareSourceConfigForDisplay(rawSourceConfig, resolvedType)
      selectedSourceValue.value = sourceEngineId ? `system:${sourceEngineId}` : null
    }

    const targetScope = (rawTargetConfig.scope || '').toLowerCase()
    const targetEngineId = normalizeId(rawTargetConfig.engine_id)

    if (targetScope === 'system' || (targetEngineId && !targetScope)) {
      const systemResource = systemResources.value.find(res => res.id === targetEngineId) || null
      if (targetEngineId && !systemResource) {
        ElMessage.warning(t('transfer.taskWizard.targetNotFound'))
      }
      const resolvedTypeRaw = normalizeConnectorType(systemResource?.engine_type, rawTargetConfig)
      const resolvedType = ['csv', 'json'].includes(resolvedTypeRaw) ? 's3' : resolvedTypeRaw
      targetConnectorType.value = resolvedType
      targetConfig.value = prepareTargetConfigForDisplay(rawTargetConfig, resolvedType)
      selectedTargetValue.value = targetEngineId ? `system:${targetEngineId}` : null
    } else if (targetScope === 'local') {
      const localResource = localResources.value.find(res => res.id === targetEngineId) || null
      if (targetEngineId && !localResource) {
        ElMessage.warning(t('transfer.taskWizard.localTargetNotFound'))
      }
      const resolvedTypeRaw = normalizeConnectorType(localResource?.engine_type, rawTargetConfig)
      const resolvedType = ['csv', 'json'].includes(resolvedTypeRaw) ? 's3' : resolvedTypeRaw
      targetConnectorType.value = resolvedType
      targetConfig.value = prepareTargetConfigForDisplay(rawTargetConfig, resolvedType)
      selectedTargetValue.value = targetEngineId ? `local:${targetEngineId}` : null
    } else {
      const resolvedTypeRaw = normalizeConnectorType(rawTargetConfig.engine_type, rawTargetConfig)
      const resolvedType = ['csv', 'json'].includes(resolvedTypeRaw) ? 's3' : resolvedTypeRaw
      targetConnectorType.value = resolvedType
      targetConfig.value = prepareTargetConfigForDisplay(rawTargetConfig, resolvedType)
      selectedTargetValue.value = targetEngineId ? `system:${targetEngineId}` : null
    }

    syncSelectedFormatFromConfig()
    syncGeometrySelectionFromConfig()
    syncGeometryFieldsToConfig()

    // 恢复表列表（确保下拉框中有当前选中的表）
    // 注意：需要在 nextTick 之后执行，确保数据源选择已经生效
    await nextTick()

    if (sourceConfig.value.table) {
      // 将当前选中的表添加到可用表列表，避免下拉框显示为空
      if (!availableSourceTables.value.includes(sourceConfig.value.table)) {
        availableSourceTables.value = [sourceConfig.value.table]
      }
      console.log('恢复源表选择:', sourceConfig.value.table)
    }

    if (targetConfig.value.table) {
      // 将当前选中的表添加到可用表列表，避免下拉框显示为空
      if (!availableTargetTables.value.includes(targetConfig.value.table)) {
        availableTargetTables.value = [targetConfig.value.table]
      }
      console.log('恢复目标表选择:', targetConfig.value.table)
    }

    if (Array.isArray(rawSourceConfig.fields) && rawSourceConfig.fields.length > 0) {
      sourceFields.value = rawSourceConfig.fields
    }
    if (Array.isArray(rawTargetConfig.fields) && rawTargetConfig.fields.length > 0) {
      targetFields.value = rawTargetConfig.fields
    }

    if (Array.isArray(mappingsData) && mappingsData.length > 0) {
      fieldMappings.value = mappingsData.map(item => ({
        id: item.id,
        source_field: item.source_field,
        target_field: item.target_field,
        default_value: item.default_value || '',
        field_type: item.field_type || 'string',
        format: item.format || '',
        nullable: item.nullable !== false
      }))

      if (sourceFields.value.length === 0) {
        const uniqueSourceFields = [...new Set(mappingsData.map(item => item.source_field).filter(Boolean))]
        if (uniqueSourceFields.length > 0) {
          sourceFields.value = uniqueSourceFields
        }
      }

      if (targetFields.value.length === 0) {
        const uniqueTargetFields = [...new Set(mappingsData.map(item => item.target_field).filter(Boolean))]
        if (uniqueTargetFields.length > 0) {
          targetFields.value = uniqueTargetFields
        }
      }
    } else {
      fieldMappings.value = []
    }
  } catch (error) {
    console.error('加载任务详情失败:', error)
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.loadTaskFailed', { error: '' }))
  } finally {
    loadingTask.value = false
    // 使用 nextTick 确保所有响应式更新完成后再解除初始化标志
    await nextTick()
    isInitializingFromTask.value = false
  }
}

const handleLoadSourceTables = async () => {
  if (!selectedSourceOption.value && !selectedSourceLocalResource.value) {
    ElMessage.warning(t('transfer.taskWizard.selectSourceFirst'))
    return
  }

  loadingSourceTables.value = true
  try {
    if (sourceIsSystem.value) {
      if (!['postgresql', 'mysql'].includes(sourceConnectorType.value)) {
        return
      }
      const tableList = await getTables(selectedSourceOption.value?.resource?.id)
      if (Array.isArray(tableList)) {
        availableSourceTables.value = tableList.map(item => item.name || item)
        ElMessage.success(t('transfer.taskWizard.tablesLoaded', { count: availableSourceTables.value.length }))
      } else {
        ElMessage.warning(t('transfer.taskWizard.noTablesFound'))
      }
    } else {
      // 本地资源：统一调用后端列出表（支持 postgresql/mysql/spatialite/sqlite）
      const res = await localEnginesAPI.listTables(selectedSourceLocalResource.value.id)
      if (Array.isArray(res)) {
        availableSourceTables.value = res
      } else if (Array.isArray(res?.data)) {
        availableSourceTables.value = res.data
      } else {
        availableSourceTables.value = []
      }
      ElMessage.success(t('transfer.taskWizard.localTablesLoaded', { count: availableSourceTables.value.length }))
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    ElMessage.error(t('transfer.taskWizard.loadTablesFailed', { error: error.response?.data?.error || error.message }))
  } finally {
    loadingSourceTables.value = false
  }
}

const handleLoadTargetTables = async () => {
  if (!targetIsSystem.value) {
    ElMessage.info(t('transfer.taskWizard.localStorageNotSupported'))
    return
  }

  if (!selectedTargetOption.value) {
    ElMessage.warning(t('transfer.taskWizard.selectTargetFirst'))
    return
  }

  if (!['postgresql', 'mysql'].includes(targetConnectorType.value)) {
    return
  }

  loadingTargetTables.value = true
  try {
    const tableList = await getTables(selectedTargetOption.value?.resource?.id)
    if (Array.isArray(tableList) && tableList.length > 0) {
      availableTargetTables.value = tableList.map(item => item.name || item)
      ElMessage.success(t('transfer.taskWizard.tablesLoaded', { count: availableTargetTables.value.length }))
    } else {
      ElMessage.warning(t('transfer.taskWizard.noTablesFound'))
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    if (error.response?.status === 404 || error.response?.data?.error?.includes('未找到')) {
      ElMessage.warning(t('transfer.taskWizard.targetNotScanned'))
    } else {
      ElMessage.error(t('transfer.taskWizard.loadTablesFailed', { error: error.response?.data?.error || error.message }))
    }
  } finally {
    loadingTargetTables.value = false
  }
}

// 目标表名改变时的处理
const handleTargetTableChange = (newTable) => {
  console.log(`目标表名改变: ${newTable}, 清空目标字段和映射`)
  // 清空目标字段,强制在字段映射步骤重新获取
  targetFields.value = []
  targetFieldDetails.value = []
  // 清空已有的字段映射
  fieldMappings.value = []
  // 提示用户
  ElMessage.info(t('transfer.taskWizard.targetTableChanged'))
}

const handleFetchFields = async (type) => {
  const isSource = type === 'source'
  const useSystem = isSource ? sourceIsSystem.value : targetIsSystem.value
  const resourceId = isSource ? selectedSourceOption.value?.resource?.id : selectedTargetOption.value?.resource?.id
  const localResource = isSource ? selectedSourceLocalResource.value : selectedTargetLocalResource.value
  const tableName = isSource ? sourceConfig.value.table : targetConfig.value.table
  const connectorType = isSource ? sourceConnectorType.value : targetConnectorType.value

  // 验证必要参数
  if (useSystem && !resourceId) {
    ElMessage.warning(t('transfer.taskWizard.selectSourceRequired'))
    return
  }

  if (!useSystem && !localResource) {
    ElMessage.warning(t('transfer.taskWizard.selectTargetRequired'))
    return
  }

  if (!tableName) {
    ElMessage.warning(isSource ? t('transfer.taskWizard.selectSourceRequired') : t('transfer.taskWizard.selectTargetRequired'))
    return
  }

  if (isSource) {
    loadingSourceFields.value = true
  } else {
    loadingTargetFields.value = true
  }

  try {
    if (useSystem) {
      // 系统资源: 从 Meta 模块获取字段
      const data = await getTableFields(resourceId, '', tableName)
      const response = { data }

      if (response.data && Array.isArray(response.data)) {
        if (isSource) {
          // 处理源字段
          if (response.data.length > 0 && typeof response.data[0] === 'object') {
            sourceFieldDetails.value = response.data.map(field => ({
              name: field.name || '',
              data_type: field.data_type || '',
              column_type: field.column_type || '',
              standard_type: field.standard_type || '',  // 添加标准化类型字段
              default_value: field.default_value || '',
              comment: field.comment || '',
              is_nullable: field.is_nullable,
              is_primary_key: field.is_primary_key,
              is_unique_key: field.is_unique_key
            }))
            sourceFields.value = sourceFieldDetails.value.map(field => field.name).filter(Boolean)
          } else {
            sourceFieldDetails.value = (response.data || []).map(name => ({
              name: String(name || ''),
              data_type: '',
              column_type: '',
              default_value: '',
              comment: '',
              is_nullable: true,
              is_primary_key: false,
              is_unique_key: false
            }))
            sourceFields.value = response.data.map(item => (item == null ? '' : String(item))).filter(Boolean)
          }

          if (hasSpatialSource.value && needsGeometrySelection.value) {
            ensureGeometrySelectionDefaults(spatialSourceFields.value)
            syncGeometryFieldsToConfig()
          } else {
            syncGeometryFieldsToConfig()
          }

          ElMessage.success(t('transfer.taskWizard.fieldCountLoaded', { count: sourceFields.value.length }))
          sourceFieldsLoaded.value = true
          if (response.data.length > 0 && typeof response.data[0] === 'object') {
            targetFieldDetails.value = response.data.map(field => ({
              name: field.name || '',
              data_type: field.data_type || '',
              column_type: field.column_type || '',
              standard_type: field.standard_type || ''  // 添加标准化类型字段
            }))
            targetFields.value = targetFieldDetails.value.map(field => field.name).filter(Boolean)
          } else {
            targetFieldDetails.value = (response.data || []).map(name => ({
              name: String(name || ''),
              data_type: '',
              column_type: ''
            }))
            targetFields.value = response.data.map(item => (item == null ? '' : String(item))).filter(Boolean)
          }
          ElMessage.success(t('transfer.taskWizard.targetFieldCountLoaded', { count: targetFields.value.length }))
          targetFieldsLoaded.value = true
        }
      } else {
        ElMessage.warning(t('transfer.taskWizard.fieldInfoNotFound'))
      }
    } else {
      // 本地资源: 直接从数据库扫描字段
      if (!localResource) {
        ElMessage.info(t('transfer.taskWizard.localResourceNotSupported'))
        return
      }

      console.log(`调用本地资源字段扫描: resourceId=${localResource.id}, table=${tableName}, type=${connectorType}`)

      const res = await localEnginesAPI.listFields(localResource.id, tableName)
      const data = Array.isArray(res?.data) ? res.data : (Array.isArray(res) ? res : [])

      if (data && Array.isArray(data) && data.length > 0) {
        if (isSource) {
          sourceFieldDetails.value = data.map(field => ({
            name: field.name || '',
            data_type: field.data_type || field.DataType || '',
            column_type: field.column_type || field.ColumnType || '',
            standard_type: field.standard_type || field.StandardType || '',  // 添加标准化类型
            nullable: (field.nullable ?? field.is_nullable) !== false
          }))
          sourceFields.value = sourceFieldDetails.value.map(f => f.name).filter(Boolean)

          if (hasSpatialSource.value && needsGeometrySelection.value) {
            ensureGeometrySelectionDefaults(spatialSourceFields.value)
            syncGeometryFieldsToConfig()
          } else {
            syncGeometryFieldsToConfig()
          }

          ElMessage.success(t('transfer.taskWizard.fieldCountLoaded', { count: sourceFields.value.length }))
          sourceFieldsLoaded.value = true
        } else {
          targetFieldDetails.value = data.map(field => ({
            name: field.name || '',
            data_type: field.data_type || field.DataType || '',
            column_type: field.column_type || field.ColumnType || '',
            standard_type: field.standard_type || field.StandardType || ''  // 添加标准化类型
          }))
          targetFields.value = targetFieldDetails.value.map(f => f.name).filter(Boolean)
          ElMessage.success(t('transfer.taskWizard.targetFieldCountLoaded', { count: targetFields.value.length }))
          targetFieldsLoaded.value = true
        }
      } else {
        ElMessage.warning(t('transfer.taskWizard.fieldInfoNotFound'))
      }
    }
  } catch (error) {
    console.error('获取字段列表失败:', error)
    const errorMsg = error.response?.data?.error || error.message || '未知错误'

    // 对于目标字段，无论什么错误都静默处理，让调用方决定如何处理
    // 这样用户输入新表名或表未扫描时都不会看到错误提示
    if (!isSource) {
      console.log(`获取目标表「${tableName}」字段失败，可能是新表或未扫描，错误: ${errorMsg}`)
      throw error  // 抛出异常，让调用方处理
    }

    // 只对源字段显示错误提示
    if (error.response?.status === 404) {
      ElMessage.error(t('transfer.taskWizard.tableFieldNotFound', { name: tableName }))
    } else {
      ElMessage.error(t('transfer.taskWizard.fieldsFetchFailed', { error: errorMsg }))
    }

    // 抛出异常，让调用方知道获取失败
    throw error
  } finally {
    if (isSource) {
      loadingSourceFields.value = false
    } else {
      loadingTargetFields.value = false
    }
  }
}

const prevStep = () => {
  if (currentStep.value > 0) {
    currentStep.value--
  }
}

const nextStep = async () => {
  if (currentStep.value === 0) {
    try {
      await basicFormRef.value?.validate()
    } catch {
      ElMessage.warning(t('transfer.taskWizard.basicInfoRequired'))
      return
    }
  } else if (currentStep.value === 1) {
    if (!selectedSourceOption.value) {
      ElMessage.warning(t('transfer.taskWizard.selectSourceRequired'))
      return
    }
  } else if (currentStep.value === 2) {
    if (!selectedTargetOption.value) {
      ElMessage.warning(t('transfer.taskWizard.selectTargetRequired'))
      return
    }
  }

  if (currentStep.value < 4) {
    currentStep.value++
    if (currentStep.value === 3) {
      await autoFetchAndMapFields()
    }
  }
}

const autoFetchAndMapFields = async () => {
  console.log('=== 进入字段映射步骤,开始自动获取字段 ===')
  console.log('当前配置:', {
    sourceTable: sourceConfig.value.table,
    targetTable: targetConfig.value.table,
    sourceFieldsCount: sourceFields.value.length,
    targetFieldsCount: targetFields.value.length
  })

  try {
    // 第一步: 获取源字段(如果还没有)
    if (sourceFields.value.length === 0) {
      console.log('源字段为空,尝试自动获取...')

      if (sourceIsSystem.value && selectedSourceOption.value && sourceConfig.value.table) {
        console.log('系统资源 + 表模式,获取源字段')
        await handleFetchFields('source')
      } else if (
        !sourceIsSystem.value &&
        selectedSourceLocalResource.value &&
        sourceConfig.value.queryType === 'table' &&
        sourceConfig.value.table
      ) {
        console.log('本地资源 + 表模式,获取源字段')
        await handleFetchFields('source')
      } else if (sourceConfig.value.queryType === 'sql') {
        console.log('SQL 模式,无法自动获取源字段')
        ElMessage.info(t('transfer.taskWizard.sqlModeInfo'))
      } else {
        console.log('缺少必要参数,无法获取源字段:', {
          isSystem: sourceIsSystem.value,
          resourceId: selectedSourceOption.value?.resource?.id,
          localResource: !!selectedSourceLocalResource.value,
          table: sourceConfig.value.table,
          queryType: sourceConfig.value.queryType
        })
      }
    } else {
      console.log(`源字段已存在: ${sourceFields.value.length} 个`)
    }

    // 等待源字段加载完成
    await nextTick()

    // 第二步: 获取目标字段(根据目标类型)
    // 需要重新获取目标字段的条件:
    // 1. 目标字段为空
    // 2. 或者目标字段不为空但需要强制刷新(表名可能已改变)
    const shouldRefetchTarget = targetFields.value.length === 0

    if (shouldRefetchTarget) {
      console.log('目标字段为空,尝试自动获取...')
      console.log('目标配置信息:', {
        isSystem: targetIsSystem.value,
        connectorType: targetConnectorType.value,
        resourceId: selectedTargetOption.value?.resource?.id,
        localResource: selectedTargetLocalResource.value?.id,
        localResourceType: selectedTargetLocalResource.value?.engine_type,
        table: targetConfig.value.table,
        sourceFieldsCount: sourceFields.value.length
      })

      if (targetIsSystem.value && ['postgresql', 'mysql'].includes(targetConnectorType.value)) {
        // 数据库目标: 从 Meta 模块获取字段
        if (targetConfig.value.table) {
          console.log('数据库目标 + 已选表,获取目标字段')
          try {
            await handleFetchFields('target')

            // 检查是否成功获取到字段
            if (targetFields.value.length === 0) {
              // 可能是新表,使用源字段作为目标字段
              console.log('目标字段为空(可能是新表),使用源字段作为目标字段')
              if (sourceFields.value.length > 0) {
                targetFields.value = [...sourceFields.value]
              ElMessage.info(t('transfer.taskWizard.targetTableNewAutoMapped'))
              } else {
              ElMessage.warning(t('transfer.taskWizard.sourceFieldsEmpty'))
              }
            }
          } catch (error) {
            // 如果获取失败(404等错误),使用源字段作为目标字段
            console.warn('获取目标字段失败,使用源字段:', error)
            if (sourceFields.value.length > 0) {
              console.log('使用源字段作为目标字段 (异常情况)')
              targetFields.value = [...sourceFields.value]
              ElMessage.info(t('transfer.taskWizard.targetTableNewAutoMapped'))
            }
          }
        } else {
          console.log('数据库目标但未选表,跳过获取目标字段')
          ElMessage.warning(t('transfer.taskWizard.selectTargetFirst'))
        }
      } else if (targetConnectorType.value === 's3' ||
                 (!targetIsSystem.value &&
                  selectedTargetLocalResource.value &&
                  ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.value.engine_type || '').toLowerCase()))) {
        // 对象存储目标: 使用源字段作为目标字段
        console.log('对象存储目标,使用源字段作为目标字段')
        targetFields.value = [...sourceFields.value]

        if (hasSpatialSource.value && needsGeometrySelection.value) {
          ensureGeometrySelectionDefaults(spatialSourceFields.value)
        }
        syncGeometryFieldsToConfig()
      } else {
        console.log('未知目标类型或缺少必要参数:', {
          isSystem: targetIsSystem.value,
          type: targetConnectorType.value,
          localResource: selectedTargetLocalResource.value?.engine_type
        })
        ElMessage.warning(t('transfer.taskWizard.selectTargetRequired'))
      }
    } else {
      console.log(`目标字段已存在: ${targetFields.value.length} 个`)
    }

    // 等待所有字段加载完成
    await nextTick()

    // 第三步: 执行自动匹配
    console.log(`准备自动匹配: 源字段 ${sourceFields.value.length} 个, 目标字段 ${targetFields.value.length} 个`)

    if (sourceFields.value.length > 0 && targetFields.value.length > 0) {
      performAutoMatch()
    } else if (sourceFields.value.length === 0) {
      console.warn('源字段列表为空,无法自动映射')
      ElMessage.warning(t('transfer.taskWizard.sourceFieldsEmpty'))
    } else if (targetFields.value.length === 0) {
      console.warn('目标字段列表为空,无法自动映射')
      ElMessage.warning(t('transfer.taskWizard.targetFieldsEmpty'))
    }

    console.log('=== 字段自动获取流程完成 ===')
  } catch (error) {
    console.error('自动字段映射失败:', error)
    ElMessage.error(t('transfer.taskWizard.autoFieldMappingFailed', { error: error.message }))
  }
}

const performAutoMatch = () => {
  const newMappings = []

  // 辅助函数：检测字段类型（优先使用后端标准化类型）
  const detectFieldType = (sourceField) => {
    // 1. 优先使用源字段的标准化类型
    const sourceDetail = sourceFieldDetails.value.find(f => f.name === sourceField)
    if (sourceDetail?.standard_type) {
      // 如果是几何类型，统一返回 'geometry'
      if (isSpatialField(sourceDetail)) {
        return 'geometry'
      }
      // 其他类型直接返回标准化类型
      return sourceDetail.standard_type
    }

    // 2. 备用：检查目标字段的标准化类型
    const targetDetail = targetFieldDetails.value.find(f => f.name === sourceField)
    if (targetDetail?.standard_type) {
      if (isSpatialField(targetDetail)) {
        return 'geometry'
      }
      return targetDetail.standard_type
    }

    // 3. 最后的备用方案：基于字段详情推断（兼容旧数据）
    if (sourceDetail && isSpatialField(sourceDetail)) {
      return 'geometry'
    }
    if (targetDetail && isSpatialField(targetDetail)) {
      return 'geometry'
    }

    return 'string'
  }

  // 第一步：完全匹配（字段名完全相同）
  sourceFields.value.forEach(sourceField => {
    if (targetFields.value.includes(sourceField)) {
      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        field_type: detectFieldType(sourceField),
        format: '',
        default_value: '',
        nullable: true
      })
    }
  })

  // 第二步：模糊匹配（去掉下划线、转小写后比较）
  sourceFields.value.forEach(sourceField => {
    const normalizedSource = sourceField.toLowerCase().replace(/_/g, '')
    targetFields.value.forEach(targetField => {
      const normalizedTarget = targetField.toLowerCase().replace(/_/g, '')
      if (normalizedSource === normalizedTarget &&
          !newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: targetField,
          field_type: detectFieldType(sourceField),
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  })

  // 第三步：对于对象存储目标，自动创建所有源字段的映射
  const targetIsObjectStorage =
    (targetIsSystem.value && targetConnectorType.value === 's3') ||
    (!targetIsSystem.value &&
      ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.value?.engine_type || '').toLowerCase()))

  if (targetIsObjectStorage) {
    sourceFields.value.forEach(sourceField => {
      if (!newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: sourceField,
          field_type: detectFieldType(sourceField),
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  }

  // 第四步：对于数据库目标，如果源和目标字段完全一致（说明是新表或者自动填充的字段）
  // 且没有任何匹配结果，则自动创建所有源字段的映射
  const targetIsDatabase = ['postgresql', 'mysql'].includes(targetConnectorType.value)
  const fieldsAreIdentical = sourceFields.value.length > 0 &&
                             sourceFields.value.length === targetFields.value.length &&
                             sourceFields.value.every(sf => targetFields.value.includes(sf))

  if (targetIsDatabase && fieldsAreIdentical && newMappings.length === 0) {
    console.log('检测到数据库目标，源和目标字段完全一致，自动创建全部映射')
    sourceFields.value.forEach(sourceField => {
      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        field_type: detectFieldType(sourceField),
        format: '',
        default_value: '',
        nullable: true
      })
    })
  }

  // 更新映射并提示用户
  if (newMappings.length > 0) {
    fieldMappings.value = newMappings
    ElMessage.success(t('transfer.taskWizard.autoMatched', { count: newMappings.length }))
  } else {
    ElMessage.warning(t('transfer.taskWizard.noMatchingFields'))
  }
}

const buildConnectorConfigFromResource = (resource) => {
  if (!resource) return {}
  const conn = resource.connection_info || {}
  const type = (resource.engine_type || '').toLowerCase()

  if (['postgresql', 'mysql'].includes(type)) {
    const portValue = conn.port
    const port = typeof portValue === 'number' ? portValue : Number(portValue) || 0
    return {
      engine_type: type,
      driver: type,
      host: conn.host || '',
      port,
      user: conn.user || '',
      password: conn.password || '',
      database: conn.database || '',
      sslmode: conn.sslmode || 'disable'
    }
  }

  if (['s3', 'minio', 'oss'].includes(type)) {
    return {
      engine_type: resource.engine_type,
      endpoint: conn.endpoint || '',
      access_key: conn.access_key || '',
      secret_key: conn.secret_key || '',
      bucket: conn.bucket || '',
      use_ssl: !!conn.use_ssl
    }
  }

  if (['spatialite', 'sqlite'].includes(type)) {
    return {
      engine_type: resource.engine_type,
      full_name: conn.full_name || '',
      connection_info: conn
    }
  }

  return {
    engine_type: resource.engine_type,
    connection_info: conn
  }
}

const handleSubmit = async () => {
  submitting.value = true
  try {
    syncGeometryFieldsToConfig()

    const config = {
      source: { ...sourceConfig.value },
      target: { ...targetConfig.value }
    }

    // 规范化 SpatiaLite 源配置的 geometry_fields（字符串 -> 数组）
    if (['spatialite', 'sqlite'].includes(sourceConnectorType.value)) {
      const gf = config.source.geometry_fields
      if (typeof gf === 'string') {
        const arr = gf.split(',').map(s => s.trim()).filter(Boolean)
        if (arr.length > 0) {
          config.source.geometry_fields = arr
        } else {
          delete config.source.geometry_fields
        }
      }
    }

    if (typeof config.source.parameters === 'string') {
      try {
        config.source.parameters = JSON.parse(config.source.parameters)
      } catch {}
    }
    if (typeof config.source.include_patterns === 'string') {
      try {
        config.source.include_patterns = JSON.parse(config.source.include_patterns)
      } catch {}
    }
    if (typeof config.source.exclude_patterns === 'string') {
      try {
        config.source.exclude_patterns = JSON.parse(config.source.exclude_patterns)
      } catch {}
    }
    if (typeof config.target.conflict_keys === 'string') {
      try {
        config.target.conflict_keys = JSON.parse(config.target.conflict_keys)
      } catch {}
    }

    if (selectedSourceOption.value?.origin === 'system') {
      config.source.scope = 'system'
      const engineId = selectedSourceOption.value?.resource?.id
      if (!engineId) {
        ElMessage.warning(t('transfer.taskWizard.selectSourceRequired'))
        submitting.value = false
        return
      }
      config.source.engine_id = engineId
      delete config.source.local_resource_name
      removeConnectionFields(config.source)
    } else if (selectedSourceLocalResource.value) {
      const localResource = selectedSourceLocalResource.value
      // 先清理不需要的字段，避免冗余
      const cleanConfig = { ...config.source }
      delete cleanConfig.full_name  // full_name应该只在connection_info里
      delete cleanConfig.engine_type
      delete cleanConfig.connection_info

      config.source = {
        ...cleanConfig,
        scope: 'local',
        engine_id: localResource.id,
        local_resource_name: localResource.name,
        engine_type: localResource.engine_type,
        connection_info: localResource.connection_info
      }
    } else {
      ElMessage.warning(t('transfer.taskWizard.selectSourceRequired'))
      submitting.value = false
      return
    }

    if (selectedTargetOption.value?.origin === 'system') {
      config.target.scope = 'system'
      const engineId = selectedTargetOption.value?.resource?.id
      if (!engineId) {
        ElMessage.warning(t('transfer.taskWizard.selectTargetRequired'))
        submitting.value = false
        return
      }
      config.target.engine_id = engineId
      delete config.target.local_resource_name
      removeConnectionFields(config.target)
    } else if (selectedTargetLocalResource.value) {
      const localResource = selectedTargetLocalResource.value
      // 先清理不需要的字段，避免冗余
      const cleanConfig = { ...config.target }
      delete cleanConfig.full_name  // full_name应该只在connection_info里
      delete cleanConfig.engine_type
      delete cleanConfig.connection_info

      config.target = {
        ...cleanConfig,
        scope: 'local',
        engine_id: localResource.id,
        local_resource_name: localResource.name,
        engine_type: localResource.engine_type,
        connection_info: localResource.connection_info
      }
    } else {
      ElMessage.warning(t('transfer.taskWizard.selectTargetRequired'))
      submitting.value = false
      return
    }

    const data = {
      ...taskForm.value,
      config,
      mappings: fieldMappings.value
    }

    let taskId
    if (isEdit.value) {
      await taskAPI.update(route.params.id, data)
      ElMessage.success(t('transfer.taskWizard.taskUpdateSuccess'))
      taskId = route.params.id
    } else {
      const result = await taskAPI.create(data)
      ElMessage.success(t('transfer.taskWizard.taskCreateSuccess'))
      taskId = result.id
      if (startImmediately.value) {
        try {
          await taskAPI.start(taskId)
          ElMessage.success(t('transfer.taskWizard.taskStarted'))
        } catch (error) {
          ElMessage.warning(t('transfer.taskWizard.taskCreateSuccessStartFailed'))
        }
      }
    }

    router.push({
      name: 'TaskDetail',
      params: { id: taskId }
    })
  } catch (error) {
    console.error('提交失败:', error)
    ElMessage.error(error.response?.data?.error || t('transfer.taskWizard.submitFailed'))
  } finally {
    submitting.value = false
  }
}

const handleBack = () => {
  router.back()
}

// 监听步骤变化,在进入字段映射步骤时自动获取字段
watch(currentStep, async (newStep, oldStep) => {
  if (newStep === 3 && oldStep !== 3) {
    // 进入字段映射步骤
    console.log('=== 检测到进入字段映射步骤,检查是否需要自动获取字段 ===')
    console.log('当前状态:', {
      mappingsCount: fieldMappings.value.length,
      sourceFieldsCount: sourceFields.value.length,
      targetFieldsCount: targetFields.value.length,
      sourceTable: sourceConfig.value.table,
      targetTable: targetConfig.value.table,
      sourceIsSystem: sourceIsSystem.value,
      targetIsSystem: targetIsSystem.value,
      targetConnectorType: targetConnectorType.value
    })

    // 如果字段映射为空,且有源和目标配置,则自动获取
    if (fieldMappings.value.length === 0 &&
        (selectedSourceOption.value || selectedSourceLocalResource.value) &&
        (selectedTargetOption.value || selectedTargetLocalResource.value)) {
      console.log('字段映射为空,触发自动获取和匹配')
      await autoFetchAndMapFields()
    } else {
      console.log(`跳过自动获取: 映射数量=${fieldMappings.value.length}, 源=${!!selectedSourceOption.value}, 目标=${!!selectedTargetOption.value}`)
    }
  }
})

onMounted(async () => {
  await Promise.all([loadSystemResources(), loadLocalResources()])
  if (isEdit.value) {
    await loadTaskForEdit()
  }
})
</script>

<style scoped>
.task-wizard {
  padding: 20px;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 20px;
  font-size: 18px;
  font-weight: 500;
}

.step-content {
  margin: 40px 0;
  min-height: 400px;
}


.step-panel {
  max-width: 800px;
  margin: 0 auto;
}

.step-section {
  margin-bottom: 32px;
}

.step-section__title {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 16px;
  color: var(--addp-text-primary);
}

.step-actions {
  display: flex;
  justify-content: center;
  gap: 20px;
  margin-top: 40px;
  padding-top: 20px;
  border-top: 1px solid #ebeef5;
}

.hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 5px;
  line-height: 1.6;
}

.hint p {
  margin: 2px 0;
}

:deep(.el-step__title) {
  font-size: 14px;
}
</style>
