# NFS 存储引擎计划

> 状态：阶段性方案已收口。当前实现以 provider 化 engine plugin 体系为准。

本文只保留 NFS 当前语义和后续注意事项。正式路径规范见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)，插件接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)。

## 当前语义

- NFS 是文件系统语义存储，不是对象存储。
- 连接配置中的 `export_path` 是挂载配置，不进入用户可见路径、`full_name` 或 ResourceLocator。
- 用户看到的根目录为透明 root，数据库中 root 节点 `name="."`、`full_name=""`。
- 数据路径从挂载根 `/` 开始，`full_name` 使用相对路径。

## 当前接口

- 目录发现：`CatalogProvider.ListChildren`
- 路径解析：`CatalogProvider.ResolvePath`
- 文件元数据：`ItemMetadataProvider.DescribeItem`
- 内容读取：`ContentReadableProvider.OpenContent`

旧的专用文件系统接口方案不再作为上层接口边界。

## 扫描与预览

- Meta 从 root 递归扫描目录和文件。
- 目录写入 `meta_node`，普通文件写入 `meta_item`。
- Parquet 文件或目录可识别为 `table` 语义。
- Manager 使用 locator `type=file` 或 `type=table` 预览。

## 后续事项

- 验证根目录文件、深层目录文件、湖表目录三类路径。
- 若引入 HDFS/local filesystem，应复用同一 Catalog/Content provider 模型。
