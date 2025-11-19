#!/bin/bash

# 修复元数据扫描问题

echo "======================================"
echo "修复元数据扫描"
echo "======================================"
echo ""

# 1. 清理旧的扫描数据
echo "步骤 1: 清理旧的扫描数据..."
docker-compose exec -T postgres psql -U addp -d addp <<EOF
-- 删除 meta_items (表数据)
DELETE FROM metadata.meta_item WHERE res_id = 2;

-- 删除 meta_nodes (schema节点)
DELETE FROM metadata.meta_node WHERE res_id = 2;

-- 查看删除结果
SELECT 'Items删除后:' as info, COUNT(*) FROM metadata.meta_item WHERE res_id = 2;
SELECT 'Nodes删除后:' as info, COUNT(*) FROM metadata.meta_node WHERE res_id = 2;
EOF

echo "✓ 旧数据已清理"
echo ""

# 2. 检查 ENCRYPTION_KEY
echo "步骤 2: 检查加密密钥..."
if grep -q "ENCRYPTION_KEY" .env; then
    echo "✓ .env 中存在 ENCRYPTION_KEY"
else
    echo "✗ .env 中缺少 ENCRYPTION_KEY"
    echo "  请确保 .env 文件包含 ENCRYPTION_KEY=..."
    exit 1
fi
echo ""

# 3. 重启开发环境
echo "步骤 3: 重启开发环境..."
./scripts/dev-stop.sh
echo "等待5秒..."
sleep 5
./scripts/dev-start.sh
echo ""

echo "======================================"
echo "修复完成！"
echo ""
echo "下一步："
echo "  1. 访问 http://localhost:5170"
echo "  2. 进入 Meta 模块"
echo "  3. 点击数据源 'pg业务库'"
echo "  4. 选择所有 schema 并点击 '扫描'"
echo "  5. 观察扫描结果"
echo ""
echo "如果仍然出现问题，请检查："
echo "  - logs/meta-backend.log 中的错误信息"
echo "  - 确保 ENCRYPTION_KEY 与创建资源时使用的密钥一致"
echo "======================================"
