import asyncio
import json

import httpx
import pytest
from langchain_core.messages import HumanMessage

from addp_common.client.inference import InferenceClient, InferenceError, Message
from addp_common.inference_langchain import InferenceChatModel


class TokenSource:
    async def token(self, tenant_id):
        return 'fixture-token'

    def invalidate(self, *args):
        raise AssertionError('no transparent retry expected')


def event(kind, sequence, **values):
    return ('data: ' + json.dumps({'schema_version': 'addp.inference-stream/v1',
        'type': kind, 'sequence': sequence, **values}, ensure_ascii=False) + '\n\n').encode()


def client_for(body, calls, streaming=True):
    async def handler(request):
        calls.append(request)
        if request.url.path.endswith('/engine-descriptors'):
            return httpx.Response(200, json={'data': [{'id': 9, 'engine_type': 'inference_runtime',
                'is_builtin': True, 'lifecycle_state': 'active', 'capabilities': {
                    'schema_version': 'engine.capabilities/v1', 'compute': {'inference': {
                        'supported': True, 'runtime_api': 'addp.inference/v1', 'streaming': streaming}}},
                'runtime_endpoint': {'protocol': 'http', 'host': 'inference', 'port': 8191}}], 'total': 1})
        return httpx.Response(200, headers={'content-type': 'text/event-stream'}, stream=body)
    return InferenceClient('http://system', TokenSource(), transport=httpx.MockTransport(handler))


@pytest.mark.asyncio
async def test_real_transport_and_model_deliver_text_before_end():
    release, ended, closed = asyncio.Event(), asyncio.Event(), asyncio.Event()

    class Body(httpx.AsyncByteStream):
        async def __aiter__(self):
            raw = event('start', 1, deployment_id='deployment', profile_version=4) + event('text_delta', 2, delta='你🙂')
            for byte in raw:  # Includes split UTF-8 and SSE delimiters.
                yield bytes([byte])
            await release.wait()
            yield event('text_delta', 3, delta='好')
            yield event('completed', 4, usage={'input_tokens': 2, 'output_tokens': 3, 'total_tokens': 5})
            ended.set()

        async def aclose(self):
            closed.set()

    calls = []
    client = client_for(Body(), calls)
    model = InferenceChatModel(client=client, tenant_id=7, model_profile_id='profile')
    stream = model.astream([HumanMessage(content='hello')])
    first = await asyncio.wait_for(anext(stream), 2)
    if not first.content:
        first = await asyncio.wait_for(anext(stream), 2)
    assert first.content == '你🙂'
    assert not ended.is_set() and not release.is_set()
    request = json.loads(calls[-1].content)
    assert request['stream'] is True and 'tools' not in request
    release.set()
    chunks = [chunk async for chunk in stream]
    assert first.content + ''.join(chunk.content for chunk in chunks) == '你🙂好'
    assert any(chunk.usage_metadata == {'input_tokens': 2, 'output_tokens': 3, 'total_tokens': 5} for chunk in chunks)
    assert closed.is_set()
    await client.close()


@pytest.mark.asyncio
async def test_closing_model_stream_closes_blocked_http_body_without_retry():
    closed = asyncio.Event()

    class Body(httpx.AsyncByteStream):
        async def __aiter__(self):
            yield event('start', 1, deployment_id='d', profile_version=1)
            yield event('text_delta', 2, delta='partial')
            await asyncio.Future()

        async def aclose(self):
            closed.set()

    calls = []
    client = client_for(Body(), calls)
    model = InferenceChatModel(client=client, tenant_id=7, model_profile_id='p')
    stream = model.astream([HumanMessage(content='hello')])
    while not (await anext(stream)).content:
        pass
    await stream.aclose()
    await asyncio.wait_for(closed.wait(), 1)
    assert len([r for r in calls if r.method == 'POST']) == 1
    await client.close()


@pytest.mark.asyncio
@pytest.mark.parametrize('frames', [
    [event('text_delta', 1, delta='before start')],
    [event('start', 1, deployment_id='d', profile_version=1), event('text_delta', 3, delta='gap')],
    [event('start', 1, deployment_id='d', profile_version=1), event('text_delta', 2, delta='partial')],
    [event('start', 1, deployment_id='d', profile_version=1), event('completed', 2, usage={})],
    [event('start', 1, deployment_id='d', profile_version=1), event('text_delta', 2, delta='ok'), event('completed', 3, usage={}), event('text_delta', 4, delta='late')],
    [b'data: {"private_reasoning":"secret"}\n\n'],
])
async def test_incomplete_or_invalid_protocol_never_completes_successfully(frames):
    class Body(httpx.AsyncByteStream):
        async def __aiter__(self):
            for frame in frames:
                yield frame
    calls = []
    client = client_for(Body(), calls)
    with pytest.raises(InferenceError):
        _ = [chunk async for chunk in client.chat_stream(tenant_id=7, model_profile_id='p', messages=[Message(role='user', content='hi')])]
    assert len([r for r in calls if r.method == 'POST']) == 1
    await client.close()


@pytest.mark.asyncio
async def test_unsupported_runtime_is_not_retried_as_json():
    calls = []
    client = client_for(None, calls, streaming=False)
    with pytest.raises(InferenceError, match='inference_operation_unsupported'):
        _ = [chunk async for chunk in client.chat_stream(tenant_id=7, model_profile_id='p', messages=[Message(role='user', content='hi')])]
    assert not any(r.method == 'POST' for r in calls)
    await client.close()
