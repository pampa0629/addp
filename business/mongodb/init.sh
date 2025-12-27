#!/bin/bash
# MongoDB 初始化脚本
# 创建业务数据库和用户，插入测试数据

set -e

echo "=== MongoDB 初始化开始 ==="

# 等待 MongoDB 启动
sleep 5

# 使用 mongosh 连接并初始化
mongosh --eval "
// 创建业务数据库
use business;

// 创建业务用户（读写权限）
db.createUser({
  user: 'business_user',
  pwd: 'business_password',
  roles: [
    { role: 'readWrite', db: 'business' }
  ]
});

// 创建测试集合并插入数据
db.test_collection.insertMany([
  {
    _id: 1,
    name: 'MongoDB Test 1',
    type: 'document',
    created_at: new Date()
  },
  {
    _id: 2,
    name: 'MongoDB Test 2',
    type: 'document',
    created_at: new Date()
  },
  {
    _id: 3,
    name: 'MongoDB Test 3',
    type: 'document',
    created_at: new Date()
  }
]);

// 验证数据
print('验证 MongoDB 数据...');
print('文档总数: ' + db.test_collection.countDocuments());
" --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin

echo "=== MongoDB 初始化完成 ==="
