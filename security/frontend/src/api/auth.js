import axios from 'axios'
import { createAuthAPI } from '@common-ui'
const client = axios.create({ baseURL: '/api/v1/system', timeout: 10000 })
export const authAPI = { ...createAuthAPI(client) }
