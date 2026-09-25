<template>
  <div class="page" v-loading="loading || saving || publishing || offlining" data-testid="data-application-editor">
    <header class="page-header editor-topbar">
      <div class="editor-identity">
        <el-button text @click="router.push('/applications')">{{ t('workbench.studio.back') }}</el-button>
        <div><h2>{{ application.name || t('workbench.createDataApplication') }}</h2><span class="save-status">{{ dirty ? t('workbench.studio.unsaved') : isCreate ? t('workbench.studio.notCreated') : t('workbench.studio.saved') }}</span></div>
      </div>
      <div class="actions">
        <el-button data-testid="draft-preview-action" :disabled="!application.snapshot.components.length" @click="openDraftPreview">{{ t('workbench.previewApplication') }}</el-button>
        <el-button :loading="saving" type="primary" :disabled="!application.snapshot.components.length" @click="save">{{ isCreate ? t('workbench.createDraft') : t('workbench.saveDraft') }}</el-button>
        <el-button v-if="application.publication_status === 'unpublished' && !isCreate" :disabled="dirty" :loading="publishing" type="success" @click="publish">{{ t('workbench.publish') }}</el-button>
        <el-button v-else-if="application.publication_status === 'offline' || application.has_unpublished_changes" :disabled="dirty" :loading="publishing" type="success" @click="publish">{{ t('workbench.publishRevision') }}</el-button>
        <el-button v-if="application.publication_status === 'published'" @click="openDelivery">{{ t('workbench.deliver') }}</el-button>
      </div>
    </header>

    <div v-if="application.snapshot.components.length" class="studio-toolbar">
      <div class="actions">
        <el-button type="primary" plain @click="openAddComponent()">{{ t('workbench.addComponent') }}</el-button>
        <el-button v-for="panel in ['filters', 'interactions', 'presets', 'page']" :key="panel" @click="settingsPanel = panel">{{ t(`workbench.studio.panels.${panel}`) }}</el-button>
      </div>
      <span v-if="application.snapshot.components.length" class="muted">{{ t('workbench.studio.componentCount', { count: application.snapshot.components.length }) }}</span>
    </div>

    <section v-if="!application.snapshot.components.length" class="studio-start" data-testid="application-start">
      <div class="start-intro"><span class="eyebrow">{{ t('workbench.studio.startEyebrow') }}</span><h1>{{ t('workbench.studio.startTitle') }}</h1><p>{{ t('workbench.studio.startHint') }}</p></div>
      <div class="starter-options">
        <button v-for="type in ['table', 'chart', 'value', 'map']" :key="type" class="starter-option" :data-testid="`start-${type}`" @click="openAddComponent(type)">
          <el-icon :size="30"><component :is="rendererIcons[type]" /></el-icon>
          <strong>{{ t(`workbench.renderers.${type}`) }}</strong><span>{{ t(`workbench.studio.starters.${type}`) }}</span>
          <span class="starter-go">{{ t('workbench.studio.chooseService') }} →</span>
        </button>
      </div>
      <div class="start-footer"><span>{{ t('workbench.studio.spatialHint') }}</span><el-button :disabled="application.snapshot.components.length > 0" @click="spatialWizardVisible = true">{{ t('workbench.spatialWizard.open') }}</el-button></div>
      <ol class="creation-steps"><li>{{ t('workbench.studio.steps.data') }}</li><li>{{ t('workbench.studio.steps.display') }}</li><li>{{ t('workbench.studio.steps.conditions') }}</li><li>{{ t('workbench.studio.steps.deliver') }}</li></ol>
    </section>

        <section v-if="isCreate && application.snapshot.components.length" class="creation-guide" data-testid="application-creation-guide">
          <strong>{{ t('workbench.creationGuide.title') }}</strong>
          <p>{{ t('workbench.creationGuide.hint') }}</p>
          <p v-if="application.snapshot.selection_bindings.length" class="muted">{{ t('workbench.creationGuide.configured', { count: application.snapshot.selection_bindings.length }) }}</p>
          <el-form v-if="creationSelectionTargets.length" label-position="top" @submit.prevent>
            <el-form-item :label="t('workbench.creationGuide.target')">
              <el-select v-model="creationTargetKey" class="full" :placeholder="t('workbench.applicationParameter')">
                <el-option v-for="parameter in creationSelectionTargets" :key="parameter.key" :value="parameter.key" :label="parameterOptionLabel(parameter)" />
              </el-select>
            </el-form-item>
            <el-button type="primary" :disabled="!creationTargetKey" @click="openCreationSelectionList">{{ t('workbench.selectionWizard.title') }}</el-button>
          </el-form>
          <div class="actions"><el-button @click="openDraftPreview">{{ t('workbench.creationGuide.preview') }}</el-button><span class="muted">{{ t('workbench.creationGuide.previewHint') }}</span></div>
        </section>

    <div v-if="application.snapshot.components.length" class="studio-workspace">
      <section class="studio-stage">

        <div class="stage-toolbar"><span>{{ t('workbench.studio.canvasHint') }}</span><el-button size="small" @click="editorCanvas?.refresh()">{{ t('workbench.studio.refreshPreview') }}</el-button></div>
        <DataApplicationCanvas v-if="!loading" :key="deliveryDialogContext" ref="editorCanvas" :application="application" mode="draft-preview" editable :selected-component-id="selectedComponentID" @select-component="selectedComponentID = $event" @edit-component="openEditComponent" @move-component="moveComponent" />
      </section>
      <aside class="studio-inspector" data-testid="component-inspector">
        <div class="inspector-section">
          <div class="inspector-heading"><strong>{{ t('workbench.studio.components') }}</strong><el-tag size="small" type="info">{{ application.snapshot.components.length }}</el-tag></div>
          <div class="component-outline">
            <div v-for="item in orderedComponents" :key="item.id" class="outline-row" :class="{ active: item.id === selectedComponentID }" data-testid="application-component" :data-component-id="item.id">
              <button class="outline-select" :aria-pressed="item.id === selectedComponentID" @click="selectComponent(item.id)"><el-icon><component :is="rendererIcons[item.renderer_type]" /></el-icon><span>{{ item.title }}</span></button>
              <el-button text size="small" data-testid="edit-component-action" :aria-label="t('workbench.editComponent')" @click="openEditComponent(item)"><el-icon><Edit /></el-icon></el-button>
            </div>
          </div>
        </div>
        <div v-if="selectedComponent" class="inspector-section">
          <div class="inspector-heading"><strong>{{ t('workbench.studio.properties') }}</strong><el-tag size="small">{{ t(`workbench.renderers.${selectedComponent.renderer_type}`) }}</el-tag></div>
          <el-form label-position="top" size="small">
            <el-form-item :label="t('workbench.componentTitle')"><el-input v-model="selectedComponent.title" maxlength="200" /></el-form-item>
            <el-form-item :label="t('workbench.description')"><el-input v-model="selectedComponent.description" type="textarea" :rows="2" maxlength="2000" /></el-form-item>
            <el-form-item :label="t('workbench.width')"><div class="size-options"><el-button v-for="size in [4, 6, 12]" :key="size" :type="placement(selectedComponent.id).width === size ? 'primary' : 'default'" @click="resizeComponent({ width: size })">{{ t(`workbench.studio.widths.${size}`) }}</el-button></div></el-form-item>
            <el-form-item :label="t('workbench.height')"><el-input-number :model-value="placement(selectedComponent.id).height" :min="3" :max="24" @change="resizeComponent({ height: $event })" /></el-form-item>
            <el-form-item :label="t('workbench.studio.order')"><el-button :disabled="selectedIndex === 0" @click="moveComponent({ componentID: selectedComponent.id, direction: -1 })">{{ t('workbench.studio.moveUp') }}</el-button><el-button :disabled="selectedIndex === orderedComponents.length - 1" @click="moveComponent({ componentID: selectedComponent.id, direction: 1 })">{{ t('workbench.studio.moveDown') }}</el-button></el-form-item>
          </el-form>
          <el-button class="full inspector-configure" type="primary" plain @click="openEditComponent(selectedComponent)">{{ t('workbench.studio.configureDisplay') }}</el-button>
          <el-button class="full inspector-configure" @click="openEditComponent(selectedComponent, true)">{{ t('workbench.studio.duplicateComponent') }}</el-button>
          <el-button text type="danger" @click="removeComponent(selectedComponent)">{{ t('workbench.studio.removeComponent') }}</el-button>
        </div>
        <div class="inspector-section"><strong>{{ t('workbench.studio.arrange') }}</strong><div class="layout-presets"><el-button size="small" @click="arrangeLayout(1)">{{ t('workbench.studio.oneColumn') }}</el-button><el-button size="small" @click="arrangeLayout(2)">{{ t('workbench.studio.twoColumns') }}</el-button></div></div>
      </aside>
    </div>

    <el-drawer v-model="settingsVisible" :title="t(`workbench.studio.panels.${settingsPanel || 'page'}`)" size="min(920px, 100vw)" class="studio-settings" destroy-on-close>
      <div class="settings-content">
    <el-card v-if="settingsPanel === 'page'">
      <el-form label-position="top">
        <el-row :gutter="16">
          <el-col :xs="24" :md="6"><el-form-item :label="t('workbench.name')"><el-input v-model="application.name" maxlength="200" /></el-form-item></el-col>
          <el-col :xs="24" :md="6"><el-form-item :label="t('workbench.pageTitle')"><el-input v-model="application.snapshot.page.title" maxlength="200" /></el-form-item></el-col>
          <el-col :xs="24" :md="6">
            <el-form-item :label="t('workbench.displayMode')">
              <el-select v-model="application.snapshot.page.display_mode" class="full" @change="displayModeChanged">
                <el-option :label="t('workbench.displayModes.desktop')" value="desktop" />
                <el-option :label="t('workbench.displayModes.wallboard')" value="wallboard" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="6">
            <el-form-item :label="t('workbench.refreshPolicy')">
              <el-select v-model="application.snapshot.page.refresh_interval_seconds" class="full" :disabled="application.snapshot.page.display_mode !== 'wallboard'" @change="refreshPolicyChanged">
                <el-option :label="t('workbench.refreshIntervals.off')" :value="0" />
                <el-option :label="t('workbench.refreshIntervals.seconds30')" :value="30" />
                <el-option :label="t('workbench.refreshIntervals.minute1')" :value="60" />
                <el-option :label="t('workbench.refreshIntervals.minutes5')" :value="300" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="t('workbench.description')"><el-input v-model="application.description" type="textarea" maxlength="2000" /></el-form-item>
        <el-form-item :label="t('workbench.presentationSections')">
          <div class="presentation-sections">
            <el-checkbox-group v-model="application.snapshot.page.visible_sections" :disabled="application.snapshot.page.display_mode !== 'wallboard'" @change="visibleSectionsChanged">
              <el-checkbox value="title">{{ t('workbench.presentationSectionOptions.title') }}</el-checkbox>
              <el-checkbox value="parameters">{{ t('workbench.presentationSectionOptions.parameters') }}</el-checkbox>
              <el-checkbox value="query_actions">{{ t('workbench.presentationSectionOptions.queryActions') }}</el-checkbox>
            </el-checkbox-group>
            <span>{{ t('workbench.presentationSectionsHint') }}</span>
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <section v-if="settingsPanel === 'filters'" class="filter-settings">
      <div class="settings-intro"><h3>{{ t('workbench.studio.filtersTitle') }}</h3><p>{{ t('workbench.studio.filtersHint') }}</p></div>
      <el-empty v-if="!application.snapshot.parameters.length" :description="t('workbench.noApplicationParameters')" />
      <div class="filter-cards">
        <article v-for="parameter in application.snapshot.parameters" :key="parameter.key" class="filter-card" data-testid="filter-card">
          <el-form label-position="top">
            <el-form-item :label="t('workbench.studio.filterName')"><el-input v-model="parameter.label" /></el-form-item>
            <el-form-item :label="t('workbench.studio.initialValue')">
              <ApplicationSelectionValue :refresh-key="nameRevision" :snapshot="application.snapshot" :descriptors="descriptorByComponent" :parameter-key="parameter.key" :parameter-values="defaultParameterValues" v-if="parameterSources(parameter.key).length" :value="parameter.default_value" :sources="parameterSources(parameter.key)" :required="parameter.required" :disabled="!parameterDomain(parameter.key).ready" @choose="openValueSelection(parameter, $event, null)" @clear="parameter.default_value = null" />
              <ParameterValueInput v-else v-model="parameter.default_value" :control-type="parameter.control_type" :options="parameterDomain(parameter.key).options" :disabled="!parameterDomain(parameter.key).ready" />
            </el-form-item>
            <template v-if="parameterSources(parameter.key).length">
              <el-form-item :label="t('workbench.selectionValue.source')" data-testid="display-source">
                <el-select :model-value="parameter.display_source?.source_component_id || ''" clearable class="full" :placeholder="t('workbench.selectionValue.configure')" @update:model-value="chooseDisplaySource(parameter, $event)">
                  <el-option v-for="source in parameterSources(parameter.key)" :key="source.id" :value="source.id" :label="source.title" :disabled="!displayFields(parameter, source.id).length" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="parameter.display_source" :label="t('workbench.selectionValue.field')" data-testid="display-label-field">
                <el-select v-model="parameter.display_source.label_field" class="full"><el-option v-for="field in displayFields(parameter, parameter.display_source.source_component_id)" :key="field.name" :value="field.name" :label="selectionFieldLabel(parameter.display_source.source_component_id, field.name)" /></el-select>
              </el-form-item>
            </template>
            <div class="filter-required"><span>{{ t('workbench.required') }}</span><el-switch v-model="parameter.required" :aria-label="t('workbench.required')" /></div>
          </el-form>
          <div class="filter-impact"><span>{{ t('workbench.studio.usedBy') }}</span><el-tag v-for="title in parameterComponentNames(parameter.key)" :key="title" size="small" type="info">{{ title }}</el-tag><span v-if="!parameterComponentNames(parameter.key).length">{{ t('workbench.studio.unusedFilter') }}</span></div>
          <el-button link type="primary" :disabled="!parameterDomain(parameter.key).ready" @click="openSelectionList(parameter)">{{ t('workbench.selectionWizard.title') }}</el-button>
          <el-button link type="primary" :disabled="!parameterDomain(parameter.key).ready || !availableSelectionSourceComponents().length" @click="configureParameterSelection(parameter.key)">{{ t('workbench.studio.chooseFromComponent') }}</el-button>
          <p v-if="parameterComponentNames(parameter.key).length && !parameterDomain(parameter.key).ready" class="configuration-warning">{{ t('workbench.parameterOptionsUnavailable', { components: parameterDomain(parameter.key).components.join(', ') }) }}</p>
        </article>
      </div>
      <div v-if="bindingRows.length" class="settings-intro"><h3>{{ t('workbench.studio.shareTitle') }}</h3><p>{{ t('workbench.studio.shareHint') }}</p></div>
      <article v-for="group in bindingGroups" :key="group.component.id" class="binding-group" data-testid="filter-binding-group">
        <strong>{{ group.component.title }}</strong>
        <div v-for="row in group.rows" :key="row.binding.component_parameter_key" class="binding-row">
          <span>{{ row.componentParameterLabel }}</span><span class="muted">←</span>
          <el-select v-model="row.binding.application_parameter_key" :aria-label="row.componentParameterLabel" class="full">
            <el-option v-for="parameter in application.snapshot.parameters" :key="parameter.key" :label="parameterOptionLabel(parameter)" :value="parameter.key" :disabled="parameter.key !== row.binding.application_parameter_key && !canBindApplicationParameter(application.snapshot, descriptorByComponent, row.binding, parameter)" />
          </el-select>
        </div>
      </article>
    </section>

    <el-card v-if="settingsPanel === 'presets'">
      <template #header>
        <div class="card-header">
          <div><strong>{{ t('workbench.applicationParameterPresets') }}</strong><span>{{ t('workbench.applicationParameterPresetsHint') }}</span></div>
          <el-button link type="primary" :disabled="application.snapshot.parameters.length === 0 || application.snapshot.parameter_presets.length >= 20" @click="addParameterPreset">{{ t('workbench.addParameterPreset') }}</el-button>
        </div>
      </template>
      <el-empty v-if="application.snapshot.parameter_presets.length === 0" :description="t('workbench.noApplicationParameterPresets')" />
      <div v-else class="parameter-presets">
        <div v-for="(preset, presetIndex) in application.snapshot.parameter_presets" :key="presetIndex" class="parameter-preset-card">
          <div class="parameter-preset-heading">
            <el-input v-model="preset.name" maxlength="100" :placeholder="t('workbench.parameterPresetName')" />
            <el-input v-model="preset.key" maxlength="64" :placeholder="t('workbench.parameterPresetKey')" />
            <el-button link type="danger" @click="removeParameterPreset(presetIndex)">{{ t('workbench.delete') }}</el-button>
          </div>
          <div class="parameter-grid">
            <component :is="parameterSources(parameter.key).length ? 'div' : 'label'" v-for="parameter in application.snapshot.parameters" :key="parameter.key" class="parameter-field">
              <span>{{ parameter.label }}<em v-if="parameter.required">*</em></span>
              <ApplicationSelectionValue :refresh-key="nameRevision" :snapshot="application.snapshot" :descriptors="descriptorByComponent" :parameter-key="parameter.key" :parameter-values="preset.parameter_values" v-if="parameterSources(parameter.key).length" :value="preset.parameter_values[parameter.key]" :sources="parameterSources(parameter.key)" :required="parameter.required" :disabled="!parameterDomain(parameter.key).ready" @choose="openValueSelection(parameter, $event, preset)" @clear="preset.parameter_values[parameter.key] = null" />
              <ParameterValueInput v-else v-model="preset.parameter_values[parameter.key]" :control-type="parameter.control_type" :options="parameterDomain(parameter.key).options" :disabled="!parameterDomain(parameter.key).ready" />
            </component>
          </div>
        </div>
      </div>
    </el-card>

    <section v-if="settingsPanel === 'interactions'" class="interaction-settings">
      <div class="settings-intro"><h3>{{ t('workbench.studio.interactionsTitle') }}</h3><p>{{ t('workbench.studio.interactionsHint') }}</p></div>
      <el-empty v-if="!application.snapshot.selection_bindings.length && !selectionDraft" :description="t('workbench.noSelectionBindings')" />
      <div class="selection-bindings">
        <article v-for="(binding, index) in application.snapshot.selection_bindings" :key="binding.source_component_id" class="selection-binding-card" data-testid="interaction-card">
          <div class="inspector-heading"><strong>{{ t('workbench.studio.whenClick', { component: componentTitle(binding.source_component_id) }) }}</strong><div><el-button link type="primary" :disabled="Boolean(selectionDraft)" @click="editSelectionBinding(index)">{{ t('workbench.studio.editInteraction') }}</el-button><el-button link type="danger" :disabled="Boolean(selectionDraft)" @click="removeSelectionBinding(index)">{{ t('workbench.delete') }}</el-button></div></div>
          <div v-for="assignment in binding.assignments" :key="assignment.application_parameter_key" class="interaction-summary"><span>{{ selectionFieldLabel(binding.source_component_id, assignment.source_field) }}</span><span>→</span><strong>{{ parameterLabel(assignment.application_parameter_key) }}</strong></div>
          <p class="impact-summary">{{ t('workbench.studio.refreshes', { components: selectionAffectedComponentNames(binding) || t('workbench.none') }) }}</p>
        </article>
      </div>
      <el-button v-if="!selectionDraft" type="primary" plain :disabled="!availableSelectionSourceComponents().length" @click="addSelectionBinding()">{{ t('workbench.addSelectionBinding') }}</el-button>
      <p v-if="!selectionDraft && !availableSelectionSourceComponents().length" class="muted">{{ t('workbench.studio.noInteractionSource') }}</p>
      <article v-if="selectionDraft" class="interaction-builder" data-testid="interaction-builder">
        <el-form label-position="top">
          <el-form-item :label="t('workbench.studio.chooseSource')" data-testid="interaction-source">
            <el-select v-model="selectionDraft.source_component_id" class="full" :placeholder="t('workbench.sourceComponent')" @change="selectionSourceChanged">
              <el-option v-for="component in availableSelectionSourceComponents(selectionEditingSource)" :key="component.id" :label="component.title" :value="component.id" />
            </el-select>
          </el-form-item>
          <div v-for="(assignment, index) in selectionDraft.assignments" :key="index" class="assignment-row" data-testid="interaction-assignment">
            <el-form-item :label="t('workbench.studio.chooseField')" data-testid="interaction-field"><el-select v-model="assignment.source_field" class="full" :disabled="!selectionDraft.source_component_id" :placeholder="t('workbench.sourceField')" @change="selectionFieldChanged(selectionDraft, assignment)"><el-option v-for="field in sourceFields(selectionDraft.source_component_id)" :key="field.name" :label="selectionFieldLabel(selectionDraft.source_component_id, field.name)" :value="field.name" /></el-select></el-form-item>
            <el-form-item :label="t('workbench.studio.chooseTarget')" data-testid="interaction-target"><el-select v-model="assignment.application_parameter_key" class="full" :disabled="!assignment.source_field" :placeholder="t('workbench.applicationParameter')"><el-option v-if="!assignment.source_field && assignment.application_parameter_key" :value="assignment.application_parameter_key" :label="parameterLabel(assignment.application_parameter_key)" disabled /><el-option v-for="parameter in selectionParameterOptions(selectionDraft, assignment)" :key="parameter.key" :label="parameterOptionLabel(parameter)" :value="parameter.key" /></el-select></el-form-item>
            <el-button text type="danger" :disabled="selectionDraft.assignments.length === 1" @click="selectionDraft.assignments.splice(index, 1)">{{ t('workbench.delete') }}</el-button>
          </div>
        </el-form>
        <el-button link type="primary" :disabled="!selectionDraft.source_component_id" @click="addSelectionAssignment">{{ t('workbench.addSelectionAssignment') }}</el-button>
        <p class="impact-summary" data-testid="interaction-impact">{{ t('workbench.studio.refreshes', { components: selectionAffectedComponentNames(selectionDraft) || t('workbench.studio.awaitingTarget') }) }}</p>
        <p class="muted">{{ t('workbench.studio.rawValueHint') }}</p>
        <div class="actions"><el-button type="primary" :disabled="!selectionDraftValid" @click="applySelectionBinding">{{ t('workbench.studio.applyInteraction') }}</el-button><el-button @click="selectionDraft = null">{{ t('workbench.cancel') }}</el-button></div>
      </article>
      <p v-if="application.snapshot.selection_bindings.length" class="muted">{{ t('workbench.studio.tryInteraction') }}</p>
    </section>

        <el-button v-if="settingsPanel === 'page' && application.publication_status === 'published'" :loading="offlining" type="danger" plain @click="offline">{{ t('workbench.offline') }}</el-button>
      </div>
      <template #footer><el-button type="primary" :disabled="Boolean(selectionDraft)" @click="settingsPanel = ''">{{ t('workbench.studio.done') }}</el-button></template>
    </el-drawer>
    <ApplicationComponentEditor v-model="componentEditorVisible" :component="editingComponent" :duplicate="duplicatingComponent" :initial-renderer="initialRenderer" :selection-target="selectionListTarget" :snapshot="application.snapshot" :descriptors="descriptorByComponent" @save="saveComponent" />
    <SpatialExplorationWizard v-model="spatialWizardVisible" @apply="applySpatialExploration" />
    <DataApplicationDeliveryDialog :key="deliveryDialogContext" ref="deliveryDialog" />
    <el-dialog v-model="draftPreviewVisible" class="draft-preview-dialog" fullscreen destroy-on-close :title="t('workbench.draftPreviewTitle')">
      <template #header="{ titleId, titleClass }">
        <div class="draft-preview-heading">
          <div><h2 :id="titleId" :class="titleClass">{{ t('workbench.draftPreviewTitle') }}</h2><p>{{ t('workbench.studio.previewDefaultsHint') }}</p></div>
          <el-button type="primary" data-testid="apply-preview-defaults" :disabled="!draftPreviewApplication?.snapshot.parameters.length" @click="applyPreviewInitialValues">{{ t('workbench.studio.applyPreviewDefaults') }}</el-button>
        </div>
      </template>
      <DataApplicationCanvas v-if="draftPreviewApplication" ref="draftPreviewCanvas" :application="draftPreviewApplication" mode="draft-preview" embedded />
    </el-dialog>
    <ApplicationSelectionDialog :context="selectionContext" @close="selectionContext = null" @select="applyChosenValue" />
  </div>
