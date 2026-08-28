<template>
  <div class="page-shell">
    <div class="page-header"><h2>{{ $t('standard.codeSet.title') }}</h2><el-button v-if="canCreate" type="primary" :icon="Plus" @click="openCreate">{{ $t('standard.codeSet.create') }}</el-button></div>
    <el-card shadow="never" class="filter-card"><el-row :gutter="12"><el-col :span="8"><el-input v-model="filters.keyword" :prefix-icon="Search" clearable :placeholder="$t('standard.codeSet.searchPlaceholder')" @change="search" /></el-col><el-col :span="6"><el-select v-model="filters.domain_id" clearable :placeholder="$t('standard.common.selectDomain')" style="width:100%" @change="search"><el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-col><el-col :span="6"><el-select v-model="filters.status" clearable :placeholder="$t('standard.common.selectStatus')" style="width:100%" @change="search"><el-option v-for="s in statuses" :key="s" :value="s" :label="statusLabel(s)" /></el-select></el-col></el-row></el-card>
    <el-card shadow="never">
      <el-table :data="rows" v-loading="loading" stripe>
        <el-table-column :label="$t('standard.codeSet.codeLabel')" prop="code" width="180" />
        <el-table-column :label="$t('standard.codeSet.nameLabel')" min-width="180"><template #default="{row}"><el-link type="primary" @click="detail(row.id)">{{ working(row)?.name || '-' }}</el-link></template></el-table-column>
        <el-table-column :label="$t('standard.glossary.domainLabel')" width="140"><template #default="{row}">{{ domainName(row.domain_id) }}</template></el-table-column>
        <el-table-column :label="$t('standard.codeSet.valueType')" width="110"><template #default="{row}">{{ working(row)?.value_type || '-' }}</template></el-table-column>
        <el-table-column :label="$t('standard.codeSet.origin')" width="100"><template #default="{row}"><el-tag size="small" type="info">{{ $t(`standard.codeSet.originValue.${row.origin}`) }}</el-tag></template></el-table-column>
        <el-table-column :label="$t('standard.revision.number')" width="80"><template #default="{row}">{{ working(row) ? `R${working(row).revision_no}` : '-' }}</template></el-table-column>
        <el-table-column :label="$t('standard.common.status')" width="110"><template #default="{row}"><el-tag size="small" :type="statusType(working(row)?.status)">{{ statusLabel(working(row)?.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right"><template #default="{row}"><el-button link type="primary" @click="detail(row.id)">{{ $t('standard.common.detail') }}</el-button><el-button v-if="canDelete && row.origin !== 'platform'" link type="danger" @click="remove(row)">{{ $t('standard.common.delete') }}</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="total" class="pagination" :total="total" :page-size="filters.page_size" :current-page="filters.page" layout="total, prev, pager, next" @current-change="p => { filters.page=p; load() }" />
    </el-card>
    <el-dialog v-model="dialog" :title="$t('standard.codeSet.createTitle')" width="600px"><el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item :label="$t('standard.codeSet.codeLabel')" prop="code"><el-input v-model="form.code" /></el-form-item><el-form-item :label="$t('standard.codeSet.nameLabel')" prop="name"><el-input v-model="form.name" /></el-form-item><el-form-item :label="$t('standard.glossary.domainLabel')" prop="domain_id"><el-select v-model="form.domain_id" style="width:100%"><el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-form-item><el-form-item :label="$t('standard.codeSet.valueType')" prop="value_type"><el-select v-model="form.value_type" style="width:100%"><el-option value="string" /><el-option value="int" /><el-option value="bigint" /></el-select></el-form-item><el-form-item :label="$t('standard.codeSet.descriptionLabel')" prop="description"><el-input v-model="form.description" type="textarea" /></el-form-item><el-form-item :label="$t('standard.revision.changeSummary')" prop="change_summary"><el-input v-model="form.change_summary" type="textarea" /></el-form-item>
    </el-form><template #footer><el-button @click="dialog=false">{{ $t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="creating" @click="create">{{ $t('standard.common.confirm') }}</el-button></template></el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { codeSetAPI, domainAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'

const router=useRouter(), {t}=useI18n(), {canCreate,canDelete}=useStandardPermissions('code_set')
const loading=ref(false), creating=ref(false), dialog=ref(false), formRef=ref(), rows=ref([]), domains=ref([]), total=ref(0)
const statuses=['draft','in_review','published','withdrawn']
const filters=reactive({keyword:'',domain_id:null,status:'',page:1,page_size:20})
const blank=()=>({code:'',name:'',domain_id:null,value_type:'string',description:'',change_summary:''})
const form=reactive(blank())
const rules=computed(()=>({code:[{required:true,message:t('standard.codeSet.codeRequired')}],name:[{required:true,message:t('standard.codeSet.nameRequired')}],domain_id:[{required:true,message:t('standard.codeSet.domainRequired')}],description:[{required:true,message:t('standard.codeSet.descriptionRequired')}],change_summary:[{required:true,message:t('standard.revision.changeSummaryRequired')}]}))
const flatten=nodes=>nodes.flatMap(n=>[n,...flatten(n.children||[])])
const working=row=>row.draft_revision||row.current_revision
const domainName=id=>domains.value.find(x=>x.id===id)?.name||'-'
const statusLabel=s=>s?t(`standard.revision.status.${s}`):'-'
const statusType=s=>({draft:'info',in_review:'warning',published:'success',withdrawn:'danger'}[s]||'info')
const loadDomains=async()=>{try{domains.value=flatten(await domainAPI.list()||[])}catch{domains.value=[]}}
const load=async()=>{loading.value=true;try{const params=Object.fromEntries(Object.entries(filters).filter(([,v])=>v!==''&&v!=null));const res=await codeSetAPI.list(params);rows.value=res.data||[];total.value=res.total||0}catch(e){ElMessage.error(getStandardErrorMessage(e,t,'standard.common.loadFailed'))}finally{loading.value=false}}
const search=()=>{filters.page=1;load()}
const openCreate=()=>{Object.assign(form,blank());dialog.value=true}
const create=async()=>{if(!await formRef.value.validate().catch(()=>false))return;creating.value=true;try{const row=await codeSetAPI.create(form);ElMessage.success(t('standard.common.createSuccess'));dialog.value=false;detail(row.id)}catch(e){ElMessage.error(getStandardErrorMessage(e,t))}finally{creating.value=false}}
const remove=async row=>{try{await ElMessageBox.confirm(t('standard.codeSet.confirmDelete',{name:working(row)?.name||row.code}),t('standard.common.hint'),{type:'warning'});await codeSetAPI.delete(row.id);ElMessage.success(t('standard.common.deleteSuccess'));load()}catch(e){if(!isCanceledInteraction(e))ElMessage.error(getStandardErrorMessage(e,t))}}
const detail=id=>navigateStandardRoute(router,`/code-sets/${id}`)
onMounted(()=>{loadDomains();load()})
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.filter-card{margin-bottom:12px}.pagination{display:flex;justify-content:flex-end;margin-top:16px}.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
</style>
