# task_executions 表结构和 API 说明

## 一、表结构概览

`transfer.task_executions` 表记录传输任务的每次执行历史，包括执行状态、数据统计、错误信息、断点状态等。

### 核心功能

- **执行历史追踪**：记录每次任务执行的详细信息
- **性能统计**：记录读写记录数、字节数、执行时间
- **断点续传**：保存断点偏移和状态
- **错误追踪**：记录执行错误和日志

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 执行记录唯一标识 |
| `task_id` | INTEGER | NOT NULL, INDEXED | 关联任务 ID |
| `status` | VARCHAR(20) | NOT NULL, INDEXED | 执行状态 |
| `start_time` | TIMESTAMP | NOT NULL | 开始时间 |
| `end_time` | TIMESTAMP | | 结束时间 |
| `records_read` | BIGINT | DEFAULT 0 | 读取记录数 |
| `records_written` | BIGINT | DEFAULT 0 | 写入记录数 |
| `bytes_read` | BIGINT | DEFAULT 0 | 读取字节数 |
| `bytes_written` | BIGINT | DEFAULT 0 | 写入字节数 |
| `error_msg` | TEXT | | 错误信息 |
| `logs` | TEXT | | 执行日志 |
| `checkpoint_offset` | BIGINT | DEFAULT 0 | 断点偏移 |
| `checkpoint_state` | JSONB | | 断点状态 |
| `trigger_type` | VARCHAR(50) | | 触发方式：manual/schedule/api |

---

## 三、相关文档

- [tasks表](./tasks表.md) - 任务定义表
- [数据库架构](../数据库架构.md) - Transfer 模块架构
