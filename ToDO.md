*，验证codex的改动，restart sh可用；OK

数据预览：
*，支持所有文件下载；OK
*，支持wps文档；OK
*，支持shape；先凑合；
*，支持md；OK

元数据和内置存储：
*，增加数据指纹；OK
*，扫描各种数据类型，插件式解析；OK
*，定时自动扫描；OK

*，portal中还残留meta扫描代码，彻底删除
*，meta登录不了

*，支持全文检索，用es

*，等做了数据传输，则开启neo4j，记录数据血缘

*，海量空间数据（如shapefile）预处理（与快显）
    用mvt？ geobuf？
*，api方式的插件，先不做，有些复杂；

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