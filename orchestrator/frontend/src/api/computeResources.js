import client from './client'

/**
 * 计算资源 API
 * 用于获取所有具有计算能力的资源（内置引擎 + 外部数据源）
 */
export default {
  /**
   * 获取所有计算资源列表
   * @returns {Promise<Array>} 计算资源列表
   * @example
   * [
   *   {
   *     id: 1,
   *     name: "Meta Metadata Scanner",
   *     unique_identifier: "meta.scanner.default",
   *     resource_type: "compute_engine",
   *     is_builtin: true,
   *     is_active: true,
   *     capabilities: "{\"compute\":[...]}",
   *     task_api_config: "{\"base_url\":\"...\",\"endpoints\":{...}}"
   *   }
   * ]
   */
  async list() {
    const data = await client.get('/compute-resources')
    return data
  }
}