</template>

<script setup>
import { parameterDisplayFields, assertParameterDisplaySources } from '../utils/applicationParameterDisplay.mjs'
import { Grid, Histogram, Odometer, Location, Edit } from '@element-plus/icons-vue'
import { arrangePlacements, orderedPlacements } from '../utils/applicationEditorLayout.mjs'
import { applicationParameterOptions, assertApplicationOptionValues, canBindApplicationParameter, newComponentParameterContext } from '../utils/applicationParameterOptions.mjs'
import { computed, onBeforeUnmount, reactive, ref, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createLatestRequestCoordinator, useUnsavedChangesGuard } from '@common-ui'
import { createDataApplication, getDataApplication, offlineDataApplication, publishDataApplication, updateDataApplication } from '../api/dataApplications'
import { getConsumerDescriptor } from '../api/services'
import { applicationParameterPresetsValid, buildDataApplicationPreview, commitLatestDataApplicationRequest, confirmDataApplicationAction, copySelectionBinding, createApplicationParameterPreset, dataApplicationEditorMutationContext, dataApplicationEditorRouteContext, normalizedApplicationSnapshot, synchronizeApplicationParameterPresets } from '../utils/dataApplicationDraft.mjs'
import { APPLICATION_PRESENTATION_SECTIONS, canHideApplicationParameters } from '../utils/dataApplicationRuntime.mjs'
import { applicationParameterSelectionSources, selectionParameterType, affectedSelectionComponentIDs, compatibleSelectionParameters as compatibleSelectionParameterList, selectionSourceFields } from '../utils/dataApplicationSelection.mjs'
import { navigateWorkbenchRoute } from '../utils/moduleNavigation'
import ApplicationComponentEditor from '../components/ApplicationComponentEditor.vue'
import ApplicationSelectionValue from '../components/ApplicationSelectionValue.vue'
import ApplicationSelectionDialog from '../components/ApplicationSelectionDialog.vue'
import ParameterValueInput from '../../../../common-frontend/basic/src/components/ParameterValueInput.vue'
import DataApplicationCanvas from '../components/DataApplicationCanvas.vue'
import DataApplicationDeliveryDialog from '../components/DataApplicationDeliveryDialog.vue'
import SpatialExplorationWizard from '../components/SpatialExplorationWizard.vue'

