"""
MinIO Contents Manager for Jupyter
让 Jupyter Lab 直接读写 MinIO 存储（无需本地文件系统）
支持多租户隔离
"""
import os
import io
import json
import mimetypes
import threading
from datetime import datetime
from tornado.web import HTTPError
from jupyter_server.services.contents.manager import ContentsManager
from jupyter_server.services.contents.checkpoints import Checkpoints
from minio import Minio
from minio.error import S3Error
import nbformat
from traitlets import Unicode, Instance
from config import config
from user_context import TenantContext, get_tenant_context_from_request, default_tenant_context


# 线程本地存储，用于存储当前请求的租户上下文
_thread_local = threading.local()


class MinIOCheckpoints(Checkpoints):
    """MinIO 检查点管理（暂不实现，返回空）"""

    def create_checkpoint(self, contents_mgr, path):
        """创建检查点"""
        return {
            'id': 'checkpoint-id',
            'last_modified': datetime.now().isoformat()
        }

    def restore_checkpoint(self, contents_mgr, checkpoint_id, path):
        """恢复检查点"""
        pass

    def rename_checkpoint(self, checkpoint_id, old_path, new_path):
        """重命名检查点"""
        pass

    def delete_checkpoint(self, checkpoint_id, path):
        """删除检查点"""
        pass

    def list_checkpoints(self, path):
        """列出检查点"""
        return []


