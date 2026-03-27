<template>
  <div class="entity-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="$router.back()">
          <el-icon><ArrowLeft /></el-icon>
          返回
        </el-button>
        <span class="entity-name">{{ entity.name || '加载中...' }}</span>
        <el-tag :type="entity.status === 'approved' ? 'success' : 'info'" size="small">
          {{ entity.status === 'approved' ? '已审批' : '草稿' }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
        <el-button
          v-if="entity.status === 'draft'"
          type="success"
          @click="handleApprove"
        >审批通过</el-button>
      </div>
    </div>

    <!-- Tab 标签页 -->
    <el-tabs v-model="activeTab" type="border-card">
      <!-- 基本信息标签页 -->
      <el-tab-pane label="基本信息" name="basic">
        <el-form :model="form" label-width="90px">
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="实体名称">
                <el-input v-model="form.name" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="英文编码">
                <el-input :value="entity.code" disabled />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="业务域">
                <el-select v-model="form.domain_id" clearable style="width:100%">
                  <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="24">
              <el-form-item label="描述">
                <el-input v-model="form.description" type="textarea" :rows="2" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-form>
      </el-tab-pane>

      <!-- 属性列表标签页 -->
      <el-tab-pane label="属性列表" name="attributes">
        <div class="tab-header">
          <el-button type="primary" size="small" @click="openAttrDialog()">
            <el-icon><Plus /></el-icon>
            添加属性
          </el-button>
        </div>

        <el-table :data="attributes" v-loading="attrLoading" stripe>
          <el-table-column label="序号" type="index" width="60" />
          <el-table-column label="属性名称" prop="name" min-width="140" />
          <el-table-column label="关联数据元" width="160">
            <template #default="{ row }">
              {{ getElementName(row.element_id) || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="主键" width="80">
            <template #default="{ row }">
              <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="可空" width="80">
            <template #default="{ row }">
              <el-tag :type="row.nullable ? 'info' : 'danger'" size="small">
                {{ row.nullable ? '是' : '否' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="描述" prop="description" show-overflow-tooltip />
          <el-table-column label="操作" width="130" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openAttrDialog(row)">编辑</el-button>
              <el-popconfirm title="确定删除该属性吗？" @confirm="deleteAttr(row.id)">
                <template #reference>
                  <el-button link type="danger">删除</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 实体关系标签页 -->
      <el-tab-pane label="实体关系" name="relations">
        <!-- 上半部分：关系管理 -->
        <div class="relations-management">
          <div class="tab-header">
            <el-button type="primary" size="small" @click="openRelationDialog()">
              <el-icon><Plus /></el-icon>
              添加关系
            </el-button>
          </div>

          <el-table :data="relations" v-loading="relationLoading" stripe>
            <el-table-column label="序号" type="index" width="60" />
            <el-table-column label="关系类型" width="120">
              <template #default="{ row }">
                <el-tag :type="getRelationTypeTag(row.relation_type)" size="small">
                  {{ formatRelationType(row.relation_type) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="方向" width="250">
              <template #default="{ row }">
                <span v-if="row.source_entity === entityId">
                  {{ entity.name }} → {{ getEntityName(row.target_entity) }}
                </span>
                <span v-else>
                  {{ getEntityName(row.source_entity) }} → {{ entity.name }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="目标实体" width="150">
              <template #default="{ row }">
                <el-link type="primary" @click="navigateToEntity(getTargetEntityId(row))">
                  {{ getTargetEntityName(row) }}
                </el-link>
              </template>
            </el-table-column>
            <el-table-column label="关系名称" prop="name" min-width="120" />
            <el-table-column label="描述" prop="description" show-overflow-tooltip />
            <el-table-column label="操作" width="130" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openRelationDialog(row)">编辑</el-button>
                <el-popconfirm title="确定删除该关系吗？" @confirm="deleteRelation(row.id)">
                  <template #reference>
                    <el-button link type="danger">删除</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 分割线 -->
        <el-divider />

        <!-- 下半部分：局部ER图 -->
        <div class="local-er-diagram">
          <div class="diagram-toolbar">
            <h3>实体关系图</h3>
            <div>
              <el-button size="small" @click="refreshLocalDiagram">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-button size="small" @click="copyMermaidCode">
                <el-icon><DocumentCopy /></el-icon> 复制Mermaid代码
              </el-button>
            </div>
          </div>

          <!-- Mermaid渲染区域 -->
          <div ref="mermaidContainer" class="mermaid-viewer" v-loading="mermaidLoading">
            <pre class="mermaid">{{ localMermaidCode }}</pre>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 属性对话框 -->
    <el-dialog
      v-model="attrDialogVisible"
      :title="editingAttr ? '编辑属性' : '添加属性'"
      width="480px"
    >
      <el-form ref="attrFormRef" :model="attrForm" :rules="attrRules" label-width="100px">
        <el-form-item label="属性名称" prop="name">
          <el-input v-model="attrForm.name" placeholder="属性中文名" />
        </el-form-item>
        <el-form-item label="关联数据元">
          <el-select
            v-model="attrForm.element_id"
            placeholder="可选，选择后自动关联标准"
            clearable
            filterable
            style="width:100%"
          >
            <el-option
              v-for="e in elements"
              :key="e.id"
              :label="`${e.name} (${e.code})`"
              :value="e.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="是否主键">
          <el-switch v-model="attrForm.is_pk" />
        </el-form-item>
        <el-form-item label="允许为空">
          <el-switch v-model="attrForm.nullable" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="attrForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="attrDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleAttrSubmit" :loading="attrSubmitting">
          {{ editingAttr ? '保存' : '添加' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关系对话框 -->
    <el-dialog
      v-model="relationDialogVisible"
      :title="editingRelation ? '编辑关系' : '添加关系'"
      width="600px"
    >
      <el-form ref="relationFormRef" :model="relationForm" :rules="relationRules" label-width="100px">
        <el-form-item label="关系方向">
          <el-radio-group v-model="relationForm.direction">
            <el-radio value="outgoing">
              {{ entity.name }} 指向其他实体
            </el-radio>
            <el-radio value="incoming">
              其他实体指向 {{ entity.name }}
            </el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="目标实体" prop="targetEntityId">
          <el-select
            v-model="relationForm.targetEntityId"
            filterable
            placeholder="选择目标实体"
            style="width:100%"
          >
            <el-option
              v-for="ent in otherEntities"
              :key="ent.id"
              :label="`${ent.name} (${ent.code})`"
              :value="ent.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="关系类型" prop="relationType">
          <el-select v-model="relationForm.relationType" style="width:100%">
            <el-option label="一对一" value="one_to_one" />
            <el-option label="一对多" value="one_to_many" />
            <el-option label="多对多" value="many_to_many" />
          </el-select>
        </el-form-item>

        <el-form-item label="关系名称">
          <el-input
            v-model="relationForm.name"
            placeholder="如：places, belongs_to, has"
          />
        </el-form-item>

        <el-form-item label="描述">
          <el-input
            v-model="relationForm.description"
            type="textarea"
            :rows="3"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="relationDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleRelationSubmit" :loading="relationSubmitting">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, Refresh, DocumentCopy } from '@element-plus/icons-vue'
import { entityAPI, entityRelationAPI, domainAPI, elementAPI } from '../api/model'
import mermaid from 'mermaid'

const route = useRoute()
const router = useRouter()
const entityId = parseInt(route.params.id)

const activeTab = ref('basic')
const saving = ref(false)
const attrLoading = ref(false)
const relationLoading = ref(false)
const mermaidLoading = ref(false)
const attrSubmitting = ref(false)
const relationSubmitting = ref(false)
const attrDialogVisible = ref(false)
const relationDialogVisible = ref(false)
const editingAttr = ref(null)
const editingRelation = ref(null)
const attrFormRef = ref(null)
const relationFormRef = ref(null)
const mermaidContainer = ref(null)

const entity = ref({})
const form = reactive({ name: '', domain_id: null, description: '' })
const attributes = ref([])
const relations = ref([])
const domains = ref([])
const elements = ref([])
const allEntities = ref([])
const localMermaidCode = ref('erDiagram\n  ENTITY {\n  }\n')

const attrForm = reactive({ name: '', element_id: null, is_pk: false, nullable: true, description: '' })
const attrRules = {
  name: [{ required: true, message: '请输入属性名称', trigger: 'blur' }]
}

const relationForm = reactive({
  direction: 'outgoing',
  targetEntityId: null,
  relationType: 'one_to_many',
  name: '',
  description: ''
})
const relationRules = {
  targetEntityId: [{ required: true, message: '请选择目标实体', trigger: 'change' }],
  relationType: [{ required: true, message: '请选择关系类型', trigger: 'change' }]
}

// 排除当前实体的其他实体列表
const otherEntities = computed(() => allEntities.value.filter(e => e.id !== entityId))

const getElementName = (id) => elements.value.find(e => e.id === id)?.name
const getEntityName = (id) => allEntities.value.find(e => e.id === id)?.name || `实体${id}`

const getRelationTypeTag = (type) => {
  const map = { one_to_one: 'success', one_to_many: 'primary', many_to_many: 'warning' }
  return map[type] || 'info'
}

const formatRelationType = (type) => {
  const map = { one_to_one: '一对一', one_to_many: '一对多', many_to_many: '多对多' }
  return map[type] || type
}

const getTargetEntityId = (relation) => {
  return relation.source_entity === entityId ? relation.target_entity : relation.source_entity
}

const getTargetEntityName = (relation) => {
  const targetId = getTargetEntityId(relation)
  return getEntityName(targetId)
}

const navigateToEntity = (id) => {
  router.push(`/entity/${id}`)
}

const loadEntity = async () => {
  const res = await entityAPI.get(entityId)
  entity.value = res || {}
  Object.assign(form, {
    name: entity.value.name,
    domain_id: entity.value.domain_id,
    description: entity.value.description || ''
  })
}

const loadAttributes = async () => {
  attrLoading.value = true
  try {
    const res = await entityAPI.getAttributes(entityId)
    attributes.value = res || []
  } finally {
    attrLoading.value = false
  }
}

const loadRelations = async () => {
  relationLoading.value = true
  try {
    const res = await entityRelationAPI.getByEntityId(entityId)
    relations.value = res || []
    await refreshLocalDiagram()
  } finally {
    relationLoading.value = false
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    await entityAPI.update(entityId, form)
    ElMessage.success('保存成功')
    loadEntity()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try {
    await entityAPI.approve(entityId)
    ElMessage.success('审批通过')
    loadEntity()
  } catch {
    ElMessage.error('审批失败')
  }
}

const openAttrDialog = (attr = null) => {
  editingAttr.value = attr
  if (attr) {
    Object.assign(attrForm, {
      name: attr.name,
      element_id: attr.element_id || null,
      is_pk: attr.is_pk,
      nullable: attr.nullable,
      description: attr.description || ''
    })
  } else {
    Object.assign(attrForm, { name: '', element_id: null, is_pk: false, nullable: true, description: '' })
  }
  attrDialogVisible.value = true
}

const handleAttrSubmit = async () => {
  await attrFormRef.value.validate()
  attrSubmitting.value = true
  try {
    if (editingAttr.value) {
      await entityAPI.updateAttribute(entityId, editingAttr.value.id, attrForm)
      ElMessage.success('更新成功')
    } else {
      await entityAPI.createAttribute(entityId, attrForm)
      ElMessage.success('添加成功')
    }
    attrDialogVisible.value = false
    loadAttributes()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    attrSubmitting.value = false
  }
}

const deleteAttr = async (attrId) => {
  try {
    await entityAPI.deleteAttribute(entityId, attrId)
    ElMessage.success('删除成功')
    loadAttributes()
  } catch {
    ElMessage.error('删除失败')
  }
}

const openRelationDialog = (relation = null) => {
  editingRelation.value = relation
  if (relation) {
    Object.assign(relationForm, {
      direction: relation.source_entity === entityId ? 'outgoing' : 'incoming',
      targetEntityId: getTargetEntityId(relation),
      relationType: relation.relation_type,
      name: relation.name || '',
      description: relation.description || ''
    })
  } else {
    Object.assign(relationForm, {
      direction: 'outgoing',
      targetEntityId: null,
      relationType: 'one_to_many',
      name: '',
      description: ''
    })
  }
  relationDialogVisible.value = true
}

const handleRelationSubmit = async () => {
  await relationFormRef.value.validate()
  relationSubmitting.value = true
  try {
    const payload = {
      source_entity: relationForm.direction === 'outgoing' ? entityId : relationForm.targetEntityId,
      target_entity: relationForm.direction === 'outgoing' ? relationForm.targetEntityId : entityId,
      relation_type: relationForm.relationType,
      name: relationForm.name,
      description: relationForm.description
    }

    if (editingRelation.value) {
      await entityRelationAPI.update(editingRelation.value.id, payload)
      ElMessage.success('关系已更新')
    } else {
      await entityRelationAPI.create(payload)
      ElMessage.success('关系已添加')
    }

    relationDialogVisible.value = false
    loadRelations()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    relationSubmitting.value = false
  }
}

const deleteRelation = async (relationId) => {
  try {
    await entityRelationAPI.delete(relationId)
    ElMessage.success('删除成功')
    loadRelations()
  } catch {
    ElMessage.error('删除失败')
  }
}

// 生成局部ER图的Mermaid代码
const refreshLocalDiagram = async () => {
  if (relations.value.length === 0) {
    localMermaidCode.value = 'erDiagram\n  ' + (entity.value.code || 'ENTITY') + ' {\n  }\n'
    await renderMermaid()
    return
  }

  mermaidLoading.value = true
  try {
    // 获取相关实体的ID
    const relatedEntityIds = new Set()
    relations.value.forEach(rel => {
      if (rel.source_entity === entityId) {
        relatedEntityIds.add(rel.target_entity)
      } else {
        relatedEntityIds.add(rel.source_entity)
      }
    })

    // 查询相关实体的完整信息（包括属性）
    const relatedEntitiesPromises = Array.from(relatedEntityIds).map(id =>
      Promise.all([
        entityAPI.get(id),
        entityAPI.getAttributes(id)
      ]).then(([entityRes, attrsRes]) => ({
        ...entityRes.data,
        attributes: attrsRes.data || []
      }))
    )

    const relatedEntities = await Promise.all(relatedEntitiesPromises)

    // 生成Mermaid代码
    let code = 'erDiagram\n'

    // 当前实体定义
    code += generateEntityDefinition(entity.value, attributes.value)

    // 相关实体定义
    relatedEntities.forEach(ent => {
      code += generateEntityDefinition(ent, ent.attributes)
    })

    // 关系定义
    relations.value.forEach(rel => {
      const sourceEntity = rel.source_entity === entityId
        ? entity.value
        : relatedEntities.find(e => e.id === rel.source_entity)
      const targetEntity = rel.target_entity === entityId
        ? entity.value
        : relatedEntities.find(e => e.id === rel.target_entity)

      if (sourceEntity && targetEntity) {
        const symbol = convertToMermaidSymbol(rel.relation_type)
        code += `  ${sourceEntity.code} ${symbol} ${targetEntity.code} : "${rel.name || 'relates'}"\n`
      }
    })

    localMermaidCode.value = code
    await renderMermaid()
  } catch (err) {
    console.error('生成ER图失败:', err)
    ElMessage.error('生成ER图失败')
  } finally {
    mermaidLoading.value = false
  }
}

// 生成单个实体的Mermaid定义
const generateEntityDefinition = (ent, attrs) => {
  let code = `  ${ent.code} {\n`
  attrs.forEach(attr => {
    const type = 'string' // 默认类型
    const pk = attr.is_pk ? ' PK' : ''
    code += `    ${type} ${attr.name}${pk}\n`
  })
  code += `  }\n`
  return code
}

// 转换关系类型为Mermaid符号
const convertToMermaidSymbol = (relationType) => {
  const map = {
    one_to_one: '||--||',
    one_to_many: '||--o{',
    many_to_many: '}o--o{'
  }
  return map[relationType] || '||--o{'
}

// 渲染Mermaid图
const renderMermaid = async () => {
  await nextTick()

  // 验证Mermaid代码
  if (!localMermaidCode.value || !localMermaidCode.value.trim()) {
    console.warn('Mermaid代码为空，跳过渲染')
    return
  }

  if (mermaidContainer.value) {
    const mermaidEl = mermaidContainer.value.querySelector('.mermaid')
    if (mermaidEl) {
      // 关键修复：恢复原始Mermaid代码文本，清除之前的渲染结果
      mermaidEl.removeAttribute('data-processed')
      mermaidEl.textContent = localMermaidCode.value

      try {
        await mermaid.run({ nodes: [mermaidEl] })
      } catch (err) {
        console.error('Mermaid渲染错误:', err)
        ElMessage.error('ER图渲染失败，请检查数据格式')
      }
    }
  }
}

// 复制Mermaid代码到剪贴板
const copyMermaidCode = async () => {
  try {
    await navigator.clipboard.writeText(localMermaidCode.value)
    ElMessage.success('Mermaid代码已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败')
  }
}

// 监听标签页切换，自动加载对应数据
watch(activeTab, (newTab) => {
  if (newTab === 'relations' && relations.value.length === 0) {
    loadRelations()
  }
})

onMounted(async () => {
  // 初始化Mermaid
  mermaid.initialize({
    startOnLoad: false,
    theme: 'default'
  })

  // 加载数据
  await loadEntity()
  loadAttributes()

  const [domainsRes, elementsRes, entitiesRes] = await Promise.all([
    domainAPI.list(),
    elementAPI.list({ page_size: 500 }),
    entityAPI.list({ page_size: 1000 })
  ])
  domains.value = domainsRes.data || []
  elements.value = elementsRes.data || []
  allEntities.value = entitiesRes.data || []

  // 加载关系（如果tab是relations）
  if (activeTab.value === 'relations') {
    loadRelations()
  }
})
</script>

<style scoped>
.entity-detail {
  padding: 20px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.entity-name {
  font-size: 18px;
  font-weight: 600;
}

.tab-header {
  margin-bottom: 16px;
  display: flex;
  justify-content: flex-start;
}

.relations-management {
  margin-bottom: 20px;
}

.local-er-diagram {
  margin-top: 20px;
}

.diagram-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
}

.diagram-toolbar h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.diagram-toolbar > div {
  display: flex;
  gap: 8px;
}

.mermaid-viewer {
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 20px;
  background: #fafafa;
  min-height: 400px;
  overflow: auto;
}

.mermaid-viewer .mermaid {
  background: transparent;
}
</style>
