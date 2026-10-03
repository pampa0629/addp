-- Fixed Business fixtures only. Existing keys are never deleted or overwritten.
local samples = {
    {key='addp:sample:string', type='string', command={'SET', 'addp:sample:string', 'ADDP Redis sample'}},
    {key='addp:sample:counter', type='string', command={'SET', 'addp:sample:counter', '9007199254740993'}},
    {key='addp:sample:hash', type='hash', command={'HSET', 'addp:sample:hash', 'name', 'customer-0', 'order_id', '9007199254740993'}},
    {key='addp:sample:list', type='list', command={'RPUSH', 'addp:sample:list', 'one', 'two'}},
    {key='addp:sample:set', type='set', command={'SADD', 'addp:sample:set', 'north', 'south'}},
    {key='addp:sample:zset', type='zset', command={'ZADD', 'addp:sample:zset', '1', 'first', '2', 'second'}},
    {key='addp:sample:stream', type='stream', command={'XADD', 'addp:sample:stream', '1-0', 'event', 'created'}},
    {key='addp:sample:ttl', type='string', command={'SET', 'addp:sample:ttl', 'expires', 'EX', '3600'}},
    {key='addp:sample:binary' .. string.char(0,255), type='string',
     command={'SET', 'addp:sample:binary' .. string.char(0,255), string.char(0,255) .. 'ADDP'}}
}
-- Validate every pre-existing type before creating any missing samples.
for _, sample in ipairs(samples) do
    local actual = redis.call('TYPE', sample.key).ok
    if actual ~= 'none' and actual ~= sample.type then
        return redis.error_reply('Business Redis sample has an unexpected native type')
    end
end
local created = 0
for _, sample in ipairs(samples) do
    if redis.call('EXISTS', sample.key) == 0 then
        redis.call(unpack(sample.command))
        created = created + 1
    end
end
return created
