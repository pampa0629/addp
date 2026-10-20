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


待核验：NFS 软链接目录与读取边界（2026-10-20）
-----------------------------------------

本轮对照 FTP 账号根时只读审查发现，`common/engine/plugins/nfs/plugin.go` 的 `listDirectory` 仅区分 IsDir 和其余条目，未显式拒绝原生符号链接；Facts／Open 路径也需一起核对。固定 NFS 客户端提供原生类型和 READLINK，但当前插件没有调用 READLINK；不能据此宣称已经验证所有链接读取或根逃逸行为。后续应在独占 NFS 门禁加入普通链接、目录链接、导出根外目标、目录／Facts／读取一致性与原始路径回归，再确定修复范围。本次仅记录，未修复、未执行实库链接回归，不作为 FTP 模块扩大 NFS 改动范围的依据。
