import asyncio
from uuid import UUID


class AgentRunCancellationRegistry:
    """Process-local cancellation signals for active Agent Runtime invocations."""

    def __init__(self):
        self._signals: dict[UUID, asyncio.Event] = {}

    def activate(self, agent_run_id: UUID) -> asyncio.Event:
        signal = asyncio.Event()
        self._signals[agent_run_id] = signal
        return signal

    def release(self, agent_run_id: UUID, signal: asyncio.Event) -> None:
        if self._signals.get(agent_run_id) is signal:
            self._signals.pop(agent_run_id, None)

    def cancel(self, agent_run_id: UUID) -> bool:
        signal = self._signals.get(agent_run_id)
        if signal is None:
            return False
        signal.set()
        return True


run_cancellation_registry = AgentRunCancellationRegistry()
