#!/usr/bin/env python3
"""
迁移脚本: 加密 system.resources 表中的敏感字段
"""
import json
import base64
import psycopg2
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.backends import default_backend
import os

# 加密密钥 (开发环境默认值,与Go代码一致)
ENCRYPTION_KEY = b"dev-encryption-key-32-bytes!"

def encrypt(plaintext: str) -> str:
    """使用 AES-256-GCM 加密"""
    aesgcm = AESGCM(ENCRYPTION_KEY)
    nonce = os.urandom(12)  # GCM 标准 nonce 大小
    ciphertext = aesgcm.encrypt(nonce, plaintext.encode(), None)
    # 拼接 nonce + ciphertext, 然后 base64 编码
    return base64.b64encode(nonce + ciphertext).decode('utf-8')

def main():
    # 连接数据库
    conn = psycopg2.connect(
        host="localhost",
        port=5432,
        user="addp",
        password="addp_password",
        database="addp"
    )
    cur = conn.cursor()

    try:
        # 查询所有资源
        cur.execute("SELECT id, connection_info FROM system.resources")
        rows = cur.fetchall()

        updated = 0
        for resource_id, conn_info in rows:
            changed = False
            sensitive_fields = ["password", "access_key", "secret_key", "token", "api_key"]

            for field in sensitive_fields:
                if field in conn_info and isinstance(conn_info[field], str):
                    value = conn_info[field]
                    # 检查是否已加密 (简单判断: 加密后长度会显著增加)
                    if value and len(value) < 50:
                        encrypted_value = encrypt(value)
                        conn_info[field] = encrypted_value
                        changed = True
                        print(f"资源 {resource_id}: 已加密字段 {field}")

            if changed:
                # 更新数据库
                cur.execute(
                    "UPDATE system.resources SET connection_info = %s WHERE id = %s",
                    (json.dumps(conn_info), resource_id)
                )
                updated += 1
                print(f"资源 {resource_id} 更新成功")

        conn.commit()
        print(f"\n迁移完成! 总共更新了 {updated} 个资源")

    except Exception as e:
        conn.rollback()
        print(f"错误: {e}")
    finally:
        cur.close()
        conn.close()

if __name__ == "__main__":
    main()
