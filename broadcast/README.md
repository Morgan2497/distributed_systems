# Chapter 3: Broadcast

Broadcast means introducing a message at one node and spreading it to the other nodes. It is a building block for replication and coordination, but the word alone does not say **when** messages arrive, **whether** they arrive despite failures, or **in what order** nodes observe them. Those are separate guarantees.

In [Fly.io's Broadcast challenge](https://fly.io/dist-sys/3a/), we will first make one node store values and return them from `read`. Later parts add multiple nodes, network partitions, and efficiency targets. The [Maelstrom broadcast workload](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-broadcast) treats this as an **eventually convergent set**: clients add values through `broadcast`, and each node reports its own known values through `read`. **This chapter does not ask us to implement atomic broadcast or a shared message order.**

## A concrete picture

Suppose a client sends value `42` to node `n1`. Initially, only `n1` knows it:

| Moment | `n1` knows | `n2` knows | `n3` knows |
| --- | --- | --- | --- |
| Before the request | `{}` | `{}` | `{}` |
| `n1` accepts `42` | `{42}` | `{}` | `{}` |
| After propagation | `{42}` | `{42}` | `{42}` |

Between the last two rows, a `read` from `n2` may legitimately return an empty list: it reports **local knowledge at that moment**. Eventually, after messages can flow and the protocol has had time to run, every node should include `42`. An acknowledgement from `n1` is not proof that every other node has already received it. [Maelstrom's workload description](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-broadcast) explains that it mixes broadcasts with reads and checks for lost values after time for convergence.

## The three RPCs in this challenge

| Request | Purpose | Reply |
| --- | --- | --- |
| `{"type":"broadcast","message":42}` | Add the integer `42` to the node's known values. | `{"type":"broadcast_ok"}` |
| `{"type":"read"}` | Ask for the values this node currently knows. | `{"type":"read_ok","messages":[42]}` |
| `{"type":"topology","topology":{"n1":["n2"]}}` | Tell a node about suggested neighbors. | `{"type":"topology_ok"}` |

Maelstrom wraps these bodies in JSON messages with source, destination, and request IDs. The [single-node specification](https://fly.io/dist-sys/3a/) says the broadcast values are integers and each value it introduces is unique. `read` returns a **set of values represented as a list**; their order does not matter. The topology is a suggested graph, not a restriction on which nodes can communicate. In part 3c, each node must return only its **own** values rather than query the whole cluster on every read.

## Delivery guarantees: what changes between variants?

In the table below, *deliver* means the application accepts a broadcast message as received. The guarantees depend on the stated failure and network assumptions. They cannot promise progress across a permanent partition that never heals.

| Variant | Main promise | What it leaves open |
| --- | --- | --- |
| **Best effort** | A node attempts to send to everyone. | Some recipients may never get the message. |
| **Reliable broadcast** | If a correct node broadcasts a message, correct nodes eventually deliver it; if one correct node delivers it, the others eventually do too. A message is delivered at most once and must come from a broadcast. | Different nodes may deliver different messages first. |
| **FIFO broadcast** | Each sender's messages are delivered in that sender's send order. If `n1` sends `A` then `B`, recipients deliver `A` before `B`. | Messages from different senders may be interleaved differently. |
| **Causal broadcast** | If `A` could have influenced `B`, recipients deliver `A` before `B`. | Concurrent, unrelated messages may arrive in either order. |
| **Total order / atomic broadcast** | Correct nodes deliver the same messages in the same order, along with reliability guarantees. | It requires coordination and is more demanding than sharing an unordered set. |

**Reliability and ordering are different dimensions.** FIFO, causal, and total order describe ordering constraints; a real protocol must also say what delivery and fault guarantees it provides. FIFO alone does not promise no loss. Total order alone is not a substitute for a reliable delivery specification. The formal reliable-broadcast properties above follow [Chandra and Toueg's paper](https://www.cs.princeton.edu/courses/archive/fall08/cos597B/papers/unreliable.pdf); [Birman and Joseph's work](https://ecommons.cornell.edu/entities/publication/c8f1def4-d39a-40cb-8510-674c2a6b6f79) discusses reliable multicast with different ordering constraints.

### FIFO versus causal: one example

`n1` broadcasts `A = "create document"`. After `n2` receives `A`, it broadcasts `B = "comment on document"`. Because receiving `A` influenced sending `B`, `A` **causally precedes** `B`. Causal broadcast makes every recipient deliver `A` before `B`, even though different nodes sent them. FIFO broadcast only constrains messages from the **same** sender, so FIFO by itself cannot protect this cross-sender dependency. [Lamport's original paper](https://lamport.azurewebsites.net/pubs/time-clocks.pdf) defines the underlying happened-before relation.

If `n1` broadcasts `A` and `n2` independently broadcasts `C`, neither may have caused the other. Causal broadcast permits different nodes to deliver `A,C` and `C,A`. Atomic broadcast makes all correct nodes choose the same order. That shared sequence is useful for replicated state machines such as [ZooKeeper's Zab protocol](https://cwiki.apache.org/confluence/spaces/ZOOKEEPER/pages/24189846/Zab1.0). [Chandra and Toueg](https://www.cs.princeton.edu/courses/archive/fall08/cos597B/papers/unreliable.pdf) show the equivalence between consensus and atomic broadcast under their asynchronous crash-failure model. That equivalence is **not** a claim that this Maelstrom set-broadcast exercise solves consensus.

## How values can spread

| Approach | Basic idea | Main tradeoff |
| --- | --- | --- |
| **Direct send / flooding** | Send a new value to peers; a recipient forwards it the first time it sees it. | Simple and fast on a small cluster, but many links can carry duplicates. |
| **Gossip / anti-entropy** | Periodically exchange values, or just the values a peer appears to be missing. | Retries help after temporary failures; convergence speed and message cost depend on peer selection and batching. |
| **Tree or ring** | Forward along a chosen path through the nodes. | Fewer transmissions in the healthy case, but a broken link or failed intermediary needs repair or another route. |

These are **propagation strategies**, not delivery guarantees by themselves. A one-time send can be lost. A single tree can split under a partition. A gossip protocol needs enough retries and eventual connectivity to make progress. The original [epidemic algorithms paper](https://www.cis.upenn.edu/~bcpierce/courses/dd/papers/demers-epidemic.pdf) is a deeper source on anti-entropy and gossip.

For this workload, thinking in terms of a **grow-only set** helps. Each node adds values it learns; merging two nodes' sets takes their union. Union is insensitive to duplicate deliveries and to the order in which values arrive. A node can therefore remember which values it has seen and avoid repeatedly processing them. This explains why the challenge can tolerate temporary differences between nodes without agreeing on a global order.

### What a partition changes

Imagine `n1` cannot reach `n2`. `n1` can still accept `42` locally and acknowledge its client. `n2` cannot learn `42` while every path to `n1` is blocked. When the partition heals, retries or anti-entropy can carry `42` over. [Part 3c](https://fly.io/dist-sys/3c/) tests eventual propagation despite temporary partitions. No broadcast algorithm can make `n2` learn information across a **permanent** separation with no communication path.

## The Fly.io sequence

| Part | Focus | Test setup |
| --- | --- | --- |
| [3a: Single node](https://fly.io/dist-sys/3a/) | Handle `broadcast`, `read`, and `topology`; store values locally. | 1 node, 20 seconds, rate 10 |
| [3b: Multiple nodes](https://fly.io/dist-sys/3b/) | Propagate values across a cluster. | 5 nodes, 20 seconds, rate 10 |
| [3c: Partitions](https://fly.io/dist-sys/3c/) | Keep spreading values after interrupted links recover. | 5 nodes plus partition nemesis |
| [3d–3e: Efficiency](https://fly.io/dist-sys/3d/) | Reduce inter-node traffic and propagation delay. | 25 nodes with 100 ms message latency; [part 3e](https://fly.io/dist-sys/3e/) tightens the message budget |

The first implementation step will be **3a only**: a local set and three handlers. It does not yet need inter-node gossip. Once a `broadcast/main.go` exists, the familiar workspace commands will be:

```sh
# From dist_sys/broadcast/
go build -o ~/go/bin/maelstrom-broadcast .
cd ../../maelstrom
./maelstrom test -w broadcast --bin ~/go/bin/maelstrom-broadcast --node-count 1 --time-limit 20 --rate 10
```

## Questions to check understanding

1. If `n1` replies `broadcast_ok`, must a simultaneous `read` on `n2` already include the value? Why?
2. Why does deduplicating a received value prevent a flooding loop?
3. What must happen after a temporary partition heals for every node's set to converge?
4. If `A` causes `B` but they come from different senders, which ordering guarantee protects their order?
5. Why can all nodes end with the same set of values without having delivered them in the same order?

### Answers

1. No. `broadcast_ok` can acknowledge local acceptance before propagation reaches `n2`.
2. A node forwards only the first copy. Later copies are ignored, so the same value cannot circulate forever along a cycle.
3. Nodes must resume exchanging missing values, and the communication paths must eventually allow those exchanges to complete.
4. Causal broadcast. FIFO only orders messages sent by one sender.
5. Adding values to a set is independent of order: `{A} ∪ {B}` equals `{B} ∪ {A}`. A shared set does not imply a shared delivery sequence.

## Further reading

- [Fly.io Broadcast challenges, beginning with 3a](https://fly.io/dist-sys/3a/) and [Maelstrom broadcast workload](https://github.com/jepsen-io/maelstrom/blob/main/doc/workloads.md#workload-broadcast): the specification for this project.
- [Chandra and Toueg, *Unreliable Failure Detectors for Reliable Distributed Systems*](https://www.cs.princeton.edu/courses/archive/fall08/cos597B/papers/unreliable.pdf): formal reliable broadcast, atomic broadcast, and consensus.
- [Lamport, *Time, Clocks, and the Ordering of Events in a Distributed System*](https://lamport.azurewebsites.net/pubs/time-clocks.pdf): causal ordering.
- [Demers et al., *Epidemic Algorithms for Replicated Database Maintenance*](https://www.cis.upenn.edu/~bcpierce/courses/dd/papers/demers-epidemic.pdf): gossip and anti-entropy.
- [Apache ZooKeeper's Zab protocol](https://cwiki.apache.org/confluence/spaces/ZOOKEEPER/pages/24189846/Zab1.0): a production use of atomic broadcast.

The [Wikipedia atomic-broadcast overview](https://en.wikipedia.org/wiki/Atomic_broadcast) and [GeeksforGeeks introduction](https://www.geeksforgeeks.org/system-design/atomic-broadcast-and-its-role-in-distributed-systems/) are approachable starting points, but the papers and project documentation above are more precise about guarantees and assumptions.
