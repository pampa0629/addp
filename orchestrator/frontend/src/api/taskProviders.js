import client from './client'

/**
 * TaskProvider API
 * 用于获取所有可编排任务提供者
 */
export default {
  /**
   * 获取所有任务提供者列表
   * @returns {Promise<Array>} 任务提供者列表
   * @example
   * [
   *   {
   *     id: 1,
   *     module_name: "meta",
   *     display_name: "元数据",
   *     base_url: "http://localhost:8082",
   *     capabilities: {"schema_version":"task.capabilities/v1","task_capabilities":[]}
   *   }
   * ]
   */
  async list() {
    const data = await client.get('/orchestrator/task-providers')
    return data
  }
}
