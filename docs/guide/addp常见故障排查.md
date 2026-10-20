# ADDP 常见故障排查指南

按现象进入对应专题。每条记录保留现象、根因、排查顺序、处理边界与验证入口；修复过程和逐次执行流水由 Git、CI 与验收报告保存。

## 按现象查找

| 范围 | 排查入口 |
| --- | --- |
| 开发服务 | [托管命令退出后服务立刻不可用](troubleshooting/开发服务与构建.md#codex-等托管命令环境中-restartsh-退出后服务立刻不可用)、[端口被浏览器占用的误报](troubleshooting/开发服务与构建.md#restartsh-误报端口被浏览器占用)、[旧进程被反复拉起](troubleshooting/开发服务与构建.md#restartsh-报告-addp-旧进程未清理) |
| 依赖与镜像 | [GeoPython 依赖安装失败](troubleshooting/开发服务与构建.md#geopython-workflow-依赖安装失败)、[Document 下载 Debian 依赖失败](troubleshooting/开发服务与构建.md#document-镜像下载-debian-依赖失败阻断全量启动)、[Python 镜像冷构建过慢](troubleshooting/开发服务与构建.md#python-镜像首次构建耗时过长) |
| 前端显示 | [Manager 预览没有数据](troubleshooting/前端与数据预览.md#manager-数据预览显示暂无数据)、[Vite 依赖分块缺失与首次导航中断](troubleshooting/前端与数据预览.md#前端页面重启后白屏vite-提示依赖分块不存在)、[血缘图自动适配异常](troubleshooting/前端与数据预览.md#血缘图自动适配后节点过小或偏离中心) |
| 条目与目录 | [条目刷新未更新属性](troubleshooting/元数据扫描与检索.md#manager-刷新-item-成功但属性未更新item-刷新误走-catalog-scan)、[上传后目录不可见或数量不一致](troubleshooting/元数据扫描与检索.md#manager-上传成功但文件只有扫描目录后才显示)、[目录扫描状态与时间异常](troubleshooting/元数据扫描与检索.md#目录仍显示扫描失败或失败子目录反而显示已扫描) |
| 扫描与检索 | [NFS 深度扫描进度与计数](troubleshooting/元数据扫描与检索.md#nfs-深度扫描长时间无新进展或完成数大于最终数据项数)、[SQLite / Excel 容器误识别](troubleshooting/元数据扫描与检索.md#sqlite-扫描要求-table-option或-excel-父文件被识别为-table)、[Meta 已登记但 Manager 搜不到](troubleshooting/元数据扫描与检索.md#meta-已登记-dataitem但-manager-搜索不到) |
| 空间成果 | [Shapefile 缺少 .prj](troubleshooting/空间数据与快显.md#transfer-写出-shapefile-后资源树看不到-prj)、[MVT 物化视图报错](troubleshooting/空间数据与快显.md#mvt-物化视图准备失败---column-id-does-not-exist)、[PostgreSQL 字段大小写](troubleshooting/空间数据与快显.md#postgresql-混合大小写字段报-column-does-not-exist)、[COPC 快显失败](troubleshooting/空间数据与快显.md#点云-copc-快显生成失败) |
| CLI 与服务认证 | [macOS 钥匙串许可](troubleshooting/认证与引擎注册.md#macos-询问-python-访问-addp-cli-钥匙串密码)、[服务令牌有效期越界](troubleshooting/认证与引擎注册.md#服务令牌有效期越界导致模块退出)、[资源搜索提示授权服务不可用](troubleshooting/认证与引擎注册.md#资源搜索提示授权服务暂时不可用) |
| Runtime 注册 | [502 与代理](troubleshooting/认证与引擎注册.md#workflow-引擎注册失败-502)、[401 / 403 与身份归属](troubleshooting/认证与引擎注册.md#工作流-runtime-注册失败-401403) |
| 数据库迁移 | [版本超前、checksum 不一致和 dirty 状态](troubleshooting/数据库迁移.md#system-backend-因数据库迁移版本超前而启动超时)、[75](troubleshooting/数据库迁移.md#75-号迁移的定向恢复)、[113](troubleshooting/数据库迁移.md#113-号迁移的定向恢复)、[130](troubleshooting/数据库迁移.md#130-号迁移的定向恢复) 号迁移定向恢复 |
| 测试隔离 | [System 后台任务与 SQLite](troubleshooting/测试夹具与隔离.md#system-后台任务测试出现-sqlite-锁竞争或提前关库)、[Infra 单测受宿主监听影响](troubleshooting/测试夹具与隔离.md#infra-生命周期单测受真实宿主机监听影响)、[Online 包装超时](troubleshooting/测试夹具与隔离.md#online-宿主机包装测试报五秒超时)、[Python 引擎包名冲突](troubleshooting/测试夹具与隔离.md#workflow-engine-python-测试包名冲突) |

## 操作与验证边界

- 先关联 request / execution、日志和元数据事实，再选择受影响范围；不把“提交成功”“租约正常”“服务健康”当成业务执行完成。
- 实际端口以标准脚本输出与 `.dev-state/ports.env` 为准。Infra 连接前运行 `bash scripts/infra/status.sh`；本地共享 PostgreSQL 测试仅用 `addp_test` / `addp_iam_test`，由标准入口管理。
- 需要重启时先说明原因和模块，由用户在自己的终端执行。AI 执行 `keepalive.sh` 必须取得本次明确许可，退出清理范围见[开发服务专题](troubleshooting/开发服务与构建.md#codex-等托管命令环境中-restartsh-退出后服务立刻不可用)。
- 改动后的验收遵循[测试与验收规范](../spec/addp测试与验收规范.md)，使用根 `Makefile` 标准入口；文档示例不是本次已运行的证据。

## 相关入口

- [部署和开发步骤](addp部署和开发步骤.md)
- [开发服务生命周期与构建身份规范](../spec/addp开发服务生命周期与构建身份规范.md)
- [运行日志规范](../spec/addp模块服务运行日志规范.md)
- [Infra 指南](../../scripts/infra/README.md)
