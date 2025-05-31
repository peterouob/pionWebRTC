package signal

import (
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

type Signal struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"`
	SDP       *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Message   string                     `json:"message,omitempty"`
}
type ClientState struct {
	PeerConnection    *webrtc.PeerConnection
	Role              string
	Conn              *websocket.Conn
	PendingCandidates []webrtc.ICECandidateInit
	RemoteDescSet     bool
}
