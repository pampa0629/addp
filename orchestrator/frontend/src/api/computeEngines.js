import client from './client'

/**
 * 计算引擎 API
 * 用于获取所有具有计算能力的引擎（内置引擎 + 外部数据源）
 */
export default {
  /**
   * 获取所有计算引擎列表
   * @returns {Promise<Array>} 计算引擎列表
   * @example
   * [
   *   {
   *     id: 1,
   *     name: "Meta Metadata Scanner",
   *     unique_identifier: "meta.scanner.default",
   *     engine_type: "compute_engine",
   *     is_builtin: true,
   *     is_active: true,
   *     capabilities: "{\"compute\":[...]}",
   *     task_api_config: "{\"base_url\":\"...\",\"endpoints\":{...}}"
   *   }
   * ]
   */
  async list() {
    const data = await client.get('/orchestrator/compute-engines')
    return data
  }
}