const rendererIcons = { table: Grid, chart: Histogram, value: Odometer, map: Location }
const defaultParameterValues = computed(() => Object.fromEntries(application.snapshot.parameters.map(p => [p.key, p.default_value])))
const displayFields = (parameter, sourceID) => parameterDisplayFields(application.snapshot, descriptorByComponent, parameter.key, sourceID)
function chooseDisplaySource(parameter, sourceID) {
  if (!sourceID) delete parameter.display_source
  else parameter.display_source = { source_component_id: sourceID, label_field: '' }
}
function cleanDisplaySources() {
  for (const parameter of application.snapshot.parameters) {
    if (parameter.display_source && !parameterSources(parameter.key).some(c => c.id === parameter.display_source.source_component_id)) delete parameter.display_source
  }
}
const nameRevision = ref(0)
const selectionContext = ref(null)
const parameterSources = key => applicationParameterSelectionSources(application.snapshot, key)
function openValueSelection(parameter, sourceID, preset) {
  selectionContext.value = { parameterKey: parameter.key, label: parameter.label, sourceID, preset, application: buildDataApplicationPreview(application) }
}
function applyChosenValue(value) {
  nameRevision.value += 1
  const context = selectionContext.value
  if (context.preset) context.preset.parameter_values[context.parameterKey] = value
  else application.snapshot.parameters.find(p => p.key === context.parameterKey).default_value = value
}
const settingsPanel = ref('')
const settingsVisible = computed({ get: () => Boolean(settingsPanel.value), set: value => { if (!value) settingsPanel.value = '' } })
const selectedComponentID = ref('')
const initialRenderer = ref('table')
const selectionListTarget = ref(null)
const creationTargetKey = ref('')
const editorCanvas = ref(null)
const { t } = useI18n()
const route = useRoute()
const rawRouter = useRouter()
const router = { push: (location) => navigateWorkbenchRoute(rawRouter, location), replace: (location) => navigateWorkbenchRoute(rawRouter, location, { history: 'replace' }) }
const isCreate = computed(() => route.name === 'DataApplicationCreate')
const deliveryDialogContext = computed(() => dataApplicationEditorRouteContext(route.name, route.params.id))
const loading = ref(false)
const saving = ref(false)
const publishing = ref(false)
const offlining = ref(false)
const baseline = ref('')
const componentEditorVisible = ref(false)
const spatialWizardVisible = ref(false)
const draftPreviewVisible = ref(false)
const draftPreviewApplication = ref(null)
const draftPreviewCanvas = ref(null)
const deliveryDialog = ref(null)
const editingComponent = ref(null)
const duplicatingComponent = ref(false)
const application = reactive(emptyApplication())
const descriptorByComponent = reactive({})
const orderedComponents = computed(() => orderedPlacements(application.snapshot.page.placements).map(p => application.snapshot.components.find(c => c.id === p.component_id)).filter(Boolean))
const selectedComponent = computed(() => application.snapshot.components.find(c => c.id === selectedComponentID.value))
const selectedIndex = computed(() => orderedComponents.value.findIndex(c => c.id === selectedComponentID.value))
function selectComponent(id) { selectedComponentID.value = id; editorCanvas.value?.focusComponent(id) }
function moveComponent(change) { application.snapshot.page.placements = arrangePlacements(application.snapshot.page.placements, change) }
function resizeComponent(change) { moveComponent({ componentID: selectedComponentID.value, ...change }) }
function arrangeLayout(columns) { moveComponent({ columns }) }
watch(() => application.snapshot.components.map(c => c.id), ids => {
  if (!ids.includes(selectedComponentID.value)) selectedComponentID.value = ids[0] || ''
})
const editorLoadRequests = createLatestRequestCoordinator()
const editorMutationRequests = createLatestRequestCoordinator()
const dirty = computed(() => baseline.value !== serializeDraft())
useUnsavedChangesGuard({ router: rawRouter, isDirty: () => dirty.value, shouldConfirmUpdate: (to, from) => to.name !== from.name || to.params.id !== from.params.id })
const bindingRows = computed(() => application.snapshot.parameter_bindings.map((binding) => {
  const component = application.snapshot.components.find((item) => item.id === binding.component_id)
  const componentParameter = component?.parameter_definitions?.find((item) => item.key === binding.component_parameter_key)
  return { binding, componentParameterLabel: componentParameter?.label || binding.component_parameter_key }
}))

