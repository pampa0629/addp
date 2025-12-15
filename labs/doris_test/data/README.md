# Apache Doris 测试数据生成脚本

本目录包含用于生成 Doris 测试数据的脚本和示例数据文件。

## 文件说明

- **`generate_data.py`** - Python 数据生成脚本（生成大量测试数据）
- **`sample_data.csv`** - CSV 示例数据（小规模测试）
- **`sample_users.json`** - JSON 示例数据（用户数据）
- **`sample_orders.json`** - JSON 示例数据（订单数据）

## 快速开始

### 1. 安装依赖

```bash
# 安装 Python 依赖
pip install faker pandas
```

### 2. 生成测试数据

```bash
# 生成 10 万行用户行为数据
python generate_data.py --type user_events --count 100000 --output user_events.csv

# 生成 1 万条订单数据
python generate_data.py --type orders --count 10000 --output orders.csv

# 生成 100 万行日志数据（大数据量测试）
python generate_data.py --type logs --count 1000000 --output logs.csv
```

### 3. 导入数据到 Doris

```bash
# 使用 Stream Load 导入 CSV
cd ..
bash sql/02-data-load/csv_import.sh
```

## 数据类型说明

### 1. user_events（用户行为数据）

| 字段 | 类型 | 说明 |
|------|------|------|
| user_id | BIGINT | 用户ID（1000-9999） |
| event_time | DATETIME | 事件时间 |
| event_type | VARCHAR | 事件类型（click/view/cart/purchase） |
| page_url | VARCHAR | 页面URL |
| device | VARCHAR | 设备类型（iOS/Android/Web） |
| duration | INT | 停留时长（秒） |

### 2. orders（订单数据）

| 字段 | 类型 | 说明 |
|------|------|------|
| order_id | BIGINT | 订单ID |
| user_id | BIGINT | 用户ID |
| product_id | INT | 商品ID |
| category | VARCHAR | 商品类别 |
| amount | DECIMAL | 订单金额 |
| quantity | INT | 购买数量 |
| order_time | DATETIME | 下单时间 |
| status | VARCHAR | 订单状态 |

### 3. logs（日志数据）

| 字段 | 类型 | 说明 |
|------|------|------|
| log_time | DATETIME | 日志时间 |
| log_level | VARCHAR | 日志级别（INFO/WARN/ERROR） |
| service | VARCHAR | 服务名称 |
| message | TEXT | 日志内容 |
| ip_address | VARCHAR | IP地址 |

## 性能测试数据量建议

- **小规模测试**：1 万 - 10 万行（验证功能）
- **中等规模测试**：10 万 - 100 万行（性能测试）
- **大规模测试**：100 万 - 1000 万行（压力测试）
- **超大规模测试**：1000 万行以上（极限测试）

## 示例使用场景

### 场景1：用户行为分析

```bash
# 生成 50 万行用户行为数据
python generate_data.py --type user_events --count 500000 --output user_events.csv

# 导入到 Doris
curl -u root: \
  -H "label:user_events_$(date +%s)" \
  -H "format: csv" \
  -H "column_separator: ," \
  -H "skip_header: 1" \
  -T user_events.csv \
  http://127.0.0.1:18030/api/learning_db/user_events/_stream_load
```

### 场景2：订单分析

```bash
# 生成 10 万条订单数据
python generate_data.py --type orders --count 100000 --output orders.csv

# 分析订单统计
mysql -h127.0.0.1 -P19030 -uroot learning_db <<EOF
LOAD LABEL orders_$(date +%s)
(
    DATA INFILE("orders.csv")
    INTO TABLE orders
    COLUMNS TERMINATED BY ","
    FORMAT AS "CSV"
    (order_id, user_id, product_id, category, amount, quantity, order_time, status)
);
EOF
```

### 场景3：日志分析

```bash
# 生成 100 万行日志数据
python generate_data.py --type logs --count 1000000 --output logs.csv

# 测试查询性能
time mysql -h127.0.0.1 -P19030 -uroot learning_db -e \
  "SELECT log_level, COUNT(*) FROM logs GROUP BY log_level;"
```

## 注意事项

1. **内存占用**：生成大量数据时注意内存使用，建议分批生成
2. **文件大小**：100 万行数据约 100-200MB，注意磁盘空间
3. **导入性能**：使用 Stream Load 导入大文件，性能最佳
4. **字符编码**：默认 UTF-8，确保 Doris 配置一致

## 清理数据

```bash
# 删除生成的数据文件
rm -f user_events.csv orders.csv logs.csv

# 清空 Doris 表数据
mysql -h127.0.0.1 -P19030 -uroot learning_db -e "TRUNCATE TABLE user_events;"
```

## 故障排查

**问题1：生成数据失败**
- 检查 Python 版本（需要 3.7+）
- 检查依赖是否安装：`pip list | grep -E "faker|pandas"`

**问题2：导入失败**
- 检查 CSV 格式是否正确
- 检查 Doris 表结构是否匹配
- 查看 Stream Load 返回的错误信息

**问题3：性能慢**
- 检查磁盘 I/O 性能
- 使用 SSD 存储数据文件
- 调整 Stream Load 并发度
