# ADDP 配置规范

本文是配置规范总入口。配置归属、作用范围与生效方式见基础规则；各模块和部署专题保留对应约束及示例。

## 阅读导航

| 阅读内容 | 专题 |
| --- | --- |
| 配置分类、事实来源、范围权限、能力登记与生效方式 | [配置归属与生效](configuration/配置归属与生效.md) |
| 部署说明、根环境文件、Secret、文件存储、持久化与节点标识 | [部署环境与密钥](configuration/部署环境与密钥.md) |
| 根 .env 示例、配置生成命令与端口 | [环境变量参考](configuration/环境变量参考.md) |
| IAM 策略、首次初始化、认证端点与栅格引擎资源策略 | [System安全与引擎策略](configuration/System安全与引擎策略.md) |
| 导入中转、mosaic、有界执行监督器、向量化、底图与快显 | [Manager配置](configuration/Manager配置.md) |
| Develop、Transfer、Service 和模块配置入口及禁止暴露的旧变量 | [执行与模块策略](configuration/执行与模块策略.md) |
| 观测边界、告警与通知、运行日志及指标中心部署 | [Monitor与运行观测](configuration/Monitor与运行观测.md) |
| Infra Kafka、Kafka Connect 与 Capture Supervisor | [Kafka与Capture部署](configuration/Kafka与Capture部署.md) |
| Business 引擎的部署参数、端口与启动约束 | [Business数据引擎部署](configuration/Business数据引擎部署.md) |

## 使用顺序

1. 先按[配置归属与生效](configuration/配置归属与生效.md)确认 owner、事实来源、范围、权限与生效方式。
2. 部署、密钥和端口配置按部署专题维护；环境变量示例配合对应专题阅读。
3. 模块普通运行配置由 owner 持久化管理，不能以环境变量建立第二条读取路径。