function serializeDraft() {
  return JSON.stringify({ name: application.name, description: application.description, snapshot: application.snapshot })
}

function emptyApplication() {
  return {
    name: '', description: '', version: 0, publication_status: 'unpublished', has_unpublished_changes: false,
    snapshot: {
      schema_version: 'addp.workbench_data_application/v1',
      page: { id: crypto.randomUUID(), title: '', display_mode: 'desktop', refresh_interval_seconds: 0, visible_sections: [...APPLICATION_PRESENTATION_SECTIONS], placements: [] },
      components: [], parameters: [], parameter_presets: [], parameter_bindings: [], selection_bindings: [],
    },
  }
}

function assignApplication(data) {
  application.id = data.id
  application.name = data.name
  application.description = data.description || ''
  application.version = data.version
  application.publication_status = data.publication_status
  application.has_unpublished_changes = data.has_unpublished_changes
  application.current_revision_number = data.current_revision_number
  application.snapshot = structuredClone(data.snapshot)
  application.snapshot.parameter_presets ||= []
  baseline.value = serializeDraft()
}

function invalidateEditorContextRequests() {
  editorLoadRequests.invalidate()
  editorMutationRequests.invalidate()
}

function resetEditorRouteContext() {
  invalidateEditorContextRequests()
  loading.value = false
  saving.value = false
  publishing.value = false
  offlining.value = false
  for (const key of Object.keys(application)) delete application[key]
  Object.assign(application, emptyApplication())
  for (const key of Object.keys(descriptorByComponent)) delete descriptorByComponent[key]
  componentEditorVisible.value = false
  spatialWizardVisible.value = false
  draftPreviewVisible.value = false
  selectionContext.value = null
  settingsPanel.value = ''
  selectedComponentID.value = ''
  draftPreviewApplication.value = null
  editingComponent.value = null
  baseline.value = serializeDraft()
}

