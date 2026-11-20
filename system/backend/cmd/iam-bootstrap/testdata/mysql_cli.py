"""Drive only the disposable MySQL install gate's three test identities through the real TTY."""
import base64
import errno
import hashlib
import hmac
import json
import os
from pathlib import Path
import pty
import re
import secrets
import select
import struct
import subprocess
import sys
import time


class Terminal:
    def __init__(self, binary, *args):
        self.fd, slave = pty.openpty()
        keys = ("PATH", "TZ", "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT",
                "ADDP_MYSQL_SSLMODE", "ADDP_MYSQL_BOOTSTRAP_USER", "ADDP_MYSQL_BOOTSTRAP_PASSWORD",
                "IAM_MFA_ENCRYPTION_KEY")
        env = {key: os.environ[key] for key in keys if key in os.environ}
        env["ENV"] = "production"
        self.process = subprocess.Popen([binary, *args], cwd=binary.parent, env=env,
                                        stdin=slave, stdout=slave, stderr=slave)
        os.close(slave)
        self.buffer = b""

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.process.kill()
        self.process.wait()
        os.close(self.fd)

    def expect(self, pattern, stage):
        deadline = time.monotonic() + 25
        while not (match := re.search(pattern, self.buffer)):
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.fd], [], [], remaining)[0]:
                raise RuntimeError(f"timed out during {stage}")
            try:
                block = os.read(self.fd, 8192)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                block = b""
            if not block:
                # The terminal may contain credentials. Report only a database error number.
                code = re.search(rb"Error ([0-9]{4})", self.buffer)
                detail = code[1].decode() if code else "none"
                raise RuntimeError(f"CLI exited during {stage}, status={self.process.wait()}, MySQL error={detail}")
            self.buffer += block
        self.buffer = self.buffer[match.end():]
        return match

    def send(self, value):
        os.write(self.fd, value.encode() + b"\n")

    def finish(self):
        if self.process.wait(timeout=20):
            raise RuntimeError("CLI returned an unsuccessful exit status")


def totp(key, counter):
    data = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = data[-1] & 15
    digits = (struct.unpack(">I", data[offset:offset + 4])[0] & 0x7fffffff) % 1000000
    return f"{digits:06d}"


def main(work):
    binary = work / "iam-bootstrap"
    roles = ["platform.system_administrator", "platform.security_administrator", "platform.audit_administrator"]
    manifest = work / "bootstrap-manifest.json"
    manifest.write_text(json.dumps({"administrators": [
        {"role_key": role, "username": f"gate-{i}", "display_name": role} for i, role in enumerate(roles)
    ]}))
    with Terminal(binary, "prepare") as terminal:
        secret = terminal.expect(rb"(addp_bs_[A-Za-z0-9_-]+)\r?\n", "preparation")[1].decode()
        terminal.finish()
    with Terminal(binary, "apply", "--manifest", manifest) as terminal:
        terminal.expect(b"Bootstrap Secret: ", "bootstrap secret prompt")
        terminal.send(secret)
        for role in roles:
            password = secrets.token_urlsafe(24)
            terminal.expect("输入独立密码（至少 14 字符）: ".encode(), role)
            terminal.send(password)
            terminal.expect("再次输入密码: ".encode(), role)
            terminal.send(password)
            encoded = terminal.expect(rb"TOTP Secret: ([A-Z2-7]+=*)\r?\n", role)[1]
            key = base64.b32decode(encoded + b"=" * (-len(encoded) % 8))
            terminal.expect("输入当前 TOTP 验证码: ".encode(), role)
            counter = int(time.time()) // 30
            terminal.send(totp(key, counter))
            terminal.expect("等待验证码变化后输入下一个 TOTP 验证码: ".encode(), role)
            time.sleep(max(0, (counter + 1) * 30 + 0.2 - time.time()))
            terminal.send(totp(key, counter + 1))
        terminal.expect("IAM Bootstrap 已永久完成".encode(), "transaction completion")
        terminal.finish()

    with Terminal(binary, "prepare") as terminal:
        terminal.expect(b"IAM bootstrap was already prepared", "permanent closure")
        if terminal.process.wait(timeout=10) != 1:
            raise RuntimeError("completed bootstrap accepted a new preparation")


if __name__ == "__main__":
    main(Path(sys.argv[1]))
