package websocket

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	wbc "github.com/peterouob/pionWebRTC/pkg/webrtc"
	"log"
	"net/http"
	"time"
)

var (
	upgrade = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	heartBeatTime = 30 * time.Second
)

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[websocket] upgrade err:", err.Error())
		return
	}
	log.Println("[websocket] websocket upgrade success")

	clientAddr := conn.RemoteAddr().String()
	client := wbc.NewClientState(conn, 10*time.Second, clientAddr)

	defer func() {
		log.Printf("[websocket] Cleaning up for client: %s", clientAddr)

		if err != nil {
			if errors.Is(client.Close(), wbc.ErrCloseClientState) {
				log.Println("[websocket] close client for webrtc peer err :", err.Error())
			}
			log.Println("[websocket] close client state err :", err.Error())
		}

	}()

	go heartBeat(client)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[websocket] Unexpected close error from %s: %v", clientAddr, err)
				if err := client.Close(); err != nil {
					log.Println("[websocket] clean err:", err.Error())
				}
			} else {
				log.Printf("[websocket] Client %s gracefully closed connection or read error: %v", clientAddr, err)
				if err := client.Close(); err != nil {
					log.Println("[websocket] clean err:", err.Error())
				}
			}
			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))

		var s wbc.Signal
		if err := json.Unmarshal(message, &s); err != nil {
			log.Println("[websocket] unmarshal err:", err.Error())
			continue
		}

		if err := wbc.HandleSignal(s, client); err != nil {
			if errors.Is(err, wbc.ErrContinue) {
				log.Printf("[websocket] continuing after recoverable error from %s", clientAddr)
				continue
			}
		}
	}
}

func heartBeat(c *wbc.ClientState) {
	ticker := time.NewTicker(heartBeatTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[websocket] ping failed for %s: %v", c.ClientAddr, err)
				return
			}
			c.UpdatePing()
			log.Printf("[websocket] ping sent to %s", c.ClientAddr)
		}
	}
}
