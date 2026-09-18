<template>
  <el-tabs :model-value="tab" @update:model-value="$emit('update:tab', $event)">
    <el-tab-pane
      v-for="section in sections"
      :key="section"
      :name="section"
      :label="`${t(`ontology.${section}`)} (${modelValue[section].length})`"
    >
      <div class="members">
        <el-empty
          v-if="!modelValue[section].length"
          :description="t('ontology.emptyMembers')"
        />
        <el-card
          v-for="(member, index) in modelValue[section]"
          :key="index"
          shadow="never"
          :data-testid="`${section}-member`"
        >
          <el-form label-position="top" :disabled="disabled">
            <div class="fields">
              <el-form-item :label="t('ontology.memberID')" required>
                <el-input v-model="member.id" maxlength="64" />
              </el-form-item>
              <el-form-item
                v-if="section !== 'rules'"
                :label="t('ontology.name')"
                required
              >
                <el-input v-model="member.name" maxlength="256" />
              </el-form-item>
              <el-form-item
                v-if="section === 'classes'"
                :label="t('ontology.parents')"
              >
                <el-select v-model="member.parents" multiple>
                  <el-option
                    v-for="c in modelValue.classes.filter(
                      (c) => c.id && c !== member
                    )"
                    :key="c.id"
                    :label="`${c.name} (${c.id})`"
                    :value="c.id"
                  />
                </el-select>
              </el-form-item>
              <template v-if="section === 'properties' || section === 'rules'">
                <el-form-item :label="t('ontology.class')" required>
                  <el-select v-model="member.class_id">
                    <el-option
                      v-for="c in modelValue.classes.filter((c) => c.id)"
                      :key="c.id"
                      :label="`${c.name} (${c.id})`"
                      :value="c.id"
                    />
                  </el-select>
                </el-form-item>
              </template>
              <template v-if="section === 'properties'">
                <el-form-item :label="t('ontology.key')" required>
                  <el-input v-model="member.key" maxlength="64" />
                </el-form-item>
                <el-form-item :label="t('ontology.kind')">
                  <el-select v-model="member.kind" @change="member.enum = []">
                    <el-option
                      v-for="kind in ['string', 'bool']"
                      :key="kind"
                      :value="kind"
                      :label="t(`ontology.${kind}`)"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item
                  v-if="member.kind === 'string'"
                  :label="t('ontology.enum')"
                >
                  <el-select
                    v-model="member.enum"
                    multiple
                    filterable
                    allow-create
                    default-first-option
                    :reserve-keyword="false"
                  >
                    <el-option
                      v-for="value in member.enum"
                      :key="value"
                      :value="value"
                      :label="value"
                    />
                  </el-select>
                </el-form-item>
              </template>
              <template v-if="section === 'relations'">
                <el-form-item
                  v-for="side in ['from', 'to']"
                  :key="side"
                  :label="t(`ontology.${side}`)"
                  required
                >
                  <el-select v-model="member[side]">
                    <el-option
                      v-for="c in modelValue.classes.filter((c) => c.id)"
                      :key="c.id"
                      :label="`${c.name} (${c.id})`"
                      :value="c.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('ontology.inverse')">
                  <el-select v-model="member.inverse_id" clearable>
                    <el-option
                      v-for="r in modelValue.relations.filter((r) => r.id)"
                      :key="r.id"
                      :label="`${r.name} (${r.id})`"
                      :value="r.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('ontology.transitive')">
                  <el-switch
                    v-model="member.transitive"
                    :aria-label="t('ontology.transitive')"
                  />
                </el-form-item>
              </template>
            </div>
            <template v-if="section === 'rules'">
              <el-form-item :label="t('ontology.expression')" required>
                <el-input
                  v-model="member.expression"
                  type="textarea"
                  :rows="3"
                  maxlength="4096"
                />
              </el-form-item>
              <el-form-item :label="t('ontology.basis')" required>
                <el-input
                  v-model="member.basis"
                  type="textarea"
                  :rows="2"
                  maxlength="4096"
                />
              </el-form-item>
              <h4>{{ t('ontology.inputs') }}</h4>
              <div
                v-for="(input, inputIndex) in member.inputs"
                :key="inputIndex"
                class="fields input-row"
              >
                <el-form-item :label="t('ontology.variable')">
                  <el-input v-model="input.variable" maxlength="64" />
                </el-form-item>
                <el-form-item :label="t('ontology.property')">
                  <el-select v-model="input.property_id">
                    <el-option
                      v-for="p in modelValue.properties.filter((p) => p.id)"
                      :key="p.id"
                      :label="`${p.name} (${p.id})`"
                      :value="p.id"
                    />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('ontology.onAbsent')">
                  <el-select v-model="input.on_absent">
                    <el-option
                      v-for="policy in ['unknown', 'false']"
                      :key="policy"
                      :value="policy"
                      :label="t(`ontology.${policy}`)"
                    />
                  </el-select>
                </el-form-item>
                <el-button
                  v-if="!disabled"
                  @click="member.inputs.splice(inputIndex, 1)"
                >
                  {{ t('ontology.removeInput') }}
                </el-button>
              </div>
              <el-button
                v-if="!disabled"
                :disabled="member.inputs.length >= 16"
                @click="
                  member.inputs.push({
                    variable: '',
                    property_id: '',
                    on_absent: 'unknown'
                  })
                "
              >
                {{ t('ontology.addInput') }}
              </el-button>
            </template>
            <el-button
              v-if="!disabled"
              class="remove"
              type="danger"
              plain
              @click="modelValue[section].splice(index, 1)"
            >
              {{ t('ontology.removeMember') }}
            </el-button>
          </el-form>
        </el-card>
        <el-button
          v-if="!disabled"
          :disabled="modelValue[section].length >= limits[section]"
          @click="modelValue[section].push(newMember(section))"
        >
          {{ t('ontology.addMember', { section: t(`ontology.${section}`) }) }}
        </el-button>
      </div>
    </el-tab-pane>
  </el-tabs>
</template>
<script setup>
import { useI18n } from 'vue-i18n'
import { sections, limits, newMember } from '../utils/definition.mjs'
defineProps({
  modelValue: { type: Object, required: true },
  disabled: Boolean,
  tab: { type: String, default: 'classes' }
})
defineEmits(['update:tab'])
const { t } = useI18n()
</script>
<style scoped>
.members {
  display: grid;
  gap: 16px;
}
.fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
}
.el-select {
  width: 100%;
}
.remove {
  margin-top: 16px;
}
.input-row {
  align-items: center;
  border-bottom: 1px solid var(--addp-border-color);
  margin-bottom: 16px;
}
h4 {
  color: var(--addp-text-primary);
}
</style>
