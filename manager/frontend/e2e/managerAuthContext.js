export const managerAuthContext = {
  context: { type: 'tenant', tenant_id: '1' },
  authorization: {
    role_assignments: [{
      scope: { type: 'tenant', tenant_id: '1' },
      permissions: [
        'manager.content.read',
        'manager.data_item.read',
        'manager.search.execute',
        'manager.derived_artifact.read',
        'manager.derived_artifact.create',
        'manager.derived_artifact.update',
        'manager.derived_artifact.delete',
        'inference.model_label.read',
        'monitor.execution.read',
        'meta.catalog.read'
      ]
    }]
  }
}
