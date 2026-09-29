# Chapter 2: Unique ID generation

In this chapter, you will create a fresh UUID for each `generate` request and reply with `generate_ok` and that ID. Each node will generate IDs locally, so it can keep serving requests even when a network partition prevents nodes from talking to one another.

The key question is **when coordination is necessary**. A globally increasing sequence would require nodes to agree on who gets the next number. Here, IDs only need to be unique, so locally generated UUIDs let every node continue working. UUID uniqueness relies on an extremely low collision probability.

Maelstrom will test this behavior across several nodes while partitioning the network. The exercise connects three ideas: **remote procedure calls**, **global uniqueness**, and **availability during partitions**.

The specification is [Fly.io's Chapter 2](https://fly.io/dist-sys/2/), with the exact request and response fields in the [Maelstrom unique-ids workload](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-unique-ids).

## The system we are building

Maelstrom launches several copies of our executable. Each copy is a **node** with its own memory and a distinct node ID such as `n1` or `n2`. Clients send requests to nodes. Maelstrom routes messages between them over a simulated network, and can interrupt routes to create partitions. A node reads JSON messages from standard input and writes JSON replies to standard output. The [Maelstrom protocol](https://github.com/jepsen-io/maelstrom/blob/main/doc/protocol.md) describes this wire format and the initial `init` message that assigns each node its ID.

The operation is small:

```json
{"src":"c7","dest":"n1","body":{"type":"generate","msg_id":41}}
```

One valid reply is:

```json
{"src":"n1","dest":"c7","body":{"type":"generate_ok","in_reply_to":41,"id":"9f6d45a8-3c2e-4a10-9b71-62d1c8e85f04"}}
```

The `id` field is **the value our service generates**. `msg_id` and `in_reply_to` are **RPC bookkeeping**: they associate a reply with a request. Their values do not serve as our generated ID. The Go Maelstrom library's `Reply` method handles the reply envelope and `in_reply_to` for us.

### Why this is an RPC

A **remote procedure call (RPC)** lets a client request work from another process and receive a result. Here the apparent operation is `generate() -> id`, but underneath it is a pair of messages with source, destination, operation type, and correlation fields. Unlike an ordinary local function call, the caller can lose contact with the server and be unsure whether the operation happened. See the [RPC overview](https://en.wikipedia.org/wiki/Remote_procedure_call) and Maelstrom's [message protocol](https://github.com/jepsen-io/maelstrom/blob/main/doc/protocol.md).

For example, a node might generate an ID and send a reply that never reaches the client. If the client retries, it may receive a *different* ID. Both IDs can still be unique. This workload checks uniqueness and availability; it does not require retries to return the same ID or prove that an application used an ID exactly once. That distinction matters when an ID generator is used inside a larger operation such as creating a payment or an order.

## What “unique” means

Let `G` be the set of IDs returned by successful `generate` calls during the test. The safety requirement is:

> Two distinct successful calls must never return the same value.

It does **not** require consecutive integers, IDs sorted by creation time, or a single cluster-wide counter. A gap is harmless; a duplicate is not. Global uniqueness means the rule holds **across all nodes**, not only within one process. Maelstrom's [unique-ids checker](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-unique-ids) examines the IDs returned by the cluster.

Uniqueness is a **safety** property: once two clients receive the same ID, later communication cannot undo that mistake. Availability is a **liveness** goal here: a reachable node should keep answering requests even when it cannot reach its peers. The challenge's `--availability total` test checks that requests succeed during the injected partitions.

### Why a plain local counter is insufficient

If every node starts a counter at zero and returns only the counter, both `n1` and `n2` can return `1`. Neither node did anything wrong locally; the collision appears when we consider the cluster as a whole.

| Node | First call | Second call |
| --- | --- | --- |
| `n1`, counter only | `1` | `2` |
| `n2`, counter only | `1` | `2` |
| `n1`, node ID + counter | `n1-1` | `n1-2` |
| `n2`, node ID + counter | `n2-1` | `n2-2` |

The node ID gives each node a separate **namespace**. The counter distinguishes calls within that namespace. This is one alternative to UUIDs: nodes divide the ID space in advance and do not need to ask one another for the next value. **Our implementation uses UUIDs instead.**

### Why node ID plus counter would work

Suppose an ID is the pair `(node ID, local counter)`, encoded unambiguously as a string. Take any two successful calls:

1. If different nodes handled them, their node-ID components differ.
2. If the same node handled them, their counter components differ because each call reserves a fresh counter value.

In either case the complete IDs differ. This argument depends on Maelstrom assigning distinct node IDs and on each node incrementing its counter without races, resets, or wraparound during the test. It does not depend on clocks or messages between nodes.

If you implemented this alternative in Go, Maelstrom's handlers could run concurrently, so a plain `counter++` could race and even lose increments. A mutex or an [atomic counter](https://pkg.go.dev/sync/atomic#Uint64.Add) would give each call a distinct local number. `n.ID()` would supply the node component after initialization.

### The UUID design we implemented

Our `unique-ids/main.go` registers a `generate` handler. Inside that handler, `uuid.New()` creates a new random UUID for **each request**. The handler puts it in the `id` field and calls `n.Reply()` to send a `generate_ok` response. Go's JSON encoder writes the UUID as a string. Neither the node ID nor a shared counter is part of the generated value.

UUIDv4 draws from a very large random space. Nodes can generate UUIDs independently, including during a partition or after a restart, without reserving ranges or coordinating their counters. A collision is still theoretically possible: the uniqueness claim is probabilistic, unlike the node-ID-plus-counter argument under its assumptions.

## Why partitions change the design

Imagine `n1` and `n2` cannot exchange messages for several seconds. A single central counter or a leader can allocate globally increasing IDs, but a node cut off from that allocator cannot issue a new ID while preserving that design. It must wait or fail the request. That conflicts with this chapter's total-availability requirement.

With our UUID design, both sides can continue generating IDs independently. There is no central allocator to reach and no counter state to reconcile when communication returns. The alternative `(node ID, local counter)` design also permits independent generation because each node owns a separate namespace. Both designs work here because the requirement is **uniqueness**, not a globally ordered sequence. A partition does not force every distributed feature to stop; the answer depends on which property needs coordination.

## Other ID designs and their tradeoffs

The linked [survey of ID generation strategies](https://blog.devtrovert.com/p/how-to-generate-unique-ids-in-distributed) covers UUIDs, Nano ID, sequences, MongoDB ObjectIds, Snowflake, and Sonyflake. They answer related but different product needs:

| Design | How it avoids collisions | What to watch |
| --- | --- | --- |
| Central sequence | One authority reserves each next number. | That authority needs durable state and may be unreachable during a partition. It can provide an order, but access to it requires coordination. |
| Node ID + local counter | Nodes own distinct namespaces; each node increments locally. | Node IDs must stay unique, and a reused node ID must not restart its counter from an old value. There is no global time order. |
| UUIDv4 or Nano ID | Large random space makes a collision extremely unlikely without coordination. | The guarantee is probabilistic, not mathematical impossibility. Random IDs do not encode generation order. |
| MongoDB ObjectId | Combines time, a process-specific random value, and a counter. | It carries time information, but clocks and independent processes do not create a strict global order. |
| Snowflake or Sonyflake family | Packs time, a worker or machine identifier, and a local sequence into a compact number. | Worker identity must be managed; clock movement and per-time-unit sequence limits need deliberate handling. |

[RFC 9562](https://www.rfc-editor.org/rfc/rfc9562.html) defines UUIDv4 and the time-oriented UUIDv7. The [Nano ID project](https://github.com/ai/nanoid), [MongoDB ObjectId documentation](https://www.mongodb.com/docs/manual/reference/bson-types/#objectid), and [Sonyflake project](https://github.com/sony/sonyflake) provide details for those schemes. Time-oriented IDs can be useful for approximate ordering, but their timestamps do not by themselves prove a total order across machines with different clocks.

For `b` uniformly random bits and `k` generated IDs, the chance of at least one collision is approximately `k(k-1) / (2 * 2^b)` while that probability remains small. This **birthday-bound** intuition explains why our random UUID design is practical, while also explaining why “very unlikely” differs from “impossible.” Node ID plus counter instead has a deterministic argument under its stated assumptions.

## Limits of the Chapter 2 solution

Our UUID solution fits this test, but these boundaries matter in a real service:

- **Collision risk:** Random UUIDs have an extremely low chance of collision, but they do not provide a mathematical guarantee that duplicates can never occur.
- **Restarts:** A fresh UUID does not depend on an in-memory counter, so restarting a node does not reset an ID sequence. A node-ID-plus-counter alternative would need durable state or a fresh node identity to avoid repeating earlier IDs after a restart.
- **Ordering:** Random UUIDs do not reveal which request happened first. Even a time-bearing ID needs careful clock assumptions before it can claim a strict global order.
- **Retry semantics:** Generating a fresh ID for a repeated RPC preserves uniqueness but does not make a larger business operation idempotent. If the caller needs the *same* result on retry, it must supply a stable request key and the service must remember the earlier result.

The [Fly.io challenge series](https://fly.io/dist-sys/1/) says Maelstrom injects network failures for these challenges and does not intentionally crash node processes. Passing the test is evidence for this workload and failure model; it does not turn probabilistic UUID uniqueness into an absolute guarantee.

## Build and test in this workspace

Fly.io's command assumes your shell is inside the downloaded `maelstrom/` directory and that the node binary exists at `~/go/bin/maelstrom-unique-ids`. From this `dist_sys/unique-ids/` directory, build the binary with that exact name, then move to the Maelstrom directory:

```sh
go build -o ~/go/bin/maelstrom-unique-ids .
cd ../../maelstrom
./maelstrom test -w unique-ids --bin ~/go/bin/maelstrom-unique-ids \
  --time-limit 30 --rate 1000 --node-count 3 \
  --availability total --nemesis partition
```

The test settings come from [Chapter 2](https://fly.io/dist-sys/2/). Maelstrom checks that returned IDs are distinct and that the nodes continue to answer while the network is partitioned. A `generate` handler that sends `generate_ok` replies is required before this test can pass.

## Questions and answers

1. **Why can two correct local counters still produce a globally incorrect result?** Each node may start at zero and correctly return `1` for its first request. If the ID is only that number, two nodes return the same ID. Local correctness does not imply uniqueness across the cluster. A node ID prefix or a different ID scheme is needed.
2. **Which assumption in the `(node ID, counter)` proof breaks if a node restarts and reuses its ID?** The proof assumes the same node ID never uses the same counter value twice. If `n1` previously returned `n1-1`, then restarts with its counter reset to zero, its next request can return `n1-1` again. Our UUID implementation does not reset a counter on restart, although random UUID collisions remain theoretically possible.
3. **If a reply is lost, can a retry return a different ID while the uniqueness requirement still holds?** Yes. The first request may generate an ID whose reply never reaches the client. A retry can generate a second, different ID. That still satisfies uniqueness, but it does not make a larger operation safe to repeat. For example, creating an order may need a separate idempotency key so a retry does not create two orders.
4. **Why can this design keep serving during a partition while a single central allocator may not?** Our nodes generate UUIDs locally, without contacting other nodes. A node cut off from a central allocator cannot obtain the next allocated value, so it must wait or fail. The alternative node-ID-plus-counter design can also generate locally because each node owns a separate namespace.
5. **Does sorting generated ID strings tell you the order in which requests completed across nodes? Why?** No. Our UUIDs are random, so their text order does not encode time. With node ID plus counter, the prefix groups IDs by node and the counter tracks local allocation, not the order in which requests across nodes finished.

## References

- [Fly.io, Challenge 2: Unique ID Generation](https://fly.io/dist-sys/2/)
- [Maelstrom unique-ids workload](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-unique-ids) and [protocol](https://github.com/jepsen-io/maelstrom/blob/main/doc/protocol.md)
- [Devtrovert, How to Generate Unique IDs in Distributed Systems](https://blog.devtrovert.com/p/how-to-generate-unique-ids-in-distributed)
- [Remote procedure call overview](https://en.wikipedia.org/wiki/Remote_procedure_call)
- [Go atomic operations](https://pkg.go.dev/sync/atomic)
