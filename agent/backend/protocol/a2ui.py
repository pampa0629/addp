from typing import Any


CATALOG_ID = "addp.catalog/v1"
A2UI_VERSION = "v0.9"


def workflow_dag_surface(surface_id: str, workflow: dict[str, Any]) -> list[dict[str, Any]]:
    return [
        {
            "version": A2UI_VERSION,
            "createSurface": {"surfaceId": surface_id, "catalogId": CATALOG_ID},
        },
        {
            "version": A2UI_VERSION,
            "updateComponents": {
                "surfaceId": surface_id,
                "components": [
                    {
                        "id": "root",
                        "component": "WorkflowDag",
                        "workflow": workflow,
                        "height": 400,
                    }
                ],
            },
        },
    ]


def clarification_surface(
    surface_id: str,
    *,
    interaction_id: str,
    prompt: str,
    options: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    return [
        {
            "version": A2UI_VERSION,
            "createSurface": {"surfaceId": surface_id, "catalogId": CATALOG_ID},
        },
        {
            "version": A2UI_VERSION,
            "updateComponents": {
                "surfaceId": surface_id,
                "components": [
                    {
                        "id": "root",
                        "component": "ClarificationChoice",
                        "interactionId": interaction_id,
                        "prompt": prompt,
                        "options": options,
                    }
                ],
            },
        },
    ]
