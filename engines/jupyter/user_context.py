"""
租户上下文工具
从 JWT token 或请求头中提取 tenant_id
"""
import jwt
from config import config


class TenantContext:
    """租户上下文（仅租户信息，不包含用户）"""

    def __init__(self, tenant_id=None):
        self.tenant_id = tenant_id or config.DEFAULT_TENANT_ID

    @property
    def base_path(self):
        """获取租户的 MinIO 基础路径"""
        return f"tenant_{self.tenant_id}/notebooks"

    def __repr__(self):
        return f"TenantContext(tenant_id={self.tenant_id})"


def parse_jwt_token(token):
    """
    解析 JWT token 提取租户信息

    Args:
        token: JWT token 字符串

    Returns:
        TenantContext 或 None
    """
    if not token:
        return None

    try:
        # 移除 "Bearer " 前缀
        if token.startswith('Bearer '):
            token = token[7:]

        # 解析 token
        payload = jwt.decode(
            token,
            config.JWT_SECRET,
            algorithms=['HS256']
        )

        # 提取租户信息
        tenant_id = payload.get('tenant_id')

        if tenant_id:
            return TenantContext(tenant_id=str(tenant_id))

    except jwt.ExpiredSignatureError:
        print("⚠️  JWT token 已过期")
    except jwt.InvalidTokenError as e:
        print(f"⚠️  无效的 JWT token: {e}")
    except Exception as e:
        print(f"⚠️  解析 JWT token 失败: {e}")

    return None


def get_tenant_context_from_request(request_headers=None):
    """
    从请求头中获取租户上下文

    优先级:
    1. Authorization header (JWT token)
    2. X-Tenant-ID header
    3. 默认值（开发环境）

    Args:
        request_headers: 请求头字典

    Returns:
        TenantContext
    """
    if not request_headers:
        return TenantContext()

    # 1. 尝试从 JWT token 解析
    auth_header = request_headers.get('Authorization') or request_headers.get('authorization')
    if auth_header:
        tenant_ctx = parse_jwt_token(auth_header)
        if tenant_ctx:
            return tenant_ctx

    # 2. 尝试从自定义 header 读取
    tenant_id = request_headers.get('X-Tenant-ID') or request_headers.get('x-tenant-id')

    if tenant_id:
        return TenantContext(tenant_id=str(tenant_id))

    # 3. 返回默认值
    return TenantContext()


# 全局默认租户上下文（用于不支持请求上下文的地方）
default_tenant_context = TenantContext()
