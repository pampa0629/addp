import {
  Coin, Reading, Tools, Folder, Shop, ChatDotRound, Memo, Setting,
  Upload, Box, DataAnalysis, Grid, CircleCheck, Edit, Link, Operation, DataLine,
  List, Timer, Connection, Search, Document, Share, DataBoard, Odometer,
  TrendCharts, SortDown, FolderOpened, Warning, Monitor, Notebook,
  Files, Tickets, Key, Delete, User,
} from '@element-plus/icons-vue'

// ─── 群组导航配置 ────────────────────────────────────────────────────────────

export const MODULE_GROUPS = [
  { key: 'data-prepare', label: '数据准备',   icon: Coin,         modules: ['transfer', 'meta', 'manager'] },
  { key: 'data-govern',  label: '数据治理',   icon: Reading,      modules: ['standard', 'modeling', 'quality'] },
  { key: 'dev-monitor',  label: '开发与监控', icon: Tools,        modules: ['develop', 'service', 'orchestrator', 'monitor'] },
  { key: 'asset',        label: '资产管理',   icon: Folder,       modules: ['asset'] },
  { key: 'portal',       label: '资产门户',   icon: Shop,         modules: [], isPortal: true },
  { key: 'agent',        label: '智能体',     icon: ChatDotRound, modules: ['agent'] },
  { key: 'graph',        label: '知识图谱',   icon: Share,        modules: ['graph'] },
  { key: 'api-docs',     label: 'API 文档',   icon: Memo,         modules: [], isApiDocs: true },
  { key: 'system',       label: '系统管理',   icon: Setting,      modules: ['system'] },
]

// ─── 首页模块卡片配置 ────────────────────────────────────────────────────────

