"""Business Compose owns the one pinned StarRocks image identity."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]

def image() -> str:
    matches = re.findall(r'^  starrocks:\n    image: (starrocks/allin1-ubuntu:[^\s]+@sha256:[a-f0-9]{64})$',
                         (ROOT / 'business/docker-compose.yml').read_text(), re.MULTILINE)
    if len(matches) != 1:
        raise RuntimeError('exactly one pinned Business StarRocks image required')
    return matches[0]

if __name__ == '__main__':
    print(image())
