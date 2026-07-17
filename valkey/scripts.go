package valkey

const acquireScript = `
if redis.call('EXISTS', KEYS[1]) == 1 then
  return {'contended'}
end
local token = redis.call('INCR', KEYS[2])
local exact = redis.call('GET', KEYS[2])
local now = redis.call('TIME')
local nowms = now[1] * 1000 + math.floor(now[2] / 1000)
local expires = nowms + tonumber(ARGV[2])
redis.call('HSET', KEYS[1], 'owner', ARGV[1], 'token', exact,
  'acquired', tostring(nowms), 'expires', tostring(expires))
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return {'ok', exact, tostring(nowms), tostring(expires)}
`

const renewScript = `
if redis.call('EXISTS', KEYS[1]) == 0 or
   redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'token') ~= ARGV[2] then
  return {'stale'}
end
local now = redis.call('TIME')
local nowms = now[1] * 1000 + math.floor(now[2] / 1000)
local expires = nowms + tonumber(ARGV[3])
local acquired = redis.call('HGET', KEYS[1], 'acquired')
redis.call('HSET', KEYS[1], 'expires', tostring(expires))
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return {'ok', ARGV[2], acquired, tostring(expires)}
`

const validateScript = `
if redis.call('EXISTS', KEYS[1]) == 0 or
   redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'token') ~= ARGV[2] then
  return {'stale'}
end
return {'ok', ARGV[2], redis.call('HGET', KEYS[1], 'acquired'),
  redis.call('HGET', KEYS[1], 'expires')}
`

const releaseScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {'missing'}
end
if redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'token') ~= ARGV[2] then
  return {'stale'}
end
redis.call('DEL', KEYS[1])
return {'ok'}
`
