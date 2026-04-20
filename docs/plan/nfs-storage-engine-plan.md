# NFS 存储引擎支持计划（修订版）

> 状态：阶段一、二已完成  
> 创建日期：2026-04-19  
> 修订日期：2026-04-20  
> 背景：用户在 NAS 上存储了大量文件型数据，希望 ADDP 能直接读写，无需复制到对象存储。

---

## 一、核心设计原则

### 两个正交维度，各自独立演进

```
存储引擎（WHERE）          文件格式（WHAT）
─────────────────          ─────────────────
MinIO                      Shapefile
S3                         GeoJSON
NFS  ← 本次新增            Parquet（含目录/文件双模式）
本地FS（未来）             GeoPackage
                           CSV / Excel
                           ...
```

**NFS 是存储引擎**，负责"如何访问文件"。  
**Shapefile/Parquet 是文件格式**，负责"如何解析数据"。  
两者通过 `FileSystemPlugin` 接口解耦，任意组合。

新增 NFS 引擎后，ADDP 已支持的所有文件格式自动对 NFS 可用，无需为每种格式单独适配。

---

## 二、现有架构基础（已有，无需重复定义）

### 2.1 FileSystemPlugin 接口（已在 interfaces.go 中定义）

```go
type FileSystemPlugin interface {
    EnginePlugin
    StoragePlugin
    ListRoots(ctx, connInfo) ([]RootInfo, error)       // NFS: 返回 export_path 作为唯一根
    ListDirectory(ctx, connInfo, path) ([]FileInfo, error)
    ReadFile(ctx, connInfo, path) (io.ReadCloser, error)
    GetFileMetadata(ctx, connInfo, path) (*FileMetadata, error)
}
```

NFS 插件直接实现此接口，与 ObjectStoragePlugin（MinIO/S3）平等。

### 2.2 FileSystemScanService（已在 Meta 模块中存在）

- 处理目录遍历、MetaNode（目录）/ MetaItem（文件）的创建
- CompositeItemDetector 链自动识别复合格式：
  - `ShapefileDetector`（优先级 90）：识别 .shp+.shx+.dbf 组合
  - `LakeTableDetector`（优先级 80）：识别 Parquet 目录表或单文件表
- NFS 引擎注册后，FileSystemScanService 自动支持，**无需新增扫描服务**

### 2.3 MetaNode / MetaItem 模型（与 MinIO 保持一致）

| 概念 | 对应 MinIO | 对应 NFS |
|------|-----------|---------|
| MetaNode | Bucket | 目录 |
| MetaItem | Object（文件）/ lake_table | 文件 / lake_table（Parquet 目录） |

---

## 三、NFS 连接配置

```json
{
  "server": "192.168.1.100",
  "export_path": "/exports/gis-data",
  "access_mode": "rw",
  "nfs_version": "3"
}
```

| 字段 | 说明 | 默认值 |
|------|------|--------|
| `server` | NFS 服务器地址 | 必填 |
| `export_path` | NFS 导出路径（根目录） | 必填 |
| `access_mode` | `rw`（读写）/ `ro`（只读） | `rw` |
| `nfs_version` | `3` / `4` | `3` |

无用户名密码（NFSv3 基于 IP 访问控制）。

---

## 四、关键设计决策：读写两个正交抽象

存储引擎（WHERE）与文件格式（WHAT）在读写两个方向上都需要解耦，分别引入两个抽象接口，放在 `transfer/backend/pkg/vfs/`。

### 4.1 FileAccessor（读抽象）

Transfer 和 Service 模块中，格式读取器（ShapefileReader、ParquetReader 等）目前依赖本地文件路径。  
为支持任意存储引擎（NFS、MinIO、S3），引入 `FileAccessor` 抽象：

