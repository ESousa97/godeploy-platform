package main

 import (
     "fmt"
     "log"
     "net/url"
     "os"

     "github.com/gorilla/websocket"
 )

 func main() {
     if len(os.Args) < 2 {
         log.Fatal("Uso: go run test_logs.go <ID_DO_CONTAINER>")
     }
     containerID := os.Args[1]

     u := url.URL{Scheme: "ws", Host: "localhost:8081", Path: "/api/ws/logs", RawQuery: "container=" +
      containerID}
     fmt.Printf("Conectando em %s...\n", u.String())

     c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
     if err != nil {
         log.Fatal("Erro ao conectar:", err)
     }
     defer c.Close()

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