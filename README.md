# Gossip Glomers: Echo

My Go solution to [Fly.io's first distributed systems challenge](https://fly.io/dist-sys/1/).
The node receives JSON messages from Maelstrom and replies to each `echo` request with an `echo_ok` response containing the same value.

Build the node with:

```sh
go build -o maelstrom-echo .
```

To test it, download [Maelstrom 0.2.3](https://github.com/jepsen-io/maelstrom/releases/tag/v0.2.3), then run its `maelstrom` launcher with the `echo` workload and the path to the built binary.