```go
// FileAccessor 为格式读取器提供统一的文件访问接口
// 屏蔽底层存储引擎差异（本地FS、NFS、MinIO、S3）
type FileAccessor interface {
    // Open 打开文件，返回可寻址流（支持 Seek，用于 Shapefile 等需要随机访问的格式）
    Open(path string) (io.ReadSeekCloser, error)
    // OpenSibling 打开同目录下的关联文件（Shapefile 的 .shx/.dbf/.prj）
    OpenSibling(basePath, ext string) (io.ReadSeekCloser, error)
    // List 列出目录内容（Parquet 目录表模式）
    List(dirPath string) ([]FileInfo, error)
}
```

各存储引擎实现：
- `LocalFileAccessor`：本地文件系统（现有 ShapefileReader 的基础）
- `NFSFileAccessor`：通过 NFS 客户端直接流式读取（NFS 支持 Seek，**无需临时目录**）
- `ObjectStorageFileAccessor`：MinIO/S3（需临时目录，因对象存储不支持 Seek）

格式读取器统一接受 `FileAccessor`，不再依赖本地路径：

```go
// 改造前（只支持本地路径）
type ShapefileReader struct { filePath string }

// 改造后（支持任意存储引擎）
type ShapefileReader struct { accessor FileAccessor; filePath string }
```

### 4.2 VFS（写抽象）

格式写入器（ShapefileWriter、ParquetWriter、GeoJSONWriter、CSVWriter）目前直接调用 `os.Create(path)`，只能写本地文件系统。引入 `VFS` 接口解耦写入目标：

```go
// VFS 为格式写入器提供统一的文件写入接口
// 屏蔽底层存储引擎差异
type VFS interface {
    Create(path string) (io.WriteCloser, error)
    MkdirAll(path string) error
}
```

各存储引擎实现：
- `LocalVFS`：封装 `os.Create` / `os.MkdirAll`（现有写入器的基础，零行为变化）
- `NFSVFS`：封装 go-nfs-client 的写入 API，**直接写入 NFS，无临时目录**

格式写入器统一接受 `VFS`，不再直接调用 `os.Create`：

```go
// 改造前
func (w *GeoJSONWriter) Open(...) error {
    w.file, _ = os.Create(path)  // 只能写本地
}

// 改造后
func (w *GeoJSONWriter) Open(...) error {
    w.file, _ = w.vfs.Create(path)  // 写任意存储引擎
}
```

**写入路径对比**：

| 目标存储 | VFS 实现 | 临时目录 | 说明 |
|---------|---------|---------|------|
| NFS | `NFSVFS` | ❌ 不需要 | go-nfs-client 直接写，发挥 NFS 性能优势 |
| 本地 FS | `LocalVFS` | ❌ 不需要 | 直接写本地路径 |
| S3/MinIO | `LocalVFS` | ✅ 需要 | 对象存储不支持流式写，仍需临时目录再上传 |

**影响范围**：ShapefileWriter、GeoJSONWriter、ParquetWriter、CSVWriter 均需将 `os.Create` 替换为 `vfs.Create`，改动量小，但改造后新增任何存储引擎时，所有格式**零改动**自动支持。

### 4.3 NFS 插件补充写入能力

当前 `common/engine/plugins/nfs/plugin.go` 只有读操作。需补充：

```go
// 在 NFS plugin 上新增（不加入 FileSystemPlugin 接口，接口保持只读）
func (p *NFSPlugin) WriteFile(ctx, connInfo, path string, r io.Reader) error
func (p *NFSPlugin) MkdirAll(ctx, connInfo, path string) error
func (p *NFSPlugin) OpenFileForWrite(ctx, connInfo, path string) (io.WriteCloser, error)
```

`NFSVFS` 内部持有 NFS plugin 引用，调用上述方法实现 `VFS` 接口。

---

## 五、Parquet 在 NFS 上的支持

Parquet 的双模式识别（来自湖仓一体化设计）在 NFS 上自动生效：

