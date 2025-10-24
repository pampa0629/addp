# 自动字段映射功能

## 概述

本文档描述数据传输模块的自动字段映射功能实现，满足用户需求：
1. ✅ **自动同名映射** - 进入字段映射步骤时自动进行同名字段匹配
2. ✅ **自动获取字段** - 从元数据模块自动获取表的字段列表
3. ✅ **自动创建目标字段** - 对象存储目标支持自动创建所有源字段

## 功能实现

### 1. 后端 API（Meta 模块）

#### 新增 API 端点

**GET `/api/meta/metadata/fields`** - 获取表的字段列表

**请求参数**:
- `resource_id` (required) - 数据源ID
- `table_name` (required) - 表名

**响应示例**:
```json
[
    "id",
    "name",
    "email",
    "phone",
    "address",
    "created_at",
    "updated_at"
]
```

#### 代码修改

**文件**: `meta/backend/internal/api/handler.go`

```go
// GetTableFields 获取表的字段列表（用于Transfer模块字段映射）
// GET /api/metadata/fields?resource_id=1&table_name=users
func (h *Handler) GetTableFields(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	tableName := c.Query("table_name")

	if resourceIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing resource_id"})
		return
	}
	if tableName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing table_name"})
		return
	}

	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	// 获取表字段信息
	fields, err := h.scanService.GetTableFields(uint(resourceID), tableName, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, fields)
}
```

**文件**: `meta/backend/internal/api/router_new.go`

```go
// 元数据相关
api.GET("/metadata/object", handler.GetObjectMetadata)
api.POST("/metadata/extract", handler.ExtractObjectMetadata)
api.GET("/metadata/tables", handler.GetTables)
api.GET("/metadata/fields", handler.GetTableFields)  // 新增路由
```

**文件**: `meta/backend/internal/service/scan_service_new.go`

```go
// GetTableFields 获取表的字段列表（用于Transfer模块字段映射）
func (s *ScanServiceNew) GetTableFields(resourceID uint, tableName string, tenantID uint) ([]string, error) {
	// 1. 先获取该资源下的所有节点ID（schema nodes）
	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND res_id = ?", tenantID, resourceID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []string{}, nil
	}

	// 2. 查询指定表名的 meta_item
	var item models.MetaItem
	err = s.db.Where("tenant_id = ? AND node_id IN (?) AND name = ? AND deleted_at IS NULL", tenantID, nodeIDs, tableName).
		First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return []string{}, fmt.Errorf("table '%s' not found in metadata", tableName)
		}
		return nil, err
	}

	// 3. 从 Attributes 中提取字段列表
	// 字段信息存储在 attributes.fields 中，格式为 []map[string]interface{}
	fieldsData, ok := item.Attributes["fields"]
	if !ok {
		return []string{}, nil
	}

	fieldsList, ok := fieldsData.([]interface{})
	if !ok {
		return []string{}, fmt.Errorf("invalid fields format in metadata")
	}

	// 4. 提取字段名
	fieldNames := make([]string, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}
		fieldName, ok := fieldMap["name"].(string)
		if !ok {
			continue
		}
		fieldNames = append(fieldNames, fieldName)
	}

	return fieldNames, nil
}
```

### 2. 前端实现（Transfer 模块）

#### 自动字段获取

**文件**: `transfer/frontend/src/views/TaskWizard.vue`

```javascript
// 获取字段列表（用于字段映射）
const handleFetchFields = async (type) => {
  const isSource = type === 'source'
  const resourceId = isSource ? taskForm.value.source_id : taskForm.value.target_id
  const tableName = isSource ? sourceConfig.value.table : targetConfig.value.table

  if (!resourceId) {
    ElMessage.warning(`请先选择${isSource ? '源' : '目标'}数据源`)
    return
  }

  if (!tableName) {
    ElMessage.warning(`请先选择${isSource ? '源' : '目标'}表`)
    return
  }

  try {
    const token = localStorage.getItem('token')
    const response = await axios.get(`http://localhost:8082/api/meta/metadata/fields`, {
      params: {
        resource_id: resourceId,
        table_name: tableName
      },
      headers: { Authorization: `Bearer ${token}` }
    })

    if (response.data && Array.isArray(response.data)) {
      if (isSource) {
        sourceFields.value = response.data
        ElMessage.success(`已加载 ${response.data.length} 个源字段`)
      } else {
        targetFields.value = response.data
        ElMessage.success(`已加载 ${response.data.length} 个目标字段`)
      }
    } else {
      ElMessage.warning('未获取到字段信息')
    }
  } catch (error) {
    console.error('获取字段列表失败:', error)
    ElMessage.error('获取字段列表失败: ' + (error.response?.data?.error || error.message))
  }
}
```

#### 自动同名映射

**进入字段映射步骤时自动触发**:

```javascript
const nextStep = async () => {
  // 验证当前步骤
  if (currentStep.value === 0) {
    try {
      await basicFormRef.value?.validate()
    } catch {
      ElMessage.warning('请完善基本信息')
      return
    }
  } else if (currentStep.value === 1) {
    if (!taskForm.value.source_id) {
      ElMessage.warning('请选择源数据源')
      return
    }
  } else if (currentStep.value === 3) {
    if (!taskForm.value.target_id) {
      ElMessage.warning('请选择目标数据源')
      return
    }
  }

  if (currentStep.value < 6) {
    currentStep.value++

    // 当进入字段映射步骤时（步骤5），自动获取字段并触发同名映射
    if (currentStep.value === 5) {
      await autoFetchAndMapFields()
    }
  }
}

