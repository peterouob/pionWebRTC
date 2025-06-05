package wbc

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"log"
)

func checkBroadcast(s Signal, client *ClientState) error {
	log.Println("[webrtc] receive join")

	client.Role = s.Role
	log.Printf("[webrtc][join] new client add ... [%s] from (%s)", client.Role, client.Conn.RemoteAddr().String())
	if client.Role == "broadcaster" {
		if Manager.HasActiveBroadcast() {
			return ErrMoreBroadcast
		}
	}
	return nil
}

func setupICE(client *ClientState) error {
	var err error
	client.PeerConnection, err = Manager.GetPionAPI().NewPeerConnection(Manager.GetPeerConnectionConfig())
	if err != nil {
		log.Println("[webrtc][offer] new peer connection err:", err.Error())
		return err
	}
	client.PeerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		payload, err := json.Marshal(Signal{Type: "candidate", Candidate: &candidate})
		if err != nil {
			log.Println("[websocket][offer] json marshal candidate error:", err)
			return
		}
		if err := client.Conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			log.Println("[websocket][offer] send candidate message error:", err)
		}
	})

	client.PeerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[webrtc][%s] connection state changed: %s", client.Role, state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			log.Printf("[webrtc][%s] connection failed/closed, cleaning up", client.Role)
			_ = client.Close()
		}
	})
	return nil
}

func handleSDP(s Signal, client *ClientState) error {
	if s.SDP == nil && len(client.CacheSDP) == 0 {
		log.Printf("[webrtc][offer] received nil SDP from %s ", client.Role)
		return ErrContinue
	}

	client.CacheSDP = append(client.CacheSDP, s.SDP)

	if s.SDP == nil && len(client.CacheSDP) != 0 {
		for _, sdp := range client.CacheSDP {
			if err := client.PeerConnection.SetLocalDescription(*sdp); err != nil {
				log.Printf("[webrtc][offer] cannot set local description for %s: %s\n", client.Role, err)
				_ = client.PeerConnection.Close()
				return ErrContinue
			}
		}
	}

	if err := client.PeerConnection.SetRemoteDescription(*s.SDP); err != nil {
		log.Printf("[webrtc][offer] cannot set remote description for %s: %s\n", client.Role, err)
		_ = client.PeerConnection.Close()
		return ErrContinue
	}

	answer, err := client.PeerConnection.CreateAnswer(nil)
	if err != nil {
		log.Printf("[webrtc][offer] cannot create answer for %s: %s\n", client.Role, err)
		_ = client.PeerConnection.Close()
		return ErrContinue
	}

	if err := client.PeerConnection.SetLocalDescription(answer); err != nil {
		log.Printf("[webrtc][offer] cannot set local description for %s: %s\n", client.Role, err)
		_ = client.PeerConnection.Close()
		return ErrContinue
	}

	client.RemoteDescSet = true

	for _, c := range client.PendingCandidates {
		if err := client.PeerConnection.AddICECandidate(c); err != nil {
			log.Printf("[webrtc][offer] cannot add ICE Candidate for %s: %s", client.Role, err.Error())
		}
	}

	client.PendingCandidates = nil

	payload, err := json.Marshal(Signal{Type: "answer", SDP: &answer})
	if err != nil {
		log.Println("[websocket][offer] json marshal answer error:", err)
		return ErrContinue
	}
	if err := client.Conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		log.Println("[websocket][offer] send answer message error:", err)
		return err
	}
	log.Println("[websocket][offer] sent answer to", client.Role)
	return nil
}