| 模式 | NFS 场景 | 识别结果 |
|------|---------|---------|
| 模式 A（目录即表） | `/exports/sales/` 下全是 `.parquet` 文件 | 整个目录 → 1 个 lake_table |
| 模式 B（文件即表） | `/exports/report.parquet` 单文件 | 单文件 → 1 个 lake_table |

`LakeTableDetector` 通过 `FileSystemPlugin.ListDirectory()` 检测目录内容，NFS 实现此接口后自动支持，**无需额外适配**。

---

## 六、分阶段实施

### 阶段一：基础设施 + 引擎注册 ✅ 已完成

**目标**：NFS 引擎可配置、可连通性测试。

#### 1. business/docker-compose.yml 新增 NFS server ✅

**实际实现**：使用 macOS 系统原生 NFS，无需 Docker 容器。macOS 内置 `nfsd`，只需配置 `/etc/exports` 即可。

测试数据目录 `business/nas-data/` 已创建，包含 `gis-data/` 子目录和 `README.md`。

docker-compose.yml 中保留了 `nfs-server` 服务定义（`profiles: [nfs]`，默认不启动），供 Linux/生产环境使用。

**换新 Mac 时的启动步骤**：

1. 创建导出目录（clone 仓库后自动存在，或手动创建）：
   ```bash
   mkdir -p /Users/<用户名>/code/addp/business/nas-data
   ```

2. 写入 `/etc/exports`（`<UID>` 用 `id -u` 查，通常为 `501`）：
   ```bash
   sudo sh -c 'echo "/Users/<用户名>/code/addp/business/nas-data -alldirs -mapall=<UID> -noresvport" >> /etc/exports'
   ```

3. 启动 nfsd：
   ```bash
   sudo nfsd start    # 首次启动
   sudo nfsd update   # 已在运行时重载配置
   ```

4. 验证：
   ```bash
   showmount -e localhost
   # 应输出：/Users/.../nas-data  Everyone
   ```

#### 2. common/engine/plugins/nfs/ 新增 NFS 引擎插件

```
common/engine/plugins/nfs/
├── plugin.go     # 实现 FileSystemPlugin 接口
├── client.go     # Go NFS 客户端封装 + 连接池（key: server:export_path）
└── register.go   # init() 注册
```

**依赖库**：`github.com/vmware/go-nfs-client`（NFSv3）

`plugin.go` 核心实现：
- `Type()` → `"nfs"`
- `DisplayName()` → `"NFS 文件系统"`
- `TestConnection()` → 调用 `ListRoots()` 验证连通性
- `ListRoots()` → 返回 `export_path` 作为唯一根节点
- `ListDirectory()` / `ReadFile()` / `GetFileMetadata()` → 调用 NFS 客户端

`client.go` 额外实现 `ReadFileSeekable(path) (io.ReadSeekCloser, error)`，  
利用 NFS 协议原生支持 Seek 的特性，避免临时目录。

#### 3. System 模块：引擎管理 UI 支持 NFS

- 引擎类型选择器新增 `nfs`
- 配置表单：server、export_path、access_mode（默认 rw）、nfs_version
- 连通性测试按钮

涉及文件：`system/frontend/src/` 引擎配置组件（后端 JSONB 存储无需改动）。

---

### 阶段二：Meta 扫描 + Manager 浏览/预览 ✅ 已完成

**目标**：ADDP 感知 NFS 上的文件，可浏览、可预览。

#### 1. Meta 模块：NFS 扫描支持 ✅

**实际实现**（与原计划有差异，以下为最终实现）：

**问题根因**：原代码用字符串 `isObjectStorageType()` 判断引擎类型，NFS 不在列表中，导致：
- `schemas/available` 接口 500（无 FileSystemPlugin 分支）
- 手动扫描报"plugin does not support metadata query"
- `meta/backend/cmd/server/main.go` 未导入 NFS 插件，`plugin.Get("nfs")` 返回 unsupported

**修复内容**：

