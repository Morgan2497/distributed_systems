# Chapter 1: Echo

My Go solution to [Fly.io's first distributed systems challenge](https://fly.io/dist-sys/1/).
The node receives JSON messages from Maelstrom and replies to each `echo` request with an `echo_ok` response containing the same value.

From this `echo/` directory, build the node with:

```sh
go build -o ~/go/bin/maelstrom-echo .
```

The Maelstrom runner is in `../../maelstrom/` relative to this directory. To run Fly.io's Echo test from there:

```sh
cd ../../maelstrom
./maelstrom test -w echo --bin ~/go/bin/maelstrom-echo --node-count 1 --time-limit 10
```
