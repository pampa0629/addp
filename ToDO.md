请AI编程工具，无论是claude code还是codex，都不要修改本文档。
这个文档是作者在编写维护的。


doing：
*，并行传输：sqlite 到 pg空间库
    排查错误，查看日志

*，business 编排的修改
    默认支持postgis，根据cpu自动拉取镜像
    初始化脚本检查

独立编排器
*，考虑用Temporal，先做进一步单独项目的验证
*，参考 labs/dolphin/docs/ 下的TEMPORAL_ARCHITECTURE.md文档
*，实现 sql 调度
*，实现 集成gis引擎，和空间算子的编排
*，考虑血缘的处理

元数据
*，调研开源元数据框架技术

海量空间数据支持：
*，先传输来实现数管的导入（依赖架构调整）
*，导入后，到pg 以及 minio（+iceberg？）
*，st sql 支持
*，快显支持（导入时，提取元数据+切片？）
*，发布服务？后续做

manager模块--数据预览
*，支持shape；后续优化； todo

meta模块
*，等做了数据传输，则开启neo4j，记录数据血缘
*，海量空间数据（如shapefile）预处理（与快显）
用mvt？ geobuf？

addp系统库
*，系统minio，需要存储什么东西？to think
代码文件？mvt快显瓦片？

transfer模块
*，如何让传输任务，给数据管理的导入、导出使用？ todo

*，kafka等的支持；todo
先询问设计：kafka的json格式说明先；done
实现；todo

*，如果断网传输，应该如何实现？todo
完成设计；done
实现；todo

数据类型：OK
*，空间数据
geojson、shape、geocsv、spatialite、
*，文档类
md、docx、wps、pdf
*，表格类
csv、sqlite、excel
*，视频
mov、mp4
*，图片
jpg、png、bmp
*，todo：
tiff、mosaic、filegdb、udb、？？？

*，Parquet 列式存储
格式研究，OK
iceberg，把parquet存储文件，变为表格管理；
性能，估计不会差，需要数据测试；

DFX：
*，换台机器的容器化部署
*，本地镜像库的部署
*，高可用，双节点热备？

*，AI coding，实现用户驱动改进；思路可行，具体待设计；OK


多端支持：
*，浏览器？
*，微信小程序（开发版）
*，安卓？
*，苹果？
*，桌面端：如何选型？qgis？ vscode？