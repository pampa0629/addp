"""
Spatial operators and workflow engine package
"""

__version__ = "0.1.0"

from .task_ref import TaskRef, TaskOutput
from .workflow_engine import SpatialWorkflowEngine

__all__ = [
    "TaskRef",
    "TaskOutput",
    "SpatialWorkflowEngine",
]
