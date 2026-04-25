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
