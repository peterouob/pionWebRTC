package wbc

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/peterouob/pionWebRTC/signal"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
)

func HandleSignal(s signal.Signal, client *signal.ClientState) error {
	if Manager == nil {
		log.Println("[FATAL] WebRTCManager not initialized!")
		return errors.New("WebRTCManager not initialized")
	}
	switch s.Type {
	case "join":
		client.Role = s.Role
		log.Printf("[webrtc] new client add ... [%s] from (%s)", client.Role, client.Conn.RemoteAddr().String())
		if client.Role == "broadcaster" {
			if Manager.HasActiveBroadcast() {
				return errors.New("already have broadcast")
			}
		}
		return nil

	case "offer":
		if s.SDP == nil {
			return ContinueErr
		}

		var err error
		client.PeerConnection, err = Manager.GetPionAPI().NewPeerConnection(Manager.GetPeerConnectionConfig())
		if err != nil {
			log.Println("[webrtc] new peer connection err:", err.Error())
			return err
		}

		client.PeerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil {
				return
			}
			candidate := c.ToJSON()
			payload, err := json.Marshal(signal.Signal{Type: "candidate", Candidate: &candidate})
			if err != nil {
				log.Println("[websocket] json marshal candidate error:", err)
				return
			}
			if err := client.Conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				log.Println("[websocket] send candidate message error:", err)
			}
		})

		if client.Role == "broadcaster" {
			Manager.broadcastMU.Lock()
			if Manager.broadcastPeer != nil {
				Manager.broadcastMU.Unlock()
				_ = client.PeerConnection.Close()
				return errors.New("[webrtc] already have broadcast")
			}
			client.PeerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				log.Printf("Broadcaster got track: %s, SSRC: %d", remoteTrack.ID(), remoteTrack.SSRC())
				localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
				if newTrackErr != nil {
					log.Println("[webrtc] new track local static rtp err:", newTrackErr.Error())
					Manager.Clean(client.PeerConnection)
					return
				}
				Manager.SetBroadcaster(client.PeerConnection, localTrack)

				rtpBuf := make([]byte, 1500)
				for {
					i, _, readErr := remoteTrack.Read(rtpBuf)
					if readErr != nil {
						if errors.Is(readErr, io.EOF) {
							log.Printf("[webrtc] broadcast track %s ended (EOF)\n", remoteTrack.ID())
						} else {
							log.Println("[webrtc] read remote track err:", readErr.Error())
						}
						Manager.broadcastMU.Lock()
						if Manager.broadcastTrack == localTrack {
							Manager.broadcastTrack = nil
							Manager.broadcastPeer = nil
							log.Println("[Manager] Broadcast track removed due to EOF/error")
						}
						Manager.broadcastMU.Unlock()
						return
					}
					if _, writeErr := localTrack.Write(rtpBuf[:i]); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
						log.Printf("[webrtc] write to broadcastTrack err: %s\n", writeErr.Error())
						return
					}
				}
			})
			Manager.broadcastMU.Unlock()

		}

		if client.Role == "viewer" {
			track, ok := Manager.GetBroadcastTrack()
			if !ok {
				_ = client.PeerConnection.Close()
				return ContinueErr
			}
			_, err = client.PeerConnection.AddTrack(track)
			if err != nil {
				log.Println("[webrtc] viewer failed to add track:", err)
				_ = client.PeerConnection.Close()
				return ContinueErr
			}
			log.Println("[webrtc] viewer added broadcast track to their peer connection")
		}

		//  Remote
		if err := client.PeerConnection.SetRemoteDescription(*s.SDP); err != nil {
			log.Printf("[webrtc] cannot set remote description for %s: %s\n", client.Role, err)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

		answer, err := client.PeerConnection.CreateAnswer(nil)
		if err != nil {
			log.Printf("[webrtc] cannot create answer for %s: %s\n", client.Role, err)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

		// Local
		if err := client.PeerConnection.SetLocalDescription(answer); err != nil {
			log.Printf("[webrtc] cannot set local description for %s: %s\n", client.Role, err)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

		client.RemoteDescSet = true

		for _, c := range client.PendingCandidates {
			if err := client.PeerConnection.AddICECandidate(c); err != nil {
				log.Printf("[webrtc] cannot add ICE Candidate for %s: %s", client.Role, err.Error())
			}
		}

		client.PendingCandidates = nil

		payload, err := json.Marshal(signal.Signal{Type: "answer", SDP: &answer})
		if err != nil {
			log.Println("[websocket] json marshal answer error:", err)
			return ContinueErr
		}
		if err := client.Conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			log.Println("[websocket] send answer message error:", err)
			return err
		}
		log.Println("[websocket] sent answer to", client.Role)
		return nil
	case "candidate":
		if s.Candidate == nil {
			log.Printf("[webrtc] received nil candidate from %s (%s)", client.Role, client.Conn.RemoteAddr().String())
			return ContinueErr
		}
		if client.PeerConnection == nil {
			log.Printf("[webrtc] received candidate for %s (%s) but peer connection not initialized", client.Role, client.Conn.RemoteAddr().String())
			return ContinueErr
		}

		if !client.RemoteDescSet {
			log.Printf("[webrtc] remote sdp not set yet caching ICE candidate for %s", client.Role)
			client.PendingCandidates = append(client.PendingCandidates, *s.Candidate)
			return nil
		}

		if err := client.PeerConnection.AddICECandidate(*s.Candidate); err != nil {
			log.Printf("[webrtc] cannot add ICE Candidate for %s: %s", client.Role, err.Error())
		}

		return nil

	default:
		log.Println("receive unknown s type:", s.Type)
		return nil
	}
}
