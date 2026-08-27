# IAM Owner 资源授权与 Asset 衔接设计（已并入企业数据目录专题）

本文原先讨论的通用 Owner ResourceRef 方案已被 CatalogEntry 稳定身份方案取代，不能作为 Asset 来源模型继续实现。

当前唯一链路是：专业模块 owner → CatalogEntry → `AssetComponent.catalog_entry_id` → Asset 授权。IAM 负责主体、权限、Resource Grant 和运行时身份，不承担跨专业资源目录身份。

后续设计和迁移清单统一见：

- [ADDP 企业数据目录能力专题](ADDP企业数据目录能力专题.md)
- [ADDP 企业数据目录实现规范](../spec/addp企业数据目录实现规范.md)
