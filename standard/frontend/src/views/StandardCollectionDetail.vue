<template>
  <div class="page-shell" v-loading="loading">
    <div class="page-header">
      <div class="title-row"><el-button link :icon="ArrowLeft" @click="back">{{ t('standard.common.back') }}</el-button><div><h2>{{ workingRevision?.name || collection?.code || '-' }}</h2><p>{{ collection?.code }}</p></div></div>
      <div v-if="collection" class="actions">
        <el-tag :type="statusType(workingRevision?.status)">{{ statusLabel(workingRevision?.status) }}</el-tag>
        <el-button v-if="canCreateDraft" @click="createDraft">{{ t('standard.revision.newDraft') }}</el-button>
        <el-button v-if="canMaintain && draft?.status === 'draft'" type="primary" :loading="saving" @click="saveDraft">{{ t('standard.common.save') }}</el-button>
        <el-button v-if="canMaintain && draft?.status === 'draft'" type="warning" :loading="acting" @click="submitDraft">{{ t('standard.revision.submit') }}</el-button>
        <el-button v-if="canReview && draft?.status === 'in_review'" @click="returnDraft">{{ t('standard.revision.return') }}</el-button>
        <el-button v-if="canReview && draft?.status === 'in_review'" type="success" :loading="acting" @click="publishDraft">{{ t('standard.revision.publish') }}</el-button>
      </div>
    </div>

    <el-alert v-if="collection && !collection.my_roles?.length" :title="t('standard.collection.readOnlyHint')" type="info" :closable="false" class="section" />

    <el-card v-if="collection" shadow="never" class="section">
      <template #header><div class="card-header"><span>{{ t('standard.collection.configuration') }}</span><span>R{{ workingRevision?.revision_no || '-' }}</span></div></template>
      <el-form :model="draftForm" label-width="110px">
        <el-form-item :label="t('standard.common.name')"><el-input v-model="draftForm.name" :disabled="!isEditable" /></el-form-item>
        <el-form-item :label="t('standard.common.description')"><el-input v-model="draftForm.description" type="textarea" :rows="3" :disabled="!isEditable" /></el-form-item>
        <el-form-item :label="t('standard.revision.changeSummary')"><el-input v-model="draftForm.change_summary" type="textarea" :rows="2" :disabled="!isEditable" /></el-form-item>
      </el-form>
    </el-card>

    <el-card v-if="collection" shadow="never" class="section">
      <template #header><div class="card-header"><span>{{ t('standard.collection.members') }}</span><el-button v-if="isEditable" type="primary" link :icon="Plus" @click="openMemberDialog">{{ t('standard.collection.addMember') }}</el-button></div></template>
      <el-table :data="draftForm.members" stripe>
        <el-table-column :label="t('standard.common.type')" width="130"><template #default="{ row }">{{ memberTypeLabel(row.member_type) }}</template></el-table-column>
        <el-table-column :label="t('standard.common.name')" prop="name" min-width="180" />
        <el-table-column :label="t('standard.common.code')" prop="code" min-width="150"><template #default="{ row }">{{ row.code || '-' }}</template></el-table-column>
        <el-table-column v-if="isEditable" :label="t('standard.common.actions')" width="90"><template #default="{ $index }"><el-button link type="danger" @click="draftForm.members.splice($index,1)">{{ t('standard.common.remove') }}</el-button></template></el-table-column>
      </el-table>
    </el-card>

    <el-card v-if="collection" shadow="never" class="section">
      <template #header><div class="card-header"><span>{{ t('standard.collection.assignments') }}</span><el-button v-if="canAssign" type="primary" link @click="openAssignmentDialog">{{ t('standard.collection.configureAssignments') }}</el-button></div></template>
      <el-table :data="collection.assignments" stripe>
        <el-table-column :label="t('standard.collection.principal')" min-width="190"><template #default="{ row }"><span v-if="row.referenceable">{{ row.principal_name }}<span v-if="row.principal_code" class="secondary">（{{ row.principal_code }}）</span></span><el-tag v-else type="danger">{{ t('standard.collection.unavailablePrincipal') }}</el-tag></template></el-table-column>
        <el-table-column :label="t('standard.collection.roleLabel')" width="150"><template #default="{ row }"><el-tag type="info">{{ roleLabel(row.role) }}</el-tag></template></el-table-column>
      </el-table>
    </el-card>

    <el-card v-if="events.length" shadow="never" class="section">
      <template #header>{{ t('standard.collection.events') }}</template>
      <el-table :data="events" stripe>
        <el-table-column :label="t('standard.common.type')" width="190"><template #default="{ row }">{{ eventTypeLabel(row.event_type) }}</template></el-table-column>
        <el-table-column :label="t('standard.collection.principal')" min-width="180"><template #default="{ row }"><span v-if="row.referenceable">{{ row.actor_name }}<span v-if="row.actor_code" class="secondary">（{{ row.actor_code }}）</span></span><el-tag v-else type="danger">{{ t('standard.collection.unavailablePrincipal') }}</el-tag></template></el-table-column>
        <el-table-column :label="t('standard.revision.number')" width="100"><template #default="{ row }">{{ eventRevisionLabel(row) }}</template></el-table-column>
        <el-table-column :label="t('standard.common.createdAt')" min-width="190"><template #default="{ row }">{{ formatDateTime(row.created_at) }}</template></el-table-column>
      </el-table>
      <el-pagination v-if="eventTotal" class="pagination" :total="eventTotal" :page-size="eventPageSize" :current-page="eventPage" layout="total, prev, pager, next" @current-change="loadEventPage" />
    </el-card>

    <el-card v-if="revisions.length" shadow="never" class="section">
      <template #header>{{ t('standard.revision.history') }}</template>
      <el-table :data="revisions" stripe>
        <el-table-column :label="t('standard.revision.number')" width="100"><template #default="{ row }">R{{ row.revision_no }}</template></el-table-column>
        <el-table-column :label="t('standard.common.status')" width="120"><template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('standard.revision.changeSummary')" prop="change_summary" min-width="240" />
        <el-table-column :label="t('standard.collection.memberCount')" width="110"><template #default="{ row }">{{ row.members?.length || 0 }}</template></el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="memberDialog" :title="t('standard.collection.addMember')" width="560px">
      <el-form label-width="100px">
        <el-form-item :label="t('standard.common.type')"><el-select v-model="memberCandidate.type" style="width:100%" @change="loadMemberCandidates"><el-option v-for="type in memberTypes" :key="type" :value="type" :label="memberTypeLabel(type)" /></el-select></el-form-item>
        <el-form-item :label="t('standard.collection.member')"><el-select v-model="memberCandidate.id" filterable style="width:100%"><el-option v-for="item in memberCandidates" :key="item.id" :value="item.id" :label="item.code ? `${item.name}（${item.code}）` : item.name" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="memberDialog=false">{{ t('standard.common.cancel') }}</el-button><el-button type="primary" @click="addMember">{{ t('standard.common.confirm') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="assignmentDialog" :title="t('standard.collection.configureAssignments')" width="680px">
      <div class="assignment-add"><el-select v-model="assignmentCandidate.principal_id" filterable remote :remote-method="searchUsers" :loading="userSearching" :placeholder="t('standard.collection.searchUser')" style="flex:1"><el-option v-for="user in userCandidates" :key="user.id" :value="user.id" :label="user.code ? `${user.name}（${user.code}）` : user.name" /></el-select><el-select v-model="assignmentCandidate.role" style="width:150px"><el-option v-for="role in roles" :key="role" :value="role" :label="roleLabel(role)" /></el-select><el-button @click="addAssignment">{{ t('standard.collection.addAssignment') }}</el-button></div>
      <el-table :data="assignmentDraft" stripe>
        <el-table-column :label="t('standard.collection.principal')" min-width="210"><template #default="{ row }"><span v-if="row.referenceable !== false">{{ row.principal_name }}</span><el-tag v-else type="danger">{{ t('standard.collection.unavailablePrincipal') }}</el-tag></template></el-table-column>
        <el-table-column :label="t('standard.collection.roleLabel')" width="150"><template #default="{ row }">{{ roleLabel(row.role) }}</template></el-table-column>
        <el-table-column :label="t('standard.common.actions')" width="90"><template #default="{ $index }"><el-button link type="danger" @click="assignmentDraft.splice($index,1)">{{ t('standard.common.remove') }}</el-button></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="assignmentDialog=false">{{ t('standard.common.cancel') }}</el-button><el-button type="primary" :loading="savingAssignments" @click="saveAssignments">{{ t('standard.common.save') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { codeSetAPI, documentAPI, elementAPI, glossaryAPI, metricAPI, standardCollectionAPI } from '../api/standard'
import { useAuthStore } from '../store/auth'
import { getStandardErrorMessage } from '../utils/apiError'
import { navigateStandardRoute } from '../utils/moduleNavigation'

const route = useRoute(), router = useRouter(), authStore = useAuthStore(), { t } = useI18n()
const loading = ref(false), saving = ref(false), acting = ref(false), savingAssignments = ref(false)
const collection = ref(null), revisions = ref([]), events = ref([]), eventTotal = ref(0), eventPage = ref(1), eventPageSize = 20, memberDialog = ref(false), assignmentDialog = ref(false), memberCandidates = ref([]), userCandidates = ref([]), userSearching = ref(false)
const memberTypes = ['element','code_set','metric','glossary','document'], roles = ['owner','maintainer','reviewer']
const draftForm = reactive({ name:'', description:'', change_summary:'', members:[] })
const memberCandidate = reactive({ type:'element', id:null }), assignmentCandidate = reactive({ principal_id:null, role:'maintainer' })
const assignmentDraft = ref([])
const draft = computed(() => collection.value?.draft_revision || null)
const workingRevision = computed(() => draft.value || collection.value?.current_revision || null)
const myRoles = computed(() => collection.value?.my_roles || [])
const canMaintain = computed(() => authStore.hasPermission('standard.collection.update') && (myRoles.value.includes('owner') || myRoles.value.includes('maintainer')))
const canReview = computed(() => authStore.hasPermission('standard.collection.publish') && myRoles.value.includes('reviewer'))
const canAssign = computed(() => authStore.hasPermission('standard.collection_assignment.update') && myRoles.value.includes('owner'))
const isEditable = computed(() => canMaintain.value && draft.value?.status === 'draft')
const canCreateDraft = computed(() => canMaintain.value && !draft.value && collection.value?.current_revision)
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const statusType = status => ({draft:'info',in_review:'warning',published:'success',withdrawn:'danger'}[status] || 'info')
const roleLabel = role => t(`standard.collection.role.${role}`)
const memberTypeLabel = type => t(`standard.collection.memberType.${type}`)
const eventTypeLabel = type => t(`standard.collection.eventType.${type}`)
const eventRevisionLabel = event => { const revision = revisions.value.find(item => item.id === event.revision_id); return event.detail?.revision_no ? `R${event.detail.revision_no}` : (revision ? `R${revision.revision_no}` : '-') }
const formatDateTime = value => value ? new Date(value).toLocaleString() : '-'
const syncDraftForm = () => { const revision = workingRevision.value; Object.assign(draftForm,{name:revision?.name || '',description:revision?.description || '',change_summary:revision?.change_summary || '',members:(revision?.members || []).map(item=>({...item}))}) }
const loadEvents = async () => { const response = await standardCollectionAPI.listEvents(route.params.id,{page:eventPage.value,page_size:eventPageSize}); events.value=response.data || []; eventTotal.value=response.total || 0 }
const loadEventPage = async page => { eventPage.value=page; try { await loadEvents() } catch(error) { ElMessage.error(getStandardErrorMessage(error,t,'standard.common.loadFailed')) } }
const load = async () => { loading.value=true; try { const [detail, revisionItems] = await Promise.all([standardCollectionAPI.get(route.params.id), standardCollectionAPI.listRevisions(route.params.id)]); collection.value=detail; revisions.value=revisionItems || []; syncDraftForm(); await loadEvents() } catch(error) { ElMessage.error(getStandardErrorMessage(error,t,'standard.common.loadFailed')) } finally { loading.value=false } }
const back = () => navigateStandardRoute(router,'/collections')
const saveDraft = async () => { if (!draftForm.name.trim() || !draftForm.description.trim() || !draftForm.change_summary.trim()) { ElMessage.warning(t('standard.collection.requiredFields')); return } saving.value=true; try { collection.value=await standardCollectionAPI.updateRevision(collection.value.id,draft.value.id,{version:collection.value.version,name:draftForm.name,description:draftForm.description,change_summary:draftForm.change_summary,members:draftForm.members.map(({member_type,member_id})=>({member_type,member_id}))}); ElMessage.success(t('standard.common.saveSuccess')); await load() } catch(error) { ElMessage.error(getStandardErrorMessage(error,t)) } finally { saving.value=false } }
const createDraft = async () => { try { const { value }=await ElMessageBox.prompt(t('standard.collection.newRevisionPrompt'),t('standard.revision.newDraft'),{inputValidator:value=>Boolean(value?.trim()) || t('standard.revision.changeSummaryRequired')}); collection.value=await standardCollectionAPI.createRevision(collection.value.id,{version:collection.value.version,change_summary:value}); await load() } catch {} }
const runAction = async (message, action) => { try { await ElMessageBox.confirm(message,t('standard.common.hint'),{type:'warning'}); acting.value=true; collection.value=await action(); await load() } catch(error) { if (error !== 'cancel' && error !== 'close') ElMessage.error(getStandardErrorMessage(error,t)) } finally { acting.value=false } }
const submitDraft = () => runAction(t('standard.collection.confirmSubmit'),()=>standardCollectionAPI.submitRevision(collection.value.id,draft.value.id,collection.value.version))
const returnDraft = () => runAction(t('standard.collection.confirmReturn'),()=>standardCollectionAPI.returnRevision(collection.value.id,draft.value.id,collection.value.version))
const publishDraft = () => runAction(t('standard.collection.confirmPublish'),()=>standardCollectionAPI.publishRevision(collection.value.id,draft.value.id,collection.value.version))
const responseItems = response => Array.isArray(response) ? response : (response?.data || [])
const loadMemberCandidates = async () => { memberCandidate.id=null; const api={element:elementAPI,code_set:codeSetAPI,metric:metricAPI,glossary:glossaryAPI,document:documentAPI}[memberCandidate.type]; try { memberCandidates.value=responseItems(await api.list({page:1,page_size:100})).map(item=>({id:item.id,code:item.code || '',name:item.draft_revision?.name || item.current_revision?.name || item.name || item.code})) } catch(error) { ElMessage.error(getStandardErrorMessage(error,t)) } }
const openMemberDialog = () => { memberCandidate.type='element'; memberCandidate.id=null; memberDialog.value=true; loadMemberCandidates() }
const addMember = () => { const item=memberCandidates.value.find(candidate=>candidate.id===memberCandidate.id); if (!item) return; if (draftForm.members.some(member=>member.member_type===memberCandidate.type && member.member_id===item.id)) { ElMessage.warning(t('standard.collection.memberExists')); return } draftForm.members.push({member_type:memberCandidate.type,member_id:item.id,name:item.name,code:item.code}); memberDialog.value=false }
const openAssignmentDialog = () => { assignmentDraft.value=collection.value.assignments.map(item=>({...item})); assignmentCandidate.principal_id=null; assignmentCandidate.role='maintainer'; assignmentDialog.value=true; searchUsers('') }
const searchUsers = async search => { userSearching.value=true; try { const response=await standardCollectionAPI.listUserCandidates({search,page:1,page_size:20}); userCandidates.value=response.data || [] } catch(error) { ElMessage.error(getStandardErrorMessage(error,t)) } finally { userSearching.value=false } }
const addAssignment = () => { const user=userCandidates.value.find(item=>item.id===assignmentCandidate.principal_id); if (!user) return; if (assignmentDraft.value.some(item=>item.principal_id===user.id && item.role===assignmentCandidate.role)) { ElMessage.warning(t('standard.collection.assignmentExists')); return } assignmentDraft.value.push({principal_id:user.id,principal_name:user.name,principal_code:user.code,referenceable:true,role:assignmentCandidate.role}); assignmentCandidate.principal_id=null }
const saveAssignments = async () => { if (!assignmentDraft.value.some(item=>item.role==='owner')) { ElMessage.warning(t('standard.collection.ownerRequired')); return } savingAssignments.value=true; try { collection.value=await standardCollectionAPI.replaceAssignments(collection.value.id,{version:collection.value.version,assignments:assignmentDraft.value.map(({principal_id,role})=>({principal_id,role}))}); assignmentDialog.value=false; ElMessage.success(t('standard.common.saveSuccess')); await load() } catch(error) { ElMessage.error(getStandardErrorMessage(error,t)) } finally { savingAssignments.value=false } }
onMounted(load)
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header,.title-row,.actions,.card-header,.assignment-add{display:flex;align-items:center}.page-header,.card-header{justify-content:space-between}.page-header{margin-bottom:16px}.title-row{gap:12px}.title-row h2{margin:0}.title-row p{margin:4px 0 0;color:var(--addp-text-secondary)}.actions{gap:8px}.section{margin-bottom:14px}.secondary{color:var(--addp-text-secondary)}.assignment-add{gap:10px;margin-bottom:12px}.pagination{display:flex;justify-content:flex-end;margin-top:14px}.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
</style>
