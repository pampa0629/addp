// Broad test account for isolated UI flows; individual permissions are tested by authorization contracts.
export const developAuthContext = {
  context: { type: 'tenant', tenant_id: '1' },
  authorization: {
    role_assignments: [{
      scope: { type: 'tenant', tenant_id: '1' },
      permissions: [
        'develop.task.read', 'develop.task.create', 'develop.task.update',
        'develop.task.delete', 'develop.task.execute',
        'develop.notebook.read', 'develop.notebook.create',
        'develop.notebook.update', 'develop.notebook.delete',
        'develop.data_read.execute', 'develop.data_write.execute',
        'develop.data_ddl.execute', 'develop.data_external_effect.execute',
        'develop.catalog.read', 'meta.catalog.read', 'monitor.execution.read'
      ]
    }]
  }
}
