NFS 测试数据目录
================

使用 macOS 内置 NFS 服务（端口 2049 已由系统占用，无需 Docker 容器）。

目录结构：
  data/
    gis-data/
      sample.geojson  - GeoJSON 点数据（中国城市）
      sample.csv      - CSV 表格数据

初始化（首次配置，需要 sudo）：
  sudo sh -c 'echo "/Users/pampa/code/addp/business/nfs/data -alldirs -mapall=501 -noresvport" >> /etc/exports'
  sudo nfsd restart

连接配置（System UI 中填写）：
  server: localhost
  export_path: /Users/pampa/code/addp/business/nfs/data


NFS 读取边界与 IFC／3DS 快显
--------------------------

NFS 插件仅接受原生普通文件和目录。目录枚举跳过软链接、特殊文件及未知类型；文件事实和读取逐级检查原生类型，拒绝末级链接、中间目录链接及其下的路径。校验和读取使用同一文件句柄，不调用 READLINK 跟随链接。实现位于 `common/engine/plugins/nfs/read_protocol.go`；边界由真实 NFSv3 的 `make test-common-nfs-integration` 验证，覆盖普通文件完整／范围读取、链接、导出根外链接目标和特殊文件拒绝。测试必须使用独立生成的夹具目录，不能直接将业务数据目录作为清理目标。

IFC／3DS 的主文件与确认的依赖由 Meta 基础扫描识别并保存。Manager 快显消费该精确组成，在正式来源授权后暂存必需文件，调用原生 Model3D 转换并清理临时输入；不在快显时扩大扫描或读取整个目录。这两个格式已删除原 NFS 宿主机直接挂载转换路径；其他格式及 MinIO 尚未纳入本次迁移。

2026-10-22 已通过本机 macOS 原生 NFS 协议及 IFC／带贴图 3DS 页面验收；两种快显重开复用原成果，临时输入物理对象已清理。独立 Linux NFS 协议及 FTP／NFS 双来源 Hosted T4 同时验证了实际 GLB、贴图内嵌、缓存复用、来源授权撤销后的拒绝与隔离资源清理，见 [Hosted 验收 39755682516](https://github.com/pampa0629/addp/actions/runs/39755682516/job/119746976547)。该结论限于上述协议边界及合成样例，不代表其他三维格式已完成迁移。
