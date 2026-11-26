package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var errCheckpointAdvanced = errors.New("search checkpoint advanced")

const checkpointPrefix = "manager:search:{continuation}:v1:"

// Expiry and memory reservations use the same atomic operation as cursor
// publication. A competing reader either wins the same input or replays it.
var saveCheckpoint = redis.NewScript(`
local prefix, id, prev, nextid, version, expires, charge = ARGV[1], ARGV[2], ARGV[3], ARGV[4], tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7])
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
if expires<=now then return -2 end
local state, expiry, sizes = KEYS[1],KEYS[2],KEYS[3]
local actual=redis.call('HGET',state,'v')
if version>0 and (not actual or tonumber(actual)~=version) then return 0 end
local expired=redis.call('ZRANGEBYSCORE',expiry,'-inf',now)
for _,old in ipairs(expired) do
 redis.call('ZREM',expiry,old)
 redis.call('HDEL',sizes,old)
end
local amounts=redis.call('HGETALL',sizes)
local total=charge
for n=1,#amounts,2 do if amounts[n]~=id then total=total+tonumber(amounts[n+1]) end end
if (version==0 and #amounts/2>=tonumber(ARGV[9])) or total>tonumber(ARGV[10]) or charge>tonumber(ARGV[11]) then return -1 end
local old=redis.call('HGET',state,'p')
if old and old~='' and old~=prev then redis.call('DEL',prefix..'c:'..old) end
redis.call('HSET',state,'v',version+1,'p',prev,'n',nextid,'d',ARGV[8],'e',expires,'s',ARGV[12])
redis.call('PEXPIREAT',state,expires)
for _,alias in ipairs({prev,nextid}) do
 if alias~='' then redis.call('SET',prefix..'c:'..alias,id,'PXAT',expires) end
end
redis.call('ZADD',expiry,expires,id)
redis.call('HSET',sizes,id,charge)
redis.call('PEXPIREAT',expiry,now+tonumber(ARGV[13]))
redis.call('PEXPIREAT',sizes,now+tonumber(ARGV[13]))
return 1`)

var readCheckpoint = redis.NewScript(`
local id=redis.call('GET',KEYS[1])
if not id then return nil end
local meta=redis.call('HMGET',ARGV[1]..'s:'..id,'v','e','s','p','n')
if not meta[1] or meta[3]~=ARGV[3] or (meta[4]~=ARGV[2] and meta[5]~=ARGV[2]) then return nil end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
return {id,meta[1],meta[2],redis.call('HGET',ARGV[1]..'s:'..id,'d'),tostring(tonumber(meta[2])-now)}`)

var discardCheckpoint = redis.NewScript(`
if redis.call('HGET',KEYS[1],'v')~=ARGV[1] then return 0 end
for _,alias in ipairs(redis.call('HMGET',KEYS[1],'p','n')) do
 if alias and alias~='' then redis.call('DEL',ARGV[2]..'c:'..alias) end
end
redis.call('DEL',KEYS[1]); redis.call('ZREM',KEYS[2],ARGV[3]); redis.call('HDEL',KEYS[3],ARGV[3]);return 1`)

type checkpointRef struct {
	id        string
	version   int64
	expires   time.Time
	remaining time.Duration
}

func checkpointKeys(id string) []string {
	return []string{checkpointPrefix + "s:" + id, checkpointPrefix + "expiry", checkpointPrefix + "sizes"}
}

func loadSearchCheckpoint(ctx context.Context, client *redis.Client, cursor uuid.UUID, subject [32]byte) (checkpointRef, string, error) {
	key := cursor.String()
	fields, err := readCheckpoint.Run(ctx, client, []string{checkpointPrefix + "c:" + key}, checkpointPrefix, key, subject[:]).StringSlice()
	if errors.Is(err, redis.Nil) {
		return checkpointRef{}, "", ErrSearchContinuationNotFound
	}
	if err != nil {
		return checkpointRef{}, "", fmt.Errorf("%w: read search progress: %w", ErrSearchVisibilityUnavailable, err)
	}
	if len(fields) != 5 {
		return checkpointRef{}, "", ErrSearchVisibilityUnavailable
	}
	version, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return checkpointRef{}, "", ErrSearchVisibilityUnavailable
	}
	ms, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return checkpointRef{}, "", ErrSearchVisibilityUnavailable
	}
	remaining, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		return checkpointRef{}, "", ErrSearchVisibilityUnavailable
	}
	return checkpointRef{id: fields[0], version: version, expires: time.UnixMilli(ms), remaining: time.Duration(remaining) * time.Millisecond}, fields[3], nil
}

func (ref checkpointRef) save(ctx context.Context, client *redis.Client, session *searchSession, input uuid.UUID) error {
	body, err := session.encodeCheckpoint()
	if err != nil {
		return err
	}
	alias := func(id uuid.UUID) string {
		if id == uuid.Nil {
			return ""
		}
		return id.String()
	}
	charge := max(int64(len(body)), session.bytes)
	out, err := saveCheckpoint.Run(ctx, client, checkpointKeys(ref.id), checkpointPrefix, ref.id, alias(input), alias(session.lastNext), ref.version, ref.expires.UnixMilli(), charge, body, searchStateMaxSessions, searchStateMaxBytes, searchStatePerQueryBytes, session.subject[:], searchStateTTL.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("%w: save search progress: %w", ErrSearchVisibilityUnavailable, err)
	}
	switch out {
	case 1:
		return nil
	case 0:
		return errCheckpointAdvanced
	case -1:
		return ErrSearchCapacityExceeded
	case -2:
		return ErrSearchContinuationExpired
	}
	return ErrSearchVisibilityUnavailable
}
