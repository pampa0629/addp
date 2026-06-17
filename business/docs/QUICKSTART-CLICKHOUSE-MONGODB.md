# ClickHouse & MongoDB 快速启动指南

## 📦 前提条件

确保 Docker 网络正常，能够拉取镜像。

## 🚀 启动服务

### 方式 1: 使用启动脚本（推荐）

```bash
cd /Users/pampa/code/addp/business

# 启动 ClickHouse 和 MongoDB
bash scripts/start.sh -clickhouse -mongodb
```

### 方式 2: 使用 docker-compose

```bash
cd /Users/pampa/code/addp/business

# 拉取镜像
docker-compose pull clickhouse mongodb

# 启动服务
docker-compose up -d clickhouse mongodb

# 查看日志
docker-compose logs -f clickhouse mongodb
```

## 📊 加载测试数据

### ClickHouse 测试数据

服务启动后，等待 30 秒让初始化脚本执行完成，然后加载额外测试数据：

```bash
# 进入容器并执行测试数据脚本
docker exec -i business-clickhouse bash < clickhouse/test-data.sh

# 或者手动执行
docker exec -it business-clickhouse clickhouse-client --database=business

# 在 ClickHouse 客户端中查询数据
SELECT * FROM user_analytics LIMIT 5;
SELECT * FROM sales_data LIMIT 5;
SELECT * FROM app_logs LIMIT 5;
```

### MongoDB 测试数据

```bash
# 设置环境变量
export MONGO_INITDB_ROOT_USERNAME=admin
export MONGO_INITDB_ROOT_PASSWORD=admin_password

# 执行测试数据脚本
docker exec -i business-mongodb bash < mongodb/test-data.sh

# 或者手动执行
docker exec -it business-mongodb mongosh -u admin -p admin_password --authenticationDatabase admin

# 在 MongoDB Shell 中查询数据
use business;
db.products.find().pretty();
db.users.find().pretty();
db.orders.find().pretty();
db.reviews.find().pretty();
```

## 📋 测试数据概览

### ClickHouse 包含以下表：

1. **test_table** (初始化脚本创建)
   - 3 条简单测试数据

2. **user_analytics** (用户行为分析)
   - 6 条用户事件数据
   - 字段: user_id, event_type, event_time, page_url, duration_seconds, device_type

3. **sales_data** (销售数据)
   - 6 条订单数据
   - 字段: order_id, product_id, product_name, quantity, price, order_date, customer_id, region

4. **app_logs** (应用日志)
   - 5 条日志记录
   - 字段: log_id, timestamp, level, service_name, message, user_id, request_id

### MongoDB 包含以下集合：

1. **test_collection** (初始化脚本创建)
   - 3 条简单文档

2. **products** (产品信息)
   - 3 个产品文档
   - 包含: 产品ID、名称、分类、价格、库存、规格、标签

3. **users** (用户信息)
   - 3 个用户文档
   - 包含: 用户ID、用户名、邮箱、个人资料、会员等级

4. **orders** (订单信息)
   - 2 个订单文档
   - 包含: 订单ID、用户ID、商品列表、金额、状态、收货地址、支付信息

5. **reviews** (评论信息)
   - 2 条评论文档
   - 包含: 评论ID、产品ID、用户ID、评分、内容、点赞数

## 🔍 验证服务

### ClickHouse

```bash
# 检查服务状态
docker ps | grep clickhouse

# 测试连接
docker exec business-clickhouse clickhouse-client --query "SELECT version()"

# 查看数据库
docker exec business-clickhouse clickhouse-client --query "SHOW DATABASES"

# 查看表
docker exec business-clickhouse clickhouse-client --database=business --query "SHOW TABLES"

# 统计查询示例
docker exec business-clickhouse clickhouse-client --database=business --query "
SELECT
    region,
    COUNT(*) as order_count,
    SUM(quantity * price) as total_revenue
FROM sales_data
GROUP BY region
ORDER BY total_revenue DESC
"
```

### MongoDB

```bash
# 检查服务状态
docker ps | grep mongodb

# 测试连接
docker exec business-mongodb mongosh --eval "db.adminCommand('ping')"

# 查看数据库
docker exec business-mongodb mongosh -u admin -p admin_password --authenticationDatabase admin --eval "show dbs"

# 查看集合
docker exec business-mongodb mongosh -u admin -p admin_password --authenticationDatabase admin --eval "use business; show collections"

# 聚合查询示例
docker exec business-mongodb mongosh -u admin -p admin_password --authenticationDatabase admin --eval "
use business;
db.orders.aggregate([
  { \$group: { _id: '\$status', count: { \$sum: 1 }, total: { \$sum: '\$total_amount' } } }
])
"
```

