<template>
  <div class="logical-table-list">
    <!-- 搜索区 -->
    <el-card shadow="never" class="search-card">
      <el-row :gutter="12" align="middle">
        <el-col :span="5">
          <el-input v-model="searchForm.keyword" placeholder="搜索表名/编码" clearable @change="handleSearch">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.domain_id" placeholder="业务域" clearable @change="handleSearch" style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.layer" placeholder="数仓分层" clearable @change="handleSearch" style="width:100%">
            <el-option label="ODS 贴源层" value="ods" />
            <el-option label="DWD 明细层" value="dwd" />
            <el-option label="DWS 汇总层" value="dws" />
            <el-option label="ADS 应用层" value="ads" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-select v-model="searchForm.status" placeholder="状态" clearable @change="handleSearch" style="width:100%">
            <el-option label="草稿" value="draft" />
            <el-option label="已审批" value="approved" />
            <el-option label="已物化" value="materialized" />
          </el-select>
        </el-col>
        <el-col :span="4">
          <el-button type="primary" @click="openCreateDialog">
            <el-icon><Plus /></el-icon>
            新建逻辑表
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 逻辑表列表 -->
    <el-card shadow="never" style="margin-top:12px">
      <el-table :data="tables" v-loading="loading" stripe>
        <el-table-column label="逻辑表名" prop="name" min-width="160">
          <template #default="{ row }">
            <el-link type="primary" @click="goToDetail(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="物理表名" prop="code" width="200" />
        <el-table-column label="数仓分层" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.layer" :type="layerTagType(row.layer)" size="small">
              {{ row.layer.toUpperCase() }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="表类型" width="120">
          <template #default="{ row }">
            <el-tag type="info" size="small">{{ row.table_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="160">
          <template #default="{ row }">
            {{ new Date(row.created_at).toLocaleString('zh-CN') }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToDetail(row)">设计</el-button>
            <el-popconfirm title="确定删除该逻辑表吗？" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.pageSize"
          :total="pagination.total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @change="loadTables"
        />
      </div>
    </el-card>

    <!-- 新建对话框 -->
    <el-dialog v-model="createDialogVisible" title="新建逻辑表" width="540px">
      <el-form ref="createFormRef" :model="createForm" :rules="createRules" label-width="100px">
        <el-form-item label="逻辑表名" prop="name">
          <el-input v-model="createForm.name" placeholder="如：客户订单明细表" />
        </el-form-item>
        <el-form-item label="物理表名" prop="code">
          <el-input v-model="createForm.code" placeholder="如：dwd_order_detail" />
        </el-form-item>
        <el-form-item label="业务域">
          <el-select v-model="createForm.domain_id" placeholder="请选择" clearable style="width:100%">
            <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="表类型" prop="table_type">
          <el-select v-model="createForm.table_type" style="width:100%">
            <el-option label="实体表 (entity)" value="entity" />
            <el-option label="事实表 (fact)" value="fact" />
            <el-option label="维度表 (dimension)" value="dimension" />
          </el-select>
        </el-form-item>
        <el-form-item label="数仓分层">
          <el-select v-model="createForm.layer" placeholder="请选择" clearable style="width:100%">
            <el-option label="ODS" value="ods" />
            <el-option label="DWD" value="dwd" />
            <el-option label="DWS" value="dws" />
            <el-option label="ADS" value="ads" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleCreate" :loading="creating">创建并设计</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { logicalTableAPI, domainAPI } from '../api/model'

const router = useRouter()
const loading = ref(false)
const creating = ref(false)
const tables = ref([])
const domains = ref([])
const createDialogVisible = ref(false)
const createFormRef = ref(null)

const searchForm = reactive({ keyword: '', domain_id: null, layer: '', status: '' })
const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const createForm = reactive({ name: '', code: '', domain_id: null, table_type: 'entity', layer: '', description: '' })
const createRules = {
  name: [{ required: true, message: '请输入逻辑表名', trigger: 'blur' }],
  code: [{ required: true, message: '请输入物理表名', trigger: 'blur' }],
  table_type: [{ required: true, message: '请选择表类型', trigger: 'change' }]
}

const layerTagType = (layer) => ({ ods: '', dwd: 'success', dws: 'warning', ads: 'danger' }[layer] ?? 'info')
const statusTagType = (s) => ({ draft: 'info', approved: 'success', materialized: 'warning' }[s] ?? 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', materialized: '已物化' }[s] ?? s)

const loadTables = async () => {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize,
      keyword: searchForm.keyword || undefined,
      domain_id: searchForm.domain_id || undefined,
      layer: searchForm.layer || undefined,
      status: searchForm.status || undefined
    }
    const res = await logicalTableAPI.list(params)
    tables.value = res.data || []
    pagination.total = res.total || 0
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  pagination.page = 1
  loadTables()
}

const openCreateDialog = () => {
  Object.assign(createForm, { name: '', code: '', domain_id: null, table_type: 'entity', layer: '', description: '' })
  createDialogVisible.value = true
}

const handleCreate = async () => {
  await createFormRef.value.validate()
  creating.value = true
  try {
    const res = await logicalTableAPI.create(createForm)
    ElMessage.success('创建成功')
    createDialogVisible.value = false
    router.push(`/modeling/logical-tables/${res.id}`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '创建失败')
  } finally {
    creating.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await logicalTableAPI.delete(id)
    ElMessage.success('删除成功')
    loadTables()
  } catch {
    ElMessage.error('删除失败')
  }
}

const goToDetail = (row) => {
  router.push(`/modeling/logical-tables/${row.id}`)
}

onMounted(async () => {
  const res = await domainAPI.list()
  domains.value = res.data || []
  loadTables()
})
</script>

<style scoped>
.logical-table-list {
  padding: 20px;
}

.search-card {
  margin-bottom: 0;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
