<template>
  <div class="sql-tasks-page">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.queryTasks.title') }}</h2>
        <el-input
          v-model="searchKeyword"
          :placeholder="t('develop.queryTasks.searchPlaceholder')"
          style="width: 300px; margin-left: 20px"
          clearable
          @change="loadTasks"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <div class="toolbar-right">
        <el-button type="primary" @click="$router.push('/sql-editor')">
          <el-icon><Plus /></el-icon>
          {{ t('develop.queryTasks.newTask') }}
        </el-button>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-bar">
      <el-space>
        <span>{{ t('develop.queryTasks.statusLabel') }}:</span>
        <el-radio-group v-model="filterStatus" @change="loadTasks">
          <el-radio-button value="">{{ t('develop.queryTasks.filterAll') }}</el-radio-button>
          <el-radio-button value="active">{{ t('develop.queryTasks.filterActive') }}</el-radio-button>
          <el-radio-button value="inactive">{{ t('develop.queryTasks.filterInactive') }}</el-radio-button>
        </el-radio-group>

        <el-divider direction="vertical" />

        <span>{{ t('develop.queryTasks.scheduleLabel') }}:</span>
        <el-checkbox v-model="filterScheduled" @change="loadTasks">
          {{ t('develop.queryTasks.filterScheduled') }}
        </el-checkbox>
      </el-space>
    </div>

    <!-- 任务列表 -->
    <div class="content-area">
      <el-table
        v-loading="loading"
        :data="tasks"
        stripe
        style="width: 100%"
        @row-click="handleRowClick"
      >
        <el-table-column prop="name" :label="t('develop.queryTasks.colName')" min-width="180">
          <template #default="{ row }">
            <div>
              <strong>{{ row.display_name || row.name }}</strong>
              <div style="font-size: 12px; color: var(--addp-text-tertiary); margin-top: 4px">
                {{ row.name }}
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column prop="description" :label="t('develop.queryTasks.colDescription')" min-width="200" show-overflow-tooltip />

        <el-table-column :label="t('develop.queryTasks.colDataSource')" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.engine_id" size="small" type="info">
              {{ t('develop.queryTasks.resourceId', { id: row.engine_id }) }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colSchedule')" width="200">
          <template #default="{ row }">
            <ScheduleDisplay
              v-if="row.schedule && row.schedule !== ''"
              :schedule="row.schedule"
            />
            <span v-else style="color: var(--addp-text-tertiary)">{{ t('develop.queryTasks.manualTrigger') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colStatus')" width="100">
          <template #default="{ row }">
            <el-tag
              :type="row.status === 'active' ? 'success' : 'info'"
              size="small"
            >
              {{ statusMap[row.status] || row.status }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colLastExecution')" width="180">
          <template #default="{ row }">
            <div v-if="row.last_executed_at">
              <el-tag
                :type="executionStatusColor(row.last_execution_status)"
                size="small"
              >
                {{ row.last_execution_status }}
              </el-tag>
              <div style="font-size: 12px; color: var(--addp-text-tertiary); margin-top: 4px">
                {{ formatTime(row.last_executed_at) }}
              </div>
            </div>
            <span v-else style="color: var(--addp-text-tertiary)">{{ t('develop.queryTasks.notExecuted') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colTags')" width="150">
          <template #default="{ row }">
            <el-tag
              v-for="tag in (row.tags || []).slice(0, 2)"
              :key="tag"
              size="small"
              style="margin-right: 4px"
            >
              {{ tag }}
            </el-tag>
            <el-tag v-if="row.tags && row.tags.length > 2" size="small">
              +{{ row.tags.length - 2 }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colCreatedAt')" width="160">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>

        <el-table-column :label="t('develop.queryTasks.colActions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button-group>
              <el-button
                size="small"
                type="primary"
                @click.stop="handleExecute(row)"
              >
                <el-icon><VideoPlay /></el-icon>
                {{ t('develop.queryTasks.execute') }}
              </el-button>
              <el-button
                size="small"
                @click.stop="handleEdit(row)"
              >
                <el-icon><Edit /></el-icon>
                {{ t('develop.queryTasks.edit') }}
              </el-button>
              <el-button
                size="small"
                type="danger"
                @click.stop="handleDelete(row)"
              >
                <el-icon><Delete /></el-icon>
              </el-button>
            </el-button-group>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="loadTasks"
          @current-change="loadTasks"
        />
      </div>
    </div>

    <!-- 编辑任务对话框 -->
    <el-dialog
      v-model="showEditDialog"
      :title="t('develop.queryTasks.editDialogTitle')"
      width="600px"
      :close-on-click-modal="false"
    >
      <SaveQueryDialog
        v-if="showEditDialog"
        v-model="showEditDialog"
        :engine-id="editingTask?.engine_id"
        :sql="editingTask?.content?.query || editingTask?.content?.sql || ''"
        @saved="handleUpdateTask"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  Plus,
  VideoPlay,
  Edit,
  Delete
} from '@element-plus/icons-vue'
import { ScheduleDisplay } from '@addp/common-frontend'
import SaveQueryDialog from '../components/SaveQueryDialog.vue'
import { listQueryTasks, deleteQueryTask, updateQueryTask } from '../api/query.js'
import { executeDevItem } from '../api/devItem.js'
import dayjs from 'dayjs'

const router = useRouter()
const { t } = useI18n()

// 状态
const loading = ref(false)
const tasks = ref([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const searchKeyword = ref('')
const filterStatus = ref('')
const filterScheduled = ref(false)

const showEditDialog = ref(false)
const editingTask = ref(null)

const statusMap = computed(() => ({
  active: t('develop.queryTasks.statusActive'),
  inactive: t('develop.queryTasks.statusInactive'),
  archived: t('develop.queryTasks.statusArchived')
}))

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const params = {
      page: currentPage.value,
      page_size: pageSize.value,
      keyword: searchKeyword.value,
      status: filterStatus.value
    }

    const response = await listQueryTasks(params)
    tasks.value = response.items || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载任务列表失败:', error)
    ElMessage.error(t('develop.queryTasks.loadFailed') + (error.response?.data?.error || error.message))
  } finally {
    loading.value = false
  }
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  return dayjs(time).format('YYYY-MM-DD HH:mm:ss')
}

// 执行状态颜色
const executionStatusColor = (status) => {
  const map = {
    success: 'success',
    failed: 'danger',
    running: 'warning',
    pending: 'info'
  }
  return map[status] || 'info'
}

// 行点击
const handleRowClick = (row) => {
  // 可以跳转到详情页或展开详情
  console.log('点击任务:', row)
}

// 执行任务
const handleExecute = async (task) => {
  try {
    await executeDevItem(task.id)
    ElMessage.success(t('develop.queryTasks.executeSubmitted'))
    // 可选：跳转到执行监控页面
    // router.push(`/executions/${executionId}`)
  } catch (error) {
    console.error('执行任务失败:', error)
    ElMessage.error(t('develop.queryTasks.executeFailed') + (error.response?.data?.error || error.message))
  }
}

// 编辑任务
const handleEdit = (task) => {
  editingTask.value = task
  showEditDialog.value = true
}

// 更新任务
const handleUpdateTask = async (taskData) => {
  try {
    await updateQueryTask(editingTask.value.id, taskData)
    ElMessage.success(t('develop.queryTasks.updateSuccess'))
    showEditDialog.value = false
    loadTasks()
  } catch (error) {
    console.error('更新任务失败:', error)
    ElMessage.error(t('develop.queryTasks.updateFailed') + (error.response?.data?.error || error.message))
  }
}

// 删除任务
const handleDelete = async (task) => {
  try {
    await ElMessageBox.confirm(
      t('develop.queryTasks.deleteConfirmMsg', { name: task.display_name || task.name }),
      t('develop.queryTasks.deleteConfirmTitle'),
      {
        confirmButtonText: t('develop.queryTasks.confirm'),
        cancelButtonText: t('develop.queryTasks.cancel'),
        type: 'warning'
      }
    )

    await deleteQueryTask(task.id)
    ElMessage.success(t('develop.queryTasks.deleteSuccess'))
    loadTasks()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除任务失败:', error)
      ElMessage.error(t('develop.queryTasks.deleteFailed') + (error.response?.data?.error || error.message))
    }
  }
}

// 页面加载
onMounted(() => {
  loadTasks()
})
</script>

<style scoped>
.sql-tasks-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar h2 {
  margin: 0;
  font-size: 18px;
  color: var(--addp-text-primary);
  font-weight: 500;
}

.filter-bar {
  padding: 12px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.content-area {
  flex: 1;
  padding: 20px;
  overflow: auto;
}

.pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
}

:deep(.el-table__row) {
  cursor: pointer;
}

:deep(.el-table__row:hover) {
  background-color: var(--addp-bg-secondary);
}
</style>
