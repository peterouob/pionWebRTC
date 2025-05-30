package websocket

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/peterouob/pionWebRTC/pkg/signal"
	wbc "github.com/peterouob/pionWebRTC/pkg/webrtc"
	"log"
	"net/http"
)

var (
	upgrade = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[websocket] upgrade err:", err.Error())
		return
	}

	defer func() {
		_ = conn.Close()
	}()

	client := &signal.ClientState{Conn: conn}

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("[websocket] read message err:", err.Error())
			break
		}

		var s signal.Signal
		if err := json.Unmarshal(message, &s); err != nil {
			log.Println("[websocket] unmarshal err:", err.Error())
			continue
		}
		if errors.Is(wbc.HandleSignal(s, client), wbc.ContinueErr) {
			continue
		}

	}
}