function commitEditorLoad(request, commit) {
  return commitLatestDataApplicationRequest(
    editorLoadRequests,
    request,
    dataApplicationEditorRouteContext(route.name, route.params.id),
    commit,
  )
}

function beginEditorMutation(action) {
  return editorMutationRequests.begin(
    dataApplicationEditorMutationContext(route.name, route.params.id, action),
  )
}

function commitEditorMutation(request, action, commit) {
  return commitLatestDataApplicationRequest(
    editorMutationRequests,
    request,
    dataApplicationEditorMutationContext(route.name, route.params.id, action),
    commit,
  )
}

const bindingGroups = computed(() => orderedComponents.value.map(component => ({ component, rows: bindingRows.value.filter(row => row.binding.component_id === component.id) })).filter(group => group.rows.length))
function componentTitle(id) { return application.snapshot.components.find(c => c.id === id)?.title || id }
const creationSelectionTargets = computed(() => {
  const snapshot = application.snapshot
  const sources = new Set(snapshot.selection_bindings.map(binding => binding.source_component_id))
  const assigned = new Set(snapshot.selection_bindings.flatMap(binding => binding.assignments.map(assignment => assignment.application_parameter_key)))
  return snapshot.parameters.filter(parameter => {
    const domain = parameterDomain(parameter.key)
    return !assigned.has(parameter.key) && domain.ready && !domain.options.length
      && selectionParameterType(snapshot, descriptorByComponent, parameter.key)
      && snapshot.parameter_bindings.some(binding => binding.application_parameter_key === parameter.key && !sources.has(binding.component_id))
  })
})
watch(creationSelectionTargets, targets => {
  if (!targets.some(parameter => parameter.key === creationTargetKey.value)) creationTargetKey.value = ''
})
function openCreationSelectionList() {
  const parameter = creationSelectionTargets.value.find(parameter => parameter.key === creationTargetKey.value)
  if (parameter) openSelectionList(parameter)
}

function parameterLabel(key) { return application.snapshot.parameters.find(p => p.key === key)?.label || key }
function parameterComponentNames(key) {
  return affectedSelectionComponentIDs(application.snapshot, [{ application_parameter_key: key }]).map(componentTitle)
}
function parameterOptionLabel(parameter) {
  return `${parameter.label} · ${parameterComponentNames(parameter.key).join(t('workbench.listSeparator')) || t('workbench.none')}`
}

function placement(componentID) {
  return application.snapshot.page.placements.find((item) => item.component_id === componentID)
}

function displayModeChanged(displayMode) {
  if (displayMode !== 'wallboard') {
    application.snapshot.page.refresh_interval_seconds = 0
    application.snapshot.page.visible_sections = [...APPLICATION_PRESENTATION_SECTIONS]
  }
}

function ensureVisibleSection(section) {
  const visible = new Set(application.snapshot.page.visible_sections)
  visible.add(section)
  application.snapshot.page.visible_sections = APPLICATION_PRESENTATION_SECTIONS.filter((item) => visible.has(item))
}

function refreshPolicyChanged(interval) {
  if (interval === 0) validatePresentationSections()
}

function validatePresentationSections() {
  const sections = application.snapshot.page.visible_sections
  let valid = true
  if (!sections.includes('query_actions') && application.snapshot.page.refresh_interval_seconds === 0) {
    ensureVisibleSection('query_actions')
    ElMessage.warning(t('workbench.queryActionsRequireRefresh'))
    valid = false
  }
  if (!sections.includes('parameters') && !canHideApplicationParameters(application.snapshot)) {
    ensureVisibleSection('parameters')
    ElMessage.warning(t('workbench.parametersRequireDefaults'))
    valid = false
  }
  return valid
}

function visibleSectionsChanged() {
  validatePresentationSections()
}

function addParameterPreset() {
  if (application.snapshot.parameters.length === 0 || application.snapshot.parameter_presets.length >= 20) return
  let index = application.snapshot.parameter_presets.length + 1
  const usedKeys = new Set(application.snapshot.parameter_presets.map((preset) => preset.key))
  while (usedKeys.has(`preset-${index}`)) index += 1
  application.snapshot.parameter_presets.push(createApplicationParameterPreset(
    application.snapshot,
    `preset-${index}`,
    t('workbench.newParameterPresetName', { index }),
  ))
}

function removeParameterPreset(index) {
  application.snapshot.parameter_presets.splice(index, 1)
}

function validateParameterPresets() {
  synchronizeApplicationParameterPresets(application.snapshot)
  if (applicationParameterPresetsValid(application.snapshot)) return true
  settingsPanel.value = 'presets'
  ElMessage.warning(t('workbench.invalidParameterPresets'))
  return false
}

const selectionDraft = ref(null)
const selectionEditingSource = ref('')
watch(settingsPanel, panel => { if (panel !== 'interactions') selectionDraft.value = null })

function sourceFields(componentID) {
  const descriptor = descriptorByComponent[componentID]
  const component = application.snapshot.components.find(c => c.id === componentID)
  if (!descriptor || descriptor.contract_fingerprint !== component?.contract_fingerprint) return []
  return selectionSourceFields(application.snapshot, componentID, descriptor)
}

function selectionFieldLabel(componentID, name) {
  const component = application.snapshot.components.find(c => c.id === componentID)
  const field = sourceFields(componentID).find(f => f.name === name)
  const label = component?.renderer_config?.field_presentations?.find(p => p.field === name)?.label || field?.comment
  return label && label !== name ? `${label} (${name})` : name
}

function selectionParameterOptions(binding, assignment) {
  const field = sourceFields(binding.source_component_id).find(f => f.name === assignment.source_field)
  const used = new Set(binding.assignments.filter(a => a !== assignment).map(a => a.application_parameter_key))
  return compatibleSelectionParameterList(application.snapshot, descriptorByComponent, field)
    .filter(parameter => !used.has(parameter.key) && parameterDomain(parameter.key).ready)
}

function availableSelectionSourceComponents(currentID = '') {
  const used = new Set(application.snapshot.selection_bindings.map(b => b.source_component_id).filter(id => id !== currentID))
  return application.snapshot.components.filter(component =>
    ['table', 'chart', 'map'].includes(component.renderer_type) && !used.has(component.id) && sourceFields(component.id).some(field =>
      compatibleSelectionParameterList(application.snapshot, descriptorByComponent, field).some(p => parameterDomain(p.key).ready)))
}

const selectionDraftValid = computed(() => {
  const binding = selectionDraft.value
  return Boolean(binding && availableSelectionSourceComponents(selectionEditingSource.value).some(c => c.id === binding.source_component_id)
    && binding.assignments.length && binding.assignments.every(assignment => selectionParameterOptions(binding, assignment).some(p => p.key === assignment.application_parameter_key)))
})

