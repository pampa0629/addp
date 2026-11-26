# ADDP Online 场景：DEM 通视与可视域

[返回测试与验收总入口](../../addp测试与验收规范.md) · [返回空间成果场景](Manager与空间成果.md)

三套均沿原页[本组专项的核验与证据边界](Manager与空间成果.md#本组独立专项的核验与证据边界)，各自保留矩阵、身份、只读取证和清理范围。准备／本地通过不计 Linux 全量或 Hosted；原场景与正交旋转扩展分别认定。

遮挡／NoData 优先级与网格准入以[通视契约](../../workflow/栅格算子契约.md#dem-两点通视)及[可视域契约](../../workflow/栅格算子契约.md#dem-单观察点可视域)为准。

| 场景 | 正文入口 |
| --- | --- |
| 两点通视 | [单行 CSV、距离与三态](#dem-两点通视独立-hosted-验收) |
| 单观察点可视域 | [Byte 三态、NoData 与半径](#dem-单观察点可视域独立-hosted-验收) |
| 正交旋转 | [双身份、北向对照及两轮未完成 Hosted](#正交旋转网格通视与可视域独立-hosted-验收) |

## DEM 两点通视独立 Hosted 验收

`raster-line-of-sight-persistence`。16 CSV链含平地／山脊、严格贴地与邻高程／端点触地／持续贴地、格角仅触及或真穿入／横纵边、NoData／mask／alpha与遮挡优先、两英尺／矩形第二波段跨块；18类中英共36通视失败，极限／注入仅Native。

独立逐格相交冻结期望，136＋576＝712次原CSV全检，核唯一行／列序／精确Float64米制距离三态，预览按原序列化另核。存16 CSV／清单，核保存目标／类型／写方式与物理＝Meta；唯一真DEM→CSV内部save／公开投影／Meta／Monitor，observer仅本suite白名单。

两点通视的 36 次双语拒绝全部命中 sight 节点。公开执行和 Monitor 核本次状态、触发者和冻结语言，隔离只读回执核同次租户与 DAG。完整执行清单仅含 16 条成功及 36 条拒绝，保留原 CSV 矩阵和全检。

## DEM 单观察点可视域独立 Hosted 验收

`raster-viewshed-persistence`。16 COG链含平地／持续贴地、山脊／沿途NoData与遮挡优先、mask／透明及部分alpha、半径等号邻浮点、重合／子像元观察点／半径内无目标、两英尺、填洼及519×515矩形跨块；18类中英共36拒绝，数值极限／异构复数／注入仅Native。

独立精确矩形裁剪，Byte三态0/1/2、NoData255／mask／六仿射／CRS／nearest／COG精核；136＋576＝712原件全检，预检＋52现场共53物理调用，存16 COG／大小SHA，期望／准备／收尾纳600秒。初始化仅本轮精确NFS文件限时metadata_read并全退出撤，填洼链保原DEM的raster_save输入；大小／内容仅隔离Hosted当轮User／scan／execution／target绑定的有界只读SQL，不改公开投影／权限。

## 正交旋转网格通视与可视域独立 Hosted 验收

`raster-sight-orthogonal-persistence`，准备者复用办理角色13权限（原11＋扫描执行／读取），只扫固定源后给执行User精确限时metadata_read，不给执行者引擎管理。源扫／物理Meta快照绑准备者，16业务链／自动扫／血缘绑原12权限User，两清单分核，全部退出撤Grant。

8组各CSV通视＋COG可视域：北向矩形／一般正交／90°／180°、子像元／格角格边、NoData/mask/alpha与遮挡优先、两英尺跨块；第二波段、半径等号邻浮点／重合中心，以完整六仿射＋独立逐格矩形相交核北向／旋转对照。两算子对剪切／近似正交／双单轴镜像／地理／无CRS／无定位／退化各中英共32拒绝，通视重合／半开界外及可视域零半径／越界另8，共40。16成功136＋40×16＝776全检，准备／56现场／清理纳600秒；存8 CSV／8 COG，分别核列序／唯一行／距离三态及声明、像元mask／网格／布局／nearest，极限／注入仅Native。

派发必填`sight_orthogonal_source_commit`完整40位小写SHA；main仅编排，checkout等于所选业务源且为编排祖先，两SHA分记，不缺参回退main／影响其他suite。

| 固定运行 | 首败与未完成范围 |
| --- | --- |
| 业务d240f16bc／编排f21728b33；[42448726028／128633145625](https://github.com/pampa0629/addp/actions/runs/42448726028/job/128633145625) | 检出／祖先／七服务预检过，真实main缺os导入NameError；无报告／8CSV8COG清单，16/40/776未验。wrapper／Infra清零不补Grant撤销报告。 |
| aeabceb3d；[42469659281／128697238003](https://github.com/pampa0629/addp/actions/runs/42469659281/job/128697238003) | 原执行User集中扫源缺引擎管理范围403，矩阵／原件未执行。双身份修正及本地／隔离身份T1不算Hosted，新源须另确认；旧两失败不追改／扩600秒。 |
