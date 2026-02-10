#!/usr/bin/env python3
"""
ADDP Data Source Auto-Loader for IPython/Jupyter Kernel

这个脚本在 IPython Kernel 启动时自动执行，负责：
1. 从环境变量读取租户信息
2. 调用 ADDP System API 获取数据源列表
3. 自动创建 ds_* 全局变量

部署位置：
  Jupyter 容器内：/root/.ipython/profile_default/startup/00-addp-datasources.py

环境变量：
  TENANT_ID: 租户 ID（必需）
  ADDP_API_BASE: ADDP API 基础 URL（默认：http://localhost:8000）
  ADDP_AUTO_LOAD: 是否自动加载（默认：true）

用户体验：
  用户创建新 Notebook → Kernel 启动 → 自动加载数据源 → 直接使用 ds_8 等变量
"""

import os
import sys

# ============================================================================
# 配置
# ============================================================================

ADDP_TENANT_ID = os.getenv('TENANT_ID') or os.getenv('ADDP_TENANT_ID')  # 兼容两种环境变量名
ADDP_API_BASE = os.getenv('ADDP_API_BASE', 'http://localhost:8000')
ADDP_AUTO_LOAD = os.getenv('ADDP_AUTO_LOAD', 'true').lower() == 'true'

# ============================================================================
# 主函数
# ============================================================================

def load_addp_datasources():
    """从 ADDP 加载数据源连接配置"""

    # 检查是否启用自动加载
    if not ADDP_AUTO_LOAD:
        return

    # 检查租户 ID
    if not ADDP_TENANT_ID:
        print("⚠️  ADDP 数据源加载已跳过（未设置 ADDP_TENANT_ID）")
        return

    print(f"🔄 正在加载 ADDP 数据源（租户 {ADDP_TENANT_ID}）...")

    try:
        import requests
    except ImportError:
        print("❌ 缺少 requests 库，无法加载数据源")
        return

    try:
        # 调用 ADDP System API 获取引擎列表
        # 注意：使用 System API（/api/system/engines）而不是 Develop API
        # 因为 Develop API 需要认证，而 System API 可以通过内网调用
        response = requests.get(
            f'{ADDP_API_BASE}/api/system/engines',
            params={'tenant_id': ADDP_TENANT_ID},
            timeout=5
        )

        if response.status_code != 200:
            print(f"❌ 获取数据源失败: HTTP {response.status_code}")
            if response.status_code == 404:
                print("   提示：System API 可能未启用引擎列表查询")
            return

        engines = response.json()

        if not engines:
            print(f"⚠️  租户 {ADDP_TENANT_ID} 暂无可用数据源")
            return

        # 注入数据源变量到全局命名空间
        loaded_count = 0
        datasource_info = []

        for engine in engines:
            engine_id = engine.get('id')
            engine_type = engine.get('engine_type')
            engine_name = engine.get('display_name') or engine.get('name')
            conn_info = engine.get('connection_info', {})

            # 构造连接配置
            ds_config = None

            if engine_type == 'postgresql':
                # 兼容 username 和 user 字段
                username = conn_info.get('username') or conn_info.get('user')
                password = conn_info.get('password')
                host = conn_info.get('host')
                port = conn_info.get('port')
                database = conn_info.get('database')

                ds_config = {
                    'type': 'postgresql',
                    'host': host,
                    'port': port,
                    'database': database,
                    'user': username,
                    'password': password,
                    'connection_string': f"postgresql://{username}:{password}@{host}:{port}/{database}"
                }
                host_display = f"{host}:{port}/{database}"

            elif engine_type == 'mysql':
                username = conn_info.get('username') or conn_info.get('user')
                password = conn_info.get('password')
                host = conn_info.get('host')
                port = conn_info.get('port')
                database = conn_info.get('database')

                ds_config = {
                    'type': 'mysql',
                    'host': host,
                    'port': port,
                    'database': database,
                    'user': username,
                    'password': password,
                    'connection_string': f"mysql://{username}:{password}@{host}:{port}/{database}"
                }
                host_display = f"{host}:{port}/{database}"

            elif engine_type == 'minio':
                endpoint = conn_info.get('endpoint')
                access_key = conn_info.get('access_key')
                secret_key = conn_info.get('secret_key')
                bucket = conn_info.get('bucket')
                secure = conn_info.get('secure', False)

                ds_config = {
                    'type': 'minio',
                    'endpoint': endpoint,
                    'access_key': access_key,
                    'secret_key': secret_key,
                    'bucket': bucket,
                    'secure': secure
                }
                host_display = f"{endpoint}/{bucket}"

            else:
                # 其他类型，保持原始 connection_info
                ds_config = {
                    'type': engine_type,
                    **conn_info
                }
                host_display = conn_info.get('host') or conn_info.get('endpoint') or 'N/A'

            if ds_config:
                # 注入到全局命名空间
                var_name = f'ds_{engine_id}'
                globals()[var_name] = ds_config

                datasource_info.append({
                    'var_name': var_name,
                    'type': engine_type,
                    'name': engine_name,
                    'host': host_display
                })

                loaded_count += 1

        # 显示加载结果
        if loaded_count > 0:
            print(f"✅ 成功加载 {loaded_count} 个数据源")
            print("")
            print("┌──────────┬──────────────┬────────────────────────┬────────────────────────────┐")
            print("│ 变量名   │ 类型         │ 名称                   │ 地址                       │")
            print("├──────────┼──────────────┼────────────────────────┼────────────────────────────┤")
            for ds in datasource_info:
                var_name = ds['var_name'].ljust(8)
                ds_type = ds['type'].ljust(12)
                name = (ds['name'][:20] + '...') if len(ds['name']) > 20 else ds['name'].ljust(22)
                host = (ds['host'][:26] + '...') if len(ds['host']) > 26 else ds['host'].ljust(28)
                print(f"│ {var_name} │ {ds_type} │ {name} │ {host} │")
            print("└──────────┴──────────────┴────────────────────────┴────────────────────────────┘")
            print("")
            print("💡 使用示例：")
            print(f"   import psycopg2")
            print(f"   conn = psycopg2.connect({datasource_info[0]['var_name']}['connection_string'])")
            print("")

    except requests.exceptions.ConnectionError:
        print(f"❌ 无法连接到 ADDP API ({ADDP_API_BASE})")
        print("   提示：请确保 ADDP Backend 正在运行")
    except requests.exceptions.Timeout:
        print(f"❌ ADDP API 请求超时 ({ADDP_API_BASE})")
    except Exception as e:
        print(f"❌ 加载数据源时出错: {e}")
        import traceback
        traceback.print_exc()

# ============================================================================
# 自动执行
# ============================================================================

try:
    # 检查是否在 IPython 环境中
    ipython = get_ipython()
    if ipython is not None:
        load_addp_datasources()
except NameError:
    # 不在 IPython 环境中，跳过
    pass
