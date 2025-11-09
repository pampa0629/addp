import { defineStore } from 'pinia'
import { taskAPI } from '../api/client'
import wsService from '../api/websocket'

export const useTaskStore = defineStore('task', {
  state: () => ({
    tasks: [],
    currentTask: null,
    loading: false,
    error: null
  }),

  actions: {
    async createTask(prompt) {
      this.loading = true
      this.error = null
      try {
        const response = await taskAPI.create(prompt)
        const task = response.data.task
        this.tasks.unshift(task)
        this.currentTask = task
        return task
      } catch (error) {
        this.error = error.response?.data?.error || error.message
        throw error
      } finally {
        this.loading = false
      }
    },

    async fetchTasks() {
      try {
        const response = await taskAPI.list()
        this.tasks = response.data.tasks || []
      } catch (error) {
        console.error('Failed to fetch tasks:', error)
      }
    },

    async fetchTask(id) {
      try {
        const response = await taskAPI.get(id)
        const task = response.data.task

        // Update task in list
        const index = this.tasks.findIndex(t => t.id === id)
        if (index !== -1) {
          this.tasks[index] = task
        }

        if (this.currentTask?.id === id) {
          this.currentTask = task
        }

        return task
      } catch (error) {
        console.error('Failed to fetch task:', error)
      }
    },

    handleProgressUpdate(update) {
      // Find and update the task
      const index = this.tasks.findIndex(t => t.id === update.task_id)

      if (index !== -1) {
        const task = this.tasks[index]

        if (update.status) {
          task.status = update.status
        }

        // Handle error field
        if (update.error) {
          task.error = update.error
        }

        if (update.log) {
          if (!task.logs) {
            task.logs = []
          }
          const timestamp = new Date().toLocaleTimeString()
          task.logs.push(`[${timestamp}] ${update.log}`)
        }

        this.tasks[index] = { ...task }

        if (this.currentTask?.id === update.task_id) {
          this.currentTask = { ...task }
        }
      }
    },

    setCurrentTask(task) {
      this.currentTask = task
    }
  }
})
