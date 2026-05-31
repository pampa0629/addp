# Transfer 转换器架构分析（历史归档）

更新时间：2026-05-30

本文曾记录旧 Transfer 私有 Reader / Writer / Pipeline / ConnectorRegistry 方案。该方案已经退出当前主路径，不再作为实现依据。

当前稳定结论：

- Transfer 不再维护私有 reader / writer 插件体系。
- 具体 engine-native 读写能力归 `common/engine`。
- 具体格式和数据类型读写能力归 `common/format`。
- content 定位、读取、写入、range 和 scope list 归 `common/contentio`。
- engine content provider 到 contentio 的适配归 `common/engine/contentadapter`。
- Transfer transform 当前稳定类型为 `field_mapping`，写在 `config.transforms[]` 中。

当前 table Transfer 数据流：

```text
source endpoint
  -> common engine / format table reader
  -> executor table batch
  -> field_mapping transform
  -> common engine / format table writer
  -> target endpoint
```

后续新增转换器或 transform 时，应先判断能力归属：

| 能力 | 归属 |
|---|---|
| 引擎读写、批量写入、COPY、bulk insert | `common/engine` |
| 格式解码 / 编码、table reader / writer | `common/format` |
| 字段映射、过滤、派生字段、表达式编排 | Transfer transform |
| 可被多个模块复用的通用数据处理能力 | 优先上收到 `common/` |

当前主文档：

- [Transfer 当前架构设计](design.md)
- [Transfer 模块基本概念及配置说明](transfer-基本概念及配置说明.md)
- [ADDP 数据类型与格式能力规范](../../docs/spec/addp数据类型与格式能力规范.md)
- [ADDP 引擎插件接口规范](../../docs/spec/addp引擎插件接口规范.md)
