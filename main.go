package main

import (
	wbc "github.com/peterouob/pionWebRTC/pkg/webrtc"
	"github.com/peterouob/pionWebRTC/pkg/websocket"
	"github.com/rs/cors"
	"log"
	"net/http"
)

func main() {
	wbc.NewPion()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocket.HandleWebsocket)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	handler := cors.Default().Handler(mux)
	log.Println("Start server ...")
	log.Fatalln(http.ListenAndServe(":8081", handler))
}
