import {
  Coin, Reading, Tools, Folder, Shop, ChatDotRound, Memo, Setting,
  Upload, Box, DataAnalysis, Grid, CircleCheck, Edit, Link, Operation, DataLine,
  List, Timer, Connection, Search, Document, Share, DataBoard, Odometer,
  TrendCharts, SortDown, FolderOpened, Warning, Monitor, Notebook,
  Files, Tickets, Key, Delete, User,
} from '@element-plus/icons-vue'

// ─── 群组导航配置 ────────────────────────────────────────────────────────────
// label 值为 i18n key，渲染时通过 t(group.label) 翻译

export const MODULE_GROUPS = [
  { key: 'data-prepare', label: 'console.groups.dataPrepare', icon: Coin,         modules: ['transfer', 'meta', 'manager'] },
  { key: 'data-govern',  label: 'console.groups.dataGovern',  icon: Reading,      modules: ['standard', 'modeling', 'quality'] },
  { key: 'dev-monitor',  label: 'console.groups.devMonitor',  icon: Tools,        modules: ['develop', 'service', 'orchestrator', 'monitor'] },
  { key: 'asset',        label: 'console.groups.asset',       icon: Folder,       modules: ['asset'] },
  { key: 'portal',       label: 'console.groups.portal',      icon: Shop,         modules: [], isPortal: true },
  { key: 'agent',        label: 'console.groups.agent',       icon: ChatDotRound, modules: ['agent'] },
  { key: 'graph',        label: 'console.groups.graph',       icon: Share,        modules: ['graph'] },
  { key: 'api-docs',     label: 'console.groups.apiDocs',     icon: Memo,         modules: [], isApiDocs: true },
  { key: 'system',       label: 'console.groups.system',      icon: Setting,      modules: ['system'] },
]

// ─── 首页模块卡片配置 ────────────────────────────────────────────────────────
// label/desc 值为 i18n key，渲染时通过 t(card.label) / t(card.desc) 翻译

export const ALL_HOME_CARDS = [
  { module: 'transfer',     label: 'console.modules.transfer.label',     icon: Upload,       cssVar: '--addp-module-transfer',     desc: 'console.modules.transfer.desc' },
  { module: 'meta',         label: 'console.modules.meta.label',         icon: Box,          cssVar: '--addp-module-meta',          desc: 'console.modules.meta.desc' },
  { module: 'manager',      label: 'console.modules.manager.label',      icon: DataAnalysis, cssVar: '--addp-module-manager',       desc: 'console.modules.manager.desc' },
  { module: 'standard',     label: 'console.modules.standard.label',     icon: Reading,      cssVar: '--addp-module-standard',      desc: 'console.modules.standard.desc' },
  { module: 'modeling',     label: 'console.modules.modeling.label',     icon: Grid,         cssVar: '--addp-module-modeling',      desc: 'console.modules.modeling.desc' },
  { module: 'quality',      label: 'console.modules.quality.label',      icon: CircleCheck,  cssVar: '--addp-module-quality',       desc: 'console.modules.quality.desc' },
  { module: 'develop',      label: 'console.modules.develop.label',      icon: Edit,         cssVar: '--addp-module-develop',       desc: 'console.modules.develop.desc' },
  { module: 'service',      label: 'console.modules.service.label',      icon: Link,         cssVar: '--addp-module-service',       desc: 'console.modules.service.desc' },
  { module: 'orchestrator', label: 'console.modules.orchestrator.label', icon: Operation,    cssVar: '--addp-module-orchestrator',  desc: 'console.modules.orchestrator.desc' },
  { module: 'monitor',      label: 'console.modules.monitor.label',      icon: DataLine,     cssVar: '--addp-module-monitor',       desc: 'console.modules.monitor.desc' },
  { module: 'asset',        label: 'console.modules.asset.label',        icon: Folder,       cssVar: '--addp-module-asset',         desc: 'console.modules.asset.desc' },
  { module: 'system',       label: 'console.modules.system.label',       icon: Setting,      cssVar: '--addp-module-system',        desc: 'console.modules.system.desc' },
  { module: 'agent',        label: 'console.modules.agent.label',        icon: ChatDotRound, cssVar: '--addp-module-agent',         desc: 'console.modules.agent.desc' },
  { module: 'graph',        label: 'console.modules.graph.label',        icon: Share,        cssVar: '--addp-module-graph',         desc: 'console.modules.graph.desc' },
]

