export async function deleteSelectedDerivedTasks(tasks, deleteTask) {
  const selectedTasks = Array.isArray(tasks) ? tasks : []
  const results = await Promise.allSettled(selectedTasks.map(task => deleteTask(task.task_type, task.id)))
  const deleted = results.filter(result => result.status === 'fulfilled').length

  return {
    requested: selectedTasks.length,
    deleted,
    failed: selectedTasks.length - deleted
  }
}
