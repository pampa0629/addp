import assert from 'node:assert/strict'
import {
  groupValidationIssues,
  validationIssueParamName,
  validationMessagesForParams
} from '../src/utils/workflowValidationIssues.js'

const issues = [
  { path: 'workflow_definition.tasks[0].params.source', message: 'Source is required', severity: 'error' },
  { path: "workflow_definition.tasks[0].params['locator']", message: 'Resource is required', severity: 'error' },
  { path: 'workflow_definition.tasks[0].params.locator', message: 'Resource is required', severity: 'error' },
  { path: 'workflow_definition.tasks[0].params.format', message: 'Format is unusual', severity: 'warning' }
]

assert.equal(validationIssueParamName(issues[0]), 'source')
assert.equal(validationIssueParamName(issues[1]), 'locator')
assert.deepEqual(validationMessagesForParams(issues, ['source']), ['Source is required'])
assert.deepEqual(
  validationMessagesForParams(issues, ['数据源', 'locator', 'resource_type']),
  ['Resource is required']
)
assert.deepEqual(validationMessagesForParams(issues, ['format']), [])

assert.deepEqual(groupValidationIssues([
  { nodeId: 'load_1', nodeLabel: '数据加载 · load_1', message: '资源不能为空' },
  { nodeId: 'load_1', nodeLabel: '数据加载 · load_1', message: '格式不正确' },
  { nodeId: null, nodeLabel: '工作流', message: '存在循环依赖' }
]), [
  {
    key: 'load_1',
    label: '数据加载 · load_1',
    issues: [
      { nodeId: 'load_1', nodeLabel: '数据加载 · load_1', message: '资源不能为空' },
      { nodeId: 'load_1', nodeLabel: '数据加载 · load_1', message: '格式不正确' }
    ]
  },
  {
    key: '__workflow__',
    label: '工作流',
    issues: [{ nodeId: null, nodeLabel: '工作流', message: '存在循环依赖' }]
  }
])

console.log('workflowValidationIssues tests passed')