1. **`meta/backend/cmd/server/main.go` 和 `worker/main.go`**：补充 NFS 插件 blank import：
   ```go
   _ "github.com/addp/common/engine/plugins/nfs"
   ```

2. **`resource_discovery_service.go`**：`ListAvailableSchemas` 新增 `FileSystemPlugin` 分支，调用 `ListRoots` 返回根目录名作为 schema；`ListObjectStorageNodes` 重构为统一入口，通过接口类型断言分别路由到 `listObjectStorageNodes`（ObjectStoragePlugin）和 `listFileSystemNodes`（FileSystemPlugin）。

3. **`scan_service.go`**：手动扫描和自动扫描的引擎类型路由，全部改为插件接口类型断言（替换字符串 `isObjectStorageType` 判断）：
   - `NoSQLPlugin` → `scanNoSQLResourceWithReporter`
   - `ObjectStoragePlugin` → `scanFileSystemResourceWithReporter`（对象存储也走此路径）
   - `FileSystemPlugin`（NFS）→ `scanFileSystemResourceWithReporter`
   - `RelationalDBPlugin` → `scanResourceSchemasWithReporter`

4. **`filesystem_scan_service.go`**：修复两个 bug：
   - **根节点 name 为空**：NFS `ListRoots` 返回 `Path: "/"`, `Name: "nas-data"`，但 `ScanPaths` 用 `strings.Trim("/", "/")` 推导名称得到空字符串。修复：始终先调 `ListRoots` 建立 path→name 映射，优先使用映射中的名称。
   - **scan_status 永远"未扫描"**：扫完后未调 `FinalizeNodeState`。修复：每个根节点扫描前调 `ResetNodeState("扫描中")`，扫描后调 `FinalizeNodeState("已扫描", itemCount, ...)`。

5. **`meta/frontend/src/views/MetadataScan.vue`**：
   - `isObjectStorageType` 新增 `'nfs'`，NFS 走对象存储 UI 路径（目录浏览模式）
   - `getSchemaTerminology` 对 NFS 返回 `'目录'`（而非 Bucket 或 Schema）

**MetaNode 节点类型**：NFS 根节点使用 `node_type: "bucket"`（与 MinIO 保持一致），前端通过引擎类型判断展示为"目录"。

扫描结果（验证通过）：
- `schemas/available` → `[{"name":"nas-data"}]`
- `storage/nodes` → `[{"name":"nas-data","path":"/","type":"bucket",...}]`
- 手动扫描 → `{"schemas_scanned":1,"tables_scanned":3,...}`
- DB 中根节点：`name="nas-data"`, `scan_status="已扫描"`, `item_count=3`

#### 2. Manager 模块：目录浏览 ✅

Manager 数据浏览通过 Meta API 查询节点，NFS 扫描后节点已正确写入，浏览功能自动可用。

#### 3. Manager 模块：文件预览

预览功能（`NFSStreamProvider`）尚未实现，点击文件时报 404 "node not found"，待阶段三处理。

---

### 阶段三：Transfer + Service + Develop（待实施）

**目标**：NFS 数据可导入、可发布服务、可在开发工作台使用。

#### 1. Transfer 模块：引入 FileAccessor + VFS 双抽象

两个接口统一放在 `transfer/backend/pkg/vfs/`（见第四节）。

**读路径改造**：

步骤 1：实现各存储引擎的 `FileAccessor`：
- `LocalFileAccessor`（重构现有逻辑）
- `NFSFileAccessor`（直接流式读取，无临时目录）
- `ObjectStorageFileAccessor`（重构现有 S3ShapefileReader 逻辑，临时目录方式）

步骤 2：改造格式读取器接受 `FileAccessor`：
- `ShapefileReader`：通过 `OpenSibling` 获取 .shx/.dbf
- `ParquetReader`：通过 `List` 支持目录表模式
- `GeoJSONReader`、`CSVReader` 等

**写路径改造**：

