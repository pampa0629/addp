package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/addp/system/pkg/utils"
)

func main() {
	// 使用开发环境默认密钥
	encryptionKey := []byte("dev-encryption-key-32-bytes!")

	// 连接数据库
	dsn := "host=localhost port=5432 user=addp password=addp_password dbname=addp sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer db.Close()

	// 查询所有资源
	rows, err := db.Query("SELECT id, connection_info FROM system.resources")
	if err != nil {
		log.Fatalf("查询资源失败: %v", err)
	}
	defer rows.Close()

	updated := 0
	for rows.Next() {
		var id int
		var connInfoJSON string

		if err := rows.Scan(&id, &connInfoJSON); err != nil {
			log.Printf("扫描行失败: %v", err)
			continue
		}

		// 解析 JSON
		var connInfo map[string]interface{}
		if err := json.Unmarshal([]byte(connInfoJSON), &connInfo); err != nil {
			log.Printf("资源 %d JSON 解析失败: %v", id, err)
			continue
		}

		// 加密敏感字段
		sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}
		changed := false

		for _, field := range sensitiveFields {
			if val, exists := connInfo[field]; exists {
				if strVal, ok := val.(string); ok && strVal != "" {
					// 检查是否已加密 (简单判断: 加密后是 Base64,长度会显著增加)
					if len(strVal) < 50 { // 未加密的密码一般较短
						encryptedVal, err := utils.Encrypt(strVal, encryptionKey)
						if err != nil {
							log.Printf("资源 %d 加密字段 %s 失败: %v", id, field, err)
							continue
						}
						connInfo[field] = encryptedVal
						changed = true
						log.Printf("资源 %d: 已加密字段 %s", id, field)
					}
				}
			}
		}

		if changed {
			// 更新数据库
			newJSON, err := json.Marshal(connInfo)
			if err != nil {
				log.Printf("资源 %d JSON 序列化失败: %v", id, err)
				continue
			}

			_, err = db.Exec("UPDATE system.resources SET connection_info = $1 WHERE id = $2", newJSON, id)
			if err != nil {
				log.Printf("资源 %d 更新失败: %v", id, err)
				continue
			}

			updated++
			log.Printf("资源 %d 更新成功", id)
		}
	}

	log.Printf("迁移完成! 总共更新了 %d 个资源", updated)
}
