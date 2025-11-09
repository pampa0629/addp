import { defineStore } from 'pinia'
import { appAPI } from '../api/client'

export const useAppCenterStore = defineStore('appCenter', {
  state: () => ({
    apps: [],
    loading: false,
    creating: false,
    launching: [],
    error: null
  }),

  getters: {
    isLaunching: (state) => (id) => state.launching.includes(id)
  },

  actions: {
    async fetchApps() {
      this.loading = true
      try {
        const { data } = await appAPI.list()
        this.apps = data.apps || []
      } catch (error) {
        console.error('获取应用列表失败', error)
        this.error = error.response?.data?.error || error.message
      } finally {
        this.loading = false
      }
    },

    async fetchAppById(id) {
      try {
        const { data } = await appAPI.get(id)
        if (data.app) {
          this.updateLocalApp(data.app)
        }
        return data.app
      } catch (error) {
        this.error = error.response?.data?.error || error.message
        throw error
      }
    },

    async registerApp(payload) {
      this.creating = true
      this.error = null
      try {
        const { data } = await appAPI.create(payload)
        if (data.app) {
          this.apps = [data.app, ...this.apps]
        }
        return data.app
      } catch (error) {
        this.error = error.response?.data?.error || error.message
        throw error
      } finally {
        this.creating = false
      }
    },

    async launchApp(id) {
      if (this.launching.includes(id)) {
        return
      }

      this.launching.push(id)
      this.error = null
      try {
        const { data } = await appAPI.launch(id)
        if (data.app) {
          this.updateLocalApp(data.app)
        }
        // 异步轮询最新状态，直到进程结束
        this.pollAppStatus(id)
        return data
      } catch (error) {
        this.error = error.response?.data?.error || error.message
        throw error
      } finally {
        this.launching = this.launching.filter(appId => appId !== id)
      }
    },

    async pollAppStatus(id, attempts = 0) {
      const maxAttempts = 15
      const delay = (ms) => new Promise(resolve => setTimeout(resolve, ms))

      while (attempts < maxAttempts) {
        await delay(2000)
        try {
          const app = await this.fetchAppById(id)
          if (!app || app.status !== 'running') {
            return
          }
        } catch (error) {
          console.error('轮询应用状态失败:', error)
          return
        }
        attempts += 1
      }
    },

    async deleteApp(id) {
      this.error = null
      try {
        await appAPI.delete(id)
        // 从本地移除
        this.apps = this.apps.filter(app => app.id !== id)
        return true
      } catch (error) {
        this.error = error.response?.data?.error || error.message
        throw error
      }
    },

    updateLocalApp(updated) {
      const index = this.apps.findIndex(app => app.id === updated.id)
      if (index === -1) {
        this.apps = [updated, ...this.apps]
        return
      }
      const next = [...this.apps]
      next[index] = updated
      this.apps = next
    }
  }
})
