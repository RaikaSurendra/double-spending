# Distributed Race Conditions & Double-Spending in Redis

## What is a Race Condition?

A race condition is a logical bug where the correctness of a program depends on the **unpredictable interleaving of operations** across concurrent threads or processes.

The classic pattern is a **read-modify-write sequence**:

```
1. READ   current value
2. MODIFY value locally
3. WRITE  new value back
```

If two actors execute this sequence concurrently, the second write silently overwrites the first. The result is incorrect, and the data is permanently corrupted — with no error, no exception, and no log entry.

---

## The Double-Spending Scenario

**Setup:**
- Redis holds `balance = 100`
- Two servers simultaneously try to deduct 10
- Expected result: `80`

**What actually happens:**

```
T=0ms  Server A: GET balance → 100
T=1ms  Server B: GET balance → 100     ← same stale value
T=2ms  Server A: computes 100 - 10 = 90
T=2ms  Server B: computes 100 - 10 = 90
T=3ms  Server A: SET balance 90
T=4ms  Server B: SET balance 90        ← overwrites A's write
```

Final balance: **90** instead of **80**. Ten units were double-spent — both servers believed they made a valid deduction, but only one actually took effect.

This happens because the **gap between `GET` and `SET`** is an open window. Any other operation can read the same stale value during that window.

---

## Why a Mutex Does Not Fix This

A `sync.Mutex` is an **in-process synchronization primitive**. It guards a critical section of code by preventing other goroutines **within the same process** from entering it simultaneously.

It has zero authority over:
- Other processes on the same machine
- Other servers in a cluster
- External state stores like Redis

```
Server A (Process 1)          Server B (Process 2)
┌──────────────────────┐      ┌──────────────────────┐
│  mutexA.Lock()       │      │  mutexB.Lock()       │
│  GET balance → 100   │      │  GET balance → 100   │  ← race!
│  SET balance 90      │      │  SET balance 90      │  ← overwrite!
│  mutexA.Unlock()     │      │  mutexB.Unlock()     │
└──────────────────────┘      └──────────────────────┘
         mutexA ≠ mutexB — they are separate objects in separate memory spaces
```

`mutexA` is a Go struct living in Server A's heap. Server B has never heard of it. Locking it does nothing to prevent Server B from reading a stale value from Redis.

**Result:** Thread-safe corruption. The mutex enforces order within one server, but the race between servers is completely unaffected.

---

## Fix 1 — Atomic Redis Instructions

Redis executes commands on a **single main thread**. Every command is processed sequentially and atomically. There is no interleaving between the internal read and write of a single command.

`DECRBY` is one such atomic instruction:

```
Client sends: DECRBY balance 10
Redis does:
  1. Read current value           ← no other command runs here
  2. Subtract 10
  3. Write new value              ← these three steps are one atomic unit
  4. Return new value
```

From the perspective of any client, the balance either still has the old value or already has the new value — there is no intermediate state visible.

```go
// Wrong — three separate round trips, race window between GET and SET
val, _ := rdb.Get(ctx, "balance").Int()
rdb.Set(ctx, "balance", val-10, 0)

// Correct — one atomic round trip
rdb.DecrBy(ctx, "balance", 10)
```

**When to use:** Simple arithmetic on a single key — increment, decrement, add, subtract.

**Available atomic commands:** `INCR`, `DECR`, `INCRBY`, `DECRBY`, `INCRBYFLOAT`, `GETSET`, `SETNX`, `GETDEL`

---

## Fix 2 — Lua Scripts via EVAL

`DECRBY` handles simple subtraction, but what if you need to **check the balance before deducting**? You cannot split that into `GET` + conditional `DECRBY` — the check and the deduction would be separate commands with a race window between them.

Redis's `EVAL` command runs a **Lua script atomically**. No other Redis command executes while the script runs, regardless of how many clients are connected.