class MinIOContentsManager(ContentsManager):
    """
    Jupyter ContentsManager 的 MinIO 实现
    直接读写 MinIO，无需本地文件系统
    """

    # 配置项（从 config.py 读取）
    minio_endpoint = Unicode(config.MINIO_ENDPOINT, help="MinIO endpoint").tag(config=True)
    minio_access_key = Unicode(config.MINIO_ACCESS_KEY, help="MinIO access key").tag(config=True)
    minio_secret_key = Unicode(config.MINIO_SECRET_KEY, help="MinIO secret key").tag(config=True)
    minio_bucket = Unicode(config.MINIO_BUCKET, help="MinIO bucket name").tag(config=True)
    minio_use_ssl = Unicode(str(config.MINIO_USE_SSL), help="Use SSL").tag(config=True)

    # MinIO 客户端实例
    minio_client = Instance(Minio, allow_none=True)

    def __init__(self, **kwargs):
        super().__init__(**kwargs)

        # 初始化 MinIO 客户端
        try:
            use_ssl = self.minio_use_ssl.lower() == 'true'
            self.minio_client = Minio(
                self.minio_endpoint,
                access_key=self.minio_access_key,
                secret_key=self.minio_secret_key,
                secure=use_ssl
            )

            # 检查 bucket 是否存在
            if not self.minio_client.bucket_exists(self.minio_bucket):
                self.log.warning(f"Bucket '{self.minio_bucket}' 不存在，尝试创建...")
                self.minio_client.make_bucket(self.minio_bucket)
                self.log.info(f"已创建 bucket: {self.minio_bucket}")

            self.log.info(f"MinIOContentsManager 已初始化")
            self.log.info(f"  Endpoint: {self.minio_endpoint}")
            self.log.info(f"  Bucket: {self.minio_bucket}")
            self.log.info(f"  SSL: {use_ssl}")

        except Exception as e:
            self.log.error(f"MinIO 初始化失败: {e}")
            raise

        # 使用自定义的 checkpoints 管理器
        self.checkpoints = MinIOCheckpoints()

    def _normalize_path(self, path):
        """标准化路径（移除开头的 /）"""
        if path.startswith('/'):
            path = path[1:]
        return path

    def _get_tenant_context(self):
        """
        获取当前租户上下文

        注意：Jupyter Server 的 ContentsManager 无法直接访问请求头。
        因此，租户上下文通过以下方式确定：

        1. 环境变量（适合多租户部署，每个租户独立实例）:
           - JUPYTER_TENANT_ID

        2. 默认配置（开发环境）:
           - DEFAULT_TENANT_ID (从 .env 读取)

        生产环境建议：
        - 为每个租户启动独立的 Jupyter Server 实例
        - 通过环境变量设置 JUPYTER_TENANT_ID
        - 使用 Gateway 或反向代理进行路由
        """
        # 优先从环境变量读取（支持多实例部署）
        tenant_id = os.getenv('JUPYTER_TENANT_ID')

        if tenant_id:
            return TenantContext(tenant_id=tenant_id)

        # 否则使用默认配置
        return default_tenant_context

    def _set_tenant_context_from_request(self, handler=None):
        """
        从请求处理器中提取租户上下文并存储到线程本地

        Args:
            handler: Tornado request handler
        """
        if handler and hasattr(handler, 'request'):
            request_headers = handler.request.headers
            tenant_ctx = get_tenant_context_from_request(request_headers)
            _thread_local.tenant_context = tenant_ctx
            self.log.debug(f"设置租户上下文: {tenant_ctx}")
        else:
            # 使用默认上下文
            _thread_local.tenant_context = default_tenant_context

    def _minio_path(self, path):
        """
        转换为 MinIO 对象路径（支持多租户隔离）

        路径格式: tenant_{tenant_id}/notebooks/{path}
        """
        path = self._normalize_path(path)

        # 获取当前租户上下文
        tenant_ctx = self._get_tenant_context()

        # 如果路径已经包含 tenant_ 前缀，直接使用（向后兼容）
        if path.startswith('tenant_'):
            return path

        # 否则，使用租户上下文构造完整路径
        base_path = tenant_ctx.base_path
        if path:
            return f"{base_path}/{path}"
        else:
            return base_path

    def _check_exists(self, path):
        """检查对象是否存在"""
        try:
            minio_path = self._minio_path(path)
            self.minio_client.stat_object(self.minio_bucket, minio_path)
            return True
        except S3Error as e:
            if e.code == 'NoSuchKey':
                return False
            raise

    def dir_exists(self, path):
        """检查目录是否存在（MinIO 中目录总是存在）"""
        return True

    def file_exists(self, path):
        """检查文件是否存在"""
        if path == '':
            return False
        return self._check_exists(path)

    def exists(self, path):
        """检查路径是否存在"""
        return self.dir_exists(path) or self.file_exists(path)

    def get(self, path, content=True, type=None, format=None):
        """
        获取文件或目录内容

        Args:
            path: 文件路径
            content: 是否返回内容
            type: 类型（notebook, file, directory）
            format: 格式（text, base64, json）
        """
        path = self._normalize_path(path)

        # 根目录，返回目录列表
        if path == '' or path == '.':
            model = {
                'name': '',
                'path': '',
                'type': 'directory',
                'mimetype': None,
                'writable': True,
                'last_modified': datetime.now(),
                'created': datetime.now(),
                'format': None,
                'content': None,
            }
            if content:
                model['content'] = self._list_directory('')
                model['format'] = 'json'
            return model

        # 检查是否为目录路径
        if path.endswith('/') or not path.split('/')[-1].count('.'):
            # 目录
            model = {
                'name': path.split('/')[-1] or '',
                'path': path,
                'type': 'directory',
                'mimetype': None,
                'writable': True,
                'last_modified': datetime.now(),
                'created': datetime.now(),
                'format': None,
                'content': None,
            }
            if content:
                model['content'] = self._list_directory(path)
                model['format'] = 'json'
            return model

        # 文件
        minio_path = self._minio_path(path)

        try:
            # 获取对象元数据
            stat = self.minio_client.stat_object(self.minio_bucket, minio_path)

            # 判断文件类型
            if path.endswith('.ipynb'):
                file_type = 'notebook'
            else:
                file_type = 'file'

            model = {
                'name': path.split('/')[-1],
                'path': path,
                'type': file_type,
                'mimetype': stat.content_type or mimetypes.guess_type(path)[0],
                'writable': True,
                'last_modified': stat.last_modified,
                'created': stat.last_modified,
                'size': stat.size,
                'format': None,
                'content': None,
            }

            if content:
                # 读取文件内容
                response = self.minio_client.get_object(self.minio_bucket, minio_path)
                data = response.read()
                response.close()
                response.release_conn()

                if file_type == 'notebook':
                    # Notebook 文件，解析为 JSON
                    try:
                        nb = nbformat.reads(data.decode('utf-8'), as_version=4)
                        model['content'] = nb
                        model['format'] = 'json'
                    except Exception as e:
                        self.log.error(f"解析 Notebook 失败: {e}")
                        raise HTTPError(400, f"解析 Notebook 失败: {e}")
                else:
                    # 普通文件，返回文本或 base64
                    try:
                        model['content'] = data.decode('utf-8')
                        model['format'] = 'text'
                    except UnicodeDecodeError:
                        import base64
                        model['content'] = base64.b64encode(data).decode('ascii')
                        model['format'] = 'base64'

            return model

        except S3Error as e:
            if e.code == 'NoSuchKey':
                raise HTTPError(404, f"文件不存在: {path}")
            else:
                self.log.error(f"MinIO 错误: {e}")
                raise HTTPError(500, f"MinIO 错误: {e}")

    def _list_directory(self, path):
        """列出目录内容"""
        path = self._normalize_path(path)
        minio_prefix = self._minio_path(path)
        if minio_prefix and not minio_prefix.endswith('/'):
            minio_prefix += '/'

        contents = []
        seen_dirs = set()

        try:
            # 列出所有对象
            objects = self.minio_client.list_objects(
                self.minio_bucket,
                prefix=minio_prefix,
                recursive=False
            )

            for obj in objects:
                # 移除前缀，获取相对路径
                relative_path = obj.object_name[len(minio_prefix):]

                if not relative_path:
                    continue

                # 判断是文件还是目录
                if obj.is_dir or relative_path.endswith('/'):
                    # 目录
                    dir_name = relative_path.rstrip('/')
                    if dir_name and dir_name not in seen_dirs:
                        seen_dirs.add(dir_name)
                        contents.append({
                            'name': dir_name,
                            'path': f"{path}/{dir_name}" if path else dir_name,
                            'type': 'directory',
                            'last_modified': datetime.now(),
                            'created': datetime.now(),
                        })
                else:
                    # 文件
                    file_type = 'notebook' if relative_path.endswith('.ipynb') else 'file'
                    contents.append({
                        'name': relative_path,
                        'path': f"{path}/{relative_path}" if path else relative_path,
                        'type': file_type,
                        'last_modified': obj.last_modified,
                        'created': obj.last_modified,
                        'size': obj.size,
                    })

            return contents

        except S3Error as e:
            self.log.error(f"列出目录失败: {e}")
            return []

    def save(self, model, path):
        """
        保存文件或目录

        Args:
            model: 文件模型（包含 content）
            path: 保存路径
        """
        path = self._normalize_path(path)

        if model['type'] == 'directory':
            # 创建目录（MinIO 中无需创建目录）
            return self.get(path, content=False)

        # 保存文件
        minio_path = self._minio_path(path)

        try:
            if model['type'] == 'notebook':
                # Notebook 文件
                # 如果 content 是字典，先转换为 nbformat 对象
                content = model['content']
                if isinstance(content, dict):
                    # 从字典创建 nbformat 对象
                    content = nbformat.from_dict(content)

                # 序列化为 JSON 字符串
                content_str = nbformat.writes(content, version=4)
                data = content_str.encode('utf-8')
                content_type = 'application/x-ipynb+json'
            elif model.get('format') == 'base64':
                # Base64 编码的二进制文件
                import base64
                data = base64.b64decode(model['content'])
                content_type = model.get('mimetype', 'application/octet-stream')
            else:
                # 文本文件
                data = model['content'].encode('utf-8')
                content_type = model.get('mimetype', 'text/plain')

            # 上传到 MinIO
            self.minio_client.put_object(
                self.minio_bucket,
                minio_path,
                io.BytesIO(data),
                length=len(data),
                content_type=content_type
            )

            self.log.info(f"已保存文件到 MinIO: {minio_path} ({len(data)} bytes)")

            # 返回保存后的模型
            return self.get(path, content=False)

        except Exception as e:
            self.log.error(f"保存文件失败: {e}")
            raise HTTPError(500, f"保存文件失败: {e}")

    def delete_file(self, path):
        """删除文件"""
        path = self._normalize_path(path)
        minio_path = self._minio_path(path)

        try:
            self.minio_client.remove_object(self.minio_bucket, minio_path)
            self.log.info(f"已删除文件: {minio_path}")
        except S3Error as e:
            if e.code == 'NoSuchKey':
                raise HTTPError(404, f"文件不存在: {path}")
            else:
                self.log.error(f"删除文件失败: {e}")
                raise HTTPError(500, f"删除文件失败: {e}")

    def rename_file(self, old_path, new_path):
        """重命名文件（复制 + 删除）"""
        old_path = self._normalize_path(old_path)
        new_path = self._normalize_path(new_path)

        old_minio_path = self._minio_path(old_path)
        new_minio_path = self._minio_path(new_path)

        try:
            # 复制文件
            self.minio_client.copy_object(
                self.minio_bucket,
                new_minio_path,
                f"{self.minio_bucket}/{old_minio_path}"
            )

            # 删除原文件
            self.minio_client.remove_object(self.minio_bucket, old_minio_path)

            self.log.info(f"已重命名文件: {old_minio_path} -> {new_minio_path}")

        except S3Error as e:
            self.log.error(f"重命名文件失败: {e}")
            raise HTTPError(500, f"重命名文件失败: {e}")

    def is_hidden(self, path):
        """判断是否为隐藏文件"""
        path = self._normalize_path(path)
        return path.startswith('.') or any(part.startswith('.') for part in path.split('/'))
