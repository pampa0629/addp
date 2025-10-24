<template>
  <div class="task-wizard">
    <el-card>
      <template #header>
        <div class="card-header">
          <el-button @click="handleBack">
            <el-icon><ArrowLeft /></el-icon>
            返回
          </el-button>
          <span>{{ isEdit ? '编辑任务' : '创建数据传输任务' }}</span>
        </div>
      </template>

      <el-steps :active="currentStep" finish-status="success" align-center>
        <el-step title="基本信息" />
        <el-step title="选择源" />
        <el-step title="配置源" />
        <el-step title="选择目标" />
        <el-step title="配置目标" />
        <el-step title="字段映射" />
        <el-step title="完成" />
      </el-steps>

      <div class="step-content">
        <!-- 步骤 1: 基本信息 -->
        <div v-show="currentStep === 0" class="step-panel">
          <el-form :model="taskForm" ref="basicFormRef" label-width="120px">
            <el-form-item label="任务名称" prop="name" :rules="[{ required: true, message: '请输入任务名称' }]">
              <el-input v-model="taskForm.name" placeholder="例如：用户数据每日同步" />
            </el-form-item>

            <el-form-item label="任务描述">
              <el-input v-model="taskForm.description" type="textarea" :rows="3"
                placeholder="描述任务的用途和注意事项" />
            </el-form-item>

            <el-form-item label="任务类型" prop="type" :rules="[{ required: true, message: '请选择任务类型' }]">
              <el-radio-group v-model="taskForm.type">
                <el-radio-button label="import">数据导入</el-radio-button>
                <el-radio-button label="export">数据导出</el-radio-button>
                <el-radio-button label="sync">数据同步</el-radio-button>
              </el-radio-group>
              <div class="hint">
                <p>• 导入：从外部系统导入数据到平台</p>
                <p>• 导出：将平台数据导出到外部系统</p>
                <p>• 同步：双向或单向数据同步</p>
              </div>
            </el-form-item>

            <el-form-item label="执行模式">
              <el-radio-group v-model="taskForm.mode">
                <el-radio-button label="batch">批处理</el-radio-button>
                <el-radio-button label="stream">流式</el-radio-button>
                <el-radio-button label="micro-batch">微批</el-radio-button>
              </el-radio-group>
              <div class="hint">
                <p>• 批处理：适合定期大批量数据传输（推荐）</p>
                <p>• 流式：适合实时数据传输</p>
                <p>• 微批：小批量实时传输</p>
              </div>
            </el-form-item>

            <el-form-item label="批量大小">
              <el-input-number v-model="taskForm.batch_size" :min="100" :max="10000" :step="100" />
              <div class="hint">每批处理的记录数，推荐 1000-5000</div>
            </el-form-item>

            <el-form-item label="最大并行度">
              <el-input-number v-model="taskForm.max_parallelism" :min="1" :max="32" />
              <div class="hint">同时运行的 Worker 数量，推荐 2-8</div>
            </el-form-item>

            <el-form-item label="定时调度">
              <div style="display: flex; gap: 10px;">
                <el-input v-model="taskForm.schedule" placeholder="Cron 表达式" style="flex: 1" />
                <el-button @click="showCronBuilder = true">生成</el-button>
              </div>
              <div class="hint">
                留空则不启用定时，只能手动触发。
                示例：<el-link type="primary" @click="taskForm.schedule = '0 0 * * *'">每天零点</el-link>、
                <el-link type="primary" @click="taskForm.schedule = '0 * * * *'">每小时</el-link>、
                <el-link type="primary" @click="taskForm.schedule = '*/15 * * * *'">每15分钟</el-link>
              </div>
            </el-form-item>
          </el-form>
        </div>

        <!-- 步骤 2: 选择源数据源 -->
        <div v-show="currentStep === 1" class="step-panel">
          <el-form label-width="120px">
            <el-form-item label="数据源类型">
              <el-radio-group v-model="sourceConnectorType" @change="handleSourceTypeChange">
                <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
                <el-radio-button label="mysql">MySQL</el-radio-button>
                <el-radio-button label="s3">S3/MinIO</el-radio-button>
              </el-radio-group>
            </el-form-item>

            <el-form-item label="选择数据源">
              <el-select
                v-model="selectedSourceValue"
                placeholder="选择数据源"
                style="width: 100%"
                filterable
                :loading="loadingSystemResources || loadingLocalResources"
              >
                <el-option
                  v-for="option in sourceOptions"
                  :key="option.value"
                  :label="`${option.name} (${option.resource_type})`"
                  :value="option.value"
                >
                  <span>{{ option.name }}</span>
                  <span style="float: right; color: #8492a6; font-size: 13px">
                    {{ option.resource_type }} · {{ option.originLabel }}
                  </span>
                </el-option>
              </el-select>
              <div class="hint">
                <p>
                  从系统管理 — 存储引擎中配置（全局可用）
                  <el-link type="primary" @click="openSystemResources">去配置</el-link>
                </p>
                <p>
                  在数据传输模块配置（只有数据传输可用）
                  <el-link type="primary" @click="openLocalResourceDialog('source')">去配置</el-link>
                  <template v-if="selectedSourceLocalResource">
                    <el-link type="primary" @click="openLocalResourceDialog('source', selectedSourceLocalResource)">编辑当前</el-link>
                    <el-link
                      type="success"
                      :loading="syncingLocalResource"
                      @click="handleSyncLocalResource('source')"
                    >同步到 System</el-link>
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
                已选择数据源：{{ selectedSourceResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedSourceResource.connection_info.host">
                  主机：{{ selectedSourceResource.connection_info.host }}:{{ selectedSourceResource.connection_info.port }}
                </p>
                <p v-if="selectedSourceResource.connection_info.database">
                  数据库：{{ selectedSourceResource.connection_info.database }}
                </p>
                <p v-if="selectedSourceResource.connection_info.bucket">
                  存储桶：{{ selectedSourceResource.connection_info.bucket }}
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
                已选择本地存储引擎：{{ selectedSourceLocalResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.host">
                  主机：{{ selectedSourceLocalResource.connection_info.host }}:{{ selectedSourceLocalResource.connection_info.port }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.endpoint">
                  端点：{{ selectedSourceLocalResource.connection_info.endpoint }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.database">
                  数据库：{{ selectedSourceLocalResource.connection_info.database }}
                </p>
                <p v-if="selectedSourceLocalResource.connection_info && selectedSourceLocalResource.connection_info.bucket">
                  存储桶：{{ selectedSourceLocalResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>
          </el-form>
        </div>

        <!-- 步骤 3: 配置源 -->
        <div v-show="currentStep === 2" class="step-panel">
          <el-alert
            v-if="selectedSourceLocalResource"
            type="warning"
            :closable="false"
            style="margin-bottom: 12px"
          >
            本地存储引擎不会自动同步元数据，请根据实际情况手动配置查询。
          </el-alert>
          <el-alert type="info" :closable="false" style="margin-bottom: 20px">
            根据源类型配置读取参数
          </el-alert>

          <!-- PostgreSQL/MySQL 源配置 -->
          <div v-if="['postgresql', 'mysql'].includes(sourceConnectorType)">
            <el-form label-width="120px">
              <el-form-item label="查询方式">
                <el-radio-group v-model="sourceConfig.queryType">
                  <el-radio-button label="table">选择表</el-radio-button>
                  <el-radio-button label="sql">自定义 SQL</el-radio-button>
                </el-radio-group>
              </el-form-item>

              <el-form-item v-if="sourceConfig.queryType === 'table'" label="表名">
                <el-select
                  v-model="sourceConfig.table"
                  placeholder="选择表"
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
                  从元数据模块自动获取可用表列表。
                  <el-button type="primary" link size="small" @click="handleLoadSourceTables">
                    刷新列表
                  </el-button>
                </div>
              </el-form-item>

              <el-form-item v-if="sourceConfig.queryType === 'sql'" label="SQL 查询">
                <el-input v-model="sourceConfig.query" type="textarea" :rows="5"
                  placeholder="SELECT * FROM users WHERE status = 'active'" />
                <div class="hint">支持使用 ? 作为参数占位符</div>
              </el-form-item>

              <el-form-item v-if="sourceConfig.queryType === 'sql' && sourceConfig.query?.includes('?')"
                label="查询参数">
                <el-input v-model="sourceConfig.parameters" placeholder='["value1", "value2"]（JSON 数组）' />
              </el-form-item>

              <el-form-item label="增量字段">
                <el-input v-model="sourceConfig.incremental_field"
                  placeholder="用于增量同步的字段，如：updated_at" />
                <div class="hint">留空则全量读取，填写字段名则只读取变更数据</div>
              </el-form-item>

              <el-form-item v-if="sourceConfig.incremental_field" label="增量类型">
                <el-select v-model="sourceConfig.incremental_type">
                  <el-option label="时间戳" value="timestamp" />
                  <el-option label="整数 ID" value="integer" />
                </el-select>
              </el-form-item>
            </el-form>
          </div>

          <!-- CSV/JSON 文件源配置 -->
          <div v-if="['csv', 'json'].includes(sourceConnectorType)">
            <el-form label-width="120px">
              <el-form-item label="文件路径">
                <el-input v-model="sourceConfig.path"
                  placeholder="文件路径，如：imports/users.csv" />
              </el-form-item>

              <el-form-item v-if="sourceConnectorType === 'csv'" label="CSV 选项">
                <el-checkbox v-model="sourceConfig.has_header">包含表头</el-checkbox>
                <el-input v-model="sourceConfig.delimiter" placeholder="分隔符"
                  style="width: 100px; margin-left: 10px" />
                <div class="hint">分隔符默认为逗号 (,)</div>
              </el-form-item>

              <el-form-item label="编码">
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
              <el-form-item label="对象前缀">
                <el-input v-model="sourceConfig.prefix"
                  placeholder="对象前缀/目录，如：data/exports/" />
                <div class="hint">留空则读取整个存储桶</div>
              </el-form-item>

              <el-form-item label="递归读取">
                <el-switch v-model="sourceConfig.recursive" />
                <div class="hint">是否递归读取子目录</div>
              </el-form-item>

              <el-form-item label="包含模式">
                <el-input v-model="sourceConfig.include_patterns"
                  placeholder='["*.json", "*.csv"]（JSON 数组）' />
              </el-form-item>

              <el-form-item label="排除模式">
                <el-input v-model="sourceConfig.exclude_patterns"
                  placeholder='["*.tmp", "*.log"]（JSON 数组）' />
              </el-form-item>
            </el-form>
          </div>
        </div>

        <!-- 步骤 4: 选择目标数据源 -->
        <div v-show="currentStep === 3" class="step-panel">
          <el-form label-width="120px">
            <el-form-item label="目标类型">
              <el-radio-group v-model="targetConnectorType" @change="handleTargetTypeChange">
                <el-radio-button label="postgresql">PostgreSQL</el-radio-button>
                <el-radio-button label="mysql">MySQL</el-radio-button>
                <el-radio-button label="s3">对象存储（MinIO/S3）</el-radio-button>
              </el-radio-group>
              <div class="hint" style="margin-top: 10px">
                <p>• 数据库：直接写入到数据库表</p>
                <p>• 对象存储：导出为文件，可选 CSV、JSON、Parquet 等格式</p>
              </div>
            </el-form-item>

            <el-form-item label="选择数据源">
              <el-select
                v-model="selectedTargetValue"
                placeholder="选择目标数据源"
                style="width: 100%"
                filterable
                :loading="loadingSystemResources || loadingLocalResources"
              >
                <el-option
                  v-for="option in targetOptions"
                  :key="option.value"
                  :label="`${option.name} (${option.resource_type})`"
                  :value="option.value"
                >
                  <span>{{ option.name }}</span>
                  <span style="float: right; color: #8492a6; font-size: 13px">
                    {{ option.resource_type }} · {{ option.originLabel }}
                  </span>
                </el-option>
              </el-select>
              <div class="hint">
                <p>
                  从系统管理 — 存储引擎中配置（全局可用）
                  <el-link type="primary" @click="openSystemResources">去配置</el-link>
                </p>
                <p>
                  在数据传输模块配置（只有数据传输可用）
                  <el-link type="primary" @click="openLocalResourceDialog('target')">去配置</el-link>
                  <template v-if="selectedTargetLocalResource">
                    <el-link type="primary" @click="openLocalResourceDialog('target', selectedTargetLocalResource)">编辑当前</el-link>
                    <el-link
                      type="success"
                      :loading="syncingLocalResource"
                      @click="handleSyncLocalResource('target')"
                    >同步到 System</el-link>
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
                已选择目标：{{ selectedTargetResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedTargetResource.connection_info.host">
                  主机：{{ selectedTargetResource.connection_info.host }}:{{ selectedTargetResource.connection_info.port }}
                </p>
                <p v-if="selectedTargetResource.connection_info.database">
                  数据库：{{ selectedTargetResource.connection_info.database }}
                </p>
                <p v-if="selectedTargetResource.connection_info.bucket">
                  存储桶：{{ selectedTargetResource.connection_info.bucket }}
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
                已选择本地存储引擎：{{ selectedTargetLocalResource.name }}
              </template>
              <div style="margin-top: 10px; font-size: 13px;">
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.host">
                  主机：{{ selectedTargetLocalResource.connection_info.host }}:{{ selectedTargetLocalResource.connection_info.port }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.endpoint">
                  端点：{{ selectedTargetLocalResource.connection_info.endpoint }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.database">
                  数据库：{{ selectedTargetLocalResource.connection_info.database }}
                </p>
                <p v-if="selectedTargetLocalResource.connection_info && selectedTargetLocalResource.connection_info.bucket">
                  存储桶：{{ selectedTargetLocalResource.connection_info.bucket }}
                </p>
              </div>
            </el-alert>
          </el-form>
        </div>

        <!-- 步骤 5: 配置目标 -->
        <div v-show="currentStep === 4" class="step-panel">
          <el-alert type="info" :closable="false" style="margin-bottom: 20px">
            根据目标类型配置写入参数
          </el-alert>

          <!-- PostgreSQL/MySQL 目标配置 -->
          <div v-if="['postgresql', 'mysql'].includes(targetConnectorType)">
            <el-form label-width="140px">
              <el-form-item label="目标表名">
                <el-select
                  v-model="targetConfig.table"
                  placeholder="选择表"
                  filterable
                  allow-create
                  style="width: 100%"
                  :loading="loadingTargetTables"
                  @focus="handleLoadTargetTables"
                >
                  <el-option
                    v-for="table in availableTargetTables"
                    :key="table"
                    :label="table"
                    :value="table"
                  />
                </el-select>
                <div class="hint">
                  从元数据模块自动获取可用表列表，也可手动输入新表名。
                  <el-button type="primary" link size="small" @click="handleLoadTargetTables">
                    刷新列表
                  </el-button>
                </div>
              </el-form-item>

              <el-form-item label="写入模式">
                <el-radio-group v-model="targetConfig.mode">
                  <el-radio-button label="insert">插入（INSERT）</el-radio-button>
                  <el-radio-button label="upsert">更新插入（UPSERT）</el-radio-button>
                  <el-radio-button label="replace">替换（REPLACE）</el-radio-button>
                </el-radio-group>
                <div class="hint">
                  <p>• 插入：遇到冲突则报错</p>
                  <p>• 更新插入：遇到冲突则更新</p>
                  <p>• 替换：先删除再插入</p>
                </div>
              </el-form-item>

              <el-form-item v-if="targetConfig.mode !== 'insert'" label="冲突键">
                <el-input v-model="targetConfig.conflict_keys"
                  placeholder='["id"]（JSON 数组，用于判断冲突的字段）' />
              </el-form-item>

              <el-form-item label="冲突策略">
                <el-select v-model="targetConfig.conflict_strategy">
                  <el-option label="跳过" value="skip" />
                  <el-option label="更新" value="update" />
                  <el-option label="报错" value="error" />
                </el-select>
              </el-form-item>
            </el-form>
          </div>

          <!-- S3/MinIO 目标配置 -->
          <div v-if="targetConnectorType === 's3'">
            <el-form label-width="140px">
              <el-form-item label="输出格式">
                <el-radio-group v-model="targetConfig.format">
                  <el-radio-button label="csv">CSV</el-radio-button>
                  <el-radio-button label="json">JSON</el-radio-button>
                  <el-radio-button label="jsonl">JSONL</el-radio-button>
                  <el-radio-button label="parquet">Parquet</el-radio-button>
                </el-radio-group>
                <div class="hint">
                  选择文件的输出格式。CSV 适合表格数据，JSON 适合嵌套结构，Parquet 适合大数据分析。
                </div>
              </el-form-item>

              <el-form-item label="输出路径">
                <el-input v-model="targetConfig.path"
                  :placeholder="`输入文件路径，如：exports/users.${targetConfig.format || 'csv'}`" />
                <div class="hint">
                  文件将存储在对象存储的指定路径。建议使用有意义的目录结构，如：exports/数据库名/表名.格式
                </div>
              </el-form-item>

              <el-form-item v-if="targetConfig.format === 'csv'" label="CSV 选项">
                <el-checkbox v-model="targetConfig.headers">包含表头</el-checkbox>
                <el-input v-model="targetConfig.delimiter" placeholder="分隔符"
                  style="width: 100px; margin-left: 10px" />
                <div class="hint">分隔符默认为逗号 (,)，也可使用制表符 (\t) 或其他字符</div>
              </el-form-item>

              <el-form-item label="压缩方式">
                <el-select v-model="targetConfig.compression">
                  <el-option label="不压缩" value="none" />
                  <el-option label="Gzip" value="gzip" />
                  <el-option label="Zip" value="zip" />
                </el-select>
                <div class="hint">压缩可以减少存储空间，但会增加处理时间</div>
              </el-form-item>

              <el-form-item label="覆盖已有文件">
                <el-switch v-model="targetConfig.overwrite" />
                <div class="hint">如果文件已存在，是否覆盖</div>
              </el-form-item>
            </el-form>
          </div>
        </div>

        <!-- 步骤 6: 字段映射 -->
        <div v-show="currentStep === 5" class="step-panel">
          <FieldMappingEditor
            :source-fields="sourceFields"
            :target-fields="targetFields"
            v-model:mappings="fieldMappings"
            :auto-create-mode="targetConnectorType === 's3' || (!targetIsSystem && selectedTargetLocalResource && ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.resource_type || '').toLowerCase()))"
            @fetch-fields="handleFetchFields"
          />
        </div>

        <!-- 步骤 7: 确认 -->
        <div v-show="currentStep === 6" class="step-panel">
          <el-result icon="success" title="任务配置完成" sub-title="请检查以下配置信息">
            <template #extra>
              <el-descriptions :column="2" border>
                <el-descriptions-item label="任务名称">{{ taskForm.name }}</el-descriptions-item>
                <el-descriptions-item label="任务类型">
                  {{ { import: '数据导入', export: '数据导出', sync: '数据同步' }[taskForm.type] }}
                </el-descriptions-item>
                <el-descriptions-item label="执行模式">{{ taskForm.mode }}</el-descriptions-item>
                <el-descriptions-item label="批量大小">{{ taskForm.batch_size }}</el-descriptions-item>
                <el-descriptions-item label="定时调度" :span="2">
                  {{ taskForm.schedule || '无（手动触发）' }}
                </el-descriptions-item>
                <el-descriptions-item label="源数据源">
                  {{ sourceResourceDisplayName }}
                </el-descriptions-item>
                <el-descriptions-item label="目标数据源">
                  {{ targetResourceDisplayName }}
                </el-descriptions-item>
                <el-descriptions-item label="字段映射" :span="2">
                  {{ fieldMappings.length }} 个字段
                </el-descriptions-item>
              </el-descriptions>

              <div style="margin-top: 20px;">
                <el-checkbox v-model="startImmediately">创建后立即执行</el-checkbox>
              </div>
            </template>
          </el-result>
        </div>
      </div>

      <div class="step-actions">
        <el-button v-if="currentStep > 0" @click="prevStep">上一步</el-button>
        <el-button v-if="currentStep < 6" type="primary" @click="nextStep">下一步</el-button>
        <el-button v-if="currentStep === 6" type="success" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? '更新任务' : '创建任务' }}
        </el-button>
      </div>
    </el-card>

    <el-dialog
      v-model="localResourceDialogVisible"
      :title="localResourceDialogMode === 'edit' ? '编辑本地存储引擎' : '新建本地存储引擎'"
      width="600px"
    >
      <StorageEngineForm
        ref="localResourceFormRef"
        v-model="localResourceForm"
        :is-edit="localResourceDialogMode === 'edit'"
      />
      <template #footer>
        <el-button @click="localResourceDialogVisible = false">取消</el-button>
        <el-button type="warning" :loading="testingLocalResource" @click="handleTestLocalResource">测试连接</el-button>
        <el-button type="primary" :loading="savingLocalResource" @click="handleSaveLocalResource">
          {{ localResourceDialogMode === 'edit' ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- Cron 表达式生成器对话框 -->
    <CronBuilderDialog v-model="showCronBuilder" @select="taskForm.schedule = $event" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { taskAPI } from '@/api/tasks'
import { localResourcesAPI } from '@/api/localResources'
import { systemResourcesAPI } from '@/api/systemResources'
import FieldMappingEditor from '@/components/FieldMappingEditor.vue'
import CronBuilderDialog from '@/components/CronBuilderDialog.vue'
import { StorageEngineForm } from '@common-ui'
import axios from 'axios'

const router = useRouter()
const route = useRoute()
const isEdit = computed(() => !!route.params.id)

const currentStep = ref(0)
const submitting = ref(false)
const startImmediately = ref(false)
const showCronBuilder = ref(false)

const basicFormRef = ref(null)
const taskForm = ref({
  name: '',
  description: '',
  type: 'sync',
  mode: 'batch',
  batch_size: 1000,
  max_parallelism: 4,
  schedule: '',
  source_id: null,
  target_id: null
})

const sourceConnectorType = ref('postgresql')
const targetConnectorType = ref('postgresql')
const sourceConfig = ref({})
const targetConfig = ref({})

const systemResources = ref([])
const localResources = ref([])
const loadingSystemResources = ref(false)
const loadingLocalResources = ref(false)

const selectedSourceValue = ref(null)
const selectedTargetValue = ref(null)

const localResourceDialogVisible = ref(false)
const localResourceDialogMode = ref('create')
const localResourceDialogScope = ref('source')
const localResourceFormRef = ref(null)
const localResourceForm = ref({
  resource_type: 'postgresql',
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

const availableSourceTables = ref([])
const availableTargetTables = ref([])
const loadingSourceTables = ref(false)
const loadingTargetTables = ref(false)

const matchesConnectorType = (resourceType, connectorType) => {
  const resource = (resourceType || '').toLowerCase()
  const type = (connectorType || '').toLowerCase()
  if (!type) return true
  if (type === 's3') {
    return ['s3', 'minio', 'oss'].includes(resource)
  }
  return resource.includes(type)
}

const filteredSourceSystemResources = computed(() =>
  systemResources.value.filter(r => matchesConnectorType(r.resource_type, sourceConnectorType.value))
)
const filteredSourceLocalResources = computed(() =>
  localResources.value.filter(r => matchesConnectorType(r.resource_type, sourceConnectorType.value))
)
const filteredTargetSystemResources = computed(() =>
  systemResources.value.filter(r => matchesConnectorType(r.resource_type, targetConnectorType.value))
)
const filteredTargetLocalResources = computed(() =>
  localResources.value.filter(r => matchesConnectorType(r.resource_type, targetConnectorType.value))
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
      resource_type: res.resource_type
    })
  })
  localList.forEach(res => {
    options.push({
      value: `local:${res.id}`,
      origin: 'local',
      originLabel: '数据传输',
      resource: res,
      name: res.name,
      resource_type: res.resource_type
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
    resource_type: 'postgresql',
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
  loadingSystemResources.value = true
  try {
    const data = await systemResourcesAPI.list()
    systemResources.value = data || []
  } catch (error) {
    console.error('加载 System 资源失败:', error)
    if (!error.response || error.response.status === 503) {
      ElMessage.closeAll()
      ElMessage.warning(
        error.response?.status === 503
          ? 'System 模块暂不可用，当前仅显示数据传输模块的存储引擎'
          : '无法连接到 System 模块，请检查网络或服务状态'
      )
    }
  } finally {
    loadingSystemResources.value = false
  }
}

const loadLocalResources = async () => {
  loadingLocalResources.value = true
  try {
    const data = await localResourcesAPI.list()
    localResources.value = data || []
  } catch (error) {
    console.error('加载本地存储引擎失败:', error)
    ElMessage.error(error.response?.data?.error || '加载本地存储引擎失败')
  } finally {
    loadingLocalResources.value = false
  }
}

const openSystemResources = () => {
  const baseUrl = (import.meta.env.VITE_SYSTEM_URL || 'http://localhost:5173').replace(/\/$/, '')
  const token = localStorage.getItem('token')
  const url = `${baseUrl}/resources${token ? `?token=${encodeURIComponent(token)}` : ''}`
  window.open(url, '_blank', 'noopener,noreferrer')
}

const removeConnectionFields = (config) => {
  const keys = [
    'type',
    'driver',
    'host',
    'port',
    'user',
    'password',
    'database',
    'sslmode',
    'endpoint',
    'access_key',
    'secret_key',
    'bucket',
    'use_ssl',
    'resource_type',
    'connection_info',
    'local_resource_id',
    'local_resource_name',
    'system_resource_id'
  ]
  keys.forEach((key) => {
    if (key in config) {
      delete config[key]
    }
  })
}

watch(selectedSourceOption, (option) => {
  availableSourceTables.value = []
  if (option?.origin === 'system') {
    taskForm.value.source_id = option.resource.id
    removeConnectionFields(sourceConfig.value)
  } else {
    taskForm.value.source_id = null
  }
})

watch(selectedTargetOption, (option) => {
  availableTargetTables.value = []
  if (option?.origin === 'system') {
    taskForm.value.target_id = option.resource.id
    removeConnectionFields(targetConfig.value)
  } else {
    taskForm.value.target_id = null
  }

  const type = (option?.resource?.resource_type || '').toLowerCase()
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
})

watch(sourceConnectorType, (newType) => {
  if (selectedSourceOption.value && !matchesConnectorType(selectedSourceOption.value.resource.resource_type, newType)) {
    selectedSourceValue.value = null
  }
  sourceConfig.value = {}
  availableSourceTables.value = []
  sourceFields.value = []
  fieldMappings.value = []
})

watch(targetConnectorType, (newType) => {
  if (selectedTargetOption.value && !matchesConnectorType(selectedTargetOption.value.resource.resource_type, newType)) {
    selectedTargetValue.value = null
  }
  targetConfig.value = {}
  availableTargetTables.value = []
  targetFields.value = []
  fieldMappings.value = []

  if (newType === 's3') {
    targetConfig.value = {
      format: 'csv',
      headers: true,
      delimiter: ',',
      compression: 'none',
      overwrite: false
    }
  }
})

const handleSourceTypeChange = () => {
  taskForm.value.source_id = null
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
  taskForm.value.target_id = null
  selectedTargetValue.value = null
  targetConfig.value = {}

  if (targetConnectorType.value === 's3') {
    targetConfig.value = {
      format: 'csv',
      headers: true,
      delimiter: ',',
      compression: 'none',
      overwrite: false
    }
  }
}

const openLocalResourceDialog = (scope, resource = null) => {
  localResourceDialogScope.value = scope
  if (resource) {
    localResourceDialogMode.value = 'edit'
    editingLocalResourceId.value = resource.id
    localResourceForm.value = {
      resource_type: resource.resource_type,
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
    ElMessage.warning('请完善存储引擎信息后再测试连接')
    return
  }

  testingLocalResource.value = true
  try {
    const result = await localResourcesAPI.testConnection(localResourceForm.value)
    if (result.success) {
      ElMessage.success('连接测试成功')
    } else {
      ElMessage.error(result.error || '连接测试失败')
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || '连接测试失败')
  } finally {
    testingLocalResource.value = false
  }
}

const handleSaveLocalResource = async () => {
  const valid = await localResourceFormRef.value?.validate()
  if (!valid) {
    ElMessage.warning('请先完善存储引擎配置')
    return
  }

  savingLocalResource.value = true
  try {
    let saved
    if (editingLocalResourceId.value) {
      saved = await localResourcesAPI.update(editingLocalResourceId.value, {
        name: localResourceForm.value.name,
        description: localResourceForm.value.description,
        is_active: localResourceForm.value.is_active,
        connection_info: localResourceForm.value.connection_info
      })
      ElMessage.success('存储引擎已更新')
    } else {
      saved = await localResourcesAPI.create(localResourceForm.value)
      ElMessage.success('存储引擎创建成功')
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
    ElMessage.error(error.response?.data?.error || '保存存储引擎失败')
  } finally {
    savingLocalResource.value = false
  }
}

const handleSyncLocalResource = async (scope) => {
  const resource = scope === 'source' ? selectedSourceLocalResource.value : selectedTargetLocalResource.value
  if (!resource) {
    ElMessage.warning('请选择需要同步的存储引擎')
    return
  }

  try {
    await ElMessageBox.confirm(
      `确定将存储引擎「${resource.name}」同步到 System 模块吗？`,
      '同步到 System',
      { type: 'info' }
    )
  } catch {
    return
  }

  syncingLocalResource.value = true
  try {
    await localResourcesAPI.syncToSystem(resource.id)
    ElMessage.success('已同步到 System 模块')
    await loadSystemResources()
  } catch (error) {
    console.error('同步到 System 失败:', error)
    ElMessage.error(error.response?.data?.error || '同步失败')
  } finally {
    syncingLocalResource.value = false
  }
}

const handleLoadSourceTables = async () => {
  if (!sourceIsSystem.value) {
    ElMessage.info('本地存储引擎暂不支持从元数据模块加载表，请手动配置')
    return
  }

  if (!taskForm.value.source_id) {
    ElMessage.warning('请先选择源数据源')
    return
  }

  if (!['postgresql', 'mysql'].includes(sourceConnectorType.value)) {
    return
  }

  loadingSourceTables.value = true
  try {
    const token = localStorage.getItem('token')
    const response = await axios.get(`http://localhost:8082/api/meta/metadata/tables`, {
      params: { resource_id: taskForm.value.source_id },
      headers: { Authorization: `Bearer ${token}` }
    })
    if (response.data && Array.isArray(response.data)) {
      availableSourceTables.value = response.data.map(item => item.name || item)
      ElMessage.success(`已加载 ${availableSourceTables.value.length} 个表`)
    } else {
      ElMessage.warning('未找到可用的表，请确认元数据模块已扫描该数据源')
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    if (error.response?.status === 404 || error.response?.data?.error?.includes('未找到')) {
      ElMessage.warning('该数据源尚未扫描元数据，请先到元数据模块进行扫描')
    } else {
      ElMessage.error('加载表列表失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    loadingSourceTables.value = false
  }
}

const handleLoadTargetTables = async () => {
  if (!targetIsSystem.value) {
    ElMessage.info('本地存储引擎暂不支持从元数据模块加载表，请手动配置')
    return
  }

  if (!taskForm.value.target_id) {
    ElMessage.warning('请先选择数据源')
    return
  }

  if (!['postgresql', 'mysql'].includes(targetConnectorType.value)) {
    return
  }

  loadingTargetTables.value = true
  try {
    const token = localStorage.getItem('token')
    const response = await axios.get(`http://localhost:8082/api/meta/metadata/tables`, {
      params: { resource_id: taskForm.value.target_id },
      headers: { Authorization: `Bearer ${token}` }
    })

    if (response.data && Array.isArray(response.data)) {
      availableTargetTables.value = response.data.map(item => item.name || item)
      ElMessage.success(`已加载 ${availableTargetTables.value.length} 个表`)
    } else {
      ElMessage.warning('未找到可用的表，请确认元数据模块已扫描该数据源')
    }
  } catch (error) {
    console.error('加载表列表失败:', error)
    if (error.response?.status === 404 || error.response?.data?.error?.includes('未找到')) {
      ElMessage.warning('该数据源尚未扫描元数据，请先到元数据模块进行扫描')
    } else {
      ElMessage.error('加载表列表失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    loadingTargetTables.value = false
  }
}

const handleFetchFields = async (type) => {
  const isSource = type === 'source'
  const useSystem = isSource ? sourceIsSystem.value : targetIsSystem.value
  if (!useSystem) {
    ElMessage.info('本地存储引擎暂不支持自动获取字段，请手动维护映射')
    return
  }

  const resourceId = isSource ? taskForm.value.source_id : taskForm.value.target_id
  const tableName = isSource ? sourceConfig.value.table : targetConfig.value.table

  if (!resourceId) {
    ElMessage.warning(`请先选择${isSource ? '源' : '目标'}数据源`)
    return
  }

  if (!tableName) {
    ElMessage.warning(`请先选择${isSource ? '源' : '目标'}表`)
    return
  }

  try {
    const token = localStorage.getItem('token')
    const response = await axios.get(`http://localhost:8082/api/meta/metadata/fields`, {
      params: {
        resource_id: resourceId,
        table_name: tableName
      },
      headers: { Authorization: `Bearer ${token}` }
    })

    if (response.data && Array.isArray(response.data)) {
      if (isSource) {
        sourceFields.value = response.data
        ElMessage.success(`已加载 ${response.data.length} 个源字段`)
      } else {
        targetFields.value = response.data
        ElMessage.success(`已加载 ${response.data.length} 个目标字段`)
      }
    } else {
      ElMessage.warning('未获取到字段信息')
    }
  } catch (error) {
    console.error('获取字段列表失败:', error)
    ElMessage.error('获取字段列表失败: ' + (error.response?.data?.error || error.message))
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
      ElMessage.warning('请完善基本信息')
      return
    }
  } else if (currentStep.value === 1) {
    if (!selectedSourceOption.value) {
      ElMessage.warning('请选择源数据源')
      return
    }
  } else if (currentStep.value === 3) {
    if (!selectedTargetOption.value) {
      ElMessage.warning('请选择目标数据源')
      return
    }
  }

  if (currentStep.value < 6) {
    currentStep.value++
    if (currentStep.value === 5) {
      await autoFetchAndMapFields()
    }
  }
}

const autoFetchAndMapFields = async () => {
  try {
    // 首先获取源字段
    if (sourceIsSystem.value && sourceFields.value.length === 0) {
      await handleFetchFields('source')
    }

    // 等待源字段加载完成
    await nextTick()

    // 对于数据库目标，获取目标字段
    if (targetIsSystem.value && ['postgresql', 'mysql'].includes(targetConnectorType.value)) {
      if (targetFields.value.length === 0 && targetConfig.value.table) {
        await handleFetchFields('target')
      }
    }
    // 对于对象存储目标（本地或系统），使用源字段作为目标字段
    else if (targetConnectorType.value === 's3' || (!targetIsSystem.value && selectedTargetLocalResource.value && ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.value.resource_type || '').toLowerCase()))) {
      targetFields.value = [...sourceFields.value]
    }

    // 等待所有字段加载完成
    await nextTick()

    // 执行自动匹配
    if (sourceFields.value.length > 0 && targetFields.value.length > 0) {
      performAutoMatch()
    } else if (sourceFields.value.length === 0) {
      ElMessage.warning('源字段列表为空，无法自动映射')
    } else if (targetFields.value.length === 0) {
      ElMessage.warning('目标字段列表为空，无法自动映射')
    }
  } catch (error) {
    console.error('自动字段映射失败:', error)
    ElMessage.error('自动字段映射失败: ' + error.message)
  }
}

const performAutoMatch = () => {
  const newMappings = []

  // 第一步：完全匹配（字段名完全相同）
  sourceFields.value.forEach(sourceField => {
    if (targetFields.value.includes(sourceField)) {
      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        field_type: 'string',
        transform: '',
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
          field_type: 'string',
          transform: '',
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
      ['s3', 'minio', 'oss'].includes((selectedTargetLocalResource.value?.resource_type || '').toLowerCase()))

  if (targetIsObjectStorage) {
    sourceFields.value.forEach(sourceField => {
      if (!newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: sourceField,
          field_type: 'string',
          transform: '',
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  }

  // 更新映射并提示用户
  if (newMappings.length > 0) {
    fieldMappings.value = newMappings
    ElMessage.success(`已自动匹配 ${newMappings.length} 个字段`)
  } else {
    ElMessage.warning('未找到可匹配的字段，请手动添加映射')
  }
}

const buildConnectorConfigFromResource = (resource) => {
  if (!resource) return {}
  const conn = resource.connection_info || {}
  const type = (resource.resource_type || '').toLowerCase()

  if (['postgresql', 'mysql'].includes(type)) {
    const portValue = conn.port
    const port = typeof portValue === 'number' ? portValue : Number(portValue) || 0
    return {
      type: 'jdbc',
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
      type: 's3',
      endpoint: conn.endpoint || '',
      access_key: conn.access_key || '',
      secret_key: conn.secret_key || '',
      bucket: conn.bucket || '',
      use_ssl: !!conn.use_ssl
    }
  }

  return {
    resource_type: resource.resource_type,
    connection_info: conn
  }
}

const handleSubmit = async () => {
  submitting.value = true
  try {
    const config = {
      source: { ...sourceConfig.value },
      target: { ...targetConfig.value }
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
      if (taskForm.value.source_id) {
        config.source.system_resource_id = taskForm.value.source_id
      }
      delete config.source.local_resource_id
      delete config.source.local_resource_name
      removeConnectionFields(config.source)
    } else if (selectedSourceLocalResource.value) {
      const localResource = selectedSourceLocalResource.value
      taskForm.value.source_id = null
      config.source = {
        ...buildConnectorConfigFromResource(localResource),
        ...config.source,
        scope: 'local',
        local_resource_id: localResource.id,
        local_resource_name: localResource.name
      }
    } else {
      ElMessage.warning('请选择源数据源')
      submitting.value = false
      return
    }

    if (selectedTargetOption.value?.origin === 'system') {
      config.target.scope = 'system'
      if (taskForm.value.target_id) {
        config.target.system_resource_id = taskForm.value.target_id
      }
      delete config.target.local_resource_id
      delete config.target.local_resource_name
      removeConnectionFields(config.target)
    } else if (selectedTargetLocalResource.value) {
      const localResource = selectedTargetLocalResource.value
      taskForm.value.target_id = null
      config.target = {
        ...buildConnectorConfigFromResource(localResource),
        ...config.target,
        scope: 'local',
        local_resource_id: localResource.id,
        local_resource_name: localResource.name
      }
    } else {
      ElMessage.warning('请选择目标数据源')
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
      ElMessage.success('任务更新成功')
      taskId = route.params.id
    } else {
      const result = await taskAPI.create(data)
      ElMessage.success('任务创建成功')
      taskId = result.id
      if (startImmediately.value) {
        try {
          await taskAPI.start(taskId)
          ElMessage.success('任务已开始执行')
        } catch (error) {
          ElMessage.warning('任务创建成功，但启动失败')
        }
      }
    }

    router.push(`/tasks/${taskId}`)
  } catch (error) {
    console.error('提交失败:', error)
    ElMessage.error(error.response?.data?.error || '提交失败')
  } finally {
    submitting.value = false
  }
}

const handleBack = () => {
  router.back()
}

onMounted(() => {
  loadSystemResources()
  loadLocalResources()
  // TODO: 如果是编辑模式，加载任务数据
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
  color: #909399;
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
