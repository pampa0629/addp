<template>
  <div class="page-shell">
    <div class="page-header"><h2>{{ $t('standard.element.title') }}</h2><el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">{{ $t('standard.element.create') }}</el-button></div>
    <el-card shadow="never" class="filter-card"><el-row :gutter="12">
      <el-col :span="8"><el-input v-model="filters.keyword" :prefix-icon="Search" clearable :placeholder="$t('standard.element.searchPlaceholder')" @change="search" /></el-col>
      <el-col :span="6"><el-select v-model="filters.domain_id" clearable :placeholder="$t('standard.common.selectDomain')" style="width:100%" @change="search"><el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-col>
      <el-col :span="6"><el-select v-model="filters.status" clearable :placeholder="$t('standard.common.selectStatus')" style="width:100%" @change="search"><el-option v-for="s in statuses" :key="s" :label="statusLabel(s)" :value="s" /></el-select></el-col>
    </el-row></el-card>
    <el-card shadow="never" class="table-card">
      <el-table :data="elements" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.element.nameLabel')" min-width="180"><template #default="{ row }"><el-link type="primary" @click="goDetail(row.id)">{{ workingRevision(row)?.name || '-' }}</el-link></template></el-table-column>
        <el-table-column :label="$t('standard.element.codeLabel')" prop="code" width="170" />
        <el-table-column :label="$t('standard.element.dataTypeLabel')" width="110"><template #default="{ row }"><el-tag size="small" type="info">{{ workingRevision(row)?.data_type || '-' }}</el-tag></template></el-table-column>
        <el-table-column :label="$t('standard.glossary.domainLabel')" width="140"><template #default="{ row }">{{ domainName(row.domain_id) }}</template></el-table-column>
        <el-table-column :label="$t('standard.revision.number')" width="90"><template #default="{ row }">{{ workingRevision(row) ? `R${workingRevision(row).revision_no}` : '-' }}</template></el-table-column>
        <el-table-column :label="$t('standard.common.status')" width="110"><template #default="{ row }"><el-tag :type="statusType(workingRevision(row)?.status)" size="small">{{ statusLabel(workingRevision(row)?.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="goDetail(row.id)">{{ $t('standard.common.detail') }}</el-button><el-button v-if="canDelete" link type="danger" @click="remove(row)">{{ $t('standard.common.delete') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="total" class="pagination" :total="total" :page-size="filters.page_size" :current-page="filters.page" layout="total, prev, pager, next" @current-change="page => { filters.page = page; load() }" />
    </el-card>
    <el-dialog v-model="dialog" :title="$t('standard.element.createTitle')" width="640px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
        <el-form-item :label="$t('standard.element.codeLabel')" prop="code"><el-input v-model="form.code" /></el-form-item>
        <el-form-item :label="$t('standard.element.nameLabel')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="$t('standard.element.definitionLabel')" prop="definition"><el-input v-model="form.definition" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="$t('standard.element.dataTypeLabel')" prop="data_type"><el-select v-model="form.data_type" style="width:100%"><el-option v-for="type in dataTypes" :key="type" :label="type" :value="type" /></el-select></el-form-item>
        <el-form-item :label="$t('standard.element.nullableLabel')"><el-switch v-model="form.nullable" /></el-form-item>
        <el-form-item :label="$t('standard.glossary.domainLabel')"><el-select v-model="form.domain_id" clearable style="width:100%"><el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-form-item>
        <el-form-item :label="$t('standard.revision.changeSummary')" prop="change_summary"><el-input v-model="form.change_summary" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialog=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="submitting" @click="create">{{ $t('standard.common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { domainAPI, elementAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const router = useRouter()
const { t } = useI18n()
const { canCreate, canDelete } = useStandardPermissions('element')
const loading = ref(false), submitting = ref(false), dialog = ref(false), formRef = ref()
const elements = ref([]), domains = ref([]), total = ref(0)
const statuses = ['draft', 'in_review', 'published', 'superseded', 'withdrawn']
const dataTypes = ['string', 'text', 'int', 'bigint', 'float', 'decimal', 'date', 'datetime', 'bool', 'json']
const filters = reactive({ keyword: '', domain_id: null, status: '', page: 1, page_size: 20 })
const emptyForm = () => ({ code: '', name: '', definition: '', data_type: 'string', nullable: true, domain_id: null, value_domain_kind: 'unrestricted', change_summary: '' })
const form = reactive(emptyForm())
const rules = computed(() => ({ code: [{ required: true, message: t('standard.element.codeRequired') }], name: [{ required: true, message: t('standard.element.nameRequired') }], definition: [{ required: true, message: t('standard.element.definitionRequired') }], data_type: [{ required: true, message: t('standard.element.dataTypeRequired') }], change_summary: [{ required: true, message: t('standard.revision.changeSummaryRequired') }] }))
const workingRevision = row => row.draft_revision || row.current_revision
const statusLabel = s => s ? t(`standard.revision.status.${s}`) : '-'
const statusType = s => ({ draft: 'info', in_review: 'warning', published: 'success', superseded: '', withdrawn: 'danger' }[s] || 'info')
const domainName = id => domains.value.find(d => d.id === id)?.name || '-'
const flatten = nodes => nodes.flatMap(node => [node, ...flatten(node.children || [])])
const loadDomains = async () => { try { domains.value = flatten(await domainAPI.list() || []) } catch { domains.value = [] } }
const load = async () => { loading.value = true; try { const params = Object.fromEntries(Object.entries(filters).filter(([,v]) => v !== '' && v != null)); const res = await elementAPI.list(params); elements.value = res.data || []; total.value = res.total || 0 } catch (e) { ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed')) } finally { loading.value = false } }
const search = () => { filters.page = 1; load() }
const openCreate = () => { Object.assign(form, emptyForm()); dialog.value = true }
const create = async () => { if (!await formRef.value.validate().catch(() => false)) return; submitting.value = true; try { const row = await elementAPI.create(form); ElMessage.success(t('standard.common.createSuccess')); dialog.value = false; goDetail(row.id) } catch (e) { ElMessage.error(getStandardErrorMessage(e, t)) } finally { submitting.value = false } }
const remove = async row => { try { await ElMessageBox.confirm(t('standard.element.confirmDelete', { name: workingRevision(row)?.name || row.code }), t('standard.common.hint'), { type: 'warning' }); await elementAPI.delete(row.id); ElMessage.success(t('standard.common.deleteSuccess')); load() } catch (e) { if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed')) } }
const goDetail = id => navigateStandardRoute(router, `/elements/${id}`)
onMounted(() => { loadDomains(); load() })
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.filter-card{margin-bottom:12px}.table-card :deep(.el-card__body){padding-top:8px}.pagination{display:flex;justify-content:flex-end;margin-top:16px}.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
</style>
