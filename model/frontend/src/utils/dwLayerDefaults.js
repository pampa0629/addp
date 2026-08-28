export const createDefaultDWLayers = t => [
  {
    layer_code: 'ods',
    layer_name: t('model.dw_layer.defaults.ods.name'),
    naming_rule: 'ods_{domain}_{entity}',
    description: t('model.dw_layer.defaults.ods.description'),
    sort_order: 1
  },
  {
    layer_code: 'dwd',
    layer_name: t('model.dw_layer.defaults.dwd.name'),
    naming_rule: 'dwd_{domain}_{entity} / dim_{domain}_{entity}',
    description: t('model.dw_layer.defaults.dwd.description'),
    sort_order: 2
  },
  {
    layer_code: 'dws',
    layer_name: t('model.dw_layer.defaults.dws.name'),
    naming_rule: 'dws_{domain}_{subject}',
    description: t('model.dw_layer.defaults.dws.description'),
    sort_order: 3
  },
  {
    layer_code: 'ads',
    layer_name: t('model.dw_layer.defaults.ads.name'),
    naming_rule: 'ads_{subject}_{scene}',
    description: t('model.dw_layer.defaults.ads.description'),
    sort_order: 4
  }
]
