写一个golang程序，用spatialite的方式打开文件： /Users/pampa/Documents/data/bigdata/dltb1000w.sqlite 
执行sql：UPDATE DLTB 
SET SmGeometry = ST_SetSRID(SmGeometry, 2360);
并告知结果