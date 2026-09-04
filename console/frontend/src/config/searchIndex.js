// 功能入口搜索索引（仅搜索模块/页面/功能，不含数据资产）
// label: 显示名 i18n key，keywords: 中英文关键词用于模糊匹配，route: 导航目标
export const SEARCH_INDEX = [
  // 数据传输
  { labelKey: 'console.menus.transfer.tasks',       module: 'transfer',     route: '/transfer/tasks',        keywords: ['传输任务', '数据导入', '数据接入', 'transfer', 'import', '同步'] },
  { labelKey: 'console.menus.transfer.executions',  module: 'transfer',     route: '/transfer/executions',   keywords: ['传输执行', '执行记录', 'execution'] },
  // 元数据
  { labelKey: 'console.menus.meta.scan',            module: 'meta',         route: '/meta/scan',             keywords: ['元数据扫描', '扫描', '元数据', 'metadata', 'scan'] },
  { labelKey: 'console.menus.meta.tasks',           module: 'meta',         route: '/meta/tasks',            keywords: ['元数据任务', '任务监控', 'meta task'] },
  // 数据管理
  { labelKey: 'console.menus.manager.dataExplorer',       module: 'manager', route: '/manager/data-explorer',       keywords: ['数据探查', '数据浏览', '数据目录', 'explorer', 'browse'] },
  { labelKey: 'console.menus.manager.dataRetrieval',      module: 'manager', route: '/manager/data-retrieval',      keywords: ['数据检索', '搜索', 'retrieval', 'search'] },
  { labelKey: 'console.menus.manager.vectorizationTasks', module: 'manager', route: '/manager/vectorization-tasks', keywords: ['向量化', '向量', 'vector', 'embedding'] },
  { labelKey: 'console.menus.manager.vectorTiles',        module: 'manager', route: '/manager/spatial-tasks/vector-tiles', keywords: ['空间任务', '矢量瓦片', '业务瓦片', 'PMTiles', 'vector tiles', 'spatial tasks'] },
  { labelKey: 'console.menus.manager.tileCache',          module: 'manager', route: '/manager/spatial-quick-view/vector-tile-cache', keywords: ['矢量快显瓦片缓存', '瓦片缓存任务', '瓦片缓存', '瓦片生成', '矢量瓦片', 'tile cache tasks', 'tile cache', 'tile'] },
  { labelKey: 'console.menus.manager.vectorMaterializedView', module: 'manager', route: '/manager/spatial-quick-view/vector-materialized-view', keywords: ['矢量快显物化视图', '物化视图任务', '快显物化视图', '动态MVT', '3857', 'materialized view', 'quick view', 'mvt'] },
  { labelKey: 'console.menus.manager.cogArtifacts',       module: 'manager', route: '/manager/spatial-quick-view/raster-cog', keywords: ['栅格快显COG', 'COG', 'COG生成', 'TIFF', 'GeoTIFF', '栅格快显', 'cloud optimized geotiff'] },
  { labelKey: 'console.menus.manager.rasterMosaic',       module: 'manager', route: '/manager/spatial-quick-view/raster-mosaic', keywords: ['栅格镶嵌数据集', '镶嵌数据集', 'Mosaic生成', 'raster mosaic', 'raster_mosaic', 'mosaic'] },
  { labelKey: 'console.menus.manager.cadPreview',         module: 'manager', route: '/manager/spatial-quick-view/cad-preview', keywords: ['CAD快显', 'CAD预览', 'DWG', 'DXF', 'cad quick view', 'cad preview'] },
  { labelKey: 'console.menus.manager.model3DQuickView',   module: 'manager', route: '/manager/model-3d-glb', keywords: ['三维快显GLB', '三维模型快显', 'GLB快显', 'OSGB', 'FBX', 'OBJ', 'gltf', '3d model quick view', 'glb'] },
  { labelKey: 'console.menus.manager.model3DTiles',       module: 'manager', route: '/manager/model-3d-tiles', keywords: ['三维瓦片快显', '倾斜摄影', '3D Tiles', 'S3M', 'osgb scene', 'photogrammetry'] },
  { labelKey: 'console.menus.manager.gaussianSplatQuickView', module: 'manager', route: '/manager/gaussian-splat-ksplat', keywords: ['高斯泼溅快显', '高斯快显', 'KSplat', 'SPLAT', 'gaussian splat', 'gaussian splatting', 'ksplat'] },
  // 数据标准
  { labelKey: 'console.menus.standard.domains',      module: 'standard', route: '/standard/domains',      keywords: ['业务域', '业务领域', 'domain'] },
  { labelKey: 'console.menus.standard.collections',  module: 'standard', route: '/standard/collections',  keywords: ['标准集', '标准包', '审核', 'standard collection'] },
  { labelKey: 'console.menus.standard.glossaries',   module: 'standard', route: '/standard/glossaries',   keywords: ['业务术语', '术语', 'glossary', 'term'] },
  { labelKey: 'console.menus.standard.elements',     module: 'standard', route: '/standard/elements',     keywords: ['数据元', '元素', 'element', 'standard'] },
  { labelKey: 'console.menus.standard.codeSets',     module: 'standard', route: '/standard/code-sets',    keywords: ['码值', '码值集', 'code', 'codeset'] },
  { labelKey: 'console.menus.standard.metrics',      module: 'standard', route: '/standard/metrics',      keywords: ['指标', '指标管理', 'metric', 'kpi'] },
  { labelKey: 'console.menus.standard.documents',    module: 'standard', route: '/standard/documents',    keywords: ['标准文档', '文档', 'document'] },
  // 数据建模
  { labelKey: 'console.menus.modeling.dwLayers',     module: 'modeling', route: '/modeling/dw-layers',    keywords: ['数仓分层', '分层', 'data warehouse', 'layer'] },
  { labelKey: 'console.menus.modeling.entities',     module: 'modeling', route: '/modeling/entities',     keywords: ['业务实体', '实体', 'entity'] },
  { labelKey: 'console.menus.modeling.logicalTables',module: 'modeling', route: '/modeling/logical-tables',keywords: ['逻辑表', '逻辑模型', 'logical table'] },
  { labelKey: 'console.menus.modeling.materializationGroups', module: 'modeling', route: '/modeling/materialization-groups', keywords: ['物化组', '原子发布', 'materialization group', 'publish'] },
  { labelKey: 'console.menus.modeling.starSchema',   module: 'modeling', route: '/modeling/star-schema',  keywords: ['星型建模', '星型模型', 'star schema'] },
  // 数据质量
  { labelKey: 'console.menus.quality.checkTasks',       module: 'quality', route: '/quality/check-tasks',      keywords: ['质量检查', '质量', 'quality check'] },
  { labelKey: 'console.menus.quality.materializationGateTasks', module: 'quality', route: '/quality/materialization-gate-tasks', keywords: ['物化门禁', '发布门禁', '强类型断言', 'materialization gate'] },
  { labelKey: 'console.menus.quality.ruleApplications', module: 'quality', route: '/quality/rule-applications',keywords: ['规则应用', '质量规则', 'rule'] },
  { labelKey: 'console.menus.quality.issues',           module: 'quality', route: '/quality/issues',           keywords: ['质量问题', '问题工单', 'issue'] },
  // 数据安全
  { labelKey: 'console.menus.security.classificationGrading', module: 'security', route: '/security/classification-grading', keywords: ['分类分级体系', '安全分类', '安全等级', '分类目录', '保护等级', 'classification', 'grading', 'grade'] },
  { labelKey: 'console.menus.security.sensitiveDataDefinitions', module: 'security', route: '/security/sensitive-data-definitions', keywords: ['敏感数据定义', '敏感数据类型', '识别方式', '检测能力', '手机号', '隐私', 'sensitive data', 'detector', 'privacy'] },
  { labelKey: 'console.menus.security.defaultProtectionRules', module: 'security', route: '/security/protection-baselines', keywords: ['默认保护规则', '保护基线', '脱敏', '遮盖', 'mask', 'protection baseline', 'protection rule'] },
  { labelKey: 'console.menus.security.protectedResources', module: 'security', route: '/security/protection-enrollments', keywords: ['受保护资源', '保护纳管', '纳入数据保护', 'enrollment', 'protected resources'] },
  // 数据开发
  { labelKey: 'console.menus.develop.sql',      module: 'develop', route: '/develop/sql',      keywords: ['SQL', 'SQL工作台', '查询', '开发', 'workbench', 'query'] },
  { labelKey: 'console.menus.develop.notebook', module: 'develop', route: '/develop/notebook', keywords: ['Notebook', 'Jupyter', '笔记本', '开发'] },
  { labelKey: 'console.menus.develop.workflow', module: 'develop', route: '/develop/workflow',  keywords: ['工作流', '算子', '工作流编辑', 'workflow', 'operator'] },
  // 数据服务
  { labelKey: 'console.menus.service.queryServices', module: 'service', route: '/service/query-services', keywords: ['查询服务', '发布API', 'API', '接口', 'query service', 'publish'] },
  { labelKey: 'console.menus.service.tile',          module: 'service', route: '/service/tile',           keywords: ['瓦片服务', '地图瓦片', 'tile', 'map tile', 'OGC'] },
  { labelKey: 'console.menus.service.services',      module: 'service', route: '/service/services',       keywords: ['服务注册', '外部服务', 'service registry'] },
  { labelKey: 'console.menus.service.catalog',       module: 'service', route: '/service/catalog',        keywords: ['服务目录', '服务列表', 'catalog'] },
  // 数据服务消费
  { labelKey: 'console.menus.workbench.dataApplications', module: 'workbench', route: '/workbench/applications', keywords: ['数据应用', '数据服务消费', '动态查询', '可视化', '联动', '应用发布', '组合展示', 'workbench', 'service consumer', 'data application', 'application revision'] },
  // 任务编排
  { labelKey: 'console.menus.orchestrator.orchestrations', module: 'orchestrator', route: '/orchestrator/orchestrations', keywords: ['编排任务', '工作流编排', 'orchestration', 'dag'] },
  { labelKey: 'console.menus.orchestrator.executions',     module: 'orchestrator', route: '/orchestrator/executions',    keywords: ['编排执行', '执行记录', 'orchestrator execution'] },
  // 执行监控
  { labelKey: 'console.menus.monitor.dashboard',   module: 'monitor', route: '/monitor/dashboard',  keywords: ['监控仪表盘', '监控', '运行状态', 'monitor', 'dashboard'] },
  { labelKey: 'console.menus.monitor.executions',  module: 'monitor', route: '/monitor/executions', keywords: ['执行记录', '任务历史', 'execution history'] },
  { labelKey: 'console.menus.monitor.alerts',      module: 'monitor', route: '/monitor/alerts',     keywords: ['告警', '风险', 'alert', 'incident'] },
  { labelKey: 'console.menus.monitor.notifications', module: 'monitor', route: '/monitor/notifications', keywords: ['Webhook', '邮件', '通知', '投递', 'email', 'notification', 'delivery'] },
  // 企业资源目录
  { labelKey: 'console.menus.catalog.entries', module: 'catalog', route: '/catalog/entries', keywords: ['企业资源目录', '企业资源', '业务元数据', 'catalog', 'enterprise catalog', 'inventory'] },
  { labelKey: 'console.menus.catalog.coverage', module: 'catalog', route: '/catalog/governance/coverage', permissions: ['catalog.inventory.read'], keywords: ['治理覆盖率', '治理完整度', 'catalog governance coverage', 'completeness'] },
  // 资产管理
  { labelKey: 'console.menus.asset.assets',          module: 'asset', route: '/asset/assets',          keywords: ['资产', '数据资产', 'asset'] },
  { labelKey: 'console.menus.asset.categories',      module: 'asset', route: '/asset/categories',      keywords: ['资产目录', '分类', 'category'] },
  { labelKey: 'console.menus.asset.applications',    module: 'asset', route: '/asset/applications',    keywords: ['申请', '审批', '授权', 'application', 'approval'] },
  { labelKey: 'console.menus.asset.dashboard',       module: 'asset', route: '/asset/dashboard',       keywords: ['资产看板', '运营', 'dashboard'] },
  // 知识图谱
  { labelKey: 'console.menus.graph.ontologies',       module: 'graph', route: '/graph/ontologies',        keywords: ['本体', '本体建模', 'ontology'] },
  { labelKey: 'console.menus.graph.graphs',            module: 'graph', route: '/graph/graphs',            keywords: ['知识图谱', '图谱', 'knowledge graph', 'graph'] },
  { labelKey: 'console.menus.graph.analysis',          module: 'graph', route: '/graph/analysis',          keywords: ['图算法', '图分析', 'graph algorithm', 'analysis'] },
  { labelKey: 'console.menus.graph.knowledgeService',  module: 'graph', route: '/graph/knowledge-service', keywords: ['知识服务', 'knowledge service'] },
  // 智能体
  { labelKey: 'console.menus.agent.label', module: 'agent', route: '/agent', keywords: ['智能体', 'AI', '对话', '助手', 'agent', 'chat', 'assistant'] },
  { labelKey: 'console.menus.inference.models', module: 'inference', route: '/inference/settings/models', keywords: ['AI', '推理', '模型', 'Provider', 'Deployment', 'Profile', 'inference', 'model'] },
  // 系统管理
  { labelKey: 'console.menus.system.iam',          module: 'system', route: '/system/iam',          keywords: ['安全设置', '身份验证器', '用户', '租户', '权限', '审计', 'IAM', 'identity', 'access', 'account', 'security', 'authenticator'] },
  { labelKey: 'console.menus.system.modules',      module: 'system', route: '/system/modules',      permissions: ['platform.module.read'], keywords: ['模块管理', '服务注册', '运行实例', 'worker', 'module', 'runtime', 'registry'] },
  { labelKey: 'console.menus.system.engines',      module: 'system', route: '/system/engines',      keywords: ['引擎管理', '数据引擎', '引擎配置', 'engine', 'database'] },
  { labelKey: 'console.menus.system.applications', module: 'system', route: '/system/applications', keywords: ['应用管理', 'API密钥', 'application', 'api key'] },
]

/**
 * 简单模糊搜索，无需外部依赖
 * @param {string} query
 * @param {(key: string) => string} translate
 * @returns {Array}
 */
export function searchIndex(query, translate, grantedPermissions = []) {
  if (!query || query.trim() === '') return []
  const q = query.trim().toLowerCase()
  const granted = new Set(grantedPermissions)
  const results = []
  for (const item of SEARCH_INDEX) {
    if (item.permissions?.length && !item.permissions.some(permission => granted.has(permission))) continue
    const label = translate(item.labelKey).toLowerCase()
    const kwMatch = item.keywords.some(k => k.toLowerCase().includes(q))
    const labelMatch = label.includes(q)
    if (labelMatch || kwMatch) {
      results.push({ ...item, label: translate(item.labelKey), score: labelMatch ? 2 : 1 })
    }
  }
  return results.sort((a, b) => b.score - a.score).slice(0, 8)
}
