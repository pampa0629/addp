import axios from 'axios'
import { createAuthAPI } from '@common-ui'

const systemClient = axios.create({
  baseURL: import.meta.env.DEV ? 'http://localhost:8180/api/v1/system' : '/api/v1/system',
  timeout: 10000
})

export const authAPI = createAuthAPI(systemClient)
