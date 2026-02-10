"""
Jupyter Lab 配置文件
配置使用 MinIO 作为存储后端，并实现租户 Kernel 隔离
"""
import os
import sys
from jupyter_client.kernelspec import KernelSpecManager

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

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Kernel 隔离配置 - 只显示租户专属的 Kernel
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TenantKernelSpecManager(KernelSpecManager):
    """
    自定义 KernelSpecManager，只显示租户专属的 Kernel (tenant_*)
    隐藏系统默认的 python3 Kernel 和其他全局 Kernel
    """

    def get_all_specs(self):
        """
        重写 get_all_specs 方法，过滤掉非租户 Kernel
        """
        all_specs = super().get_all_specs()

        # 只保留 tenant_ 前缀的 Kernel
        tenant_specs = {
            name: spec
            for name, spec in all_specs.items()
            if name.startswith('tenant_')
        }

        return tenant_specs

    def get_kernel_spec(self, kernel_name):
        """
        重写 get_kernel_spec 方法，支持 fallback 到第一个租户 Kernel
        """
        # 如果请求的是系统 Kernel (如 python3)，自动 fallback 到第一个租户 Kernel
        if not kernel_name.startswith('tenant_'):
            tenant_specs = self.get_all_specs()
            if tenant_specs:
                # 返回第一个租户 Kernel
                first_tenant = list(tenant_specs.keys())[0]
                return super().get_kernel_spec(first_tenant)

        return super().get_kernel_spec(kernel_name)

# 使用自定义的 KernelSpecManager
c.ServerApp.kernel_spec_manager_class = TenantKernelSpecManager

print("=" * 60)
print("Jupyter Lab 配置已加载")
print(f"  ContentsManager: MinIO 存储后端")
print(f"  端口: {c.ServerApp.port}")
print(f"  日志级别: {c.ServerApp.log_level}")
print(f"  Kernel 过滤: 仅显示租户专属 Kernel (tenant_*)")
print("=" * 60)
