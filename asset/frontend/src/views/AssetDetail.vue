<template>
  <div class="asset-detail">
    <!-- 顶部工具栏 -->
    <div class="page-header">
      <div class="breadcrumb">
        <el-button link @click="$router.push('/asset/assets')">
          <el-icon><ArrowLeft /></el-icon> 返回列表
        </el-button>
        <span class="separator">/</span>
        <span>{{ isCreate ? '新建资产' : (editMode ? '编辑资产' : '资产详情') }}</span>
      </div>
      <div class="header-actions" v-if="!isCreate && !editMode && asset">
        <!-- 详情模式下的操作按钮 -->
        <el-button v-if="canEdit" @click="editMode = true">编辑</el-button>
        <el-button v-if="asset.status === 'draft' || asset.status === 'rejected'" type="success" @click="handleSubmit">
          提交上架
        </el-button>
        <el-button v-if="asset.status === 'pending'" type="success" @click="handleApprove">审批通过</el-button>
        <el-button v-if="asset.status === 'pending'" type="danger" plain @click="openReject">驳回</el-button>
        <el-button v-if="asset.status === 'published'" type="warning" plain @click="handleOffline">下架</el-button>
        <el-button v-if="asset.status === 'offline'" type="primary" @click="handleRepublish">重新上架</el-button>
        <el-popconfirm v-if="asset.status === 'draft'" title="确定删除该资产？" @confirm="handleDelete">
          <template #reference>
            <el-button type="danger" plain>删除</el-button>
          </template>
        </el-popconfirm>
      </div>
    </div>

    <!-- 状态横幅（仅详情模式） -->
    <el-alert
      v-if="!isCreate && !editMode && asset"
      :type="statusAlertType(asset.status)"
      :closable="false"
      style="margin-bottom: 20px"
    >
      <template #title>
        <span>状态：{{ statusLabel(asset.status) }}</span>
        <span v-if="asset.status === 'published' && asset.published_at" style="margin-left: 12px; font-size: 13px">
          上架时间：{{ formatDate(asset.published_at) }}
        </span>
      </template>
    </el-alert>

    <!-- 被驳回时显示原因 -->
    <el-alert
      v-if="rejectNote && !isCreate && !editMode"
      type="error"
      :title="`驳回原因：${rejectNote}`"
      :closable="false"
      style="margin-bottom: 20px"
    />

    <div v-loading="loading">
      <!-- 详情展示模式 -->
      <template v-if="!isCreate && !editMode && asset">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="资产名称">{{ asset.name }}</el-descriptions-item>
          <el-descriptions-item label="资产类型">
            <el-tag size="small">{{ asset.type_name || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="主题分类">{{ asset.category?.name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="来源模块">{{ asset.source_module || '-' }}</el-descriptions-item>
          <el-descriptions-item label="来源引用" :span="2">{{ asset.source_reference || '-' }}</el-descriptions-item>
          <el-descriptions-item label="标签" :span="2">
            <el-tag v-for="tag in (asset.tags || [])" :key="tag" size="small" style="margin-right: 4px">{{ tag }}</el-tag>
            <span v-if="!asset.tags?.length">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="说明" :span="2">{{ asset.description || '-' }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ formatDate(asset.created_at) }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ formatDate(asset.updated_at) }}</el-descriptions-item>
        </el-descriptions>

        <!-- 扩展字段展示 -->
        <template v-if="asset.ext_fields?.length">
          <h3 class="section-title">扩展字段</h3>
          <el-descriptions :column="2" border>
            <el-descriptions-item
              v-for="ef in asset.ext_fields.filter(f => !f.field_key.startsWith('_'))"
              :key="ef.field_key"
              :label="ef.field_key"
            >
              {{ formatExtValue(ef.value) }}
            </el-descriptions-item>
          </el-descriptions>
        </template>
      </template>

      <!-- 创建/编辑表单模式 -->
      <el-form
        v-if="isCreate || editMode"
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="100px"
        class="asset-form"
      >
        <!-- 基础信息 -->
        <h3 class="section-title">基础信息</h3>

        <el-form-item label="资产名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入资产中文名称" maxlength="500" />
        </el-form-item>

        <el-form-item label="资产类型" prop="type_id">
          <el-select
            v-model="form.type_id"
            placeholder="请选择资产类型"
            style="width: 100%"
            :disabled="!isCreate"
            @change="onTypeChange"
          >
            <el-option v-for="t in types" :key="t.id" :label="t.name" :value="t.id">
              <span>{{ t.name }}</span>
              <span style="float: right; color: #999; font-size: 12px">{{ t.description }}</span>
            </el-option>
          </el-select>
          <div v-if="!isCreate" class="form-hint">资产类型创建后不可更改</div>
        </el-form-item>

        <el-form-item label="主题分类">
          <el-cascader
            v-model="form.category_id_path"
            :options="categoryOptions"
            :props="{ checkStrictly: true, value: 'id', label: 'name', children: 'children', emitPath: false }"
            placeholder="选择主题分类（选填）"
            clearable
            style="width: 100%"
            @change="onCategoryChange"
          />
        </el-form-item>

        <el-form-item label="说明">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="资产业务描述" maxlength="2000" />
        </el-form-item>

        <el-form-item label="标签">
          <div class="tag-input">
            <el-tag
              v-for="tag in form.tags"
              :key="tag"
              closable
              @close="removeTag(tag)"
              style="margin-right: 6px; margin-bottom: 4px"
            >
              {{ tag }}
            </el-tag>
            <el-input
              v-if="tagInputVisible"
              ref="tagInputRef"
              v-model="tagInputValue"
              size="small"
              style="width: 120px"
              @keyup.enter="addTag"
              @blur="addTag"
            />
            <el-button v-else size="small" @click="showTagInput">+ 添加标签</el-button>
          </div>
        </el-form-item>

        <!-- 来源关联 -->
        <h3 class="section-title">来源关联</h3>

        <el-form-item label="来源模块">
          <el-select v-model="form.source_module" placeholder="选择来源模块（选填）" clearable style="width: 200px">
            <el-option label="Meta 模块" value="meta" />
            <el-option label="Service 模块" value="service" />
            <el-option label="Standard 模块" value="standard" />
            <el-option label="Develop 模块" value="develop" />
            <el-option label="手动录入" value="manual" />
          </el-select>
        </el-form-item>

        <el-form-item label="来源引用">
          <el-input
            v-model="form.source_reference"
            placeholder="来源数据的唯一标识或路径（选填）"
            maxlength="500"
          />
          <div class="form-hint">如：数据库表路径、服务ID、指标编码等</div>
        </el-form-item>

        <!-- 扩展字段（按类型动态渲染）-->
        <template v-if="typeFieldSchemas.length > 0">
          <h3 class="section-title">扩展字段</h3>
          <el-form-item v-for="schema in typeFieldSchemas" :key="schema.field_key" :label="schema.field_name">
            <el-input
              v-if="schema.field_type === 'string'"
              v-model="form.ext_fields[schema.field_key]"
              :placeholder="`请输入${schema.field_name}`"
            />
            <el-input-number
              v-else-if="schema.field_type === 'number'"
              v-model="form.ext_fields[schema.field_key]"
            />
            <el-switch
              v-else-if="schema.field_type === 'boolean'"
              v-model="form.ext_fields[schema.field_key]"
            />
            <el-date-picker
              v-else-if="schema.field_type === 'date'"
              v-model="form.ext_fields[schema.field_key]"
              type="date"
              style="width: 100%"
            />
            <el-input
              v-else
              v-model="form.ext_fields[schema.field_key]"
              type="textarea"
              :rows="2"
              :placeholder="`请输入${schema.field_name}（JSON格式）`"
            />
            <div v-if="schema.required" class="form-hint form-hint--required">必填</div>
          </el-form-item>
        </template>

        <el-form-item>
          <el-button type="primary" :loading="submitting" @click="save('draft')">保存草稿</el-button>
          <el-button type="success" :loading="submitting" @click="save('submit')">保存并提交上架</el-button>
          <el-button @click="cancelEdit">取消</el-button>
        </el-form-item>
      </el-form>
    </div>

    <!-- 驳回对话框 -->
    <el-dialog v-model="rejectVisible" title="驳回原因" width="400px" :close-on-click-modal="false">
      <el-input v-model="rejectNoteInput" type="textarea" :rows="3" placeholder="请填写驳回原因（必填）" maxlength="500" />
      <template #footer>
        <el-button @click="rejectVisible = false">取消</el-button>
        <el-button type="danger" :loading="actionLoading" @click="submitReject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { assetAPI, typeDefinitionAPI, categoryAPI } from '../api/asset'

const route = useRoute()
const router = useRouter()

const isCreate = computed(() => route.name === 'AssetCreate' || route.params.id === 'create')
const assetId = computed(() => isCreate.value ? null : route.params.id)

const loading = ref(false)
const submitting = ref(false)
const actionLoading = ref(false)
const editMode = ref(false)
const asset = ref(null)
const types = ref([])
const categoryTree = ref([])
const typeFieldSchemas = ref([])
const rejectNote = ref('')
const rejectVisible = ref(false)
const rejectNoteInput = ref('')

const formRef = ref()
const tagInputVisible = ref(false)
const tagInputRef = ref()
const tagInputValue = ref('')

const form = reactive({
  name: '',
  type_id: null,
  category_id: null,
  category_id_path: null,
  description: '',
  tags: [],
  source_module: '',
  source_reference: '',
  ext_fields: {}
})

const rules = {
  name: [{ required: true, message: '请输入资产名称', trigger: 'blur' }],
  type_id: [{ required: true, message: '请选择资产类型', trigger: 'change' }]
}

const canEdit = computed(() => asset.value && (asset.value.status === 'draft' || asset.value.status === 'rejected'))

// 将分类树扁平化为 el-cascader 所需格式（保留树形结构）
const categoryOptions = computed(() => categoryTree.value)

async function loadData() {
  loading.value = true
  try {
    const [typesRes, catRes] = await Promise.all([
      typeDefinitionAPI.list(),
      categoryAPI.tree()
    ])
    types.value = typesRes.data || []
    categoryTree.value = catRes.data || []

    if (!isCreate.value) {
      await loadAsset()
    }
  } catch (e) {
    ElMessage.error('加载数据失败')
  } finally {
    loading.value = false
  }
}

async function loadAsset() {
  const res = await assetAPI.get(assetId.value)
  asset.value = res.data

  // 提取驳回原因
  const rejectEf = asset.value.ext_fields?.find(f => f.field_key === '_reject_note')
  rejectNote.value = rejectEf?.value?.note || ''

  // 如果是创建模式不需要填充表单
  if (asset.value.type_id) {
    await loadTypeFields(asset.value.type_id)
  }
}

async function onTypeChange(typeId) {
  form.ext_fields = {}
  if (typeId) {
    await loadTypeFields(typeId)
  } else {
    typeFieldSchemas.value = []
  }
}

async function loadTypeFields(typeId) {
  try {
    const res = await assetAPI.typeFields(typeId)
    typeFieldSchemas.value = res.data || []
  } catch (e) {
    typeFieldSchemas.value = []
  }
}

function onCategoryChange(val) {
  form.category_id = val || null
}

// 填充编辑表单
watch(editMode, (val) => {
  if (val && asset.value) {
    form.name = asset.value.name
    form.type_id = asset.value.type_id
    form.category_id = asset.value.category_id || null
    form.category_id_path = asset.value.category_id || null
    form.description = asset.value.description || ''
    form.tags = [...(asset.value.tags || [])]
    form.source_module = asset.value.source_module || ''
    form.source_reference = asset.value.source_reference || ''
    // 填充扩展字段
    form.ext_fields = {}
    asset.value.ext_fields?.forEach(ef => {
      if (!ef.field_key.startsWith('_')) {
        form.ext_fields[ef.field_key] = ef.value?.value ?? ef.value
      }
    })
  }
})

async function save(mode) {
  await formRef.value.validate()
  submitting.value = true
  try {
    const payload = {
      name: form.name,
      type_id: form.type_id,
      category_id: form.category_id || null,
      description: form.description,
      tags: form.tags,
      source_module: form.source_module,
      source_reference: form.source_reference,
      ext_fields: form.ext_fields
    }

    let savedId
    if (isCreate.value) {
      const res = await assetAPI.create(payload)
      savedId = res.data.id
      ElMessage.success('资产已创建')
    } else {
      await assetAPI.update(assetId.value, payload)
      savedId = assetId.value
      editMode.value = false
      ElMessage.success('已保存')
    }

    if (mode === 'submit') {
      await assetAPI.submit(savedId)
      ElMessage.success('已提交上架申请')
    }

    if (isCreate.value) {
      router.replace(`/asset/assets/${savedId}`)
    } else {
      await loadAsset()
    }
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    submitting.value = false
  }
}

function cancelEdit() {
  if (isCreate.value) {
    router.push('/asset/assets')
  } else {
    editMode.value = false
  }
}

async function handleSubmit() {
  try {
    await assetAPI.submit(assetId.value)
    ElMessage.success('已提交上架申请')
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  }
}

async function handleApprove() {
  try {
    await assetAPI.approve(assetId.value)
    ElMessage.success('资产已上架')
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  }
}

function openReject() {
  rejectNoteInput.value = ''
  rejectVisible.value = true
}

async function submitReject() {
  if (!rejectNoteInput.value.trim()) {
    ElMessage.warning('请填写驳回原因')
    return
  }
  actionLoading.value = true
  try {
    await assetAPI.reject(assetId.value, { note: rejectNoteInput.value })
    ElMessage.success('已驳回')
    rejectVisible.value = false
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    actionLoading.value = false
  }
}

async function handleOffline() {
  try {
    await assetAPI.offline(assetId.value)
    ElMessage.success('资产已下架')
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  }
}

async function handleRepublish() {
  try {
    await assetAPI.republish(assetId.value)
    ElMessage.success('已重新提交上架申请')
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  }
}

async function handleDelete() {
  try {
    await assetAPI.delete(assetId.value)
    ElMessage.success('已删除')
    router.push('/asset/assets')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '删除失败')
  }
}

// 标签操作
function showTagInput() {
  tagInputVisible.value = true
  nextTick(() => tagInputRef.value?.focus())
}

function addTag() {
  const val = tagInputValue.value.trim()
  if (val && !form.tags.includes(val)) {
    form.tags.push(val)
  }
  tagInputVisible.value = false
  tagInputValue.value = ''
}

function removeTag(tag) {
  form.tags = form.tags.filter(t => t !== tag)
}

function statusLabel(status) {
  const map = { draft: '草稿', pending: '待审核', published: '已上架', rejected: '已驳回', offline: '已下架' }
  return map[status] || status
}

function statusAlertType(status) {
  const map = { draft: 'info', pending: 'warning', published: 'success', rejected: 'error', offline: 'info' }
  return map[status] || 'info'
}

function formatDate(dt) {
  if (!dt) return '-'
  return new Date(dt).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function formatExtValue(val) {
  if (!val) return '-'
  if (typeof val === 'object') return val.value ?? JSON.stringify(val)
  return String(val)
}

onMounted(() => {
  loadData()
  if (isCreate.value) {
    editMode.value = true
  }
})
</script>

<style scoped>
.asset-detail { padding: 24px; max-width: 900px; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.breadcrumb { display: flex; align-items: center; gap: 8px; font-size: 14px; color: var(--el-text-color-secondary); }
.separator { color: var(--el-border-color); }
.header-actions { display: flex; gap: 8px; }
.section-title { font-size: 15px; font-weight: 600; color: var(--el-text-color-primary); margin: 24px 0 16px; padding-left: 8px; border-left: 3px solid var(--el-color-primary); }
.asset-form { max-width: 700px; }
.tag-input { display: flex; flex-wrap: wrap; align-items: center; }
.form-hint { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; }
.form-hint--required { color: var(--el-color-danger); }
</style>
