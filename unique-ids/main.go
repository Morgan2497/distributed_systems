package main

import (
	"log"

	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
)

func main() {
	n := maelstrom.NewNode()

	// TODO: Register a handler for "generate" requests.

	if err := n.Run(); err != nil {
		log.Fatal(err)
	}
}
