"""
ADDP Data Source Loader - IPython Magic Extension

参考：
- AWS SageMaker Magic Commands: https://docs.aws.amazon.com/sagemaker/latest/dg/nbi-add-external.html
- IPython Magics: https://ipython.readthedocs.io/en/stable/config/custommagics.html

用法：
    在 Notebook 第一个 Cell 中执行：

    %load_ext addp_datasources
    %load_addp_datasources

部署位置: engines/jupyter/addp_datasources.py
"""

from IPython.core.magic import Magics, line_magic, magics_class
from IPython.core.display import display, HTML
import requests
import os
import json

@magics_class
class ADDPDataSourceMagics(Magics):
    """ADDP 数据源加载 Magic Commands"""

    @line_magic
    def load_addp_datasources(self, line):
        """
        加载 ADDP 数据源连接配置到当前 Notebook 环境

        用法：
            %load_addp_datasources
            %load_addp_datasources http://localhost:8085 <your_token>

        参数：
            api_base: ADDP API 基础 URL（可选，默认从环境变量读取）
            token: JWT token（可选，从环境变量或参数读取）
        """

        # 1. 解析参数
        args = line.strip().split()
        api_base = args[0] if len(args) > 0 else os.getenv('ADDP_API_BASE', 'http://localhost:8085')
        token = args[1] if len(args) > 1 else os.getenv('ADDP_TOKEN')

        if not token:
            display(HTML("""
            <div style="padding: 10px; background: #fff3cd; border-left: 4px solid #ffc107; margin: 10px 0;">
                <strong>⚠️  需要提供 ADDP Token</strong><br>
                用法：<code>%load_addp_datasources http://localhost:8085 &lt;your_token&gt;</code><br>
                或设置环境变量：<code>ADDP_TOKEN=&lt;your_token&gt;</code>
            </div>
            """))
            return

        # 2. 调用 ADDP API 获取数据源
        print(f"🔄 正在从 ADDP 加载数据源...")
        print(f"   API: {api_base}/api/develop/engines")

        try:
            headers = {'Authorization': f'Bearer {token}'}
            response = requests.get(
                f'{api_base}/api/develop/engines',
                headers=headers,
                timeout=10
            )

            if response.status_code != 200:
                display(HTML(f"""
                <div style="padding: 10px; background: #f8d7da; border-left: 4px solid #dc3545; margin: 10px 0;">
                    <strong>❌ 加载失败</strong><br>
                    HTTP {response.status_code}: {response.text[:200]}
                </div>
                """))
                return

            engines = response.json()

            if not engines:
                display(HTML("""
                <div style="padding: 10px; background: #d1ecf1; border-left: 4px solid #0c5460; margin: 10px 0;">
                    <strong>ℹ️  暂无可用数据源</strong><br>
                    请先在 System 模块中注册引擎。
                </div>
                """))
                return

            # 3. 注入数据源变量到全局命名空间
            loaded_sources = []

            for engine in engines:
                engine_id = engine.get('id')
                engine_type = engine.get('engine_type')
                engine_name = engine.get('display_name') or engine.get('name')
                conn_info = engine.get('connection_info', {})

                # 构造连接配置
                if engine_type == 'postgresql':
                    username = conn_info.get('username') or conn_info.get('user')
                    ds_config = {
                        'type': 'postgresql',
                        'host': conn_info.get('host'),
                        'port': conn_info.get('port'),
                        'database': conn_info.get('database'),
                        'user': username,
                        'password': conn_info.get('password'),
                        'connection_string': f"postgresql://{username}:{conn_info.get('password')}@{conn_info.get('host')}:{conn_info.get('port')}/{conn_info.get('database')}"
                    }
                    host_display = f"{conn_info.get('host')}:{conn_info.get('port')}/{conn_info.get('database')}"

                elif engine_type == 'mysql':
                    username = conn_info.get('username') or conn_info.get('user')
                    ds_config = {
                        'type': 'mysql',
                        'host': conn_info.get('host'),
                        'port': conn_info.get('port'),
                        'database': conn_info.get('database'),
                        'user': username,
                        'password': conn_info.get('password'),
                        'connection_string': f"mysql://{username}:{conn_info.get('password')}@{conn_info.get('host')}:{conn_info.get('port')}/{conn_info.get('database')}"
                    }
                    host_display = f"{conn_info.get('host')}:{conn_info.get('port')}/{conn_info.get('database')}"

                elif engine_type == 'minio':
                    ds_config = {
                        'type': 'minio',
                        'endpoint': conn_info.get('endpoint'),
                        'access_key': conn_info.get('access_key'),
                        'secret_key': conn_info.get('secret_key'),
                        'bucket': conn_info.get('bucket'),
                        'secure': conn_info.get('secure', False)
                    }
                    host_display = f"{conn_info.get('endpoint')}/{conn_info.get('bucket')}"

                else:
                    # 其他类型
                    ds_config = {
                        'type': engine_type,
                        **conn_info
                    }
                    host_display = conn_info.get('host') or conn_info.get('endpoint') or 'N/A'

                # 注入到全局命名空间
                var_name = f'ds_{engine_id}'
                self.shell.user_ns[var_name] = ds_config

                loaded_sources.append({
                    'var_name': var_name,
                    'type': engine_type,
                    'name': engine_name,
                    'host': host_display
                })

            # 4. 显示加载成功信息
            rows_html = '\n'.join([
                f"""
                <tr>
                    <td><code style="background: #e9ecef; padding: 2px 6px; border-radius: 3px;">{s['var_name']}</code></td>
                    <td><span style="background: #d1ecf1; padding: 2px 8px; border-radius: 3px; font-size: 12px;">{s['type']}</span></td>
                    <td>{s['name']}</td>
                    <td style="color: #6c757d; font-size: 12px;">{s['host']}</td>
                </tr>
                """
                for s in loaded_sources
            ])

            display(HTML(f"""
            <div style="padding: 15px; background: #d4edda; border-left: 4px solid #28a745; margin: 10px 0; border-radius: 4px;">
                <strong>✅ 数据源加载成功</strong><br>
                <table style="margin-top: 10px; border-collapse: collapse; width: 100%; font-size: 14px;">
                    <thead>
                        <tr style="background: #c3e6cb;">
                            <th style="padding: 8px; text-align: left;">变量名</th>
                            <th style="padding: 8px; text-align: left;">类型</th>
                            <th style="padding: 8px; text-align: left;">名称</th>
                            <th style="padding: 8px; text-align: left;">地址</th>
                        </tr>
                    </thead>
                    <tbody>
                        {rows_html}
                    </tbody>
                </table>
                <div style="margin-top: 12px; padding: 10px; background: white; border-radius: 3px; font-size: 13px;">
                    <strong>💡 使用示例：</strong><br>
                    <code style="background: #f8f9fa; padding: 2px 6px; border-radius: 3px;">import psycopg2</code><br>
                    <code style="background: #f8f9fa; padding: 2px 6px; border-radius: 3px;">conn = psycopg2.connect(ds_{loaded_sources[0]['var_name'].split('_')[1]}['connection_string'])</code>
                </div>
            </div>
            """))

        except requests.exceptions.RequestException as e:
            display(HTML(f"""
            <div style="padding: 10px; background: #f8d7da; border-left: 4px solid #dc3545; margin: 10px 0;">
                <strong>❌ 网络错误</strong><br>
                {str(e)}
            </div>
            """))
        except Exception as e:
            display(HTML(f"""
            <div style="padding: 10px; background: #f8d7da; border-left: 4px solid #dc3545; margin: 10px 0;">
                <strong>❌ 加载失败</strong><br>
                {str(e)}
            </div>
            """))
            import traceback
            traceback.print_exc()

def load_ipython_extension(ipython):
    """
    IPython Extension 入口点

    用法：
        %load_ext addp_datasources
    """
    ipython.register_magics(ADDPDataSourceMagics)
    print("✅ ADDP Data Source Magic 已加载")
    print("   使用方法：%load_addp_datasources")
