package plugin

import "fmt"

// MySQLStyleDSN 构建 MySQL 风格的 DSN 字符串
// 格式：user:password@tcp(host:port)/database?params
// 支持 MySQL、Doris、TiDB 等兼容 MySQL 协议的数据库
func MySQLStyleDSN(user, password, host string, port int, database string, params map[string]string) string {
	// 构建基础连接字符串
	var dsn string
	if password == "" {
		dsn = fmt.Sprintf("%s@tcp(%s:%d)/%s", user, host, port, database)
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, database)
	}

	// 添加额外参数
	if len(params) > 0 {
		dsn += "?"
		first := true
		for key, value := range params {
			if !first {
				dsn += "&"
			}
			dsn += fmt.Sprintf("%s=%s", key, value)
			first = false
		}
	}

	return dsn
}

// PostgreSQLStyleDSN 构建 PostgreSQL 风格的 DSN 字符串
// 格式：host=xxx port=xxx user=xxx password=xxx dbname=xxx sslmode=xxx
func PostgreSQLStyleDSN(user, password, host string, port int, database, sslmode string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslmode)
}

// ClickHouseStyleDSN 构建 ClickHouse 风格的 DSN 字符串
// 格式：tcp://host:port?username=xxx&password=xxx&database=xxx
func ClickHouseStyleDSN(user, password, host string, port int, database string, params map[string]string) string {
	dsn := fmt.Sprintf("tcp://%s:%d", host, port)

	// 添加基础参数
	allParams := make(map[string]string)
	if user != "" {
		allParams["username"] = user
	}
	if password != "" {
		allParams["password"] = password
	}
	if database != "" {
		allParams["database"] = database
	}

	// 合并额外参数
	for key, value := range params {
		allParams[key] = value
	}

	// 构建查询字符串
	if len(allParams) > 0 {
		dsn += "?"
		first := true
		for key, value := range allParams {
			if !first {
				dsn += "&"
			}
			dsn += fmt.Sprintf("%s=%s", key, value)
			first = false
		}
	}

	return dsn
}

// MongoDBStyleDSN 构建 MongoDB 风格的 DSN 字符串
// 格式：mongodb://user:password@host:port/database?authSource=authSource
// authSource 是用户凭据所在的数据库（root 用户通常是 admin），与操作用的 database 是两个不同的概念
func MongoDBStyleDSN(user, password, host string, port int, database, authSource string) string {
	if user == "" {
		return fmt.Sprintf("mongodb://%s:%d/", host, port)
	}

	// authSource 默认 admin（MongoDB root 用户的认证库）
	if authSource == "" {
		authSource = "admin"
	}

	dbPath := ""
	if database != "" {
		dbPath = database
	}

	query := fmt.Sprintf("?authSource=%s", authSource)

	if password != "" {
		return fmt.Sprintf("mongodb://%s:%s@%s:%d/%s%s", user, password, host, port, dbPath, query)
	}
	return fmt.Sprintf("mongodb://%s@%s:%d/%s%s", user, host, port, dbPath, query)
}
