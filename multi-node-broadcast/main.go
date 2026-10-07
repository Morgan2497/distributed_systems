package main

import (
	"sync"
	"encoding/json"
	maelstrom "github.com/jepsen-io/maelstrom/demo/go"
	"log"
)

func main() {
	n := maelstrom.NewNode()
	messages := make(map[int]bool)
	var mu sync.Mutex

	n.Handle("broadcast", func (msg maelstrom.Message) error {
		var requestData struct {
			Message int `json:"message"`
		}
		if err := json.Unmarshal(msg.Body, &requestData); err != nil {
			return err
		}
		
		mu.Lock()
		messages[requestData.Message] = true
		mu.Unlock()
		
		for _, peer := range n.NodeIDs() {
			if peer == n.ID() {
				continue
			}
			if err := n.Send(peer, map[string]any {
				"type": "replicate",
				"message": requestData.Message,
			}); err != nil {
				return err
			}
		}

		return n.Reply(msg, map[string]any {
			"type": "broadcast_ok",
		})
	})

	n.Handle("topology", func(msg maelstrom.Message) error {
		return n.Reply(msg, map[string]any {
			"type": "topology_ok",
		})
	})

	n.Handle("replicate", func(msg maelstrom.Message) error {
		var requestData struct {
			Message int `json:"message"`
		}

		if err := json.Unmarshal(msg.Body, &requestData); err != nil {
			return err
		}

		mu.Lock()
		messages[requestData.Message] = true
		mu.Unlock()
		return nil
	})

	n.Handle("read", func(msg maelstrom.Message) error {
		mu.Lock()
		values := []int{}
		for value := range messages {
			values = append(values, value)
		}
		mu.Unlock()

		return n.Reply(msg, map[string]any {
			"type": "read_ok",
			"messages": values,
		})
	})
	if err := n.Run(); err != nil {
		log.Fatal(err)
	}	
}

