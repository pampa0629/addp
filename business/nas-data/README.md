NFS 测试数据目录
================

此目录挂载到 NFS server 容器的 /exports/data 路径。

目录结构：
  gis-data/
    sample.geojson  - GeoJSON 点数据（中国城市）
    sample.csv      - CSV 表格数据

启动 NFS server：
  cd business && docker-compose --profile nfs up -d nfs-server

连接配置（System UI 中填写）：
  server: host.docker.internal
  export_path: /exports/data
  access_mode: rw
  nfs_version: 3
