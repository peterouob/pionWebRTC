package wbc

import (
	"errors"
	"log"
)

func HandleSignal(s Signal, client *ClientState) error {
	if Manager == nil {
		log.Println("[FATAL] WebRTCManager not initialized!")
		return errors.New("WebRTCManager not initialized")
	}

	if s.SDP == nil && (s.Type == "offer" || s.Type == "answer") {
		log.Printf("[webrtc] receive signal but SDP is nil for type %s", s.Type)
		return ErrContinue // retry
	} else if s.SDP != nil {
		log.Printf("[webrtc] receive signal with SDP type: %s", s.SDP.Type)
	}

	switch s.Type {
	case "join":
		if err := checkBroadcast(s, client); err != nil {
			return err
		}
	case "offer":
		if s.Candidate == nil {
			if err := setupICE(client); err != nil {
				return err
			}
		}
		if err := handleOffer(s, client); err != nil {
			return err
		}

	case "candidate":
		if s.Candidate == nil {
			if err := setupICE(client); err != nil {
				return err
			}
		}
		if err := handleCandidate(s, client); err != nil {
			return err
		}
	default:
		log.Println("receive unknown s type:", s.Type)
		return nil
	}
	return nil
}

func handleOffer(s Signal, client *ClientState) error {
	log.Println("[webrtc] receive offer")

	switch client.Role {
	case "broadcaster":
		if err := setupBroadcaster(client); err != nil {
			log.Printf("[webrtc][offer] failed to setup broadcaster: %v", err)
			_ = client.Close()
			return ErrContinue
		}
	case "viewer":
		if err := setupViewer(client); err != nil {
			log.Printf("[webrtc][offer] failed to setup viewer: %v", err)
			_ = client.Close()
			return ErrContinue
		}
	}
	if err := handleSDP(s, client); err != nil {
		log.Println("[webrtc][candidate] failed to handle sdp:", err)
		return err
	}
	return nil
}

func handleCandidate(s Signal, client *ClientState) error {
	log.Println("[webrtc] receive candidate")
	if s.Candidate == nil {
		log.Printf("[webrtc][candidate] received nil candidate from %s (%s)", client.Role, client.Conn.RemoteAddr().String())
		return ErrContinue
	}
	if client.PeerConnection == nil {
		log.Printf("[webrtc][candidate] received candidate for %s (%s) but peer connection not initialized", client.Role, client.Conn.RemoteAddr().String())
		return ErrContinue
	}

	if !client.RemoteDescSet {
		log.Printf("[webrtc][candidate] remote sdp not set yet caching ICE candidate for %s", client.Role)
		client.PendingCandidates = append(client.PendingCandidates, *s.Candidate)
		if err := handleSDP(s, client); err != nil {
			log.Println("[webrtc][candidate] failed to handle sdp:", err)
			return err
		}
		return nil
	}

	if err := client.PeerConnection.AddICECandidate(*s.Candidate); err != nil {
		log.Printf("[webrtc][candidate] cannot add ICE Candidate for %s: %s", client.Role, err.Error())
	}

	return nil
}
