# data_mappings 表结构和 API 说明

## 一、表结构概览

`transfer.data_mappings` 表定义源字段到目标字段的映射关系，支持字段转换、默认值、类型转换等。

### 核心功能

- **字段映射**：定义源字段到目标字段的映射
- **数据转换**：支持转换函数（如类型转换、格式化）
- **默认值**：支持设置字段默认值
- **类型控制**：支持字段类型和格式定义

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 映射唯一标识 |
| `task_id` | INTEGER | NOT NULL, INDEXED | 关联任务 ID |
| `source_field` | VARCHAR(255) | NOT NULL | 源字段名 |
| `target_field` | VARCHAR(255) | NOT NULL | 目标字段名 |
| `transform` | VARCHAR(500) | | 转换函数 |
| `default_value` | TEXT | | 默认值 |
| `field_type` | VARCHAR(50) | | 字段类型 |
| `format` | VARCHAR(100) | | 格式（日期等） |
| `nullable` | BOOLEAN | DEFAULT true | 是否可为空 |

---

## 三、相关文档

- [tasks表](./tasks表.md) - 任务定义表
- [数据库架构](../数据库架构.md) - Transfer 模块架构