function configureParameterSelection(key) {
  settingsPanel.value = 'interactions'
  addSelectionBinding(key)
}
function addSelectionBinding(targetKey = '') {
  selectionEditingSource.value = ''
  selectionDraft.value = { source_component_id: '', assignments: [{ source_field: '', application_parameter_key: targetKey }] }
}
function editSelectionBinding(index) {
  selectionDraft.value = copySelectionBinding(application.snapshot.selection_bindings[index])
  selectionEditingSource.value = selectionDraft.value.source_component_id
}
function applySelectionBinding() {
  if (!selectionDraftValid.value) return
  const binding = copySelectionBinding(selectionDraft.value)
  const index = application.snapshot.selection_bindings.findIndex(b => b.source_component_id === selectionEditingSource.value)
  if (index < 0) application.snapshot.selection_bindings.push(binding)
  else application.snapshot.selection_bindings.splice(index, 1, binding)
  cleanDisplaySources()
  selectionDraft.value = null
}
function removeSelectionBinding(index) { application.snapshot.selection_bindings.splice(index, 1); cleanDisplaySources() }
function selectionSourceChanged() {
  selectionDraft.value.assignments = selectionDraft.value.assignments.map(assignment => ({ source_field: '', application_parameter_key: assignment.application_parameter_key }))
}
function addSelectionAssignment() { selectionDraft.value.assignments.push({ source_field: '', application_parameter_key: '' }) }
function selectionFieldChanged(binding, assignment) {
  if (!selectionParameterOptions(binding, assignment).some(p => p.key === assignment.application_parameter_key)) assignment.application_parameter_key = ''
}

function selectionAffectedComponentNames(binding) {
  const ids = affectedSelectionComponentIDs(application.snapshot, binding.assignments)
  return ids.map((id) => application.snapshot.components.find((component) => component.id === id)?.title || id).join(t('workbench.listSeparator'))
}

async function loadComponentDescriptor(component, loadRequest = null) {
  try {
    const { data } = await getConsumerDescriptor(component.service_ref)
    const commit = () => {
      if (data.contract_fingerprint === component.contract_fingerprint) descriptorByComponent[component.id] = data
      else delete descriptorByComponent[component.id]
    }
    if (loadRequest) commitEditorLoad(loadRequest, commit)
    else commit()
  } catch {
    const commit = () => { delete descriptorByComponent[component.id] }
    if (loadRequest) commitEditorLoad(loadRequest, commit)
    else commit()
  }
}

function normalizedSnapshot() {
  return normalizedApplicationSnapshot(application.snapshot)
}

function openDraftPreview() {
  if (!validateParameterOptions()) return
  if (!validatePresentationSections()) return
  if (!validateParameterPresets()) return
  if (!application.name.trim() || !application.snapshot.page.title.trim() || application.snapshot.components.length === 0) {
    settingsPanel.value = 'page'
    return ElMessage.warning(t('workbench.incompleteDataApplicationPreview'))
  }
  draftPreviewApplication.value = buildDataApplicationPreview(application)
  draftPreviewVisible.value = true
}

function applyPreviewInitialValues() {
  if (!draftPreviewCanvas.value || !draftPreviewVisible.value) return
  try {
    const values = draftPreviewCanvas.value.captureInitialValues()
    for (const parameter of application.snapshot.parameters) {
      if (Object.prototype.hasOwnProperty.call(values, parameter.key)) parameter.default_value = values[parameter.key]
    }
    draftPreviewVisible.value = false
    ElMessage.success(t('workbench.studio.previewDefaultsApplied'))
  } catch {
    ElMessage.warning(t('workbench.studio.previewDefaultsInvalid'))
  }
}

async function load(routeName, applicationID) {
  const targetContext = dataApplicationEditorRouteContext(routeName, applicationID)
  resetEditorRouteContext()
  const request = editorLoadRequests.begin(targetContext)
  if (routeName === 'DataApplicationCreate') {
    loading.value = false
    return
  }
  const targetID = String(applicationID || '').trim()
  loading.value = true
  try {
    const { data } = await getDataApplication(targetID)
    if (!commitEditorLoad(request, () => assignApplication(data))) return
    await Promise.all(data.snapshot.components.map((component) => loadComponentDescriptor(component, request)))
  } catch (error) {
    commitEditorLoad(request, () => {
      ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
    })
  } finally {
    commitEditorLoad(request, () => { loading.value = false })
  }
}

function parameterDomain(key) { return applicationParameterOptions(application.snapshot, descriptorByComponent, key) }
function validateParameterOptions() {
 try {
  const snapshot = normalizedSnapshot()
  assertParameterDisplaySources(snapshot, descriptorByComponent)
  assertApplicationOptionValues(snapshot, descriptorByComponent, Object.fromEntries(snapshot.parameters.map((p) => [p.key, p.default_value])))
  for (const preset of snapshot.parameter_presets || []) assertApplicationOptionValues(snapshot, descriptorByComponent, preset.parameter_values)
  return true
 } catch (error) { settingsPanel.value = 'filters'; ElMessage.error(t(error.message === 'invalid-display-source' ? 'workbench.selectionValue.invalid' : 'workbench.parameterOptionsInvalid')); return false }
}
async function save() {
  if (!validateParameterOptions()) return
  if (!validatePresentationSections()) return
  if (!validateParameterPresets()) return
  if (!application.name.trim() || !application.snapshot.page.title.trim() || application.snapshot.components.length === 0) {
    settingsPanel.value = 'page'
    return ElMessage.warning(t('workbench.incompleteDataApplication'))
  }
  const action = 'save'
  const creating = isCreate.value
  const request = beginEditorMutation(action)
  if (!commitEditorMutation(request, action, () => { saving.value = true })) return
  try {
    const payload = { name: application.name.trim(), description: application.description.trim(), snapshot: normalizedSnapshot() }
    const { data } = creating
      ? await createDataApplication(payload)
      : await updateDataApplication(application.id, { ...payload, version: application.version })
    if (!commitEditorMutation(request, action, () => {
      assignApplication(data)
      ElMessage.success(t('workbench.dataApplicationSaved'))
    })) return
    if (creating) await router.replace(`/applications/${data.id}`)
  } catch (error) {
    commitEditorMutation(request, action, () => {
      ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
    })
  } finally {
    commitEditorMutation(request, action, () => { saving.value = false })
  }
}

async function publish() {
  if (!validateParameterOptions()) return
  if (dirty.value) return ElMessage.warning(t('workbench.saveBeforePublish'))
  const action = 'publish'
  const request = beginEditorMutation(action)
  if (!await confirmDataApplicationAction(ElMessageBox.confirm, t('workbench.publishConfirm'))) return
  if (!commitEditorMutation(request, action, () => { publishing.value = true })) return
  try {
    const { data } = await publishDataApplication(application.id, application.version)
    commitEditorMutation(request, action, () => {
      assignApplication(data)
      ElMessage.success(t('workbench.published'))
      deliveryDialog.value?.open(data.id, data.current_revision_number)
    })
  } catch (error) {
    commitEditorMutation(request, action, () => {
      ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
    })
  } finally {
    commitEditorMutation(request, action, () => { publishing.value = false })
  }
}

async function offline() {
  const action = 'offline'
  const request = beginEditorMutation(action)
  if (!await confirmDataApplicationAction(ElMessageBox.confirm, t('workbench.offlineConfirm'))) return
  if (!commitEditorMutation(request, action, () => { offlining.value = true })) return
  try {
    const { data } = await offlineDataApplication(application.id, application.version)
    commitEditorMutation(request, action, () => {
      assignApplication(data)
      ElMessage.success(t('workbench.offlined'))
    })
  } catch (error) {
    commitEditorMutation(request, action, () => {
      ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
    })
  } finally {
    commitEditorMutation(request, action, () => { offlining.value = false })
  }
}

function openDelivery() {
  return deliveryDialog.value?.open(application.id, application.current_revision_number)
}

function openSelectionList(parameter) {
  settingsPanel.value = ''
  openAddComponent('table')
  selectionListTarget.value = { key: parameter.key, label: parameter.label }
}

function openAddComponent(renderer = 'table') {
  selectionListTarget.value = null
  duplicatingComponent.value = false
  initialRenderer.value = renderer
  editingComponent.value = null
  componentEditorVisible.value = true
}

function applySpatialExploration({ generated, descriptors }) {
  if (application.snapshot.components.length > 0) return ElMessage.warning(t('workbench.spatialWizard.emptyOnly'))
  application.name = generated.applicationName
  application.snapshot.page.title = generated.pageTitle
  application.snapshot.components = generated.components
  application.snapshot.parameters = generated.parameters
  application.snapshot.parameter_bindings = generated.parameterBindings
  application.snapshot.selection_bindings = generated.selectionBindings
  application.snapshot.parameter_presets = []
  application.snapshot.page.placements = generated.placements
  for (const component of generated.components) {
    const descriptor = sameServiceReference(component.service_ref, descriptors.aggregate.ref) ? descriptors.aggregate : descriptors.spatial
    descriptorByComponent[component.id] = descriptor
  }
  ElMessage.success(t('workbench.spatialWizard.applied'))
}

