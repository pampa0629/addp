import client from './client'

/**
 * 获取存储引擎资源列表
 * @returns {Promise}
 */
export const listResources = () => {
  return client.get('/develop/resources')
}

/**
 * 获取指定资源的 schema 列表
 * @param {number} resourceId - 资源ID
 * @returns {Promise}
 */
export const listSchemas = (resourceId) => {
  return client.get(`/develop/resources/${resourceId}/schemas`)
}

/**
 * 获取指定资源和 schema 下的表列表
 * @param {number} resourceId - 资源ID
 * @param {string} schema - Schema名称
 * @returns {Promise}
 */
export const listTables = (resourceId, schema) => {
  return client.get(`/develop/resources/${resourceId}/tables`, {
    params: { schema }
  })
}
