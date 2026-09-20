-- KEYS[1] = rate-limit key
-- ARGV[1] = now_ms
-- ARGV[2] = window_ms
-- ARGV[3] = limit
--
-- Returns: { allowed(0/1), remaining, oldest_ts_ms }

local key          = KEYS[1]
local now          = tonumber(ARGV[1])
local window       = tonumber(ARGV[2])
local limit        = tonumber(ARGV[3])
local clear_before = now - window

-- 1. Убираем всё, что старше окна.
redis.call('ZREMRANGEBYSCORE', key, 0, clear_before)

-- 2. Смотрим текущее количество ДО добавления.
local current = redis.call('ZCARD', key)
local allowed = 0
local oldest  = 0

if current < limit then
    -- Есть место — добавляем запись.
    local member = tostring(now) .. '-' .. tostring(math.random(1, 1000000))
    redis.call('ZADD', key, now, member)
    redis.call('PEXPIRE', key, window)
    current = current + 1
    allowed = 1
else
    -- Лимит исчерпан — запоминаем самый старый запрос
    -- (по нему клиент поймёт, когда можно ретраить).
    local first = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    if first[2] then
        oldest = tonumber(first[2])
    end
end

local remaining = limit - current
if remaining < 0 then remaining = 0 end

return { allowed, remaining, oldest }