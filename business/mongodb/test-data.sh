#!/bin/bash
# MongoDB 测试数据脚本
# 创建多个集合并插入示例数据

set -e

echo "=== MongoDB 测试数据初始化开始 ==="

# 使用 root 用户连接
mongosh --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval "
// 切换到业务数据库
use business;

// 1. 创建产品集合并插入数据
db.products.insertMany([
  {
    _id: ObjectId(),
    product_id: 'PROD-001',
    name: 'MacBook Pro 16',
    category: '笔记本电脑',
    price: 16999,
    stock: 50,
    specs: {
      cpu: 'M3 Max',
      ram: '64GB',
      storage: '2TB SSD'
    },
    tags: ['高性能', '专业', 'Apple'],
    created_at: new Date('2025-12-01'),
    updated_at: new Date('2025-12-25')
  },
  {
    _id: ObjectId(),
    product_id: 'PROD-002',
    name: 'Magic Keyboard',
    category: '键盘',
    price: 1299,
    stock: 200,
    specs: {
      type: '机械键盘',
      layout: '87键',
      backlight: true
    },
    tags: ['无线', 'RGB', '机械'],
    created_at: new Date('2025-12-05'),
    updated_at: new Date('2025-12-20')
  },
  {
    _id: ObjectId(),
    product_id: 'PROD-003',
    name: 'Magic Mouse 2',
    category: '鼠标',
    price: 799,
    stock: 150,
    specs: {
      type: '光学鼠标',
      dpi: 4000,
      wireless: true
    },
    tags: ['无线', '充电', '便携'],
    created_at: new Date('2025-12-10'),
    updated_at: new Date('2025-12-22')
  }
]);

// 2. 创建用户集合并插入数据
db.users.insertMany([
  {
    _id: ObjectId(),
    user_id: 'U-10001',
    username: 'alice_wang',
    email: 'alice@example.com',
    profile: {
      full_name: '王小丽',
      age: 28,
      gender: '女',
      city: '上海',
      interests: ['编程', '阅读', '旅行']
    },
    membership: {
      level: 'gold',
      points: 5800,
      joined_date: new Date('2024-01-15')
    },
    created_at: new Date('2024-01-15'),
    last_login: new Date('2025-12-25')
  },
  {
    _id: ObjectId(),
    user_id: 'U-10002',
    username: 'bob_chen',
    email: 'bob@example.com',
    profile: {
      full_name: '陈大明',
      age: 35,
      gender: '男',
      city: '北京',
      interests: ['科技', '摄影', '音乐']
    },
    membership: {
      level: 'platinum',
      points: 12500,
      joined_date: new Date('2023-06-20')
    },
    created_at: new Date('2023-06-20'),
    last_login: new Date('2025-12-24')
  },
  {
    _id: ObjectId(),
    user_id: 'U-10003',
    username: 'carol_li',
    email: 'carol@example.com',
    profile: {
      full_name: '李佳佳',
      age: 24,
      gender: '女',
      city: '深圳',
      interests: ['设计', '美食', '健身']
    },
    membership: {
      level: 'silver',
      points: 2300,
      joined_date: new Date('2024-11-01')
    },
    created_at: new Date('2024-11-01'),
    last_login: new Date('2025-12-23')
  }
]);

// 3. 创建订单集合并插入数据
db.orders.insertMany([
  {
    _id: ObjectId(),
    order_id: 'ORD-20251225-001',
    user_id: 'U-10001',
    items: [
      { product_id: 'PROD-002', quantity: 1, price: 1299 },
      { product_id: 'PROD-003', quantity: 2, price: 799 }
    ],
    total_amount: 2897,
    status: 'delivered',
    shipping_address: {
      province: '上海市',
      city: '浦东新区',
      street: '张江高科技园区',
      zip_code: '201203'
    },
    payment: {
      method: '支付宝',
      transaction_id: 'PAY-2025122510001'
    },
    created_at: new Date('2025-12-20'),
    updated_at: new Date('2025-12-25')
  },
  {
    _id: ObjectId(),
    order_id: 'ORD-20251224-002',
    user_id: 'U-10002',
    items: [
      { product_id: 'PROD-001', quantity: 1, price: 16999 }
    ],
    total_amount: 16999,
    status: 'processing',
    shipping_address: {
      province: '北京市',
      city: '海淀区',
      street: '中关村软件园',
      zip_code: '100080'
    },
    payment: {
      method: '微信支付',
      transaction_id: 'PAY-2025122410001'
    },
    created_at: new Date('2025-12-24'),
    updated_at: new Date('2025-12-24')
  }
]);

// 4. 创建评论集合并插入数据
db.reviews.insertMany([
  {
    _id: ObjectId(),
    review_id: 'REV-001',
    product_id: 'PROD-002',
    user_id: 'U-10001',
    rating: 5,
    title: '非常好用的键盘',
    content: '手感很棒，RGB灯效也很漂亮，打字很舒服，值得购买！',
    images: ['https://example.com/review1.jpg'],
    helpful_count: 42,
    created_at: new Date('2025-12-22'),
    verified_purchase: true
  },
  {
    _id: ObjectId(),
    review_id: 'REV-002',
    product_id: 'PROD-003',
    user_id: 'U-10003',
    rating: 4,
    title: '不错的鼠标',
    content: '手感还可以，就是续航时间有点短，整体满意。',
    images: [],
    helpful_count: 15,
    created_at: new Date('2025-12-23'),
    verified_purchase: true
  }
]);

// 5. 验证数据
print('\\n=== 验证 MongoDB 测试数据 ===\\n');
print('产品总数: ' + db.products.countDocuments());
print('用户总数: ' + db.users.countDocuments());
print('订单总数: ' + db.orders.countDocuments());
print('评论总数: ' + db.reviews.countDocuments());
print('');

// 6. 创建索引优化查询性能
db.products.createIndex({ product_id: 1 });
db.products.createIndex({ category: 1 });
db.users.createIndex({ user_id: 1 });
db.users.createIndex({ email: 1 }, { unique: true });
db.orders.createIndex({ order_id: 1 }, { unique: true });
db.orders.createIndex({ user_id: 1 });
db.reviews.createIndex({ product_id: 1 });

print('✓ 索引创建完成');
"

echo "=== MongoDB 测试数据初始化完成 ==="