// ─── 模块 URL（开发用动态 hostname+port，生产用 Nginx 路由）─────────────────

const _dev = import.meta.env.DEV
const _protocol = window.location.protocol
const _host = window.location.hostname

function _url(devPort, prodPath) {
  return _dev ? `${_protocol}//${_host}:${devPort}` : `${_protocol}//${_host}/${prodPath}`
}

export const MODULE_URLS = {
  system:       _url(5173, 'system'),
  manager:      _url(5174, 'manager'),
  meta:         _url(5175, 'meta'),
  transfer:     _url(5176, 'transfer'),
  orchestrator: _url(5177, 'orchestrator'),
  develop:      _url(5178, 'develop'),
  service:      _url(5180, 'service'),
  monitor:      _url(5179, 'monitor'),
  standard:     _url(5181, 'standard'),
  modeling:     _url(5182, 'model'),
  quality:      _url(5183, 'quality'),
  asset:        _url(5184, 'asset'),
  agent:        _url(5186, 'agent'),
  graph:        _url(5187, 'graph'),
}

export const PORTAL_URL = _dev
  ? `${_protocol}//${_host}:5185`
  : `${_protocol}//${_host}/portal`

// ─── 页面映射（menu index 的 page 段 → 实际 URL 路径段）────────────────────
// 规则：map[page] 存在则用 map[page]；否则 page 直接透传。
// '' 键表示无 page 时的默认路由。

export const PAGE_MAPS = {
  manager: {
    'data-explorer': 'data-explorer',
    'data-retrieval': 'data-retrieval',
    'vectorization-tasks': 'vectorization-tasks',
    '': 'data-explorer',
  },
  meta: {
    'scan': 'scan',
    'tasks': 'tasks',
    '': 'scan',
  },
  transfer: {
    'tasks': 'tasks',
    'executions': 'executions',
    'local-engines': 'local-engines',
    '': 'tasks',
  },
  orchestrator: {
    'orchestrations': 'orchestrations',
    'executions': 'executions',
    '': 'orchestrations',
  },
  develop: {
    'sql': 'sql',
    'notebook': 'notebook',
    'gis-workflow': 'gis-workflow',
    'gis-tasks': 'gis-tasks',
    'gis-executions': 'gis-executions',
    '': 'sql',
  },
  service: {
    'services': 'services',
    'query-services': 'query-services',
    'catalog': 'catalog',
    'tile': 'tile',
    '': 'query-services',
  },
  monitor: {
    'dashboard': 'dashboard',
    'executions': 'executions',
    '': 'dashboard',
  },
  standard: {
    'domains': 'standard/domains',
    'glossaries': 'standard/glossaries',
    'elements': 'standard/elements',
    'code-sets': 'standard/code-sets',
    'units': 'standard/units',
    'classifications': 'standard/classifications',
    'metrics': 'standard/metrics',
    'documents': 'standard/documents',
    'dimension-hierarchies': 'standard/dimension-hierarchies',
    '': 'standard/domains',
  },
  modeling: {
    'dw-layers': 'modeling/dw-layers',
    'entities': 'modeling/entities',
    'logical-tables': 'modeling/logical-tables',
    'er-diagram': 'modeling/er-diagram',
    'star-schema': 'modeling/star-schema',
    '': 'modeling/dw-layers',
  },
  quality: {
    'rule-applications': 'quality/rule-applications',
    'check-tasks': 'quality/check-tasks',
    'executions': 'quality/executions',
    'issues': 'quality/issues',
    '': 'quality/check-tasks',
  },
  asset: {
    'type-definitions': 'asset/type-definitions',
    'categories': 'asset/categories',
    'assets': 'asset/assets',
    'applications': 'asset/applications',
    'dashboard': 'asset/dashboard',
    '': 'asset/assets',
  },
  graph: {
    'ontologies':        'ontologies',
    'graphs':            'graphs',
    'analysis':          'analysis',
    'knowledge-service': 'knowledge-service',
    '':                  'ontologies',
  },
  // system 和 agent 无需映射，page 直接透传
}

// ─── 模块默认路由（navigateToModule 使用）───────────────────────────────────