## 🌐 访问信息

### ClickHouse

- **Native 协议**: `localhost:9000`
  ```bash
  clickhouse-client --host localhost --port 9000
  ```

- **HTTP 接口**: `http://localhost:8123`
  ```bash
  curl 'http://localhost:8123/?query=SELECT%201'
  ```

### MongoDB

- **连接字符串**: `mongodb://admin:admin_password@localhost:27017/business?authSource=admin`
  ```bash
  mongosh "mongodb://admin:admin_password@localhost:27017/business?authSource=admin"
  ```

## 🔧 在 ADDP Manager 中添加数据源

### 添加 ClickHouse 数据源

1. 登录 ADDP Manager 模块
2. 进入"存储引擎"管理
3. 点击"新增存储引擎"
4. 选择类型: **ClickHouse**
5. 填写连接信息:
   - 主机地址: `host.docker.internal` (或 `localhost`)
   - 端口: `9000`
   - 数据库名: `business`
   - 用户名: `default`
   - 密码: (留空)
6. 点击"测试连接"，成功后保存

### 添加 MongoDB 数据源

1. 登录 ADDP Manager 模块
2. 进入"存储引擎"管理
3. 点击"新增存储引擎"
4. 选择类型: **MongoDB**
5. 填写连接信息:
   - 主机地址: `host.docker.internal` (或 `localhost`)
   - 端口: `27017`
   - 数据库名: `business`
   - 用户名: `admin`
   - 密码: `admin_password`
6. 点击"测试连接"，成功后保存

## 📚 示例查询

### ClickHouse 示例

```sql
-- 1. 用户活跃度分析
SELECT
    toDate(event_time) as date,
    event_type,
    COUNT(*) as event_count,
    COUNT(DISTINCT user_id) as unique_users
FROM user_analytics
GROUP BY date, event_type
ORDER BY date DESC;

-- 2. 销售趋势分析
SELECT
    toYYYYMM(order_date) as month,
    region,
    COUNT(*) as orders,
    SUM(quantity) as total_quantity,
    SUM(quantity * price) as revenue
FROM sales_data
GROUP BY month, region
ORDER BY month DESC, revenue DESC;

-- 3. 日志级别分布
SELECT
    level,
    service_name,
    COUNT(*) as count
FROM app_logs
GROUP BY level, service_name
ORDER BY count DESC;
```

### MongoDB 示例

```javascript
// 1. 按分类统计产品
db.products.aggregate([
  { $group: {
      _id: "$category",
      count: { $sum: 1 },
      avg_price: { $avg: "$price" }
  } }
])

// 2. 查找高价值用户
db.users.find({
  "membership.level": { $in: ["gold", "platinum"] },
  "membership.points": { $gte: 5000 }
}).sort({ "membership.points": -1 })

// 3. 订单统计
db.orders.aggregate([
  { $unwind: "$items" },
  { $group: {
      _id: "$items.product_id",
      total_quantity: { $sum: "$items.quantity" },
      total_revenue: { $sum: { $multiply: ["$items.quantity", "$items.price"] } }
  } },
  { $sort: { total_revenue: -1 } }
])

// 4. 产品评分统计
db.reviews.aggregate([
  { $group: {
      _id: "$product_id",
      avg_rating: { $avg: "$rating" },
      review_count: { $sum: 1 }
  } },
  { $sort: { avg_rating: -1 } }
])
```

## 🛠 故障排查

### 镜像拉取失败

```bash
# 检查网络
ping -c 3 registry-1.docker.io

# 使用国内镜像源（可选）
# 编辑 /etc/docker/daemon.json 添加:
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn"
  ]
}

# 重启 Docker
sudo systemctl restart docker
```

### 服务无法启动

```bash
# 查看日志
docker-compose logs clickhouse
docker-compose logs mongodb

# 检查端口占用
lsof -nP -i :9000
lsof -nP -i :27017

# 清理并重启
docker-compose down
docker-compose up -d clickhouse mongodb
```

### 数据加载失败

```bash
# ClickHouse: 确保服务已启动
docker exec business-clickhouse clickhouse-client --query "SELECT 1"

# MongoDB: 确保认证信息正确
docker exec business-mongodb mongosh --eval "db.version()"
```

## 📖 更多资源

- [ClickHouse 官方文档](https://clickhouse.com/docs)
- [MongoDB 官方文档](https://www.mongodb.com/docs/)
- [ADDP 数据引擎扩展指南](../../docs/spec/addp数据引擎扩展指南.md)
