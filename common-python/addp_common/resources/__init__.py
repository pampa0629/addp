"""ADDP resource facts shared by Agent adapters and Copilot domain services."""

from .models import ResourceFact, ResourceQueryReference
from .references import query_reference_parts, reference_parts

__all__ = ["ResourceFact", "ResourceQueryReference", "query_reference_parts", "reference_parts"]
