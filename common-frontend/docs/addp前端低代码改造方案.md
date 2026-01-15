# ADDP 前端低代码改造方案

## 📋 文档信息

- **创建日期**: 2026-01-14
- **方案版本**: v1.0
- **状态**: 待实施

---

## 1. 背景与目标

### 1.1 业务目标

**主要目标**: 对外产品化（兼顾内部开发提效）

- 让其他企业在低代码平台（宜搭、AMIS等）中集成ADDP能力，作为标准组件使用
- 客户可在低代码平台中拖拽"ADDP资源树"、"存储引擎表单"等组件来构建自己的应用
- 同时保留传统开发方式，供技术团队使用

### 1.2 用户画像

- **技术人员**: 会配置JSON Schema，能理解API调用
- **业务人员**: 使用低代码平台可视化拖拽，需要简单易用的配置界面

### 1.3 核心约束

- ✅ 保持 Vue 3 技术栈不变
- ✅ 不重复实现现有组件
- ✅ UI组件 + 带业务逻辑的组件（不需要整个页面）
- ✅ 不着急，按需迭代

---

## 2. 核心决策

### 2.1 低代码平台选择

**方案: Web Components（浏览器原生自定义元素）**

**理由**:
- ✅ **平台无关**: 任何低代码平台都能用（宜搭、AMIS、Tmagic、飞书等）
- ✅ **保持Vue技术栈**: Vue 3 原生支持 `defineCustomElement`，开发体验不变
- ✅ **复用现有代码**: 直接转换 `common-frontend` 现有组件，无需重写
- ✅ **渐进式集成**: 可先提供通用版本，按需为特定平台开发深度适配器

**备选方案**:
- 腾讯 Tmagic Editor: 官方支持Vue 3，但生态较小
- 阿里低代码引擎: Vue支持是二等公民，主要面向React

### 2.2 API策略

**方案: 客户自部署 + SDK**

**架构**:
```
客户A自己部署ADDP (https://addp.customer-a.com)
    ↓
客户的低代码应用使用ADDP组件
    ↓
组件通过SDK调用客户自己的ADDP API
```

**关键特性**:
- 数据在客户自己控制，私有化部署
- 复用现有JWT认证机制
- 客户自己配置CORS和生成API Token
- 不需要维护公共API网关

**对比方案**:

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| **客户自部署 + SDK** ⭐ | 数据安全可控，无需维护公共服务，客户可自定义 | 客户需要自己部署和配置 | 私有化部署为主的产品 |
| 公共API网关 + SaaS | 客户零部署，自动跨域和认证 | 需承担所有流量成本，数据集中存储 | 纯SaaS产品 |
| 混合模式 | 灵活支持两种场景 | 实现复杂度高 | 未来扩展方向 |

---

## 3. 技术方案

### 3.1 Web Components 核心架构

**转换流程**:
```
现有: common-frontend (Vue 3组件库)
        ↓ 转换
Web Components (标准自定义元素)
        ↓ 使用
任何低代码平台 + 配置JSON
```

**示例代码**:

```javascript
// 1. 原有Vue组件（保持不变）
// common-frontend/basic/src/components/ResourceTree.vue
export default {
  name: 'ResourceTree',
  props: ['treeData', 'loading'],
  emits: ['node-click']
}

// 2. Web Component包装器（新增）
// common-frontend/webcomponents/src/ResourceTree.js
import { defineCustomElement } from 'vue';
import ResourceTreeVue from '@addp/common-frontend/ResourceTree.vue';

const ResourceTreeElement = defineCustomElement(ResourceTreeVue);
customElements.define('addp-resource-tree', ResourceTreeElement);

// 3. 在任何HTML页面中使用
<addp-resource-tree
  api-url="https://your-addp.com/api/resources"
  access-token="YOUR_JWT_TOKEN"
></addp-resource-tree>
```

**构建产物**:
```
common-frontend/webcomponents/dist/
├── addp-components.js      # UMD/ESM 格式
├── addp-components.css     # 样式
└── metadata.json           # 组件元数据（给低代码编辑器用）
```

### 3.2 与低代码平台集成方式

#### 方式A: 直接使用（最简单）

大部分低代码平台都支持"自定义组件"或"HTML组件"：

**宜搭示例**:
```javascript
// 1. 在宜搭页面配置中引入JS
<script src="https://cdn.addp.com/components/addp-components.js"></script>

// 2. 使用自定义组件
{
  "componentName": "HTMLCustom",
  "props": {
    "html": "<addp-resource-tree api-url='${dataSource.url}'></addp-resource-tree>"
  }
}
```