步骤 3：实现各存储引擎的 `VFS`：
- `LocalVFS`（封装 os.Create，现有写入器零行为变化）
- `NFSVFS`（调用 NFS plugin 的 OpenFileForWrite，直接写 NFS）

步骤 4：改造格式写入器接受 `VFS`（将 `os.Create` 替换为 `vfs.Create`）：
- `ShapefileWriter`、`GeoJSONWriter`、`ParquetWriter`、`CSVWriter`

步骤 5：新增 `NFSWriter`（`transfer/backend/plugins/writers/nfs_writer.go`），  
采用与 S3Writer 相同的委托模式，根据 `file_type` 选择格式写入器，传入 `NFSVFS`：

```go
type NFSWriter struct {
    vfs        vfs.VFS          // NFSVFS 实例
    fileWriter pipeline.Writer  // 委托给 ShapefileWriter / ParquetWriter 等
    engineID   uint
    path       string
    fileType   string
}
```

步骤 6：在 `builtin_registration.go` 注册 `"nfs"` writer。

**任务配置结构**（读写统一用 engine_id + connector 两个维度）：

```json
{
  "source": {
    "engine_id": 1,
    "connector": "postgresql",
    "schema": "public",
    "table": "roads"
  },
  "target": {
    "engine_id": 5,
    "connector": "nfs",
    "path": "exports/roads/",
    "file_name": "roads_2024",
    "file_type": "shapefile"
  }
}
```

**前端改造**：Transfer 任务创建 UI 新增 NFS 目标选项（engine 下拉 + 路径输入 + file_type 选择）。

#### 2. Service 模块：基于 FileSystemPlugin 的文件型数据服务

新增服务数据源类型 `file_spatial`（替代原计划的 `nfs_spatial`），  
适用于任何实现了 `FileSystemPlugin` 的存储引擎（NFS、MinIO、S3）。

**FileShapefileQueryHandler**（通用，非 NFS 专用）：

```go
type FileShapefileQueryHandler struct {
    accessor  FileAccessor   // 由 engine_id 对应的 FileSystemPlugin 构造
    filePath  string
    metaCache *MetadataCache // Redis 缓存：字段、CRS、bbox、要素数
}

func (h *FileShapefileQueryHandler) QueryFeatures(ctx, filter) ([]Feature, error) {
    // 1. 读取 Redis 缓存的元数据
    // 2. 通过 accessor.Open(.shx) 读取空间索引，定位 bbox 命中的要素偏移
    // 3. 通过 accessor.Open(.shp) 只读命中的要素块
    // 4. 通过 accessor.OpenSibling(.dbf) 读取属性，执行属性过滤
    // 5. 返回 GeoJSON Feature 列表
}
```

服务配置：

```json
{
  "source_type": "file_spatial",
  "engine_id": 5,
  "file_path": "/gis-data/roads/roads.shp",
  "file_format": "shapefile",
  "protocols": { "wfs": {"enabled": true}, "ogc_api": {"enabled": true} },
  "crs": "EPSG:4326"
}
```

#### 3. Develop 模块：工作流算子 + Notebook

**工作流算子**：
- 数据源选择器支持 `FileSystemPlugin` 类型的引擎（NFS、MinIO、S3 统一入口）
- 选择引擎后展示目录树（调用 Manager Explorer API）
- 算子配置记录 `engine_id` + `file_path`
- Python Workflow Engine 通过 ADDP 文件代理 API 获取文件流

**Notebook**：
- 注入 Python helper，通过 ADDP 文件代理 API 下载到 Jupyter 临时目录：
  ```python
  df = addp.open_file(engine_id=5, path="/gis-data/roads/roads.shp", format="shapefile")
  ```
- 内部统一走 `FileSystemPlugin.ReadFile()`，与存储引擎无关

---

### 阶段四：高级能力（按需）

