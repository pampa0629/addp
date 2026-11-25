# ADDP Online 场景：SQL连接池验收

[返回同步与引擎消费](同步与引擎消费.md) · [T4 准入、报告与退出](../T4-Online验收规范.md)

## PostgreSQL 与 MySQL 的连接池配置生效

`sql-pool-consumer-flow`仅手动 Hosted Linux x86_64，自有 PG／MySQL 随机回环、合成表／SELECT 账号。登记者 `tenant.infrastructure_administrator`同 User 在 System 页只改本轮两 Engine，准备者扫两表／签精确 read，页面前清两类 Token；普通消费八项：引擎／Meta 目录、Manager 项／内容、Develop 任务读／执行、数据读、执行授权签发，不随改池增权。当前构建／保护投影先就绪。

Develop 原生 PostgreSQL／MySQL 读取中，引擎配置版本用于选取连接池。System 仅对连续的纯调池变更保留旧读取版本的有效性，依据实际提交前后的连接配置、生命周期及能力判定。地址、登录身份、凭据或其他来源配置变化后，先前的读取版本失效，即使后来改回原值也不恢复。每次读取和交付仍按当前用户、执行租约及 DataItem Grant 重核。源端取证同时覆盖 PostgreSQL 直接 SELECT 与只读游标。

**固定四步。** 真实 Manager／Develop 先建立闲连接，System 页顺序：`127.0.0.1`→同实例 `localhost`（正式同实例确认＋原生身份／容器 ID）、同表同 SELECT 的第二 DB 账号、该账号密码、最大空闲 1→0。最大连接一直 2，前三留 1 闲置、最后无闲；库／端口／Engine 身份／两 Grant／八权限不变，DB 账号不等 ADDP User。

PG TCP 含回环 `scram-sha-256`、socket 仅初始化；MySQL 原生密码。启动前全新连接证错密码认证拒绝／正确成功，不用 trust 或网络失败充数。改密先一次源端更新、全新连接证旧拒新成，再页面保存。每次正式连接测试 online、不再次增 version；核请求连续版本、两页面精确值、当前 DB 账号原生会话／旧连接释放／新连接接替且消费进程未重启。日志只会话标识／分类、无 SQL／密码。

| 切片 | 必要反证与恢复 |
| --- | --- |
| 凭据离线／恢复 | 空闲 0 时仅源账号临时密码，System 配置／版本／Grant／权限不动。原生连接证登记密码拒绝／临时成功，正式测试 offline；已显示一行的 Manager 刷新后清旧／提示不可用且不再发预览，查询页列离线但禁选／不执行，原读取裁决仍 200。恢复原密后反向证密、正式 online／版本不变，两页面实读一行、新 execution／新连接、读完关闭。留两引擎离线／禁选／恢复截图，不声称已验预览 503／失败 execution／原 DB 错误面板。 |
| 在途改池 v4 | 夹具锁原表，旧页面查询须源端锁等待／借出实连接；System 只空闲 0→1、version5→6、最大 2，其余不变，新页同 User 查询。解锁前两不同连接同时等原表；只释放自有锁，旧／新 execution 各读一行、旧关闭／新闲置，Manager 也读一行。不以 pending／sleep 代真实重叠。`addp.sql-pool-console/v4`含原四步／16execution／密与连接替换链。 |
| 排队与 SQL v5 | 两引擎各最大 1，长预算 API Manager／Develop 分别占各进程唯一连接；源端两者都锁等待后才页面同源请求。超时须排队者未入源读取／引擎在线，解锁恢复；另空闲池 Develop 页面证真实 SQL 锁等待。`addp.sql-pool-console/v5`保原矩阵，缺源端反证不认池饱和。 |
| v6 历史与 v7 边界 | Provider 预览 55 秒早于浏览器 60 秒；连接获取／SQL HTTP504 安全码、未知阶段 deadline 通用，页面持久失败；Develop 按终态。v6 Manager504 后曾仍有原源端等待，不能推 SQL 即停。v7 排队终态原 Manager 等待须结束，两库仅原 Develop 长预算占池者；保旧事实、不充新通过。Develop 排队时无新增源端等待，SQL 执行超时另验；Manager 获取连接超时由实库 Provider／HTTP 映射／页面合约分层覆盖。 |

停服前原身份撤两 read／验拒绝，自有环境销毁。迟到版本拒绝沿 T2；`make test-sql-pool-online-runner`、原 CI 发现／`sql-pool-hosted-t4`，各报告版本／本地与 Hosted 单独认定，不新 suite／服务／端口。

Manager 获取连接超时的页面验收使用 `addp.sql-pool-console/v8`。对每种引擎各先打开一个预览，隔离夹具在回环地址暂缓该样例的一次成功 Meta 响应。另一预览实际占用唯一连接后，夹具放行响应。先发页面须显示 HTTP 504 和 `preview_connection_acquisition_timeout`，同时核对源库中等待的两个会话身份。占用连接的预览须显示 `preview_sql_execution_timeout`。解锁后，先发页面须读回原样例行。

### MySQL 数据库节点授权

独立准备阶段先给普通用户签发一份 MySQL database 节点 read。以实际 Develop 查询分别读取已有表、获权后新建的表，再访问另一 database 中的同名表，最后撤权并以新 execution 重读。两个库的源账号均有原生 SELECT 权。记录成功时的行值及业务 SELECT、拒绝时的权限原因及零业务 SELECT，输出 `mysql-node-read.json`。命令签发前写入清理清单。此阶段使用独立表名，完成后再进入原连接池验收。

### 关系型原生预览来源授权入口回归

原 SQL 池及 HighGo business 复用身份／范围：准备者仅对 MySQL `pool_fixture.pool_source`、HighGo `addp_business.customers`临签 metadata_read，普通 User 见同 Meta 身份但内容 read-check／真实 Manager 均 403、失败面板无旧表。撤临时 metadata 核隐藏后才办原 read，继续换池／查询／replace，不扩权限或把办理 Token 给浏览器。GaussDB／StarRocks 仍五表及 metadata-only 反例。

签前原私有 journal 登记，失败停服前清；共享入口单元不代每厂商实测，不证并发 DDL／间接来源闭包。入口 `make test-module MODULE=manager GO_ONLY=1`、`make test-highgo-online-runner`、`make test-sql-pool-online-runner`、`make test-starrocks-online-runner`和原固定 SHA Hosted，`test-online-registration-unit`核共享登记／夹具；运行／失败／通过按当轮分别记。