// 自动获取字段并进行同名映射
const autoFetchAndMapFields = async () => {
  try {
    // 1. 自动获取源字段
    if (sourceFields.value.length === 0) {
      await handleFetchFields('source')
    }

    // 2. 自动获取目标字段（如果目标是数据库类型）
    if (['postgresql', 'mysql'].includes(targetConnectorType.value)) {
      if (targetFields.value.length === 0 && targetConfig.value.table) {
        await handleFetchFields('target')
      }
    } else {
      // 对象存储类型，目标字段使用源字段（自动创建模式）
      targetFields.value = [...sourceFields.value]
    }

    // 3. 等待字段加载完成后，触发自动同名映射
    await nextTick()
    if (sourceFields.value.length > 0 && targetFields.value.length > 0) {
      performAutoMatch()
    }
  } catch (error) {
    console.error('自动字段映射失败:', error)
  }
}

// 执行自动同名映射
const performAutoMatch = () => {
  const newMappings = []

  // 1. 完全同名匹配
  sourceFields.value.forEach(sourceField => {
    if (targetFields.value.includes(sourceField)) {
      newMappings.push({
        source_field: sourceField,
        target_field: sourceField,
        field_type: 'string',
        transform: '',
        format: '',
        default_value: '',
        nullable: true
      })
    }
  })

  // 2. 模糊匹配（去掉下划线、转小写后比较）
  sourceFields.value.forEach(sourceField => {
    const normalizedSource = sourceField.toLowerCase().replace(/_/g, '')

    targetFields.value.forEach(targetField => {
      const normalizedTarget = targetField.toLowerCase().replace(/_/g, '')

      if (normalizedSource === normalizedTarget &&
          !newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: targetField,
          field_type: 'string',
          transform: '',
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  })

  // 3. 对象存储目标：未匹配的源字段也添加到映射（自动创建）
  if (targetConnectorType.value === 's3') {
    sourceFields.value.forEach(sourceField => {
      if (!newMappings.find(m => m.source_field === sourceField)) {
        newMappings.push({
          source_field: sourceField,
          target_field: sourceField,
          field_type: 'string',
          transform: '',
          format: '',
          default_value: '',
          nullable: true
        })
      }
    })
  }

  if (newMappings.length > 0) {
    fieldMappings.value = newMappings
    ElMessage.success(`已自动匹配 ${newMappings.length} 个字段`)
  }
}
```

#### FieldMappingEditor 组件更新

**文件**: `transfer/frontend/src/components/FieldMappingEditor.vue`

添加 `autoCreateMode` prop 用于标识自动创建模式：

```vue
<template>
  <div class="field-mapping-editor">
    <el-alert type="info" :closable="false" style="margin-bottom: 20px">
      <template #title>
        配置源字段到目标字段的映射关系
      </template>
      <div>
        <p>系统已自动进行同名匹配。您可以手动调整映射关系、添加转换函数或设置默认值。</p>
        <p v-if="autoCreateMode" style="color: #67C23A; margin-top: 5px;">
          <el-icon><Check /></el-icon>
          目标为对象存储，将自动创建目标字段（所有源字段都会导出到文件）
        </p>
      </div>
    </el-alert>
    <!-- ... -->
  </div>
</template>

<script setup>
import { Check } from '@element-plus/icons-vue'

const props = defineProps({
  sourceFields: {
    type: Array,
    default: () => []
  },
  targetFields: {
    type: Array,
    default: () => []
  },
  mappings: {
    type: Array,
    default: () => []
  },
  autoCreateMode: {
    type: Boolean,
    default: false
  }
})
</script>
```

在 TaskWizard 中使用：

```vue
<FieldMappingEditor
  :source-fields="sourceFields"
  :target-fields="targetFields"
  v-model:mappings="fieldMappings"
  :auto-create-mode="targetConnectorType === 's3'"
  @fetch-fields="handleFetchFields"
/>
```

## 使用流程

### 数据库到数据库传输

1. **选择源数据源** → 自动从元数据加载表列表
2. **选择源表** → 点击"下一步"
3. **选择目标数据源** → 自动从元数据加载表列表
4. **选择目标表** → 点击"下一步"
5. **进入字段映射步骤** → **自动触发**:
   - 自动获取源表字段（从 Meta 模块）
   - 自动获取目标表字段（从 Meta 模块）
   - 自动进行同名匹配（完全匹配 + 模糊匹配）
   - 显示匹配结果
6. **用户可手动调整** → 添加转换函数、修改映射关系等

### 数据库到对象存储传输

1. **选择源数据源和表** → 同上
2. **选择目标类型** → 选择"对象存储（MinIO/S3）"
3. **配置目标** → 选择 MinIO/S3 资源，配置路径和格式
4. **进入字段映射步骤** → **自动触发**:
   - 自动获取源表字段
   - **目标字段自动设置为源字段列表**（自动创建模式）
   - 所有源字段都自动映射为同名目标字段
   - 显示提示："目标为对象存储，将自动创建目标字段"
5. **用户可手动调整** → 移除不需要的字段、添加转换等

## 匹配策略

### 1. 完全同名匹配（优先级最高）
```
source: id      → target: id      ✅
source: name    → target: name    ✅
source: email   → target: email   ✅
```

### 2. 模糊匹配（去掉下划线、忽略大小写）
```
source: user_id    → target: userid     ✅
source: first_name → target: FirstName  ✅
source: created_at → target: createdat  ✅
```

### 3. 对象存储自动创建（所有字段）
```
source: id         → target: id         ✅ (自动创建)
source: username   → target: username   ✅ (自动创建)
source: email      → target: email      ✅ (自动创建)
source: created_at → target: created_at ✅ (自动创建)
```

## 测试验证

### API 测试

```bash
# 1. 登录获取 token
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"SuperAdmin","password":"admin123"}'

# 2. 获取表列表
curl -H "Authorization: Bearer <TOKEN>" \
  "http://localhost:8082/api/meta/metadata/tables?resource_id=4"

# 响应: ["administrative_regions", "categories", "customers", ...]

# 3. 获取表字段
curl -H "Authorization: Bearer <TOKEN>" \
  "http://localhost:8082/api/meta/metadata/fields?resource_id=4&table_name=customers"

# 响应: ["id", "name", "email", "phone", "address", "created_at", "updated_at"]
```

### 前端测试

1. 访问 http://localhost:5170
2. 导航到 Transfer 模块
3. 创建新任务，选择源和目标
4. 进入字段映射步骤，观察：
   - 是否自动加载字段
   - 是否自动进行同名匹配
   - 提示消息是否正确
   - 对象存储目标是否显示"自动创建"提示

## 文件清单

### 后端文件（Meta 模块）
- [x] `meta/backend/internal/api/handler.go` - 新增 GetTableFields 方法
- [x] `meta/backend/internal/api/router_new.go` - 新增路由
- [x] `meta/backend/internal/service/scan_service_new.go` - 新增 GetTableFields 方法

### 前端文件（Transfer 模块）
- [x] `transfer/frontend/src/views/TaskWizard.vue` - 自动字段获取和映射逻辑
- [x] `transfer/frontend/src/components/FieldMappingEditor.vue` - 支持自动创建模式提示

## 注意事项

1. **前提条件**: 数据源必须已在 Meta 模块中扫描元数据
2. **权限要求**: 需要有效的 JWT token，用户需有对应资源的访问权限
3. **Tenant 隔离**: API 自动根据用户的 tenant_id 过滤数据
4. **错误处理**: 如果表未扫描或字段不存在，会返回友好的错误提示

## 未来优化

- [ ] 支持更多模糊匹配规则（驼峰命名、拼音等）
- [ ] 支持字段类型自动推断（根据元数据中的数据类型）
- [ ] 支持字段映射历史记录和模板
- [ ] 支持批量编辑字段属性
