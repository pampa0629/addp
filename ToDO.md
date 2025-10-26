请AI编程工具，无论是claude code还是codex，都不要修改本文档。
这个文档是作者在编写维护的。

体系：
*，内嵌AI coding工具和编译工具，实现自举；to think


移动端支持：
*，浏览器？
*，微信小程序（开发版）
*，安卓？
*，苹果？ 

数据预览：
*，支持所有文件下载；OK
*，支持wps文档；OK
*，支持shape；先凑合用； todo
*，支持md；OK

元数据：
*，等做了数据传输，则开启neo4j，记录数据血缘
*，海量空间数据（如shapefile）预处理（与快显）
    用mvt？ geobuf？

*，验证数据传输任务可用；doing 
*，修改 mydesign 和 架构图；todo  

数据传输：todo
*，如何让传输任务，给数据管理的导入、导出使用？

*，Parquet 格式研究，看如何用起来？
    性能；

*，kafka等的支持；todo
    先询问设计：kafka的json格式说明先；done
    实现；todo

*，如果断网传输，应该如何实现？todo
    完成设计；done
    实现；todo


数据类型：OK
*，文档类
    md、docx、wps、pdf
*，表格类
    csv、sqlite、excel
*，视频
    mov、
*，空间数据
    geojson、shape、
*，图片
    jpg、png、bmp
*，todo：
    tiff、mosaic、？？？

DFX：
*，换台机器的容器化部署
*，本地镜像库的部署
*，高可用，双节点热备？
*，