function sameServiceReference(left, right) {
  return left?.service_type === right?.service_type && left?.service_id === right?.service_id
}

function openEditComponent(component, duplicate = false) {
  selectionListTarget.value = null
  duplicatingComponent.value = duplicate
  editingComponent.value = structuredClone(toRaw(component))
  componentEditorVisible.value = true
}

function applicationParameterKey(componentID, parameterKey) {
  return `component_${componentID.replaceAll('-', '').slice(0, 12)}.${parameterKey}`
}

function saveComponent(nextComponent, reusedParameters, descriptor, selectionAssignment = null, displayLabelField = '') {
  if (selectionAssignment) {
    const candidate = { ...application.snapshot, components: [...application.snapshot.components, nextComponent] }
    const field = selectionSourceFields(candidate, nextComponent.id, descriptor).find(field => field.name === selectionAssignment.source_field)
    if (application.snapshot.components.some(component => component.id === nextComponent.id)
      || !parameterDomain(selectionAssignment.application_parameter_key).ready
      || !compatibleSelectionParameterList(application.snapshot, descriptorByComponent, field).some(parameter => parameter.key === selectionAssignment.application_parameter_key)) return ElMessage.warning(t('workbench.selectionWizard.unsupported'))
  }
  const index = application.snapshot.components.findIndex((item) => item.id === nextComponent.id)
  if (index < 0) {
    const context = newComponentParameterContext(application.snapshot, descriptorByComponent, nextComponent, descriptor, reusedParameters)
    for (const [key, target] of Object.entries(reusedParameters)) {
      const binding = context.snapshot.parameter_bindings.find(b => b.component_id === nextComponent.id && b.component_parameter_key === key)
      const parameter = application.snapshot.parameters.find(p => p.key === target)
      if (!binding || !parameter || !canBindApplicationParameter(context.snapshot, context.descriptors, binding, parameter)) return ElMessage.warning(t('workbench.studio.reuseUnavailable'))
    }
  }
  selectedComponentID.value = nextComponent.id
  if (index >= 0) application.snapshot.components.splice(index, 1, nextComponent)
  else {
    application.snapshot.components.push(nextComponent)
    const nextY = application.snapshot.page.placements.reduce((bottom, item) => Math.max(bottom, item.y + item.height), 0)
    application.snapshot.page.placements.push({ component_id: nextComponent.id, x: 0, y: nextY, width: 12, height: 6 })
    if (duplicatingComponent.value) {
      const source = application.snapshot.page.placements.find(p => p.component_id === editingComponent.value.id)
      if (source) {
        const placements = orderedPlacements(application.snapshot.page.placements).filter(p => p.component_id !== nextComponent.id)
        placements.splice(placements.findIndex(p => p.component_id === source.component_id) + 1, 0, { ...source, component_id: nextComponent.id })
        // arrangePlacements sorts first; give the intended order distinct temporary rows.
        application.snapshot.page.placements = arrangePlacements(placements.map((p, y) => ({ ...p, x: 0, y })))
      }
    }
  }
  const definitions = new Map((nextComponent.parameter_definitions || []).map((item) => [item.key, item]))
  application.snapshot.parameter_bindings = application.snapshot.parameter_bindings.filter((binding) => binding.component_id !== nextComponent.id || definitions.has(binding.component_parameter_key))
  for (const definition of definitions.values()) {
    let binding = application.snapshot.parameter_bindings.find((item) => item.component_id === nextComponent.id && item.component_parameter_key === definition.key)
    if (!binding) {
      const sharedKey = reusedParameters[definition.key]
      const key = sharedKey || applicationParameterKey(nextComponent.id, definition.key)
      binding = { application_parameter_key: key, component_id: nextComponent.id, component_parameter_key: definition.key }
      application.snapshot.parameter_bindings.push(binding)
      if (!sharedKey) application.snapshot.parameters.push({
        key,
        label: definition.label,
        control_type: definition.control_type,
        required: definition.required,
        ...(Object.prototype.hasOwnProperty.call(nextComponent.default_parameter_values || {}, definition.key) ? { default_value: nextComponent.default_parameter_values[definition.key] } : {}),
      })
    }
  }
  if (selectionAssignment) {
    application.snapshot.selection_bindings.push({ source_component_id: nextComponent.id, assignments: [{ ...selectionAssignment }] })
    const parameter = application.snapshot.parameters.find(p => p.key === selectionAssignment.application_parameter_key)
    if (!parameter.display_source && displayLabelField && parameterDisplayFields(application.snapshot, { ...descriptorByComponent, [nextComponent.id]: descriptor }, parameter.key, nextComponent.id).some(f => f.name === displayLabelField)) {
      parameter.display_source = { source_component_id: nextComponent.id, label_field: displayLabelField }
    }
    if (isCreate.value) {
      const targets = new Set(affectedSelectionComponentIDs(application.snapshot, [selectionAssignment]))
      const placements = orderedPlacements(application.snapshot.page.placements)
      const source = placements.find(placement => placement.component_id === nextComponent.id)
      const rest = placements.filter(placement => placement !== source)
      const targetIndex = rest.findIndex(placement => targets.has(placement.component_id))
      rest.splice(targetIndex < 0 ? rest.length : targetIndex, 0, source)
      application.snapshot.page.placements = arrangePlacements(rest.map((placement, y) => ({ ...placement, x: 0, y })))
    }
  }
  pruneUnusedApplicationParameters()
  loadComponentDescriptor(nextComponent)
  if (application.snapshot.components.length === 1) {
    if (!application.name.trim()) application.name = nextComponent.title
    if (!application.snapshot.page.title.trim()) application.snapshot.page.title = application.name
  }
}

async function removeComponent(component) {
  const action = `remove-component:${component.id}`
  const request = beginEditorMutation(action)
  const confirmed = await confirmDataApplicationAction(
    (message) => ElMessageBox.confirm(message, t('workbench.confirmTitle'), {
      confirmButtonText: t('workbench.delete'), cancelButtonText: t('workbench.cancel'), confirmButtonClass: 'el-button--danger', customClass: 'addp-message-box',
    }),
    t('workbench.deleteComponentConfirm'),
  )
  if (!confirmed) return
  commitEditorMutation(request, action, () => {
    application.snapshot.components = application.snapshot.components.filter((item) => item.id !== component.id)
    application.snapshot.page.placements = application.snapshot.page.placements.filter((item) => item.component_id !== component.id)
    application.snapshot.parameter_bindings = application.snapshot.parameter_bindings.filter((item) => item.component_id !== component.id)
    application.snapshot.selection_bindings = application.snapshot.selection_bindings.filter((item) => item.source_component_id !== component.id)
    delete descriptorByComponent[component.id]
    pruneUnusedApplicationParameters()
  })
}

function pruneUnusedApplicationParameters() {
  const used = new Set(application.snapshot.parameter_bindings.map((binding) => binding.application_parameter_key))
  application.snapshot.parameters = application.snapshot.parameters.filter((parameter) => used.has(parameter.key))
  application.snapshot.selection_bindings = application.snapshot.selection_bindings
    .map((binding) => ({ ...binding, assignments: binding.assignments.filter((assignment) => used.has(assignment.application_parameter_key)) }))
    .filter((binding) => binding.assignments.length > 0)
  cleanDisplaySources()
  synchronizeApplicationParameterPresets(application.snapshot)
}

onBeforeUnmount(invalidateEditorContextRequests)
watch(() => [route.name, route.params.id], ([routeName, applicationID]) => load(routeName, applicationID), { immediate: true })
</script>

