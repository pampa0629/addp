<template>
  <div class="asset-detail">
    <!-- 顶部工具栏 -->
    <div class="page-header">
      <div class="breadcrumb">
        <el-button link @click="backToList">
          <el-icon><ArrowLeft /></el-icon> {{ t('asset.assetDetail.backToList') }}
        </el-button>
        <span class="separator">/</span>
        <span>{{ editMode ? t('asset.assetDetail.editAsset') : t('asset.assetDetail.assetDetail') }}</span>
      </div>
      <div class="header-actions" v-if="!editMode && asset">
        <el-button v-if="canEdit" @click="startEdit">{{ t('asset.assetDetail.edit') }}</el-button>
        <el-button v-if="asset.status === 'draft'" type="success" @click="handlePublish">{{ t('asset.assetDetail.submitPublish') }}</el-button>
        <el-button v-if="asset.status === 'published'" type="warning" plain @click="handleOffline">{{ t('asset.assetDetail.offline') }}</el-button>
        <el-button v-if="asset.status === 'offline'" type="primary" @click="handlePublish">{{ t('asset.assetDetail.republish') }}</el-button>
        <el-popconfirm v-if="asset.status === 'draft'" :title="t('asset.assetDetail.deleteConfirm')" @confirm="handleDelete">
          <template #reference>
            <el-button type="danger" plain>{{ t('asset.assetDetail.delete') }}</el-button>
          </template>
        </el-popconfirm>
      </div>
    </div>

    <!-- 状态横幅（仅详情模式） -->
    <el-alert
      v-if="!editMode && asset"
      :type="statusAlertType(asset.status)"
      :closable="false"
      style="margin-bottom: 20px"
    >
      <template #title>
        <span>{{ t('asset.assetDetail.statusPrefix') }}{{ statusLabel(asset.status) }}</span>
        <span v-if="asset.status === 'published' && asset.published_at" style="margin-left: 12px; font-size: 13px">
          {{ t('asset.assetDetail.publishedAtPrefix') }}{{ formatDate(asset.published_at) }}
        </span>
      </template>
    </el-alert>

    <div v-loading="loading">
      <!-- 详情展示模式 -->
      <template v-if="!editMode && asset">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('asset.assetDetail.assetName')">{{ asset.name }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.assetType')">
            <el-tag size="small">{{ asset.type_name || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.category')">{{ asset.category?.name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.sourceModule')">{{ asset.source_module || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.sourceReference')" :span="2">{{ asset.source_reference || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.tags')" :span="2">
            <el-tag v-for="tag in (asset.tags || [])" :key="tag" size="small" style="margin-right: 4px">{{ tag }}</el-tag>
            <span v-if="!asset.tags?.length">-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.description')" :span="2">{{ asset.description || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.createdAt')">{{ formatDate(asset.created_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('asset.assetDetail.updatedAt')">{{ formatDate(asset.updated_at) }}</el-descriptions-item>
        </el-descriptions>

        <template v-if="asset.ext_fields?.length">
          <h3 class="section-title">{{ t('asset.assetDetail.extFields') }}</h3>
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

      <!-- 编辑表单模式 -->
      <el-form
        v-if="editMode"
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="100px"
        class="asset-form"
      >
        <h3 class="section-title">{{ t('asset.assetDetail.basicInfo') }}</h3>

        <el-form-item :label="t('asset.assetDetail.assetName')" prop="name">
          <el-input v-model="form.name" :placeholder="t('asset.assetDetail.assetNamePlaceholder')" maxlength="500" />
        </el-form-item>

        <el-form-item :label="t('asset.assetDetail.category')">
          <el-cascader
            v-model="form.category_id_path"
            :options="categoryOptions"
            :props="{ checkStrictly: true, value: 'id', label: 'name', children: 'children', emitPath: false }"
            :placeholder="t('asset.assetDetail.categoryPlaceholder')"
            clearable
            style="width: 100%"
            @change="onCategoryChange"
          />
        </el-form-item>

        <el-form-item :label="t('asset.assetDetail.description')">
          <el-input v-model="form.description" type="textarea" :rows="3" :placeholder="t('asset.assetDetail.descriptionPlaceholder')" maxlength="2000" />
        </el-form-item>

        <el-form-item :label="t('asset.assetDetail.tags')">
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
            <el-button v-else size="small" @click="showTagInput">{{ t('asset.assetDetail.addTag') }}</el-button>
          </div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="submitting" @click="save">{{ t('asset.assetDetail.save') }}</el-button>
          <el-button @click="cancelEdit">{{ t('asset.assetDetail.backToList') }}</el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { assetAPI, catalogAPI } from '../api/asset'
import { useI18n } from 'vue-i18n'
import { navigateAssetRoute } from '../utils/moduleNavigation'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()

const editMode = computed(() => route.name === 'AssetEdit')
const assetId = computed(() => route.params.id)

const loading = ref(false)
const submitting = ref(false)
const asset = ref(null)
const categoryTree = ref([])

const formRef = ref()
const tagInputVisible = ref(false)
const tagInputRef = ref()
const tagInputValue = ref('')

const form = reactive({
  name: '',
  category_id: null,
  category_id_path: null,
  description: '',
  tags: []
})

const rules = computed(() => ({
  name: [{ required: true, message: t('asset.assetDetail.assetNameRequired'), trigger: 'blur' }]
}))

const canEdit = computed(() => Boolean(asset.value))

// 将分类树扁平化为 el-cascader 所需格式（保留树形结构）
const categoryOptions = computed(() => categoryTree.value)

async function loadData() {
  loading.value = true
  try {
    const catRes = await catalogAPI.tree()
    categoryTree.value = catRes || []
    await loadAsset()
  } catch (e) {
    ElMessage.error(t('asset.assetDetail.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadAsset() {
  const res = await assetAPI.get(assetId.value)
  asset.value = res
}

function onCategoryChange(val) {
  form.category_id = val || null
}

watch([editMode, asset], ([editing, currentAsset]) => {
  if (editing && currentAsset) {
    form.name = currentAsset.name
    form.category_id = currentAsset.category_id || null
    form.category_id_path = currentAsset.category_id || null
    form.description = currentAsset.description || ''
    form.tags = [...(currentAsset.tags || [])]
  }
})

async function save() {
  await formRef.value.validate()
  submitting.value = true
  try {
    const payload = {
      name: form.name,
      category_id: form.category_id || null,
      description: form.description,
      tags: form.tags
    }

    await assetAPI.update(assetId.value, payload)
    if (form.category_id === null && asset.value.category_id !== null) {
      await assetAPI.batchCatalog([assetId.value], null)
    }
    ElMessage.success(t('asset.assetDetail.saved'))
    await navigateAssetRoute(router, `/assets/${assetId.value}`, { history: 'replace' })
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.assetDetail.saveFailed'))
  } finally {
    submitting.value = false
  }
}

function cancelEdit() {
  navigateAssetRoute(router, `/assets/${assetId.value}`, { history: 'replace' })
}

function backToList() {
  navigateAssetRoute(router, '/assets', { history: 'replace' })
}

function startEdit() {
  navigateAssetRoute(router, `/assets/${assetId.value}/edit`)
}

async function handlePublish() {
  try {
    await assetAPI.publish(assetId.value)
    ElMessage.success(t('asset.assetDetail.assetPublished'))
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.assetDetail.operationFailed'))
  }
}

async function handleOffline() {
  try {
    await assetAPI.offline(assetId.value)
    ElMessage.success(t('asset.assetDetail.assetOfflined'))
    await loadAsset()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.assetDetail.operationFailed'))
  }
}

async function handleDelete() {
  try {
    await assetAPI.delete(assetId.value)
    ElMessage.success(t('asset.assetDetail.deleted'))
    backToList()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('asset.assetDetail.deleteFailed'))
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
  const map = {
    draft: t('asset.assetDetail.statusDraft'),
    published: t('asset.assetDetail.statusPublished'),
    offline: t('asset.assetDetail.statusOffline')
  }
  return map[status] || status
}

function statusAlertType(status) {
  const map = { draft: 'info', published: 'success', offline: 'info' }
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
})

watch(assetId, () => {
  loadData()
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
