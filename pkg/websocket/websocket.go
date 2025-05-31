package websocket

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	wbc "github.com/peterouob/pionWebRTC/pkg/webrtc"
	"github.com/peterouob/pionWebRTC/signal"
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
	log.Println("[websocket] websocket upgrade success")

	client := &signal.ClientState{Conn: conn}

	defer func() {
		log.Printf("[websocket] Cleaning up for client: %s", conn.RemoteAddr().String())

		if client.PeerConnection != nil {
			log.Printf("[webrtc] Closing PeerConnection for client %s (Role: %s)", conn.RemoteAddr().String(), client.Role)
			if err := client.PeerConnection.Close(); err != nil {
				log.Printf("[webrtc] Error closing PeerConnection for %s: %v", conn.RemoteAddr().String(), err)
			}
		}

		if client.Role == "broadcaster" {
			log.Printf("[Manager] Client %s was a broadcaster. Attempting to clean broadcaster state.", conn.RemoteAddr().String())
			wbc.Manager.Clean(client.PeerConnection)
		}

	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[websocket] Unexpected close error from %s: %v", conn.RemoteAddr().String(), err)
			} else {
				log.Printf("[websocket] Client %s gracefully closed connection or read error: %v", conn.RemoteAddr().String(), err)
			}
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