| 能力 | 模块 | 说明 |
|------|------|------|
| Orchestrator 编排 | Orchestrator | 编排步骤引用 NFS 数据源，串联"读取→导入→发布"流程 |
| Manager 全文检索 | Manager + Meta | 基于 Meta 扫描的 Meilisearch 索引，支持文件名/属性检索 |
| Meta 向量化 | Meta | NFS 上的文档类文件（PDF、文本）向量嵌入，支持语义搜索 |
| NFSv4 支持 | common | 当前 NFSv3，按需升级 |

---

## 七、模块改动汇总

| 模块 | 改动类型 | 核心内容 | 状态 |
|------|---------|---------|------|
| `business/` | 新增 | docker-compose.yml 新增 NFS server；nas-data/ 测试数据 | ✅ 完成 |
| `common/engine/plugins/nfs/` | 全新 | NFS 引擎插件（plugin.go、client.go、register.go） | ✅ 完成 |
| `system/frontend/` | 小改 | 引擎配置表单新增 NFS 字段 | ✅ 完成 |
| `meta/backend/cmd/` | 小改 | server/main.go 和 worker/main.go 补充 NFS 插件 blank import | ✅ 完成 |
| `meta/backend/internal/service/` | 修复 | resource_discovery_service.go：新增 FileSystemPlugin 分支；scan_service.go：路由改为接口类型断言；filesystem_scan_service.go：修复根节点 name 为空 + scan_status 未更新 | ✅ 完成 |
| `meta/frontend/` | 小改 | MetadataScan.vue：NFS 走对象存储 UI 路径，术语显示"目录" | ✅ 完成 |
| `manager/backend/` | 待实施 | 新增 NFSStreamProvider（适配预览架构） | ⏳ 待实施 |
| `transfer/backend/` | 待实施 | FileAccessor 接口；各存储引擎实现；格式读取器改造 | ⏳ 待实施 |
| `service/backend/` | 待实施 | FileShapefileQueryHandler（通用）；file_spatial 服务类型 | ⏳ 待实施 |
| `develop/` | 待实施 | 算子数据源选择器支持 FileSystemPlugin 引擎；Notebook helper | ⏳ 待实施 |

---

## 八、技术风险与应对

| 风险 | 说明 | 应对 |
|------|------|------|
| Go NFS 客户端 Seek 支持 | `go-nfs-client` 的 ReadSeek 能力需验证 | 阶段一先验证；不支持则退化为临时目录 |
| Transfer 格式读取器改造范围 | ShapefileReader 等改造影响现有功能 | 保持接口兼容，LocalFileAccessor 封装现有逻辑 |
| Docker NFS server 权限 | 需要 SYS_ADMIN | 仅开发模拟；生产用真实 NAS |
| Shapefile DBF 编码 | 可能是 GBK/GB2312 | 复用 Transfer 现有编码处理逻辑 |

---

## 九、阶段依赖关系

```
阶段一（NFS 引擎插件）
    ↓
阶段二（Meta 扫描 + Manager 浏览/预览）← 依赖阶段一
    ↓
阶段三（Transfer + Service + Develop）← 依赖阶段一；Transfer 改造可并行
    ↓
阶段四（高级能力）← 依赖阶段二的 Meta 索引
```

---

## 十、macOS 本机验证路径

```bash
# 阶段一验证
cd business && docker-compose up -d nfs-server
# System UI 配置 NFS 引擎，server=host.docker.internal，export_path=/exports/data
# 点击"测试连接"，应成功

# 阶段二验证
# 触发 Meta 扫描，检查 MetaNode/MetaItem 是否正确创建
# Manager 浏览 NFS 目录树
# 点击 Shapefile 文件，验证预览（地图渲染）

# 阶段三验证
# Transfer：NFS Shapefile → PostgreSQL 导入任务
# Service：发布 NFS Shapefile 为 WFS 服务，curl 验证要素查询
# Develop：Notebook 中 addp.open_file() 读取 NFS 文件
```
