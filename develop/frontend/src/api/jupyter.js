/**
 * Jupyter 虚拟环境管理 API
 * 用于租户独立虚拟环境的初始化、状态查询等操作
 */
import client from './client'

/**
 * 获取租户虚拟环境状态
 * @returns {Promise} 虚拟环境信息
 */
export function getVenvStatus() {
  return client.get('/develop/jupyter/venv/status')
}

/**
 * 初始化租户虚拟环境
 * @returns {Promise} 初始化结果
 */
export function initVenv() {
  return client.post('/develop/jupyter/venv/init')
}

/**
 * 删除租户虚拟环境
 * @returns {Promise}
 */
export function deleteVenv() {
  return client.delete('/develop/jupyter/venv')
}

/**
 * 获取 Jupyter Server 状态
 * @returns {Promise}
 */
export function getJupyterServerStatus() {
  return client.get('/develop/jupyter/server/status')
}

/**
 * 列出所有租户的虚拟环境 (管理员)
 * @returns {Promise}
 */
export function listVenvs() {
  return client.get('/develop/jupyter/venvs')
}

/**
 * 为指定租户初始化虚拟环境 (管理员)
 * @param {number} tenantId - 租户 ID
 * @returns {Promise}
 */
export function initVenvByTenantId(tenantId) {
  return client.post(`/develop/jupyter/venv/${tenantId}/init`)
}