**百度AMIS示例**:
```json
{
  "type": "html",
  "html": "<addp-resource-tree api-url='${apiUrl}'></addp-resource-tree>"
}
```

**优点**: 零适配工作，立即可用
**缺点**: 低代码编辑器不认识组件，没有可视化属性面板

#### 方式B: 深度集成（推荐）

为特定低代码平台提供"适配器"，让编辑器认识ADDP组件：

**架构**:
```
Web Component (核心逻辑，所有平台通用)
    ↓
├─ 宜搭适配器 → 在宜搭编辑器中可拖拽、可配置
├─ AMIS适配器 → 在AMIS编辑器中可拖拽、可配置
└─ Tmagic适配器 → 在Tmagic编辑器中可拖拽、可配置
```

**AMIS适配器示例**:

```javascript
// Step 1: AMIS渲染器包装（调用Web Component）
import { Renderer } from 'amis';

@Renderer({
  type: 'addp-resource-tree',
  autoVar: true
})
class ResourceTreeAMISWrapper {
  render() {
    const { apiUrl, title, searchable } = this.props;

    return `
      <addp-resource-tree
        api-url="${apiUrl}"
        title="${title}"
        searchable="${searchable}"
      ></addp-resource-tree>
    `;
  }
}

// Step 2: AMIS编辑器插件（拖拽+属性面板）
import { BasePlugin } from 'amis-editor';

class ResourceTreePlugin extends BasePlugin {
  rendererName = 'addp-resource-tree';
  name = 'ADDP资源树';
  icon = 'fa fa-sitemap';

  // 属性配置面板
  panelBody = [
    { type: 'input-text', name: 'apiUrl', label: 'API地址' },
    { type: 'input-text', name: 'title', label: '标题' },
    { type: 'switch', name: 'searchable', label: '启用搜索' }
  ];
}
```

**用户体验**:
1. 在AMIS编辑器左侧组件面板看到"ADDP资源树"
2. 拖拽到画布
3. 右侧属性面板配置API地址、标题等
4. 实时预览
5. 发布后页面正常运行

**优点**:
- ✅ 核心逻辑只写一次（Web Component）
- ✅ 各平台体验原生（有可视化编辑）
- ✅ 维护成本可控（适配器代码很薄）

**缺点**:
- ❌ 需要为每个平台写适配器（但工作量不大，1-2天/平台）

### 3.3 ADDP SDK 设计

**目的**: 封装API调用逻辑，处理认证、跨域、错误处理

**核心代码**:

```javascript
// addp-sdk.js
class ADDPClient {
  constructor({ baseURL, token }) {
    this.baseURL = baseURL;
    this.token = token;
  }

  async request(endpoint, options = {}) {
    const response = await fetch(`${this.baseURL}${endpoint}`, {
      ...options,
      headers: {
        'Authorization': `Bearer ${this.token}`,
        'Content-Type': 'application/json',
        ...options.headers
      }
    });

    if (!response.ok) {
      throw new Error(`ADDP API Error: ${response.statusText}`);
    }

    return response.json();
  }

  // Manager模块API
  getResourceTree() {
    return this.request('/api/manager/resources/tree');
  }

  previewObject(engineId, path) {
    return this.request(`/api/manager/object/${engineId}/preview?path=${path}`);
  }

  // System模块API
  getStorageEngines() {
    return this.request('/api/system/engines');
  }

  createStorageEngine(data) {
    return this.request('/api/system/engines', {
      method: 'POST',
      body: JSON.stringify(data)
    });
  }
}

export default ADDPClient;
```

**在Web Component中使用**:

```javascript
// ResourceTree.js
import { defineCustomElement } from 'vue';
import ADDPClient from './addp-sdk';

export default defineCustomElement({
  props: {
    apiBase: String,
    accessToken: String,
    title: String
  },

  setup(props) {
    const client = new ADDPClient({
      baseURL: props.apiBase,
      token: props.accessToken
    });

    const loadData = async () => {
      try {
        const data = await client.getResourceTree();
        // 更新组件状态
      } catch (error) {
        console.error('加载资源树失败:', error);
      }
    };

    onMounted(() => {
      loadData();
    });
  }
});
```

---

## 4. 认证与跨域处理

### 4.1 客户端配置

**客户在低代码平台中的配置**:

```html
<addp-resource-tree
  api-base="https://addp.customer-a.com"
  access-token="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
></addp-resource-tree>
```

### 4.2 服务端配置（客户需要做的）

#### 步骤1: 配置CORS

编辑客户ADDP实例的 `.env` 文件:

```bash
# 允许的跨域来源（客户的低代码平台域名）
CORS_ALLOWED_ORIGINS=https://yida-customer-a.com,https://amis-customer-a.com
```

