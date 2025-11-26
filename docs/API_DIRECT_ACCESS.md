# 如何直接访问 ADDP API

## 问题
浏览器直接访问 ADDP 后端 API 时提示: `{"error":"missing authorization token"}`

## 解决方案

### 方法 1: 从浏览器复制 Token (最简单)

1. **登录 ADDP Portal**:
   - 访问 http://localhost:5170 并登录

2. **打开浏览器开发者工具**:
   - 按 `F12` 或右键 → 检查

3. **获取 Token**:

   **方式 A: 从 LocalStorage 复制**
   - 切换到 `Application` (Chrome) 或 `Storage` (Firefox) 标签
   - 左侧展开 `Local Storage` → 选择域名
   - 找到 key 为 `token` 的项
   - 复制 value 值 (以 `eyJ` 开头的长字符串)

   **方式 B: 从 Network 请求复制**
   - 切换到 `Network` 标签
   - 刷新页面或触发一个 API 请求
   - 点击任意 API 请求
   - 查看 `Request Headers` → `Authorization`
   - 复制 `Bearer ` 后面的 token

4. **使用 Token 访问 API**:

   ```bash
   # 方法 A: 使用 curl (推荐)
   TOKEN="your-copied-token-here"
   curl "http://localhost:8081/api/resources/2/spatial/tiles/public/dltb/7/102/72?geom=smgeometry" \
     -H "Authorization: Bearer $TOKEN"

   # 方法 B: 使用浏览器插件 (如 ModHeader)
   # 1. 安装 Chrome 插件: ModHeader
   # 2. 添加 Request Header:
   #    Name: Authorization
   #    Value: Bearer your-copied-token-here
   # 3. 直接在浏览器地址栏访问 URL
   ```

### 方法 2: 使用脚本自动获取 Token

创建一个辅助脚本:

```bash
#!/bin/bash
# scripts/get-token.sh

# 登录获取 token
TOKEN=$(curl -s http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' \
  | jq -r '.token')

echo "Token: $TOKEN"
echo ""
echo "Use it like this:"
echo "curl \"http://localhost:8081/api/resources/2/spatial/tiles/public/dltb/7/102/72?geom=smgeometry\" \\"
echo "  -H \"Authorization: Bearer \$TOKEN\""
```

使用:
```bash
chmod +x scripts/get-token.sh
./scripts/get-token.sh
```

### 方法 3: 浏览器插件 (长期使用)

**推荐插件**: [ModHeader](https://chrome.google.com/webstore/detail/modheader)

**配置步骤**:
1. 安装 ModHeader
2. 点击插件图标
3. 添加 Request Header:
   - Name: `Authorization`
   - Value: `Bearer <your-token>`
4. 启用插件后,所有浏览器请求都会自动带上 token
5. 可以在地址栏直接访问 API

**注意**: Token 有过期时间 (默认 30 天),过期后需要重新登录获取

## MVT Tile 请求示例

### 完整示例 (已验证)

```bash
# 步骤 1: 登录获取 token
curl -s http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"zuhu1","password":"xx123zzm"}'

# 输出示例:
# {"access_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...","token_type":"Bearer"}

# 步骤 2: 提取 token (手动复制 access_token 的值)
TOKEN='eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...'

# 步骤 3: 请求 MVT 瓦片 (binary)
curl "http://localhost:8081/api/resources/2/spatial/tiles/public/dltb/7/101/54?geom=smgeometry" \
  -H "Authorization: Bearer $TOKEN" \
  -o tile.mvt

# 输出示例:
#   % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
#                                  Dload  Upload   Total   Spent    Left  Speed
# 100 2207k    0 2207k    0     0  99.3M      0 --:--:-- --:--:-- --:--:--  113M
#
# 这表示成功下载了 2207k (约 2.2MB) 的瓦片数据

# 步骤 4: 验证文件格式
ls -lh tile.mvt
file tile.mvt
# 输出: tile.mvt: gzip compressed data

# 步骤 5: 查看文件头 (前 2 字节应该是 0x1f 0x8b - gzip 压缩标记)
xxd tile.mvt | head -2
# 输出:
# 00000000: 1f8b 0800 0000 0000 00ff 74fd 0b90 5dd7  ..........t...].
# 00000010: 751e 08ef b5f7 3ee7 dc7d efed dedd a7bb  u.....>..}......

# 步骤 6: 解压缩查看 Protobuf 内容
gunzip < tile.mvt > tile.pbf
ls -lh tile.pbf
# 输出: 6.0M (解压后大小)

# 步骤 7: 查看 MVT layer 信息
strings tile.pbf | head -20
# 输出:
# dltb          <- Layer 名称
# SmID          <- 属性字段
# ...

# 步骤 8: 查看 Protobuf 二进制结构
xxd tile.pbf | head -4
# 输出:
# 00000000: 1ac2 a780 030a 0464 6c74 6212 8e01 1202  .......dltb.....
#           ^^                ^^^^
#           |                 |
#           0x1A = MVT layer  "dltb" = layer name
```

### 理解 curl 输出

当您看到这样的输出时：
```
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100 2207k    0 2207k    0     0  99.3M      0 --:--:-- --:--:-- --:--:--  113M
```

**含义解释**:
- `100` = 进度 100% (完成)
- `2207k` = 下载了 **2207 KB** (约 2.2 MB)
- `99.3M` = 平均下载速度 99.3 MB/s
- **这表示成功下载了 2.2MB 的瓦片数据**

**空瓦片的输出**:
```
100    0    0    0    0     0      0      0 --:--:-- --:--:-- --:--:--     0
```
- 表示返回了 **0 字节** (该区域没有数据)

**错误的输出**:
```
100   47  100   47    0     0  12620      0 --:--:-- --:--:-- --:--:-- 47000
```
- 只有 47 字节通常是 JSON 错误信息，如 `{"error":"invalid authorization header format"}`

## 安全建议

⚠️ **不要在生产环境禁用认证**

如果只是为了开发调试,可以临时修改代码:

```go
// manager/backend/internal/api/router.go (仅限开发环境)
func SetupRouter() *gin.Engine {
    r := gin.Default()

    // 开发模式: 允许未认证的 tile 请求 (仅限特定路由)
    if os.Getenv("GIN_MODE") != "release" {
        r.GET("/api/resources/:id/spatial/tiles/*path", spatialHandler.GetTile)
    }

    // 正常需要认证的路由
    protected := r.Group("/api")
    protected.Use(middleware.AuthMiddleware())
    {
        protected.GET("/resources/:id/spatial/tiles/*path", spatialHandler.GetTile)
    }

    return r
}
```

**但是**: 生产环境必须保留认证!正确的做法是使用 token。

## Token 过期处理

Token 默认有效期在 `system/backend/internal/service/auth_service.go`:

```go
// 生成 30 天有效期的 token
expirationTime := time.Now().Add(30 * 24 * time.Hour)
```

如果 token 过期,会收到 `401 Unauthorized` 错误,需要重新登录获取新 token。
