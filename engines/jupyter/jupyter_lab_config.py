"""
Jupyter Lab 配置文件
配置使用 MinIO 作为存储后端
"""
import os
import sys

# 将当前目录添加到 Python 路径（以便导入 minio_contents_manager）
sys.path.insert(0, os.path.dirname(__file__))

# 配置 ContentsManager 使用 MinIO
c.ServerApp.contents_manager_class = 'minio_contents_manager.MinIOContentsManager'

# 禁用浏览器自动打开
c.ServerApp.open_browser = False

# 允许所有 IP 访问
c.ServerApp.ip = '0.0.0.0'

# 端口配置
c.ServerApp.port = int(os.getenv('JUPYTER_PORT', '8088'))

# 禁用 token 和密码（开发环境）
c.ServerApp.token = ''
c.ServerApp.password = ''

# 允许跨域
c.ServerApp.allow_origin = '*'
c.ServerApp.disable_check_xsrf = True

# 配置 CSP 以允许在 iframe 中嵌入
c.ServerApp.tornado_settings = {
    'headers': {
        'Content-Security-Policy': "frame-ancestors 'self' http://localhost:5170 http://localhost:5178"
    }
}

# 日志配置
c.ServerApp.log_level = os.getenv('LOG_LEVEL', 'INFO').upper()

# 启用日志到控制台
c.ServerApp.allow_root = True

print("=" * 60)
print("Jupyter Lab 配置已加载")
print(f"  ContentsManager: MinIO 存储后端")
print(f"  端口: {c.ServerApp.port}")
print(f"  日志级别: {c.ServerApp.log_level}")
print("=" * 60)
