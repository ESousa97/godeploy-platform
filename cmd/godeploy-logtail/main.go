package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"

	"godeploy-platform/internal/platform/iox"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: godeploy-logtail <CONTAINER_ID>")
	}
	containerID := os.Args[1]

	base := strings.TrimSpace(os.Getenv("GODEPLOY_LOG_WS_URL"))
	if base == "" {
		base = "ws://127.0.0.1:8081/api/ws/logs"
	}
	u, err := url.Parse(base)
	if err != nil {
		log.Fatalf("invalid GODEPLOY_LOG_WS_URL: %v", err)
	}
	q := u.Query()
	q.Set("container", containerID)
	u.RawQuery = q.Encode()

	fmt.Printf("Connecting to %s...\n", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("connection error:", err)
	}
	defer iox.Close(c)

	fmt.Println("Connected. Streaming logs (Ctrl+C to exit)...")
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("connection closed:", err)
			return
		}
		fmt.Printf("%s\n", message)
	}
}
