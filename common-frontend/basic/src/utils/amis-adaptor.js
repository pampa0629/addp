/**
 * ADDP API → amis 响应格式适配器
 *
 * ADDP 使用灵活响应策略（直接返回数据 + HTTP 状态码），
 * amis 期望 { status: 0, msg: '', data: {...} } 格式。
 * 本模块在前端适配层处理，后端不做任何改动。
 *
 * 使用方式：
 * 1. 在 amis 组件的 api.adaptor 中使用
 * 2. 在 axios 拦截器中统一处理（推荐）
 */

/**
 * 将 ADDP API 响应转换为 amis 格式
 * @param {any} data - 响应数据
 * @param {number} httpStatus - HTTP 状态码
 * @returns {{ status: number, msg: string, data: any }}
 */
export function toAmisResponse(data, httpStatus = 200) {
  if (httpStatus >= 400) {
    return {
      status: 1,
      msg: data?.error || '请求失败',
    }
  }
  return {
    status: 0,
    msg: '',
    data: data,
  }
}

/**
 * 将 ADDP 分页响应转换为 amis CRUD 列表格式
 *
 * ADDP 分页格式: { data: [], total, page, page_size, total_pages }
 * amis CRUD 期望: { items: [], total }
 *
 * @param {any} data - 响应数据
 * @param {number} httpStatus - HTTP 状态码
 * @returns {{ status: number, msg: string, data: { items: any[], total: number } }}
 */
export function toAmisListResponse(data, httpStatus = 200) {
  if (httpStatus >= 400) {
    return {
      status: 1,
      msg: data?.error || '请求失败',
    }
  }

  // 处理 ADDP 分页响应格式
  if (data && Array.isArray(data.data) && data.total !== undefined) {
    return {
      status: 0,
      msg: '',
      data: {
        items: data.data,
        total: data.total,
      },
    }
  }

  // 处理直接返回数组的响应
  if (Array.isArray(data)) {
    return {
      status: 0,
      msg: '',
      data: {
        items: data,
        total: data.length,
      },
    }
  }

  return {
    status: 0,
    msg: '',
    data: data,
  }
}

/**
 * 创建 amis 专用 axios 实例的响应拦截器
 *
 * 用法：
 * import axios from 'axios'
 * import { createAmisInterceptor } from '@addp/basic/utils/amis-adaptor'
 *
 * const amisAxios = axios.create({ baseURL: '/api/v1' })
 * createAmisInterceptor(amisAxios)
 *
 * @param {import('axios').AxiosInstance} axiosInstance
 */
export function createAmisInterceptor(axiosInstance) {
  axiosInstance.interceptors.response.use(
    (response) => toAmisResponse(response.data, response.status),
    (error) => {
      const status = error.response?.status || 500
      const data = error.response?.data
      return Promise.resolve(toAmisResponse(data, status))
    }
  )
}

/**
 * amis api.adaptor 函数（用于单个 amis 组件配置）
 *
 * 用法（amis JSON 配置）：
 * {
 *   "type": "crud",
 *   "api": {
 *     "url": "/api/v1/system/users",
 *     "adaptor": "return window.addpAmis.toAmisListResponse(payload.data, payload.status)"
 *   }
 * }
 *
 * 需要在全局挂载：window.addpAmis = { toAmisResponse, toAmisListResponse }
 */
export default {
  toAmisResponse,
  toAmisListResponse,
  createAmisInterceptor,
}
