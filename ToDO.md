*，验证codex的改动，restart sh可用；OK

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

元数据提取：todo
    做一个统一的设计思考
    让AI核实
    检查代码
    验证效果


元数据：
*，增加数据指纹；OK
*，扫描各种数据类型，插件式解析；OK
*，支持定时自动扫描；OK
*，支持全文检索；OK
*，向量存储和语义检索；OK

*，刷新元数据，只刷新新的数据；todo
*，等做了数据传输，则开启neo4j，记录数据血缘
*，海量空间数据（如shapefile）预处理（与快显）
    用mvt？ geobuf？


数据传输：doing
*，验证可用性；doing
*，改进跳转到system做存储引擎配置；doing  

*，各类小bug的解决；todo
    先搞定这个；

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