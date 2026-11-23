import asyncio

from addp_common.infra.schema import install
from services.schema import migrate

if __name__ == "__main__":
    asyncio.run(install("agent", migrate))