```lua
-- This entire script is one atomic operation
local balance = tonumber(redis.call('GET', KEYS[1]))
local amount  = tonumber(ARGV[1])
if balance < amount then
    return redis.error_reply('INSUFFICIENT_FUNDS')
end
return redis.call('DECRBY', KEYS[1], amount)
```

```go
var deductScript = redis.NewScript(`
    local balance = tonumber(redis.call('GET', KEYS[1]))
    local amount  = tonumber(ARGV[1])
    if balance < amount then
        return redis.error_reply('INSUFFICIENT_FUNDS')
    end
    return redis.call('DECRBY', KEYS[1], amount)
`)

newBalance, err := deductScript.Run(ctx, rdb, []string{"balance"}, 10).Int()
if err != nil && strings.Contains(err.Error(), "INSUFFICIENT_FUNDS") {
    // handle insufficient funds
}
```

**When to use:** Any time you need conditional read-modify-write logic — balance checks, inventory checks, rate limiting with threshold enforcement.

**Performance note:** `redis.NewScript` uses `EVALSHA` after the first call, sending only the SHA1 of the script rather than the full script body.

---

## Comparison Table

| Approach | Atomic | Cross-Process Safe | Overdraft Protection | Use When |
|---|---|---|---|---|
| GET + SET | ✗ | ✗ | ✗ | Never for shared state |
| Mutex + GET + SET | ✗ | ✗ | ✗ | Never (false confidence) |
| `DECRBY` | ✓ | ✓ | ✗ | Simple increment/decrement |
| Lua `EVAL` | ✓ | ✓ | ✓ | Conditional logic required |
| Distributed lock (Redlock) | ✓ | ✓ | ✓ | Complex multi-key transactions |

---

## Beyond Redis: Production Patterns

### Redis is not a ledger

Redis is optimized for speed, not durability. In default configuration, it can lose up to a second of writes on crash (`appendfsync everysec`). For financial balances:

- **Use Redis as a cache and rate-limiter**, not as the source of truth
- **Back every balance change with a durable ledger** in PostgreSQL, MySQL, or a dedicated financial database
- Redis balance = "fast read replica" of the authoritative ledger balance

### Idempotency Keys

Network failures cause retries. Retries without idempotency cause double-charges.

Every deduction operation should carry an idempotency key (e.g., a UUID generated by the client). The server stores this key and rejects duplicate operations:

```
SETNX idempotency:{uuid} 1 EX 86400   ← atomic: only one server processes this key
if key already existed → return previous result
else → execute deduction
```

### The Redlock Algorithm

For operations that must lock across multiple Redis keys or require longer critical sections, use the [Redlock algorithm](https://redis.io/docs/manual/patterns/distributed-locks/) — a distributed mutex built on top of Redis that works correctly across multiple Redis instances.

---

## Running the Examples

```bash
# Start Redis
make up

# Demo 1: Race condition (non-atomic GET+SET)
make demo-broken

# Demo 2: Mutex doesn't fix distributed races
make demo-mutex-wrong

# Demo 3: Atomic DECRBY
make demo-fixed-incrby

# Demo 4: Lua script with overdraft protection
make demo-fixed-lua

# All four in sequence
make demo-all

# Stop Redis
make down
```

### Interpreting the output

**`demo-broken`**: `Actual final` will be significantly higher than `Expected` — lost writes due to the race.

**`demo-mutex-wrong`**: Same corruption, despite the mutex. The race is between processes, not goroutines.

**`demo-fixed-incrby`**: `Actual == Expected` on every run. Deterministic.

**`demo-fixed-lua`**: `Succeeded + Insufficient == 100`, `Actual == 0`, never negative.

---

## Further Reading

- [Redis Atomicity](https://redis.io/docs/manual/transactions/) — MULTI/EXEC transactions and Lua scripting
- [Redlock Algorithm](https://redis.io/docs/manual/patterns/distributed-locks/) — distributed locking across Redis nodes
- [CRDT-based approaches](https://redis.io/docs/data-types/probabilistic/) — eventual consistency for distributed counters
- [The Go Memory Model](https://go.dev/ref/mem) — formal spec for goroutine synchronization
