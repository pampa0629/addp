"""Fixed entrypoint inside one non-root, single-use execution container."""
import json
import os
import sys
from pathlib import Path


def run(mode):
    options = json.loads(Path("/job/job.json").read_text())
    os.chdir("/job/files")
    if mode == "batch":
        import papermill
        papermill.execute_notebook("input.ipynb", "output.ipynb", parameters=options["parameters"],
                                   kernel_name="python3", progress_bar=False)
        return
    if mode != "lab":
        raise ValueError("unknown Notebook execution mode")
    env = {"HOME": "/tmp", "PATH": "/usr/local/bin:/usr/bin:/bin", "PYTHONNOUSERSITE": "1",
           "PYTHONUTF8": "1", **options["session_environment"]}
    from urllib.parse import quote
    command = [sys.executable, "-m", "jupyterlab", "--no-browser", "--ServerApp.ip=0.0.0.0",
               "--ServerApp.port=8888", "--ServerApp.port_retries=0",
               "--ServerApp.base_url=" + options["base_path"], "--ServerApp.root_dir=/job/files",
               "--ServerApp.token=" + options["runtime_token"], "--ServerApp.allow_remote_access=True",
               "--ServerApp.trust_xheaders=True", "--ServerApp.quit_button=False",
               "--LabApp.default_url=/lab/tree/" + quote(options["name"])]
    os.execve(sys.executable, command, env)


if __name__ == "__main__":
    run(sys.argv[1])
