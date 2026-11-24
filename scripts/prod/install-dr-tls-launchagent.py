#!/usr/bin/env python3
"""Install the six-hour standby certificate check for the macOS Colima account."""
import argparse
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys

p = argparse.ArgumentParser(description=__doc__)
p.add_argument("--kit", required=True, help="absolute recovery kit path inside Colima")
p.add_argument("--colima-profile", required=True)
p.add_argument("--hostname", required=True)
p.add_argument("--ssh-config", required=True, type=Path)
p.add_argument("--issuer", required=True, help="restricted SSH alias")
args = p.parse_args()
os.umask(0o077)
conf = args.ssh_config.resolve(strict=True)
runner = Path(__file__).with_name("k3s-dr-tls.py").resolve(strict=True)
colima = shutil.which("colima")
if not colima or not Path(args.kit).is_absolute():
    p.error("Colima and an absolute Linux kit path are required")
label = "com.addp.validation-dr-tls"
domain = "gui/" + str(os.getuid())
path = Path.home() / "Library/LaunchAgents" / (label + ".plist")
data = {
    "Label": label, "StartInterval": 21600, "RunAtLoad": True,
    "ProgramArguments": [sys.executable, str(runner), "renew", "--kit", args.kit, "--hostname", args.hostname,
                         "--ssh-config", str(conf), "--issuer", args.issuer, "--colima-profile", args.colima_profile,
                         "--alert-file", str(conf.parent / "ALERT.json")],
    "EnvironmentVariables": {"PATH": str(Path(colima).parent) + ":/usr/bin:/bin:/usr/sbin:/sbin"},
    "StandardOutPath": str(conf.parent / "renew.log"), "StandardErrorPath": str(conf.parent / "renew.err"),
}
path.parent.mkdir(exist_ok=True)
new = path.with_suffix(".new")
new.write_bytes(plistlib.dumps(data))
subprocess.run(["plutil", "-lint", new], check=True)
new.replace(path)
if subprocess.run(["launchctl", "print", domain + "/" + label], capture_output=True).returncode == 0:
    subprocess.run(["launchctl", "bootout", domain + "/" + label], check=True)
subprocess.run(["launchctl", "bootstrap", domain, path], check=True)
