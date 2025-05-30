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
	PeerConnection *webrtc.PeerConnection
	Role           string
	Conn           *websocket.Conn
	// 如果此結構體會被多個 goroutine 存取，可能需要一個互斥鎖，
	// 儘管在這個每個連線一個處理器的模型中，如果 PC 和 Role
	// 只被客戶端自己的處理器 goroutine 修改，則可能不是嚴格必要的。
}
