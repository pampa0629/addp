import {
  createDevTask,
  executeDevTask,
  updateDevTask
} from './devTask'
import { buildWorkflowDevTaskPayload } from '@/utils/workflowDevTaskPayload'

export const saveWorkflowTask = (taskData) => {
  return createDevTask(buildWorkflowDevTaskPayload(taskData))
}

export const updateWorkflowTask = (id, taskData) => {
  return updateDevTask(id, buildWorkflowDevTaskPayload({
    ...taskData,
    includeDevType: false
  }))
}

export const createTemporaryWorkflowTask = (taskData) => {
  return createDevTask(buildWorkflowDevTaskPayload(taskData))
}

export const executeWorkflowTask = (id, inputs = {}) => {
  return executeDevTask(id, inputs)
}
