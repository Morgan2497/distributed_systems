# Chapter 3b: Multi-Node Broadcast

This is the five-node solution to [Fly.io's Multi-Node Broadcast challenge](https://fly.io/dist-sys/3b/). A client gives an integer to one node; eventually every node should return that integer from `read`. The challenge has no network partitions, so this implementation sends each new value directly to every other node once.

## Broadcast, direct fan-out, and gossip

The word **broadcast** describes the goal: information from one sender reaches all intended recipients. The [GeeksforGeeks introduction](https://www.geeksforgeeks.org/computer-networks/what-is-broadcasting-in-computer-network/) illustrates this with network-domain broadcast, such as an ARP request. This exercise operates at a different layer. Maelstrom gives us named processes (`n0`, `n1`, and so on), and our Go program sends a separate addressed message to each process. It does not use an IP broadcast address.

The [High Scalability gossip explanation](https://highscalability.com/gossip-protocol-explained/) compares several ways to spread information. In particular, direct point-to-point sending contacts recipients individually, while gossip normally selects a subset of peers repeatedly so information spreads over multiple rounds. Our code uses **direct fan-out**:

| Strategy | Who sends a new value? | What this code does |
| --- | --- | --- |
| Direct fan-out | The node that receives the client broadcast sends to every other node. | **Yes:** one `replicate` message per peer. |
| Eager forwarding | Receiving nodes also forward the value to other nodes. | No: a `replicate` receiver stores the value and stops. |
| Periodic gossip or anti-entropy | Nodes repeatedly exchange updates with selected peers. | No: there is no timer, peer sampling, or retry. |

Calling our `n.Send` loop a broadcast describes its **one-to-all goal**. It is not the periodic gossip protocol described in the article. This distinction matters when failures are introduced: a one-time send may be missed, while a protocol with later exchanges has another opportunity to deliver it.

## One value moving through five nodes

Each Maelstrom node runs a separate copy of [`main.go`](main.go). Therefore each has its **own** in-memory `messages` map. Suppose a client broadcasts `42` to `n0`:

```text
Client ── broadcast 42 ──→ n0
                            ├── replicate 42 ──→ n1
                            ├── replicate 42 ──→ n2
                            ├── replicate 42 ──→ n3
                            └── replicate 42 ──→ n4
```

| Moment | `n0` | `n1` | `n2` | `n3` | `n4` |
| --- | --- | --- | --- | --- | --- |
| Initially | `{}` | `{}` | `{}` | `{}` | `{}` |
| `n0` stores `42` | `{42}` | `{}` | `{}` | `{}` | `{}` |
| All peer messages arrive | `{42}` | `{42}` | `{42}` | `{42}` | `{42}` |

The middle row is possible: a read on `n1` may occur before its `replicate` message arrives. A read reports what **that node** knows at that moment. The challenge checks that values spread to all nodes within a few seconds.

## How the Go code executes

### Startup and local state

`main()` creates a Maelstrom node, an empty `map[int]bool`, and a mutex. The integer map keys are the values seen so far; assigning `messages[42] = true` records `42` once. Each process has a separate map and mutex. The `n.Handle` calls register functions; `n.Run()` then reads incoming JSON and dispatches each body by its `type`.

Maelstrom initializes each node before workload requests. After initialization, `n.ID()` is this process's ID and `n.NodeIDs()` returns **all** node IDs, including its own. `NodeIDs()` is not the neighbor list from the topology request.

### Client `broadcast`: store, send, acknowledge

For `{"type":"broadcast","message":42}`, the `broadcast` handler:

1. Decodes `msg.Body` into a fresh `requestData` struct, making `requestData.Message` equal to `42`.
2. Locks `mu`, saves `messages[42] = true` in this node's map, and unlocks.
3. Loops over `n.NodeIDs()`. It skips `n.ID()` so it does not send to itself.
4. Calls `n.Send(peer, map[string]any{"type":"replicate", "message":42})` for each remaining ID.
5. Calls `n.Reply` with `{"type":"broadcast_ok"}` for the original client request.

`peer` is the destination, such as `n1`. The `map[string]any` is a temporary **outgoing message body**: its string keys become JSON fields, and `any` permits both a string (`"replicate"`) and an integer (`42`) as values. The map of broadcast values is the separate, long-lived `messages` variable.

`n.Send` is fire-and-forget: it does not wait for a peer acknowledgement. `broadcast_ok` means this node stored the value and issued the sends; it does not prove all peers have processed them already.

### Peer `replicate`: receive and store

When `n1` receives `{"type":"replicate","message":42}`, its `replicate` handler decodes `42`, locks **n1's** mutex, stores `messages[42] = true` in **n1's** map, unlocks, and returns `nil`. It does not send a reply because the sender used `n.Send`, and it does not forward the value because the original node already sent to every peer.

The map also absorbs duplicate values: assigning the same integer key again leaves one entry. The handler's `requestData` variable is fresh for each request, while its node's `messages` map persists across requests.

### `read` and `topology`

The `read` handler locks the local map, appends its integer keys to a `[]int`, unlocks, and replies with `{"type":"read_ok","messages":[...]}`. Go does not guarantee map iteration order, and the broadcast workload does not require a particular order.

The `topology` handler returns `topology_ok`. This implementation uses the full list from `n.NodeIDs()` to contact peers directly, so it does not save the topology supplied in that request. The [challenge specification](https://fly.io/dist-sys/3b/) permits building your own topology.

## Cost and limits

With `N` nodes, one client broadcast produces `N - 1` inter-node `replicate` messages. In the five-node test, that is four sends per broadcast. Only the new integer is sent; the code does not resend the entire data set on every broadcast.

This is enough for 3b's no-partition scenario, but it does not retry lost or blocked sends. It has no periodic reconciliation, delivery acknowledgement from peers, or alternate path if the sender cannot reach a peer. Those limits matter for [part 3c's partition test](https://fly.io/dist-sys/3c/) and distinguish this implementation from the gossip and anti-entropy strategies described by High Scalability.

## Build and test

From `dist_sys/multi-node-broadcast`:

```sh
go build -o ~/go/bin/maelstrom-broadcast .
cd ../../maelstrom
./maelstrom test -w broadcast --bin ~/go/bin/maelstrom-broadcast --node-count 5 --time-limit 20 --rate 10
```

Rebuild before testing: Maelstrom runs the binary named by `--bin`, which can otherwise be an older build. A passing run ends with `:valid? true`; the workload section should report `:lost-count 0`. Maelstrom also writes `store/broadcast/<timestamp>/results.edn`, `history.txt`, and per-node logs under `node-logs/` for inspecting individual messages.

## Further reading

- [High Scalability: Gossip Protocol Explained](https://highscalability.com/gossip-protocol-explained/) — direct sending, eager forwarding, periodic gossip, and the tradeoffs among them.
- [GeeksforGeeks: What is Broadcasting in Computer Network?](https://www.geeksforgeeks.org/computer-networks/what-is-broadcasting-in-computer-network/) — the one-to-all idea and examples of broadcast at the network layer.
- [Fly.io: Challenge 3b](https://fly.io/dist-sys/3b/) — the requirements this program implements.
