package scanners

// 注意：Scanners 在运行时根据 resource 配置动态创建
// factory.go 中的 NewScanner() 函数会调用本包中的构造函数
//
// 可用的 Scanner 实现：
// - MySQLScanner (mysql_scanner.go)
// - PostgresScanner (postgres_scanner.go)
// - S3Scanner (s3_scanner.go) - 支持 S3/MinIO/OSS
