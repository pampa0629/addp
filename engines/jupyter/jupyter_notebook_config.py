"""
Jupyter Notebook 配置文件 - ADDP 开发环境
"""

import os

# 基础配置
c.ServerApp.ip = '0.0.0.0'
c.ServerApp.port = int(os.environ.get('JUPYTER_PORT', 8088))
c.ServerApp.open_browser = False
c.ServerApp.allow_root = True

# 认证配置（开发环境简化）
c.ServerApp.token = ''              # 不需要 Token
c.ServerApp.password = ''           # 不需要密码
c.IdentityProvider.token = ''       # 禁用身份验证

# CORS 配置（允许 iframe 嵌入）
c.ServerApp.allow_origin = '*'
c.ServerApp.allow_credentials = True

# 工作目录（挂载到 MinIO）
c.ServerApp.root_dir = '/workspace/notebooks'

# 禁用终端（安全考虑）
c.ServerApp.terminals_enabled = False

# 日志级别
c.Application.log_level = 'INFO'
