package main

import (
	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
	"log"
	"github.com/google/uuid"
)

func main() {
	n := maelstrom.NewNode()
	// var counter atomic.Uint64

	n.Handle("generate", func(msg maelstrom.Message) error {
		id := uuid.New()

		return n.Reply(msg, map[string]any{
			"type": "generate_ok",
			"id":   id,
		})
	})
	if err := n.Run(); err != nil { 
		log.Fatal(err)
	}
}
