#!/bin/bash
#
# init-minio-mvt.sh - 初始化 MinIO MVT 瓦片缓存 Bucket
#
# 用途:
#   为 ADDP 空间数据快显功能创建必要的 MinIO 存储桶
#   MVT 瓦片将预处理后存储在此 Bucket 中
#
# 前提:
#   - MinIO 服务已启动（docker-compose up -d）
#   - mc (MinIO Client) 已安装
#
# 使用方法:
#   ./scripts/init-minio-mvt.sh
#   或
#   make init-minio-mvt

set -e

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🚀 初始化 MinIO MVT 瓦片缓存 Bucket...${NC}"

# MinIO 连接配置（从 .env 读取或使用默认值）
MINIO_ENDPOINT=${BUSINESS_MINIO_ENDPOINT:-"localhost:9002"}
MINIO_ACCESS_KEY=${BUSINESS_MINIO_ACCESS_KEY:-"minioadmin"}
MINIO_SECRET_KEY=${BUSINESS_MINIO_SECRET_KEY:-"minioadmin"}
BUCKET_NAME="mvt-tiles"

# 检查 mc 是否安装
if ! command -v mc &> /dev/null; then
    echo -e "${RED}❌ 错误: MinIO Client (mc) 未安装${NC}"
    echo "请先安装 mc: brew install minio/stable/mc  (macOS)"
    echo "或访问: https://min.io/docs/minio/linux/reference/minio-mc.html"
    exit 1
fi

# 配置 MinIO alias
echo -e "${YELLOW}📡 连接到 MinIO (${MINIO_ENDPOINT})...${NC}"
mc alias set addp-minio "http://${MINIO_ENDPOINT}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" --api S3v4 >/dev/null 2>&1

# 检查连接
if ! mc admin info addp-minio >/dev/null 2>&1; then
    echo -e "${RED}❌ 无法连接到 MinIO${NC}"
    echo "请确保 MinIO 服务已启动: docker-compose up -d"
    exit 1
fi

echo -e "${GREEN}✓ MinIO 连接成功${NC}"

# 创建 Bucket（如果不存在）
if mc ls addp-minio/${BUCKET_NAME} >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  Bucket '${BUCKET_NAME}' 已存在，跳过创建${NC}"
else
    echo -e "${YELLOW}📦 创建 Bucket '${BUCKET_NAME}'...${NC}"
    mc mb addp-minio/${BUCKET_NAME}
    echo -e "${GREEN}✓ Bucket 创建成功${NC}"
fi

# 设置访问策略为公开读（瓦片需要被前端直接访问）
echo -e "${YELLOW}🔓 设置 Bucket 访问策略为公开读...${NC}"
mc anonymous set download addp-minio/${BUCKET_NAME}
echo -e "${GREEN}✓ 访问策略设置完成${NC}"

# 创建目录结构说明文件
cat > /tmp/README.txt <<EOF
ADDP MVT 瓦片缓存目录

此 Bucket 用于存储空间数据的 MVT (Mapbox Vector Tiles) 预处理结果。

目录结构:
  /{fingerprint}/                    - 数据指纹目录（SHA256 hash）
    metadata.json                    - 元数据信息
    tiles/                           - 瓦片文件
      z0/                            - 缩放级别 0
        0_0.mvt.gz                   - 瓦片文件 (z=0, x=0, y=0)
      z1/                            - 缩放级别 1
        0_0.mvt.gz
        0_1.mvt.gz
        1_0.mvt.gz
        1_1.mvt.gz
      ...
      z{max}/                        - 最大缩放级别

说明:
  - fingerprint: 基于 resID + schema.table 的 SHA256 哈希
  - 同一张表的瓦片始终存储在同一个 fingerprint 目录
  - 数据更新时，只删除和重新生成变更区域的瓦片
  - 瓦片文件采用 gzip 压缩

生成时间: $(date '+%Y-%m-%d %H:%M:%S')
EOF

mc cp /tmp/README.txt addp-minio/${BUCKET_NAME}/README.txt >/dev/null 2>&1
rm /tmp/README.txt

# 显示 Bucket 信息
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✅ MinIO MVT 瓦片缓存 Bucket 初始化完成！${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "  Bucket 名称: ${BUCKET_NAME}"
echo "  访问端点: http://${MINIO_ENDPOINT}"
echo "  访问策略: 公开读 (download)"
echo "  Web 控制台: http://$(echo ${MINIO_ENDPOINT} | sed 's/:9002/:9003/')"
echo ""
echo "使用方法:"
echo "  - 预处理完成后，瓦片将自动存储在此 Bucket"
echo "  - 前端可直接访问: http://${MINIO_ENDPOINT}/${BUCKET_NAME}/{fingerprint}/tiles/z{z}/{x}_{y}.mvt.gz"
echo "  - 管理瓦片: mc ls addp-minio/${BUCKET_NAME}"
echo ""