export const ALL_HOME_CARDS = [
  { module: 'transfer',     label: '数据传输',   icon: Upload,       cssVar: '--addp-module-transfer',     desc: '数据导入、数据导出、任务调度' },
  { module: 'meta',         label: '元数据管理', icon: Box,          cssVar: '--addp-module-meta',          desc: '元数据解析、数据血缘、数据目录' },
  { module: 'manager',      label: '数据管理',   icon: DataAnalysis, cssVar: '--addp-module-manager',       desc: '数据探查、目录组织、数据预览' },
  { module: 'standard',     label: '数据标准',   icon: Reading,      cssVar: '--addp-module-standard',      desc: '业务域、业务术语、数据元、码值集' },
  { module: 'modeling',     label: '数据建模',   icon: Grid,         cssVar: '--addp-module-modeling',      desc: '数仓分层、业务实体、逻辑表设计' },
  { module: 'quality',      label: '数据质量',   icon: CircleCheck,  cssVar: '--addp-module-quality',       desc: '规则应用、质量检查、执行记录' },
  { module: 'develop',      label: '数据开发',   icon: Edit,         cssVar: '--addp-module-develop',       desc: '查询工作台、工作流编辑器、Notebook' },
  { module: 'service',      label: '数据服务',   icon: Link,         cssVar: '--addp-module-service',       desc: '外部服务注册、OGC 服务、查询服务' },
  { module: 'orchestrator', label: '任务编排',   icon: Operation,    cssVar: '--addp-module-orchestrator',  desc: '工作流编排、任务调度、执行管理' },
  { module: 'monitor',      label: '执行监控',   icon: DataLine,     cssVar: '--addp-module-monitor',       desc: '任务执行监控、统计分析、健康检查' },
  { module: 'asset',        label: '资产管理',   icon: Folder,       cssVar: '--addp-module-asset',         desc: '资产类型、分类管理、申请与授权' },
  { module: 'system',       label: '系统管理',   icon: Setting,      cssVar: '--addp-module-system',        desc: '用户管理、日志查询、引擎配置' },
  { module: 'agent',        label: '智能体',     icon: ChatDotRound, cssVar: '--addp-module-agent',         desc: '自然语言对话、数据管理、智能分析' },
  { module: 'graph',        label: '知识图谱',   icon: Share,        cssVar: '--addp-module-graph',         desc: '本体建模、知识图谱构建、图谱探索' },
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

export const SIDEBAR_MENUS = {
  transfer: {
    label: '数据传输', icon: Upload,
    items: [
      { index: '/transfer/tasks',        icon: List,       label: '传输任务' },
      { index: '/transfer/executions',   icon: Timer,      label: '执行记录' },
      { index: '/transfer/local-engines',icon: Connection, label: '本地资源' },
    ],
  },
  meta: {
    label: '元数据', icon: Box,
    items: [
      { index: '/meta/scan',  icon: Search,  label: '元数据扫描' },
      { index: '/meta/tasks', icon: Monitor, label: '任务监控' },
    ],
  },
  manager: {
    label: '数据管理', icon: DataAnalysis,
    items: [
      { index: '/manager/data-explorer',       icon: Search,   label: '数据探查' },
      { index: '/manager/data-retrieval',      icon: Document, label: '数据检索' },
      { index: '/manager/vectorization-tasks', icon: List,     label: '向量化任务' },
    ],
  },
  standard: {
    label: '数据标准', icon: Reading,
    items: [
      { index: '/standard/domains',              icon: Share,        label: '业务域管理' },
      { index: '/standard/glossaries',           icon: Document,     label: '业务术语' },
      { index: '/standard/elements',             icon: DataBoard,    label: '数据元管理' },
      { index: '/standard/code-sets',            icon: List,         label: '码值集管理' },
      { index: '/standard/units',                icon: Odometer,     label: '计量单位' },
      { index: '/standard/classifications',      icon: Share,        label: '分类与分级' },
      { index: '/standard/dimension-hierarchies',icon: SortDown,     label: '维度层级' },
      { index: '/standard/metrics',              icon: TrendCharts,  label: '指标管理' },
      { index: '/standard/documents',            icon: FolderOpened, label: '标准文档' },
    ],
  },
  modeling: {
    label: '数据建模', icon: Grid,
    items: [
      { index: '/modeling/dw-layers',      icon: Grid,       label: '数仓分层' },
      { index: '/modeling/entities',       icon: DataBoard,  label: '业务实体' },
      { index: '/modeling/er-diagram',     icon: Share,      label: '实体关系图' },
      { index: '/modeling/logical-tables', icon: Connection, label: '逻辑表设计' },
      { index: '/modeling/star-schema',    icon: Grid,       label: '星型建模视图' },
    ],
  },
  quality: {
    label: '数据质量', icon: CircleCheck,
    items: [
      { index: '/quality/rule-applications', icon: Setting, label: '规则应用配置' },
      { index: '/quality/check-tasks',       icon: List,    label: '检查任务' },
      { index: '/quality/executions',        icon: Timer,   label: '执行记录' },
      { index: '/quality/issues',            icon: Warning, label: '问题工单' },
    ],
  },
  develop: {
    label: '数据开发', icon: Edit,
    items: [
      { index: '/develop/sql',       icon: Monitor,    label: '查询工作台' },
      { index: '/develop/notebook',  icon: Notebook,   label: 'Notebook 开发' },
      { index: '/develop/workflow',  icon: Connection, label: '工作流编辑器' },
      { index: '/develop/tasks',     icon: List,       label: '任务管理' },
      { index: '/develop/executions',icon: Timer,      label: '执行监控' },
    ],
  },
  service: {
    label: '数据服务', icon: Link,
    items: [
      { index: '/service/query-services',  icon: Upload,       label: '查询服务' },
      { index: '/service/tile',            icon: Grid,         label: '瓦片服务' },
      { index: '/service/graph-services', icon: Share,        label: '图查询服务' },
      { index: '/service/services',        icon: Connection,   label: '服务注册' },
      { index: '/service/catalog',         icon: FolderOpened, label: '服务目录' },
    ],
  },
  orchestrator: {
    label: '任务编排', icon: Operation,
    items: [
      { index: '/orchestrator/orchestrations', icon: List,  label: '编排任务' },
      { index: '/orchestrator/executions',     icon: Timer, label: '执行记录' },
    ],
  },
  monitor: {
    label: '执行监控', icon: DataLine,
    items: [
      { index: '/monitor/dashboard',  icon: Monitor, label: '监控仪表盘' },
      { index: '/monitor/executions', icon: List,    label: '执行记录' },
    ],
  },
  asset: {
    label: '资产管理', icon: Folder,
    items: [
      { index: '/asset/type-definitions', icon: Grid,         label: '资产类型' },
      { index: '/asset/categories',       icon: Files,        label: '目录管理' },
      { index: '/asset/assets',           icon: List,         label: '资产工作台' },
      { index: '/asset/applications',     icon: Tickets,      label: '审批管理' },
      { index: '/asset/dashboard',        icon: DataAnalysis, label: '运营看板' },
    ],
  },
  agent: {
    label: '智能体', icon: ChatDotRound,
    flat: true,
    index: '/agent',
  },
  graph: {
    label: '知识图谱', icon: Share,
    items: [
      { index: '/graph/ontologies',        icon: Share,      label: '本体建模' },
      { index: '/graph/graphs',            icon: Connection, label: '知识图谱' },
      { index: '/graph/analysis',          icon: DataLine,   label: '图算法分析' },
      { index: '/graph/knowledge-service', icon: Link,       label: '知识服务' },
    ],
  },
  system: {
    label: '系统管理', icon: Setting,
    items: [
      { index: '/system/users',        icon: User,       label: '用户管理' },
      { index: '/system/engines',      icon: Connection, label: '引擎管理' },
      { index: '/system/applications', icon: Key,        label: '应用管理' },
      { index: '/system/logs',         icon: Document,   label: '日志审计' },
      { index: '/system/cleanup',      icon: Delete,     label: '垃圾清理' },
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
