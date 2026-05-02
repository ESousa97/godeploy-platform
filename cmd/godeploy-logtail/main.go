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
		log.Fatal("Uso: godeploy-logtail <ID_DO_CONTAINER>")
	}
	containerID := os.Args[1]

	base := strings.TrimSpace(os.Getenv("GODEPLOY_LOG_WS_URL"))
	if base == "" {
		base = "ws://127.0.0.1:8081/api/ws/logs"
	}
	u, err := url.Parse(base)
	if err != nil {
		log.Fatalf("GODEPLOY_LOG_WS_URL invalida: %v", err)
	}
	q := u.Query()
	q.Set("container", containerID)
	u.RawQuery = q.Encode()

	fmt.Printf("Conectando em %s...\n", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("Erro ao conectar:", err)
	}
	defer iox.Close(c)

	fmt.Println("Conectado! Aguardando logs (pressione Ctrl+C para sair)...")
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("Conexão fechada:", err)
			return
		}
		fmt.Printf("%s\n", message)
	}
}
