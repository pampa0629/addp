<template>
  <el-dialog :model-value="modelValue" :title="t('common.plaintextAccess.title')" width="min(560px, calc(100vw - 32px))" append-to-body destroy-on-close @update:model-value="emit('update:modelValue', $event)">
    <el-alert type="warning" :closable="false" :title="t('common.plaintextAccess.hint')" />
    <el-form label-position="top" v-loading="loading">
      <el-form-item :label="t('common.plaintextAccess.field')" required>
        <el-select v-model="assessmentId" style="width:100%">
          <el-option v-for="target in fields" :key="target.assessment_id" :value="target.assessment_id" :label="target.component.key" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('common.plaintextAccess.scope')" required>
        <el-checkbox-group v-model="selectedOwners">
          <el-checkbox v-for="scope in scopes" :key="scope.consumer_owner" :value="scope.consumer_owner" :disabled="!scopeAvailable(scope.consumer_owner)">{{ t(`common.plaintextAccess.outlets.${scope.consumer_owner}`) }}</el-checkbox>
        </el-checkbox-group>
        <small>{{ t('common.plaintextAccess.scopeHint') }}</small>
      </el-form-item>
      <el-form-item :label="t('common.plaintextAccess.deadline')" required><el-date-picker v-model="expiresAt" type="datetime" :disabled-date="disabledDate" style="width:100%" /><small v-if="selectedTarget?.protection_expires_at">{{ t('common.plaintextAccess.protectionDeadline',{at:new Date(selectedTarget.protection_expires_at).toLocaleString()}) }}</small></el-form-item>
      <el-form-item :label="t('common.plaintextAccess.rationale')" required><el-input v-model="rationale" type="textarea" :rows="4" maxlength="2000" show-word-limit /></el-form-item>
      <el-alert v-if="error" type="error" :closable="false" :title="error" />
      <el-empty v-if="!loading && !error && !fields.length" :description="t('common.plaintextAccess.noFields')" :image-size="64" />
    </el-form>
    <template #footer><el-button @click="emit('update:modelValue',false)">{{ t('common.plaintextAccess.cancel') }}</el-button><el-button type="primary" :loading="saving" :disabled="loading || !selectedOwners.length" @click="submit">{{ t('common.plaintextAccess.submit') }}</el-button></template>
  </el-dialog>
</template>

<script setup>
import { computed,ref,watch,onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { plaintextAccessScopes, plaintextRequestPayload } from '../utils/plaintextAccess.mjs'
const props=defineProps({modelValue:Boolean,targetIdentity:{type:String,required:true},initialOwner:{type:String,required:true},api:{type:[Object,Function],required:true}})
const emit=defineEmits(['update:modelValue','submitted'])
const {t}=useI18n()
const scopes=plaintextAccessScopes
const loading=ref(false),saving=ref(false),error=ref(''),assessmentId=ref(''),selectedOwners=ref([]),expiresAt=ref(null),rationale=ref(''),targets=ref({})
let sequence=0
const fields=computed(()=>Object.values(targets.value).flat().filter((target,index,all)=>target.requestable && all.findIndex(item=>item.assessment_id===target.assessment_id)===index))
const selectedTarget=computed(()=>fields.value.find(item=>item.assessment_id===assessmentId.value))
const maximum=()=>Math.min(Date.now()+30*24*60*60*1000,selectedTarget.value?.protection_expires_at ? Date.parse(selectedTarget.value.protection_expires_at):Infinity)
const disabledDate=date=>date.getTime()<Date.now()-24*60*60*1000 || date.getTime()>maximum()
const scopeAvailable=owner=>targets.value[owner]?.some(item=>item.assessment_id===assessmentId.value && item.requestable && !item.active_exemption_id && item.access_request?.state!=='pending')===true
watch(assessmentId,()=>{selectedOwners.value=scopes.filter(s=>scopeAvailable(s.consumer_owner) && (s.consumer_owner===props.initialOwner || selectedOwners.value.includes(s.consumer_owner))).map(s=>s.consumer_owner); if(!selectedOwners.value.length)selectedOwners.value=scopes.filter(s=>scopeAvailable(s.consumer_owner)).slice(0,1).map(s=>s.consumer_owner)})
watch(()=>[props.modelValue,props.targetIdentity],async([open,identity])=>{
  const request=++sequence;targets.value={};error.value='';selectedOwners.value=[];saving.value=false
  if(!open || !identity)return
  loading.value=true;assessmentId.value='';rationale.value='';expiresAt.value=new Date(Date.now()+8*60*60*1000)
  try {
    const result=await Promise.all(scopes.map(async scope=>[scope.consumer_owner,(await props.api.get('/security/protection-access-request-targets',{params:{target_identity:identity,...scope}}))?.data || []]))
    if(request!==sequence)return
    targets.value=Object.fromEntries(result);assessmentId.value=fields.value[0]?.assessment_id || ''
    expiresAt.value=new Date(Math.min(expiresAt.value.getTime(),maximum()))
  }catch {if(request===sequence)error.value=t('common.plaintextAccess.loadFailed')}
  finally {if(request===sequence)loading.value=false}
},{immediate:true})
async function submit(){
  error.value=''
  let payload
  try {payload=plaintextRequestPayload({assessmentId:assessmentId.value,owners:selectedOwners.value,expiresAt:expiresAt.value,rationale:rationale.value,maximum:maximum()})}
  catch {error.value=t('common.plaintextAccess.required');return}
  if(selectedOwners.value.some(owner=>!scopeAvailable(owner)))return
  saving.value=true;const request=sequence
  try {await props.api.post('/security/protection-access-requests',payload);if(request===sequence){emit('submitted');emit('update:modelValue',false)}}
  catch {if(request===sequence)error.value=t('common.plaintextAccess.failed')}
  finally {if(request===sequence)saving.value=false}
}
onBeforeUnmount(()=>{sequence++})
</script>

<style scoped>small{display:block;color:var(--addp-text-secondary);line-height:1.5;margin-top:6px}</style>