#### 步骤2: 创建API专用账号

1. 登录ADDP管理后台
2. 创建专用账号（如 `lowcode-api`）
3. 生成长期有效的JWT token（或配置token自动续期）
4. 配置账号权限（建议只读，限制敏感操作）

#### 步骤3: 获取Token

**方式A: 通过登录接口获取**:
```bash
curl -X POST https://addp.customer-a.com/api/system/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"lowcode-api","password":"..."}'
```

**方式B: 在后台直接生成**（未来可实现）:
```
系统设置 → API管理 → 创建Token → 复制Token
```

### 4.3 安全增强（可选）

**IP白名单**:
```go
// gateway/internal/middleware/ip_whitelist.go
func IPWhitelist(allowedIPs []string) gin.HandlerFunc {
  return func(c *gin.Context) {
    clientIP := c.ClientIP()
    if !contains(allowedIPs, clientIP) {
      c.JSON(403, gin.H{"error": "IP not allowed"})
      c.Abort()
      return
    }
    c.Next()
  }
}
```

**Token权限控制**:
```go
// system/backend/internal/middleware/auth.go
func AuthMiddleware() gin.HandlerFunc {
  return func(c *gin.Context) {
    token := extractToken(c)
    claims := validateToken(token)

    // 检查token的权限范围
    if claims.Scope == "readonly" && c.Request.Method != "GET" {
      c.JSON(403, gin.H{"error": "Read-only token"})
      c.Abort()
      return
    }

    c.Next()
  }
}
```

---

## 5. 实施计划

### 阶段1: 开发Web Components核心（1周）

**目标**: 将3个核心组件转换为Web Components

**任务清单**:
- [ ] 创建 `common-frontend/webcomponents` 目录
- [ ] 配置构建工具（Vite/Rollup）
- [ ] 转换组件:
  - [ ] ResourceTree → `<addp-resource-tree>`
  - [ ] StorageEngineForm → `<addp-storage-engine-form>`
  - [ ] ScheduleConfig → `<addp-schedule-config>`
- [ ] 开发ADDP SDK (`addp-sdk.js`)
- [ ] 打包生成 `dist/addp-components.js`

**验证方式**:
```html
<!-- 创建简单HTML测试页面 -->
<!DOCTYPE html>
<html>
<head>
  <script src="dist/addp-components.js"></script>
  <link rel="stylesheet" href="dist/addp-components.css">
</head>
<body>
  <addp-resource-tree
    api-base="http://localhost:8000"
    access-token="YOUR_TOKEN"
  ></addp-resource-tree>
</body>
</html>
```

### 阶段2: 文档和示例（3天）

**产出**:
- [ ] **集成指南** (`docs/LOWCODE_INTEGRATION.md`)
  - 客户如何配置CORS
  - 如何创建API账号和获取Token
  - 各低代码平台的集成示例
- [ ] **API文档** (`docs/COMPONENT_API.md`)
  - 每个组件的属性列表
  - 事件列表
  - 使用示例
- [ ] **在线Demo**
  - 部署到 `https://demo.addp.com/lowcode`
  - 包含所有组件的演示

### 阶段3: 深度集成（按需）

**根据客户需求选择平台**:

| 平台 | 工作量 | 产物 |
|------|--------|------|
| 百度AMIS | 2-3天 | `addp-components-amis-adapter.js` |
| 宜搭 | 2-3天 | `addp-components-yida-adapter.js` |
| 腾讯Tmagic | 2-3天 | `addp-components-tmagic-adapter.js` |

**每个适配器包含**:
- 渲染器包装（调用Web Component）
- 编辑器插件（拖拽、属性面板）
- 组件元数据（icon、tags、scaffold）

---

## 6. 目录结构

```
common-frontend/
├── basic/                          # 现有Vue组件（保持不变）
│   └── src/components/
│       ├── ResourceTree.vue
│       ├── StorageEngineForm.vue
│       └── ScheduleConfig.vue
│
├── webcomponents/                  # 新增：Web Components
│   ├── src/
│   │   ├── core/
│   │   │   └── addp-sdk.js        # ADDP API SDK
│   │   ├── components/
│   │   │   ├── ResourceTree.js    # Web Component包装
│   │   │   ├── StorageEngineForm.js
│   │   │   └── ScheduleConfig.js
│   │   └── index.js               # 统一导出
│   ├── dist/
│   │   ├── addp-components.js     # 构建产物（UMD）
│   │   ├── addp-components.css
│   │   └── metadata.json
│   ├── package.json
│   └── vite.config.js             # 构建配置
│
├── adapters/                       # 可选：低代码平台适配器
│   ├── amis/
│   │   ├── src/
│   │   │   ├── renderers/
│   │   │   └── plugins/
│   │   └── dist/
│   │       └── addp-amis-adapter.js
│   ├── yida/
│   └── tmagic/
│
└── docs/
    ├── ARCHITECTURE.md
    ├── addp前端低代码改造方案.md     # 本文档
    ├── LOWCODE_INTEGRATION.md        # 待创建：集成指南
    └── COMPONENT_API.md              # 待创建：组件API文档
```

