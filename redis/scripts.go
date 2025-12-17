package redis

import redisv9 "github.com/redis/go-redis/v9"

const createScriptSrc = `
local cur = redis.call("GET", KEYS[1])
if cur then
  return {0, cur}
end
redis.call("PSETEX", KEYS[1], ARGV[2], ARGV[1])
return {1, ARGV[1]}
`

// ARGV[1] = token
// ARGV[2] = newValue
// ARGV[3] = ttlMs
const commitScriptSrc = `
local cur = redis.call("GET", KEYS[1])
if not cur then
  return 0
end
local nl = string.find(cur, "\n", 1, true)
if not nl then
  return -1
end
local token = string.sub(cur, 1, nl - 1)
if token ~= ARGV[1] then
  return -1
end
redis.call("PSETEX", KEYS[1], ARGV[3], ARGV[2])
return 1
`

// ARGV[1] = token
const deleteScriptSrc = `
local cur = redis.call("GET", KEYS[1])
if not cur then
  return 0
end
local nl = string.find(cur, "\n", 1, true)
if not nl then
  return -1
end
local token = string.sub(cur, 1, nl - 1)
if token ~= ARGV[1] then
  return -1
end
redis.call("DEL", KEYS[1])
return 1
`

var (
	createScript = redisv9.NewScript(createScriptSrc)
	commitScript = redisv9.NewScript(commitScriptSrc)
	deleteScript = redisv9.NewScript(deleteScriptSrc)
)
