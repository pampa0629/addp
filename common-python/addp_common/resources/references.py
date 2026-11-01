"""Consumption of exact owner references; no dotted-name compatibility parser."""
from typing import Any

from pydantic import ValidationError

from .models import ResourceQueryReference


def reference_parts(value: Any) -> tuple[str, ...] | None:
    try:
        reference = ResourceQueryReference.model_validate(value)
    except (ValidationError, ValueError, TypeError):
        return None
    return tuple(reference.parts)


def query_reference_parts(resource: dict[str, Any], language: str) -> tuple[str, ...] | None:
    references = resource.get('query_references')
    if not isinstance(references, dict) or language not in references:
        return None
    return reference_parts(references[language])
