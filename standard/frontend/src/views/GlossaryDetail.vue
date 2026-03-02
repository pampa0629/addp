<template>
  <div class="glossary-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">返回</el-button>
        <h2>业务术语详情</h2>
        <el-tag :type="statusType(glossary.status)" size="small" v-if="glossary.status">
          {{ statusLabel(glossary.status) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="saveChanges" :loading="saving">保存</el-button>
        <el-button type="success" @click="handleApprove" v-if="glossary.status === 'draft'" :disabled="saving">审批</el-button>
        <el-button type="warning" @click="handleDeprecate" v-if="glossary.status === 'approved'" :disabled="saving">废弃</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>基本信息</h3></template>
          <el-form :model="glossary" label-width="100px" size="default">
            <el-form-item label="术语名称">
              <el-input v-model="glossary.name" placeholder="如：客户" />
            </el-form-item>
            <el-form-item label="别名">
              <el-select
                v-model="glossary.alias"
                multiple
                filterable
                allow-create
                default-first-option
                placeholder="输入后回车添加别名"
                style="width: 100%"
              />
            </el-form-item>
            <el-form-item label="所属业务域">
              <el-select v-model="glossary.domain_id" clearable filterable style="width: 100%">
                <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="业务定义">
              <el-input v-model="glossary.definition" type="textarea" :rows="4" placeholder="请输入业务定义" />
            </el-form-item>
            <el-form-item label="使用示例">
              <el-input v-model="glossary.example" type="textarea" :rows="2" />
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="glossary.note" type="textarea" :rows="2" />
            </el-form-item>
            <el-form-item label="标签">
              <el-select
                v-model="glossary.tags"
                multiple
                filterable
                allow-create
                default-first-option
                placeholder="输入后回车添加标签"
                style="width: 100%"
              />
            </el-form-item>
          </el-form>
        </el-card>

        <!-- 关联的数据元 -->
        <el-card class="section-card">
          <template #header>
            <div class="card-header">
              <h3>关联的数据元</h3>
              <el-button size="small" @click="openAddElementDialog">添加数据元</el-button>
            </div>
          </template>
          <div v-if="mappedElements.length === 0" class="empty-tip">
            <el-empty description="暂未关联数据元" />
          </div>
          <el-table v-else :data="mappedElements" size="small">
            <el-table-column label="名称" prop="name" min-width="120" />
            <el-table-column label="编码" prop="code" width="150" />
            <el-table-column label="数据类型" prop="data_type" width="110">
              <template #default="{ row }">
                <el-tag size="small" type="info">{{ row.data_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="业务含义" prop="definition" show-overflow-tooltip />
            <el-table-column label="操作" width="80" align="center" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="removeElement(row.id)">移除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>

        <!-- 关联文档 -->
        <DocumentPanel v-if="glossary.id" entity-type="glossary" :entity-id="glossary.id" />
      </el-col>

      <el-col :span="8">
        <!-- 元数据 -->
        <el-card class="section-card">
          <template #header><h3>元数据</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item label="ID">{{ glossary.id }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatTime(glossary.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ formatTime(glossary.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>

    <!-- 添加数据元对话框 -->
    <el-dialog v-model="addElementDialogVisible" title="添加数据元" width="560px" @open="onAddDialogOpen">
      <div class="add-element-tip">搜索并选择要关联的数据元（已关联的不可重复选择）</div>
      <el-select
        v-model="selectedElementIds"
        multiple
        filterable
        remote
        :remote-method="searchElements"
        :loading="elementSearchLoading"
        placeholder="输入名称或编码搜索数据元"
        style="width: 100%; margin-top: 12px"
        size="large"
      >
        <el-option
          v-for="el in searchedElements"
          :key="el.id"
          :label="`${el.name}（${el.code}）`"
          :value="el.id"
          :disabled="isAlreadyMapped(el.id)"
        />
      </el-select>
      <template #footer>
        <el-button @click="addElementDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmAddElements" :disabled="selectedElementIds.length === 0">确认添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, glossaryAPI, elementAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const glossary = ref({})
const domains = ref([])
const mappedElements = ref([])
const addElementDialogVisible = ref(false)
const elementSearchLoading = ref(false)
const searchedElements = ref([])
const selectedElementIds = ref([])

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const goBack = () => router.push('/standard/glossaries')

const flattenDomains = (nodes) => {
  const result = []
  const traverse = (list) => {
    for (const n of list) {
      result.push(n)
      if (n.children) traverse(n.children)
    }
  }
  traverse(nodes)
  return result
}

const loadGlossary = async () => {
  loading.value = true
  try {
    const res = await glossaryAPI.get(route.params.id)
    glossary.value = res.data || {}
    if (!glossary.value.alias) glossary.value.alias = []
    if (!glossary.value.tags) glossary.value.tags = []
  } catch (e) {
    ElMessage.error('加载失败')
    goBack()
  } finally {
    loading.value = false
  }
}

const loadMappedElements = async () => {
  try {
    const res = await glossaryAPI.getElements(route.params.id)
    mappedElements.value = res.data || []
  } catch (e) {
    console.error('加载关联数据元失败:', e)
  }
}

const loadDomains = async () => {
  try {
    const res = await domainAPI.list()
    domains.value = flattenDomains(res.data || [])
  } catch (e) {
    console.error('加载业务域失败:', e)
  }
}

const saveChanges = async () => {
  saving.value = true
  try {
    const elementIds = mappedElements.value.map(e => e.id)
    await Promise.all([
      glossaryAPI.update(route.params.id, glossary.value),
      glossaryAPI.setElements(route.params.id, elementIds)
    ])
    ElMessage.success('保存成功')
    await loadGlossary()
    await loadMappedElements()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try {
    await ElMessageBox.confirm('确认审批通过？', '提示', { type: 'info' })
    await glossaryAPI.approve(route.params.id)
    ElMessage.success('审批成功')
    await loadGlossary()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('审批失败')
  }
}

const handleDeprecate = async () => {
  try {
    await ElMessageBox.confirm('确认废弃该术语？', '提示', { type: 'warning' })
    await glossaryAPI.deprecate(route.params.id)
    ElMessage.success('已废弃')
    await loadGlossary()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
  }
}

const isAlreadyMapped = (elementId) => {
  return mappedElements.value.some(e => e.id === elementId)
}

const searchElements = async (keyword) => {
  elementSearchLoading.value = true
  try {
    const res = await elementAPI.list({ keyword, page_size: 50 })
    searchedElements.value = res.data || []
  } catch (e) {
    searchedElements.value = []
  } finally {
    elementSearchLoading.value = false
  }
}

const onAddDialogOpen = () => {
  selectedElementIds.value = []
  searchedElements.value = []
  // 初始加载一批数据元供选择
  searchElements('')
}

const confirmAddElements = () => {
  const toAdd = searchedElements.value.filter(
    el => selectedElementIds.value.includes(el.id) && !isAlreadyMapped(el.id)
  )
  mappedElements.value = [...mappedElements.value, ...toAdd]
  addElementDialogVisible.value = false
  selectedElementIds.value = []
}

const openAddElementDialog = () => {
  addElementDialogVisible.value = true
}

const removeElement = (elementId) => {
  mappedElements.value = mappedElements.value.filter(e => e.id !== elementId)
}

onMounted(async () => {
  await Promise.all([loadGlossary(), loadDomains(), loadMappedElements()])
})
</script>

<style scoped>
.glossary-detail {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-left h2 {
  margin: 0;
  font-size: 18px;
  color: var(--el-text-color-primary);
}

.section-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  font-size: 15px;
  color: var(--el-text-color-primary);
}

.empty-tip {
  padding: 20px 0;
}

.add-element-tip {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
