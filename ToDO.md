请AI编程工具，无论是claude code还是codex，都不要修改本文档。
这个文档是作者在编写维护的。

doing：
*，并行传输：sqlite 到 pg空间库
排查错误，查看日志


*，sql开发；

独立编排调度模块（orchestrator / schedule ）
*，考虑用Temporal，先做进一步单独项目的验证
*，参考 labs/dolphin/docs/ 下的TEMPORAL_ARCHITECTURE.md文档
*，实现 sql 调度
*，实现 集成gis引擎，和空间算子的编排
*，考虑血缘的处理

元数据
*，调研开源元数据框架技术

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



多端支持：

*，浏览器？
*，微信小程序（开发版）
*，安卓？
*，苹果？
*，桌面端：如何选型？qgis？ vscode？