---

## 7. 工作量估算

| 阶段 | 任务 | 工时 | 备注 |
|------|------|------|------|
| **阶段1** | Web Components核心开发 | 3-5天 | 包含3个核心组件 + SDK |
| | ADDP SDK | 1-2天 | 封装API调用逻辑 |
| **阶段2** | 文档和示例 | 2-3天 | 集成指南、API文档、Demo |
| **阶段3** | 深度集成（可选） | 2-3天/平台 | AMIS/宜搭/Tmagic适配器 |
| **总计** | 最小版本（阶段1+2） | **1-2周** | 可对外发布的组件包 |
| | 完整版本（含1个适配器） | **2-3周** | 包含深度集成 |

---

## 8. 风险与应对

### 8.1 技术风险

| 风险 | 影响 | 应对措施 |
|------|------|----------|
| Vue 3 `defineCustomElement` 兼容性问题 | 高 | 提前验证，准备降级方案（纯JS实现） |
| 样式隔离冲突 | 中 | 使用Shadow DOM + CSS变量 |
| 低代码平台限制自定义组件 | 高 | 优先选择支持自定义组件的平台 |
| 跨域CORS配置复杂 | 低 | 提供详细文档和配置工具 |

### 8.2 业务风险

| 风险 | 影响 | 应对措施 |
|------|------|----------|
| 客户不愿意自己部署ADDP | 中 | 未来提供SaaS版本（混合模式） |
| Token安全问题 | 高 | 实现IP白名单、权限控制、短期token |
| 文档不完善导致集成困难 | 中 | 投入充足时间编写文档和示例 |

---

## 9. 成功指标

### 9.1 技术指标

- ✅ 3个核心组件成功转换为Web Components
- ✅ 在HTML页面中独立运行正常
- ✅ 组件包体积 < 500KB（gzip后）
- ✅ 首次加载时间 < 2s

### 9.2 业务指标

- ✅ 至少1个客户成功集成到低代码平台
- ✅ 集成文档完整率 > 90%
- ✅ 客户集成成功率 > 80%（首次尝试成功）
- ✅ 客户反馈满意度 > 4分（5分制）

---

## 10. 下一步行动

### 10.1 立即行动（本周）

1. [ ] **技术验证**: 创建最小POC，验证 Vue 3 `defineCustomElement` 可行性
2. [ ] **需求确认**: 与产品/销售确认目标客户使用的低代码平台
3. [ ] **资源分配**: 确定开发人员和时间安排

### 10.2 短期计划（2周内）

1. [ ] 完成阶段1：Web Components核心开发
2. [ ] 完成阶段2：文档和示例
3. [ ] 内部测试和反馈收集

### 10.3 中期计划（1-2月）

1. [ ] 与1-2个试点客户合作集成
2. [ ] 根据反馈优化组件和文档
3. [ ] 按需开发低代码平台深度适配器

---

## 11. 附录

### 11.1 参考资源

**Vue 3 Web Components**:
- [Vue 3 官方文档 - defineCustomElement](https://vuejs.org/guide/extras/web-components.html)
- [Building Web Components with Vue 3](https://vueschool.io/articles/vuejs-tutorials/building-web-components-with-vue-3/)

**低代码平台**:
- [百度AMIS官方文档](https://aisuda.bce.baidu.com/amis/)
- [腾讯Tmagic Editor](https://tmagic-editor.com/)
- [阿里低代码引擎](https://lowcode-engine.com/)

**Web Components标准**:
- [MDN - Web Components](https://developer.mozilla.org/en-US/docs/Web/Web_Components)
- [Custom Elements Everywhere](https://custom-elements-everywhere.com/)

### 11.2 相关文档

- [ADDP核心概念说明](../../docs/addp核心概念说明.md)
- [ADDP共享模块介绍](../../docs/addp共享模块介绍.md)
- [Common Frontend README](../README.md)
- [Common Frontend架构设计](./ARCHITECTURE.md)

---

## 12. 更新记录

| 日期 | 版本 | 变更内容 | 作者 |
|------|------|----------|------|
| 2026-01-14 | v1.0 | 初始版本 | - |