<style scoped>
.creation-guide { display: flex; flex-direction: column; gap: 12px; padding: 18px; border: 1px solid var(--addp-border-color); border-radius: 10px; background: var(--addp-bg-secondary); }
.creation-guide p { margin: 0; line-height: 1.6; }
.creation-guide .actions { flex-wrap: wrap; }
.page{display:flex;flex-direction:column;gap:16px}.page-header,.actions,.card-header,.layout-actions,.component-heading,.component-actions,.selection-binding-heading,.selection-binding-actions,.parameter-preset-heading{display:flex;align-items:center}.page-header,.card-header,.component-heading,.selection-binding-heading{justify-content:space-between}.actions,.layout-actions,.component-actions,.selection-binding-actions{gap:8px;flex-wrap:wrap}.page-header h2{margin:0;color:var(--addp-text-primary)}.page-header p,.card-header span,.component-heading span,.component-heading small,.selection-binding-actions span,.presentation-sections>span{color:var(--addp-text-secondary)}.page-header p{margin:6px 0 0}.card-header>div:first-child{display:flex;flex-direction:column;gap:4px}.card-header span,.selection-binding-actions span,.presentation-sections>span{font-size:12px}.full{width:100%}.presentation-sections{display:flex;flex-direction:column;gap:6px}.components,.selection-bindings,.parameter-presets{display:flex;flex-direction:column;gap:12px}.component-card,.selection-binding-card,.parameter-preset-card{padding:16px;border:1px solid var(--addp-border-color);border-radius:8px}.component-heading,.selection-binding-heading,.parameter-preset-heading{margin-bottom:12px}.component-heading>div:first-child{display:flex;gap:12px;align-items:center}.parameter-preset-heading{gap:12px}.parameter-preset-heading>.el-input:first-child{flex:2}.parameter-preset-heading>.el-input:nth-child(2){flex:1}.parameter-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}.parameter-field{display:flex;flex-direction:column;gap:6px;color:var(--addp-text-primary)}.parameter-field em{color:var(--el-color-danger);font-style:normal}.selection-binding-heading>.el-select{width:min(320px,100%)}:deep(.draft-preview-dialog .el-dialog__body){padding:0;overflow:hidden}@media(max-width:900px){.component-heading,.selection-binding-heading,.parameter-preset-heading{align-items:stretch;flex-direction:column}.component-actions,.selection-binding-actions{justify-content:space-between}}

.editor-topbar { position: sticky; top: 0; z-index: 10; padding: 12px 0; background: var(--addp-bg-primary); gap: 16px; }
.editor-identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
.editor-identity h2 { font-size: 20px; overflow-wrap: anywhere; }
.save-status, .muted { font-size: 12px; color: var(--addp-text-secondary); }
.studio-toolbar, .stage-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.studio-workspace { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; align-items: start; }
.studio-stage { min-width: 0; border: 1px solid var(--addp-border-color); border-radius: 12px; overflow: hidden; }
.stage-toolbar { padding: 12px 16px; background: var(--addp-bg-primary); color: var(--addp-text-secondary); font-size: 12px; }
.studio-inspector { position: sticky; top: 92px; max-height: calc(100vh - 112px); overflow: auto; background: var(--addp-bg-primary); border: 1px solid var(--addp-border-color); border-radius: 12px; }
.inspector-section { padding: 16px; border-bottom: 1px solid var(--addp-border-color); }
.inspector-section:last-child { border-bottom: 0; }
.inspector-heading { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 16px; }
.component-outline { display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow: auto; }
.outline-row { display: flex; align-items: center; border-radius: 6px; }
.outline-row.active { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }
.outline-select { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; padding: 9px 6px; text-align: left; background: none; border: 0; cursor: pointer; color: inherit; font: inherit; font-size: 13px; }
.outline-select span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.size-options { display: flex; gap: 4px; }.size-options .el-button + .el-button { margin-left: 0; }
.layout-presets { margin-top: 12px; display: flex; gap: 8px; }.layout-presets .el-button + .el-button { margin-left: 0; }
.inspector-configure { margin-left: 0; margin-bottom: 10px; }
.settings-content { display: flex; flex-direction: column; gap: 20px; }
.draft-preview-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding-right: 24px; flex-wrap: wrap; }
.draft-preview-heading h2 { margin: 0; }.draft-preview-heading p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.6; }
:global(.draft-preview-dialog) { display: flex; flex-direction: column; overflow: hidden; }
:global(.draft-preview-dialog .el-dialog__body) { flex: 1; min-height: 0; padding: 0; overflow: hidden; }
.studio-start { padding: 40px; border: 1px solid var(--addp-border-color); border-radius: 16px; background: var(--addp-bg-primary); }
.start-intro { text-align: center; max-width: 620px; margin: 0 auto 32px; }.start-intro h1 { font-size: 28px; margin: 12px 0; }.start-intro p { color: var(--addp-text-secondary); line-height: 1.7; }.eyebrow { color: var(--el-color-primary); font-size: 13px; }
.starter-options { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.starter-option { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; padding: 24px; border: 1px solid var(--addp-border-color); border-radius: 12px; text-align: left; background: var(--addp-bg-primary); color: var(--addp-text-primary); font: inherit; cursor: pointer; }
.starter-option:hover, .starter-option:focus-visible { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }.starter-option .el-icon, .starter-go { color: var(--el-color-primary); }.starter-option strong { font-size: 18px; }.starter-option > span:not(.starter-go) { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.7; }.starter-go { margin-top: auto; font-size: 13px; }
.start-footer { display: flex; justify-content: center; align-items: center; flex-wrap: wrap; gap: 16px; padding: 28px 0; color: var(--addp-text-secondary); font-size: 13px; }.creation-steps { display: flex; justify-content: space-around; flex-wrap: wrap; gap: 24px; margin: 12px 0 0; padding: 24px 20px 0; border-top: 1px solid var(--addp-border-color); color: var(--addp-text-secondary); font-size: 13px; }
@media(max-width:1050px) { .studio-workspace { grid-template-columns: minmax(0,1fr) 270px; }.starter-options { grid-template-columns: repeat(2, minmax(0,1fr)); }.editor-topbar { flex-wrap: wrap; } }
@media(max-width:760px) { .studio-workspace { display: flex; flex-direction: column; }.studio-stage, .studio-inspector { width: 100%; }.studio-inspector { position: static; max-height: none; }.studio-start { padding: 20px; }.starter-options { grid-template-columns: 1fr; }.studio-toolbar { align-items: flex-start; flex-direction: column; }.editor-topbar { position: static; } }

.filter-settings, .interaction-settings { display: flex; flex-direction: column; gap: 20px; }
.settings-intro h3 { margin: 0 0 8px; font-size: 17px; }.settings-intro p { margin: 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.7; }
.filter-cards { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.filter-card, .binding-group, .interaction-builder { padding: 18px; border: 1px solid var(--addp-border-color); border-radius: 10px; min-width: 0; }
.filter-card .el-form-item { margin-bottom: 14px; }.filter-required { display: flex; justify-content: space-between; align-items: center; font-size: 13px; }
.filter-impact { display: flex; flex-wrap: wrap; gap: 6px; border-top: 1px solid var(--addp-border-color); padding-top: 12px; margin-top: 10px; font-size: 12px; color: var(--addp-text-secondary); }.filter-impact .el-tag { max-width: 100%; height: auto; white-space: normal; overflow-wrap: anywhere; }
.binding-row { display: grid; grid-template-columns: minmax(100px, 1fr) 20px minmax(0, 2fr); gap: 12px; align-items: center; margin-top: 16px; font-size: 13px; }
.configuration-warning { color: var(--el-color-warning); font-size: 13px; }.assignment-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: 12px; align-items: center; }.assignment-row .el-form-item { min-width: 0; }.interaction-builder { background: var(--addp-bg-secondary); }.interaction-summary { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 8px 0; font-size: 14px; }.impact-summary { padding: 12px; background: var(--el-color-primary-light-9); color: var(--el-color-primary); border-radius: 6px; font-size: 13px; line-height: 1.7; overflow-wrap: anywhere; }
@media(max-width:600px) { .filter-cards, .assignment-row { grid-template-columns: minmax(0,1fr); }.binding-row { grid-template-columns: minmax(0,1fr); }.binding-row > .muted { display: none; }.selection-binding-card .inspector-heading { flex-wrap: wrap; } }
</style>
