-- 删除未使用的 managed_file 表
-- 该表从未被实际使用，仅有模型定义和转换方法（未调用）
-- 删除时间: 2026-01-07

DROP TABLE IF EXISTS manager.managed_file;
