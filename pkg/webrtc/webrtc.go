package wbc

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/peterouob/pionWebRTC/pkg/signal"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
	"sync"
	"time"
)

var (
	pionAPI              *webrtc.API
	ContinueErr          = errors.New("continue")
	mu                   sync.RWMutex
	broadcastPC          *webrtc.PeerConnection
	broadcastTrack       *webrtc.TrackLocalStaticRTP
	peerConnectionConfig = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
)

const DefaultPLITimeout = 3000 * time.Microsecond

func NewPion() {
	media := &webrtc.MediaEngine{}
	if err := media.RegisterDefaultCodecs(); err != nil {
		log.Println("[webrtc] media register default codec err:", err.Error())
	}

	interceptors := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(media, interceptors); err != nil {
		log.Println("[webrtc] register default interceptors err:", err.Error())
	}

	intervalPli, err := intervalpli.NewReceiverInterceptor(
		intervalpli.GeneratorInterval(DefaultPLITimeout))
	if err != nil {
		log.Println("[webrtc] new interval pli err:", err.Error())
	}

	interceptors.Add(intervalPli)

	pionAPI = webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(interceptors))
	log.Println("[webrtc] create pion api ...")
}

func HandleSignal(s signal.Signal, client *signal.ClientState) error {
	switch s.Type {
	case "join":
		client.Role = s.Role
		log.Printf("[webrtc] new client add ... [%s] from (%s)", client.Role, client.Conn.RemoteAddr().String())
		if client.Role == "broadcaster" {
			mu.Lock()
			if broadcastPC != nil {
				sendErrMessageToClient("[webrtc] already have broadcast", client.Conn)
				mu.Unlock()
				return errors.New("already have broadcast")
			}
			mu.Unlock()
		}
		return nil

	case "offer":
		if s.SDP == nil {
			sendErrMessageToClient("[webrtc] offer sdp is nil", client.Conn)
			return ContinueErr
		}

		var err error
		client.PeerConnection, err = pionAPI.NewPeerConnection(peerConnectionConfig)
		if err != nil {
			log.Println("[webrtc] new peer connection err:", err.Error())
			sendErrMessageToClient("[webrtc] failed to create peer connection", client.Conn)
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
			mu.Lock()
			if broadcastPC != nil {
				sendErrMessageToClient("[webrtc] already have broadcast", client.Conn)
				mu.Unlock()
				_ = client.PeerConnection.Close()
				return errors.New("[webrtc] already have broadcast")
			}
			broadcastPC = client.PeerConnection
			mu.Unlock()

			client.PeerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				log.Printf("Broadcaster got track: %s, SSRC: %d", remoteTrack.ID(), remoteTrack.SSRC())
				localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
				if newTrackErr != nil {
					log.Println("[webrtc] new track local static rtp err:", newTrackErr.Error())
					return
				}
				mu.Lock()
				broadcastTrack = localTrack
				mu.Unlock()

				rtpBuf := make([]byte, 1500)
				for {
					i, _, readErr := remoteTrack.Read(rtpBuf)
					if readErr != nil {
						if errors.Is(readErr, io.EOF) {
							log.Printf("[webrtc] broadcast track %s ended (EOF)\n", remoteTrack.ID())
						} else {
							log.Println("[webrtc] read remote track err:", readErr.Error())
						}
						mu.Lock()
						if broadcastTrack == localTrack {
							broadcastTrack = nil
						}
						mu.Unlock()
						return
					}
					if _, writeErr := localTrack.Write(rtpBuf[:i]); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
						log.Printf("[webrtc] write to broadcastTrack err: %s\n", writeErr.Error())
						return
					}
				}
			})

		} else if client.Role == "viewer" {
			mu.RLock()
			if broadcastTrack == nil {
				sendErrMessageToClient("[webrtc] broadcast not ready yet", client.Conn)
				mu.RUnlock()
				_ = client.PeerConnection.Close()
				return ContinueErr
			}
			_, err = client.PeerConnection.AddTrack(broadcastTrack)
			mu.RUnlock()
			if err != nil {
				log.Println("[webrtc] viewer failed to add track:", err)
				sendErrMessageToClient("[webrtc] failed to add broadcast track", client.Conn)
				_ = client.PeerConnection.Close()
				return ContinueErr
			}
			log.Println("[webrtc] viewer added broadcast track to their peer connection")
		}

		if err := client.PeerConnection.SetRemoteDescription(*s.SDP); err != nil {
			log.Printf("[webrtc] cannot set remote description for %s: %s\n", client.Role, err)
			sendErrMessageToClient("[webrtc] failed to set remote description", client.Conn)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

		answer, err := client.PeerConnection.CreateAnswer(nil)
		if err != nil {
			log.Printf("[webrtc] cannot create answer for %s: %s\n", client.Role, err)
			sendErrMessageToClient("[webrtc] failed to create answer", client.Conn)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

		if err := client.PeerConnection.SetLocalDescription(answer); err != nil {
			log.Printf("[webrtc] cannot set local description for %s: %s\n", client.Role, err)
			sendErrMessageToClient("[webrtc] failed to set local description", client.Conn)
			_ = client.PeerConnection.Close()
			return ContinueErr
		}

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
			log.Printf("Received nil candidate from %s (%s)", client.Role, client.Conn.RemoteAddr().String())
			return ContinueErr
		}
		if client.PeerConnection == nil {
			log.Printf("Received candidate for %s (%s) but peer connection not initialized", client.Role, client.Conn.RemoteAddr().String())
			sendErrMessageToClient("[webrtc] peer connection not initialized for candidate", client.Conn)
			return ContinueErr
		}
		if err := client.PeerConnection.AddICECandidate(*s.Candidate); err != nil {
			log.Printf("Cannot add ICE Candidate for %s: %s", client.Role, err.Error())
		}
		return nil

	default:
		log.Println("receive unknown s type:", s.Type)
		return nil
	}
}

func sendErrMessageToClient(msg string, conn *websocket.Conn) {
	payload, err := json.Marshal(signal.Signal{Type: "error", Message: msg})
	if err != nil {
		log.Println("[websocket] json marshal error message failed:", err)
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		log.Println("[websocket] send error message failed:", err)
	}
}
