#!/bin/bash
# 测试 MVT 瓦片 API 的脚本

set -e

# 颜色输出
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== ADDP Quick View MVT Tile API 测试工具 ==="
echo ""

# 1. 登录获取 token
echo "📝 步骤 1: 登录获取 token..."
read -p "用户名 [zuhu1]: " USERNAME
USERNAME=${USERNAME:-zuhu1}

read -sp "密码: " PASSWORD
echo ""
PASSWORD=${PASSWORD:-xx123zzm}

RESPONSE=$(curl -s http://localhost:8180/api/v1/system/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")

TOKEN=$(echo "$RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo -e "${RED}❌ 登录失败:${NC}"
  echo "$RESPONSE"
  exit 1
fi

echo -e "${GREEN}✅ Token 获取成功${NC}"
echo "Token: ${TOKEN:0:50}..."
echo ""

# 2. 测试瓦片请求
echo "📍 步骤 2: 请求 Quick View MVT 瓦片..."
read -p "ResourceLocator [addp://engine/2/path/public/dltb?type=table]: " LOCATOR
LOCATOR=${LOCATOR:-addp://engine/2/path/public/dltb?type=table}

read -p "Zoom/X/Y [7/102/72]: " TILE_COORDS
TILE_COORDS=${TILE_COORDS:-7/102/72}

read -p "几何字段（可选）[smgeometry]: " GEOM_FIELD
GEOM_FIELD=${GEOM_FIELD:-smgeometry}

QUERY=$(python3 - "$LOCATOR" "$GEOM_FIELD" <<'PY'
import sys
from urllib.parse import urlencode

params = {"locator": sys.argv[1]}
if sys.argv[2]:
    params["geometry_column"] = sys.argv[2]
print(urlencode(params))
PY
)

URL="http://localhost:8081/api/v1/manager/quick-view/tiles/${TILE_COORDS}.mvt?${QUERY}"

echo ""
echo "请求 URL:"
echo "  $URL"
echo ""

# 发起请求
OUTPUT_FILE="/tmp/tile-output.mvt"
HTTP_CODE=$(curl -s -w "%{http_code}" -o "$OUTPUT_FILE" \
  "$URL" \
  -H "Authorization: Bearer $TOKEN")

echo "HTTP 状态码: $HTTP_CODE"
echo ""

# 3. 分析结果
if [ "$HTTP_CODE" -eq 200 ]; then
  FILE_SIZE=$(ls -lh "$OUTPUT_FILE" | awk '{print $5}')

  if [ "$FILE_SIZE" = "0B" ]; then
    echo -e "${YELLOW}⚠️  返回成功但瓦片为空 (该区域可能没有数据)${NC}"
  else
    echo -e "${GREEN}✅ 瓦片下载成功!${NC}"
    echo "文件大小: $FILE_SIZE"
    echo "文件类型: $(file -b $OUTPUT_FILE)"
    echo ""
    echo "文件前 32 字节 (十六进制):"
    xxd "$OUTPUT_FILE" | head -2
    echo ""

    # 检查是否是 gzip 压缩
    FIRST_BYTES=$(xxd -p "$OUTPUT_FILE" | head -1 | cut -c1-4)
    if [ "$FIRST_BYTES" = "1f8b" ]; then
      echo "✅ 文件是 gzip 压缩的 MVT"
      echo ""
      echo "解压缩后的内容 (前 64 字节):"
      gunzip < "$OUTPUT_FILE" | xxd | head -4
    fi
  fi

  echo ""
  echo "保存位置: $OUTPUT_FILE"

elif [ "$HTTP_CODE" -eq 401 ]; then
  echo -e "${RED}❌ 认证失败 (401)${NC}"
  cat "$OUTPUT_FILE"

elif [ "$HTTP_CODE" -eq 404 ]; then
  echo -e "${RED}❌ 资源未找到 (404)${NC}"
  cat "$OUTPUT_FILE"

else
  echo -e "${RED}❌ 请求失败 (HTTP $HTTP_CODE)${NC}"
  cat "$OUTPUT_FILE"
fi

echo ""
echo "=== 完整 curl 命令 (可直接复制使用) ==="
echo "TOKEN='$TOKEN'"
echo "curl \"$URL\" \\"
echo "  -H \"Authorization: Bearer \$TOKEN\" \\"
echo "  -o tile.mvt"
echo ""
