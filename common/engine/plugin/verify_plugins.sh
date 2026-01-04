#!/bin/bash

# 插件接口验证测试脚本
# 单独运行验证测试，避免与 registry_test.go 冲突

set -e

echo "========================================="
echo "ADDP Plugin Interface Verification Test"
echo "========================================="
echo ""

cd /Users/pampa/code/addp/common/database/plugin/integration

echo "运行所有插件接口验证测试..."
go test -v -tags=integration

echo ""
echo "========================================="
echo "✅ 所有验证测试通过"
echo "========================================="
