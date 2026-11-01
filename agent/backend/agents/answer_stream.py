"""Only an explicitly tool-free answer may become public model text."""

import asyncio
import anyio
import time
from contextlib import aclosing

from agents.events import text_event


async def stream_answer(model, messages):
    async with aclosing(model.astream(messages)) as chunks:
        async for chunk in chunks:
            if chunk.tool_calls or getattr(chunk, 'tool_call_chunks', None):
                raise RuntimeError('answer_phase_tool_call_forbidden')
            if not isinstance(chunk.content, str):
                raise RuntimeError('answer_phase_requires_text')
            if chunk.content:
                yield text_event(chunk.content)


async def coalesce_events(source, cancellation=None):
    """Bounded backpressure; first text immediately, then <=50ms or 4KiB batches."""
    queue = asyncio.Queue(maxsize=16)

    async def produce():
        try:
            async for event in source:
                await queue.put(event)
        except BaseException as exc:
            if isinstance(exc, asyncio.CancelledError):
                raise
            await queue.put(exc)
        finally:
            with anyio.CancelScope(shield=True):
                await source.aclose()

    producer = asyncio.create_task(produce())
    cancelled = asyncio.create_task(cancellation.wait()) if cancellation is not None else None
    pending, size, first, deadline = [], 0, True, 0.0
    get = None
    try:
        while True:
            if queue.empty() and producer.done():
                break
            timeout = max(0, deadline - time.monotonic()) if pending else None
            get = asyncio.create_task(queue.get())
            done, _ = await asyncio.wait([get, producer] + ([cancelled] if cancelled else []), timeout=timeout, return_when=asyncio.FIRST_COMPLETED)
            if cancelled is not None and cancelled in done:
                get.cancel()
                await asyncio.gather(get, return_exceptions=True)
                raise asyncio.CancelledError
            if get not in done:
                get.cancel()
                await asyncio.gather(get, return_exceptions=True)
                if pending:
                    yield text_event(''.join(pending))
                    pending, size = [], 0
                if producer.done() and queue.empty():
                    break
                continue
            event = get.result()
            if isinstance(event, BaseException) or event.kind != 'text':
                if pending:
                    yield text_event(''.join(pending))
                    pending, size = [], 0
                if isinstance(event, BaseException):
                    raise event
                yield event
                continue
            delta = str(event.payload.get('delta') or '')
            if not delta:
                continue
            if first:
                first = False
                yield text_event(delta)
                continue
            if not pending:
                deadline = time.monotonic() + .05
            pending.append(delta)
            size += len(delta.encode('utf-8'))
            if size >= 4096 or time.monotonic() >= deadline:
                yield text_event(''.join(pending))
                pending, size = [], 0
        if pending:
            yield text_event(''.join(pending))
        await producer
    finally:
        producer.cancel()
        if get is not None:
            get.cancel()
        if cancelled is not None:
            cancelled.cancel()
        with anyio.CancelScope(shield=True):
            await asyncio.gather(producer, *([get] if get is not None else []),
                *([cancelled] if cancelled is not None else []), return_exceptions=True)
