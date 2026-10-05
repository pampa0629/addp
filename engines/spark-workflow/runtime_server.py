"""The single native/container HTTP entry; Spark executors remain remote."""
import os
import threading
import time
from urllib.request import urlopen

from gunicorn.app.base import BaseApplication


def register_after_ready(port):
    # The hook runs before the worker request loop. Probe from a daemon thread,
    # so registration starts only after this process can serve HTTP requests.
    while True:
        try:
            with urlopen(f'http://127.0.0.1:{port}/health', timeout=2) as response:
                if response.status == 200:
                    break
        except OSError:
            pass
        time.sleep(1)
    from api_server import register_to_system_with_retry
    register_to_system_with_retry()


def post_worker_init(worker):
    threading.Thread(target=register_after_ready, args=(int(os.getenv('PORT', '8098')),),
                     name='spark-runtime-registration', daemon=True).start()


class RuntimeApplication(BaseApplication):
    def load_config(self):
        # Fixed process model: all requests share the in-worker registry. Do not
        # preload the application or create a JVM in the Gunicorn master.
        for key, value in {
            'bind': f"0.0.0.0:{int(os.getenv('PORT', '8098'))}",
            'workers': 1, 'worker_class': 'gthread', 'threads': 4,
            'timeout': 300, 'preload_app': False,
            'post_worker_init': post_worker_init,
        }.items():
            self.cfg.set(key, value)

    def load(self):
        from api_server import app
        return app


def run():
    RuntimeApplication().run()
