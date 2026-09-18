"""Read activated native ontology definitions through the owner API."""
from urllib.parse import quote

from .base import BaseClient


class OntologyClient(BaseClient):
    async def list_classes(self, ontology_id: str) -> dict:
        return await self.get(f"/api/v1/ontology/ontologies/{quote(ontology_id, safe='')}/semantic/classes")

    async def class_context(self, ontology_id: str, class_id: str, revision: int,
                            generation: str, activation_version: int) -> dict:
        return await self.get(
            f"/api/v1/ontology/ontologies/{quote(ontology_id, safe='')}/semantic/classes/{quote(class_id, safe='')}",
            params={"revision": revision, "generation": generation, "activation_version": activation_version},
        )