export const DEFAULT_ROUTES = {
  system:       '/system/users',
  manager:      '/manager/data-explorer',
  meta:         '/meta/scan',
  transfer:     '/transfer/tasks',
  orchestrator: '/orchestrator/orchestrations',
  develop:      '/develop/sql',
  service:      '/service/query-services',
  monitor:      '/monitor/dashboard',
  standard:     '/standard/domains',
  modeling:     '/modeling/dw-layers',
  quality:      '/quality/check-tasks',
  asset:        '/asset/assets',
  agent:        '/agent',
  graph:        '/graph/ontologies',
}

// ─── 侧边栏菜单配置（数据驱动渲染，替代 300 行硬编码 template）──────────────
// flat: true 表示直接渲染一个 el-menu-item，而不是 el-sub-menu

// ─── 侧边栏菜单配置（label 值为 i18n key，渲染时通过 t(label) 翻译）─────────
export const SIDEBAR_MENUS = {
  transfer: {
    label: 'console.menus.transfer.label', icon: Upload,
    items: [
      { index: '/transfer/tasks',        icon: List,       label: 'console.menus.transfer.tasks' },
      { index: '/transfer/executions',   icon: Timer,      label: 'console.menus.transfer.executions' },
      { index: '/transfer/local-engines',icon: Connection, label: 'console.menus.transfer.localEngines' },
    ],
  },
  meta: {
    label: 'console.menus.meta.label', icon: Box,
    items: [
      { index: '/meta/scan',  icon: Search,  label: 'console.menus.meta.scan' },
      { index: '/meta/tasks', icon: Monitor, label: 'console.menus.meta.tasks' },
    ],
  },
  manager: {
    label: 'console.menus.manager.label', icon: DataAnalysis,
    items: [
      { index: '/manager/data-explorer',       icon: Search,   label: 'console.menus.manager.dataExplorer' },
      { index: '/manager/data-retrieval',      icon: Document, label: 'console.menus.manager.dataRetrieval' },
      { index: '/manager/vectorization-tasks', icon: List,     label: 'console.menus.manager.vectorizationTasks' },
    ],
  },
  standard: {
    label: 'console.menus.standard.label', icon: Reading,
    items: [
      { index: '/standard/domains',              icon: Share,        label: 'console.menus.standard.domains' },
      { index: '/standard/glossaries',           icon: Document,     label: 'console.menus.standard.glossaries' },
      { index: '/standard/elements',             icon: DataBoard,    label: 'console.menus.standard.elements' },
      { index: '/standard/code-sets',            icon: List,         label: 'console.menus.standard.codeSets' },
      { index: '/standard/units',                icon: Odometer,     label: 'console.menus.standard.units' },
      { index: '/standard/classifications',      icon: Share,        label: 'console.menus.standard.classifications' },
      { index: '/standard/dimension-hierarchies',icon: SortDown,     label: 'console.menus.standard.dimensionHierarchies' },
      { index: '/standard/metrics',              icon: TrendCharts,  label: 'console.menus.standard.metrics' },
      { index: '/standard/documents',            icon: FolderOpened, label: 'console.menus.standard.documents' },
    ],
  },
  modeling: {
    label: 'console.menus.modeling.label', icon: Grid,
    items: [
      { index: '/modeling/dw-layers',      icon: Grid,       label: 'console.menus.modeling.dwLayers' },
      { index: '/modeling/entities',       icon: DataBoard,  label: 'console.menus.modeling.entities' },
      { index: '/modeling/er-diagram',     icon: Share,      label: 'console.menus.modeling.erDiagram' },
      { index: '/modeling/logical-tables', icon: Connection, label: 'console.menus.modeling.logicalTables' },
      { index: '/modeling/star-schema',    icon: Grid,       label: 'console.menus.modeling.starSchema' },
    ],
  },
  quality: {
    label: 'console.menus.quality.label', icon: CircleCheck,
    items: [
      { index: '/quality/rule-applications', icon: Setting, label: 'console.menus.quality.ruleApplications' },
      { index: '/quality/check-tasks',       icon: List,    label: 'console.menus.quality.checkTasks' },
      { index: '/quality/executions',        icon: Timer,   label: 'console.menus.quality.executions' },
      { index: '/quality/issues',            icon: Warning, label: 'console.menus.quality.issues' },
    ],
  },
  develop: {
    label: 'console.menus.develop.label', icon: Edit,
    items: [
      { index: '/develop/sql',       icon: Monitor,    label: 'console.menus.develop.sql' },
      { index: '/develop/notebook',  icon: Notebook,   label: 'console.menus.develop.notebook' },
      { index: '/develop/workflow',  icon: Connection, label: 'console.menus.develop.workflow' },
      { index: '/develop/tasks',     icon: List,       label: 'console.menus.develop.tasks' },
      { index: '/develop/executions',icon: Timer,      label: 'console.menus.develop.executions' },
    ],
  },
  service: {
    label: 'console.menus.service.label', icon: Link,
    items: [
      { index: '/service/query-services',  icon: Upload,       label: 'console.menus.service.queryServices' },
      { index: '/service/tile',            icon: Grid,         label: 'console.menus.service.tile' },
      { index: '/service/graph-services',  icon: Share,        label: 'console.menus.service.graphServices' },
      { index: '/service/services',        icon: Connection,   label: 'console.menus.service.services' },
      { index: '/service/catalog',         icon: FolderOpened, label: 'console.menus.service.catalog' },
    ],
  },
  orchestrator: {
    label: 'console.menus.orchestrator.label', icon: Operation,
    items: [
      { index: '/orchestrator/orchestrations', icon: List,  label: 'console.menus.orchestrator.orchestrations' },
      { index: '/orchestrator/executions',     icon: Timer, label: 'console.menus.orchestrator.executions' },
    ],
  },
  monitor: {
    label: 'console.menus.monitor.label', icon: DataLine,
    items: [
      { index: '/monitor/dashboard',  icon: Monitor, label: 'console.menus.monitor.dashboard' },
      { index: '/monitor/executions', icon: List,    label: 'console.menus.monitor.executions' },
    ],
  },
  asset: {
    label: 'console.menus.asset.label', icon: Folder,
    items: [
      { index: '/asset/type-definitions', icon: Grid,         label: 'console.menus.asset.typeDefinitions' },
      { index: '/asset/categories',       icon: Files,        label: 'console.menus.asset.categories' },
      { index: '/asset/assets',           icon: List,         label: 'console.menus.asset.assets' },
      { index: '/asset/applications',     icon: Tickets,      label: 'console.menus.asset.applications' },
      { index: '/asset/dashboard',        icon: DataAnalysis, label: 'console.menus.asset.dashboard' },
    ],
  },
  agent: {
    label: 'console.menus.agent.label', icon: ChatDotRound,
    flat: true,
    index: '/agent',
  },
  graph: {
    label: 'console.menus.graph.label', icon: Share,
    items: [
      { index: '/graph/ontologies',        icon: Share,      label: 'console.menus.graph.ontologies' },
      { index: '/graph/graphs',            icon: Connection, label: 'console.menus.graph.graphs' },
      { index: '/graph/analysis',          icon: DataLine,   label: 'console.menus.graph.analysis' },
      { index: '/graph/knowledge-service', icon: Link,       label: 'console.menus.graph.knowledgeService' },
    ],
  },
  system: {
    label: 'console.menus.system.label', icon: Setting,
    items: [
      { index: '/system/users',        icon: User,       label: 'console.menus.system.users' },
      { index: '/system/engines',      icon: Connection, label: 'console.menus.system.engines' },
      { index: '/system/applications', icon: Key,        label: 'console.menus.system.applications' },
      { index: '/system/logs',         icon: Document,   label: 'console.menus.system.logs' },
      { index: '/system/cleanup',      icon: Delete,     label: 'console.menus.system.cleanup' },
    ],
  },
}

// ─── 核心纯函数：根据模块名+页面名+token 构建 iframe URL ────────────────────

export function buildModuleUrl(module, page, token) {
  const base = MODULE_URLS[module]
  if (!base) return null

  const map = PAGE_MAPS[module]
  // map[page] 存在则用映射值；否则透传 page（'' 键作为默认路由）
  const actualPage = (map && map[page] !== undefined) ? map[page] : page

  const url = actualPage ? `${base}/${actualPage}` : base

  if (token) {
    const sep = url.includes('?') ? '&' : '?'
    return `${url}${sep}token=${encodeURIComponent(token)}`
  }
  return url
}
