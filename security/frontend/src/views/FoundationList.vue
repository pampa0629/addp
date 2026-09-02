<template>
  <section class="page">
    <div class="page-header"><div><h2>{{ title }}</h2><p>{{ t(`security.descriptions.${resource}`) }}</p></div><el-button v-if="can('create')" type="primary" @click="openCreate">{{ t('security.common.create') }}</el-button></div>
    <el-card><el-table v-loading="loading" :data="rows" row-key="id">
      <el-table-column v-for="column in columns" :key="column.prop" :prop="column.prop" :label="t(column.label)" :min-width="column.width || 120" />
      <el-table-column :label="t('security.common.actions')" width="150" fixed="right"><template #default="{ row }"><el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button><el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button></template></el-table-column>
    </el-table><el-empty v-if="!loading && rows.length === 0" :description="t('security.common.empty')" /></el-card>
    <el-dialog v-model="dialog" :title="editing ? t('security.common.edit') : t('security.common.create')" width="560px"><el-form label-width="170px">
      <el-form-item v-for="field in fields" :key="field.key" :label="t(field.label)" :required="field.required">
        <el-switch v-if="field.type === 'boolean'" v-model="form[field.key]" />
        <el-input-number v-else-if="field.type === 'number'" v-model="form[field.key]" :min="field.min ?? 0" :max="field.max" class="wide" />
        <el-select v-else-if="field.options" v-model="form[field.key]" class="wide"><el-option v-for="option in field.options" :key="option" :value="option" :label="option" /></el-select>
        <el-input v-else v-model="form[field.key]" :type="field.type === 'textarea' ? 'textarea' : 'text'" />
      </el-form-item>
    </el-form><template #footer><el-button @click="dialog=false">{{ t('security.common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="save">{{ t('security.common.save') }}</el-button></template></el-dialog>
  </section>
</template>
<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { classificationAPI, gradeAPI, sensitiveDataTypeAPI, protectionBaselineAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { buildFoundationPayload, initialFoundationFieldValue } from '../utils/foundationForm.mjs'

const { t } = useI18n(), route = useRoute(), auth = useAuthStore()
const resource = computed(() => route.meta.resource)
const specs = {
  classification: { api: classificationAPI, permission: 'classification', fields: [text('code', true), text('name', true), text('description', false, 'textarea'), number('parent_id', false, 1, undefined, true), number('sort_order')], columns: ['code','name','description','sort_order'] },
  grade: { api: gradeAPI, permission: 'grade', fields: [text('code', true), text('name', true), text('description', false, 'textarea'), number('risk_order', true, 1)], columns: ['code','name','risk_order','description'] },
  sensitiveDataType: { api: sensitiveDataTypeAPI, permission: 'sensitive_data_type', fields: [text('code', true), text('name', true), text('description', false, 'textarea'), number('security_classification_id', true, 1), number('default_security_grade_id', true, 1), number('protection_threshold', true, 0.01, 1)], columns: ['code','name','security_classification_id','default_security_grade_id','protection_threshold'] },
  protectionBaseline: { api: protectionBaselineAPI, permission: 'protection_baseline', fields: [number('sensitive_data_type_id', true, 1), number('security_grade_id', true, 1), select('effect',['mask','suppress','deny']), text('algorithm'), number('keep_prefix'), number('keep_suffix'), select('invalid_value_effect',['suppress','deny']), bool('enabled')], columns: ['sensitive_data_type_id','security_grade_id','effect','algorithm','enabled'] }
}
function label(key){return `security.fields.${key}`}; function text(key,required=false,type='text'){return{key,label:label(key),required,type}}; function number(key,required=false,min=0,max,nullable=false){return{key,label:label(key),required,type:'number',min,max,nullable}}; function select(key,options){return{key,label:label(key),required:true,options}}; function bool(key){return{key,label:label(key),type:'boolean'}}
const spec = computed(() => specs[resource.value]); const title = computed(() => t(`security.resources.${resource.value}`)); const fields = computed(() => spec.value.fields); const columns = computed(() => spec.value.columns.map(prop=>({prop,label:label(prop)})))
const rows=ref([]),loading=ref(false),saving=ref(false),dialog=ref(false),editing=ref(null),form=reactive({})
function can(action){return auth.hasPermission(`security.${spec.value.permission}.${action}`)}
function reset(values={}){Object.keys(form).forEach(k=>delete form[k]); fields.value.forEach(f=>{form[f.key]=initialFoundationFieldValue(f,values)}); if(values.version)form.version=Number(values.version)}
function openCreate(){editing.value=null;reset({effect:'mask',algorithm:'addp.mask.keep_prefix_suffix/v1',invalid_value_effect:'suppress',enabled:true,protection_threshold:0.8});dialog.value=true}
function openEdit(row){editing.value=row;reset(row);dialog.value=true}
async function load(){loading.value=true;try{rows.value=await spec.value.api.list()||[]}catch(e){ElMessage.error(e.message||t('security.common.failed'))}finally{loading.value=false}}
async function save(){saving.value=true;try{const payload=buildFoundationPayload(fields.value,form);if(editing.value)await spec.value.api.update(editing.value.id,payload);else await spec.value.api.create(payload);dialog.value=false;await load();ElMessage.success(t('security.common.saved'))}catch(e){ElMessage.error(e.message||t('security.common.failed'))}finally{saving.value=false}}
async function remove(row){try{await ElMessageBox.confirm(t('security.common.confirmDelete',{name:row.name||row.code||row.id}),t('security.common.hint'),{type:'warning'});const payload=resource.value==='protectionBaseline'?{version:Number(row.version)}:undefined;await spec.value.api.delete(row.id,payload);await load()}catch(e){if(e!=='cancel'&&e!=='close')ElMessage.error(e.message||t('security.common.failed'))}}
watch(resource,load,{immediate:true})
</script>
<style scoped>.page{padding:20px;min-height:100%;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:16px}.page-header h2{margin:0}.page-header p{color:var(--addp-text-secondary)}.wide{width:100%}:deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}</style>
