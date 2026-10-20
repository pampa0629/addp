"""The native development and product container HTTP entry; Spark executors remain remote."""
import os
import threading

from gunicorn.app.base import BaseApplication


def register_runtime(port):
    from api_server import register_to_system_with_retry
    from addp_common.client.runtime_registration import register_after_listener

    register_after_listener(register_to_system_with_retry, port)


def post_worker_init(worker):
    threading.Thread(target=register_runtime, args=(int(os.getenv('PORT', '8098')),),
                     name='spark-runtime-registration', daemon=True).start()


class RuntimeApplication(BaseApplication):
    def load_config(self):
        # Fixed process model: all requests share the in-worker registry. Do not
        # preload the application or create a JVM in the Gunicorn master.
        for key, value in {
            'bind': f"{os.getenv('WORKFLOW_BIND_HOST', '0.0.0.0')}:{int(os.getenv('PORT', '8098'))}",
            'workers': 1, 'worker_class': 'gthread', 'threads': 4,
            'timeout': 300, 'preload_app': False,
            'accesslog': '-',
            'access_log_format': '%(t)s %(m)s %(U)s %(s)s %(L)s',
            'post_worker_init': post_worker_init,
        }.items():
            self.cfg.set(key, value)

    def load(self):
        from api_server import app
        return app


def run():
    RuntimeApplication().run()
