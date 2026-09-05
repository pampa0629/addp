import assert from 'node:assert/strict'
import test from 'node:test'

import { buildExecutionLineageSummary } from '../src/utils/executionLineagePresentation.js'

test('presents business inputs and infra outputs from versioned execution facts', () => {
  const summary = buildExecutionLineageSummary({
    lineage_facts: {
      schema_version: 'addp.lineage-facts/v1',
      inputs: [
        {
          port: 'activities',
          locator: 'addp://engine/2/path/outdoor/ods_outdoor_activities?type=table&item_id=52897',
          item_id: 52897
        },
        {
          port: 'person',
          locator: 'addp://engine/2/path/outdoor/ods_outdoor_persons?type=table&item_id=52899'
        }
      ],
      outputs: [{
        port: 'target',
        locator: 'addp-infra://minio/manager/tenant_1/export/develop/session/result.csv?type=object',
        write_mode: 'replace'
      }],
      operations: [{
        kind: 'derive',
        operator: 'transfer',
        input_ports: ['activities', 'person'],
        output_ports: ['target']
      }]
    }
  })

  assert.deepEqual(summary.inputs.map(item => [item.port, item.displayName, item.resourceType, item.itemId, item.explorable]), [
    ['activities', 'outdoor.ods_outdoor_activities', 'table', 52897, true],
    ['person', 'outdoor.ods_outdoor_persons', 'table', 52899, true]
  ])
  assert.deepEqual(summary.outputs.map(item => [item.port, item.displayName, item.resourceType, item.platformInternal, item.writeMode, item.explorable]), [
    ['target', 'result.csv', 'object', true, 'replace', false]
  ])
  assert.deepEqual(summary.operations[0], {
    kind: 'derive',
    operator: 'transfer',
    inputPorts: ['activities', 'person'],
    outputPorts: ['target']
  })
})

test('ignores unsupported versions and malformed resource entries', () => {
  assert.deepEqual(buildExecutionLineageSummary({}), {
    schemaVersion: '',
    inputs: [],
    outputs: [],
    operations: []
  })
  assert.equal(buildExecutionLineageSummary({
    lineage_facts: { schema_version: 'addp.lineage-facts/v2', inputs: [{ locator: 'addp://ignored' }] }
  }).inputs.length, 0)
  assert.equal(buildExecutionLineageSummary({
    lineage_facts: {
      schema_version: 'addp.lineage-facts/v1',
      inputs: [null, {}, { locator: '' }]
    }
  }).inputs.length, 0)
})

test('keeps malformed business locators visible but not explorable', () => {
  const summary = buildExecutionLineageSummary({
    lineage_facts: {
      schema_version: 'addp.lineage-facts/v1',
      inputs: [{ port: 'source', locator: 'not-a-resource-locator' }]
    }
  })

  assert.equal(summary.inputs[0].displayName, 'not-a-resource-locator')
  assert.equal(summary.inputs[0].explorable, false)
})
