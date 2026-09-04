<template>
  <div class="page-shell">
    <div class="page-header">
      <div><h2>{{ t('standard.collection.title') }}</h2><p>{{ t('standard.collection.subtitle') }}</p></div>
      <el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">{{ t('standard.collection.create') }}</el-button>
    </div>
    <el-card shadow="never" class="filter-card">
      <el-row :gutter="12">
        <el-col :span="10"><el-input v-model="filters.keyword" clearable :prefix-icon="Search" :placeholder="t('standard.collection.searchPlaceholder')" @change="search" /></el-col>
        <el-col :span="6"><el-select v-model="filters.status" clearable :placeholder="t('standard.common.selectStatus')" style="width:100%" @change="search"><el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" /></el-select></el-col>
      </el-row>
    </el-card>
    <el-card shadow="never">
      <el-table v-loading="loading" :data="collections" stripe>
        <el-table-column :label="t('standard.common.name')" min-width="190"><template #default="{ row }"><el-link type="primary" @click="goDetail(row.id)">{{ workingRevision(row)?.name || row.code }}</el-link></template></el-table-column>
        <el-table-column prop="code" :label="t('standard.common.code')" min-width="150" />
        <el-table-column :label="t('standard.common.status')" width="110"><template #default="{ row }"><el-tag :type="statusType(workingRevision(row)?.status)">{{ statusLabel(workingRevision(row)?.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('standard.collection.memberCount')" width="110"><template #default="{ row }">{{ workingRevision(row)?.members?.length || 0 }}</template></el-table-column>
        <el-table-column :label="t('standard.collection.myRoles')" min-width="180"><template #default="{ row }"><el-tag v-for="role in row.my_roles" :key="role" class="role-tag" type="info">{{ roleLabel(role) }}</el-tag><span v-if="!row.my_roles?.length">-</span></template></el-table-column>
        <el-table-column :label="t('standard.common.actions')" width="100"><template #default="{ row }"><el-button link type="primary" @click="goDetail(row.id)">{{ t('standard.common.detail') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="total" class="pagination" :total="total" :page-size="filters.page_size" :current-page="filters.page" layout="total, prev, pager, next" @current-change="page => { filters.page = page; load() }" />
    </el-card>

    <el-dialog v-model="dialogVisible" :title="t('standard.collection.create')" width="620px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
        <el-form-item :label="t('standard.common.code')" prop="code"><el-input v-model="form.code" /></el-form-item>
        <el-form-item :label="t('standard.common.name')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('standard.common.description')" prop="description"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="t('standard.revision.changeSummary')" prop="change_summary"><el-input v-model="form.change_summary" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogVisible=false">{{ t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="create">{{ t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { standardCollectionAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getStandardErrorMessage } from '../utils/apiError'
import { navigateStandardRoute } from '../utils/moduleNavigation'

const router = useRouter()
const { t } = useI18n()
const { canCreate } = useStandardPermissions('collection')
const loading = ref(false), submitting = ref(false), dialogVisible = ref(false), formRef = ref()
const collections = ref([]), total = ref(0)
const filters = reactive({ keyword: '', status: '', page: 1, page_size: 20 })
const statuses = ['draft', 'in_review', 'published']
const emptyForm = () => ({ code: '', name: '', description: '', change_summary: '', members: [] })
const form = reactive(emptyForm())
const rules = computed(() => ({ code:[{required:true,message:t('standard.collection.codeRequired')}], name:[{required:true,message:t('standard.collection.nameRequired')}], description:[{required:true,message:t('standard.collection.descriptionRequired')}], change_summary:[{required:true,message:t('standard.revision.changeSummaryRequired')}] }))
const workingRevision = row => row.draft_revision || row.current_revision
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const statusType = status => ({draft:'info',in_review:'warning',published:'success',withdrawn:'danger'}[status] || 'info')
const roleLabel = role => t(`standard.collection.role.${role}`)
const load = async () => { loading.value = true; try { const params = Object.fromEntries(Object.entries(filters).filter(([,value]) => value !== '')); const response = await standardCollectionAPI.list(params); collections.value = response.data || []; total.value = response.total || 0 } catch (error) { ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed')) } finally { loading.value = false } }
const search = () => { filters.page = 1; load() }
const openCreate = () => { Object.assign(form, emptyForm()); dialogVisible.value = true }
const create = async () => { if (!await formRef.value.validate().catch(() => false)) return; submitting.value = true; try { const result = await standardCollectionAPI.create({...form}); ElMessage.success(t('standard.common.createSuccess')); dialogVisible.value = false; goDetail(result.id) } catch (error) { ElMessage.error(getStandardErrorMessage(error, t)) } finally { submitting.value = false } }
const goDetail = id => navigateStandardRoute(router, `/collections/${id}`)
onMounted(load)
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.page-header h2{margin:0}.page-header p{margin:6px 0 0;color:var(--addp-text-secondary)}.filter-card{margin-bottom:12px}.pagination{display:flex;justify-content:flex-end;margin-top:16px}.role-tag{margin-right:6px}.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
</style>
