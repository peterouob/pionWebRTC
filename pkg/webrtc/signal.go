package wbc

import (
	"errors"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"log"
	"sync"
	"time"
)

var (
	ErrCloseClientState = errors.New("error in close client state whe peer connection not nil")
	ErrRole             = errors.New("error in role not known")
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
	PendingCandidates []webrtc.ICECandidateInit
	RemoteDescSet     bool

	Role        string
	ClientAddr  string
	CacheSDP    []*webrtc.SessionDescription
	Conn        *websocket.Conn
	mu          sync.Mutex
	IsActive    bool // for websocket not webrtc
	LastTime    time.Time
	expiredTime time.Duration
}

func NewClientState(conn *websocket.Conn, expiredTime time.Duration, addr string) *ClientState {
	return &ClientState{
		Conn:              conn,
		PendingCandidates: make([]webrtc.ICECandidateInit, 0),
		ClientAddr:        addr,
		IsActive:          true,
		LastTime:          time.Now(),
		expiredTime:       expiredTime,
	}
}

func (c *ClientState) UpdatePing() {
	c.LastTime = time.Now()
}

func (c *ClientState) IsExpired() bool {
	log.Printf("[websocket] websocket client state is expired %s\n", c.ClientAddr)
	return time.Since(c.LastTime) > c.expiredTime
}

func (c *ClientState) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.IsActive = false

	if c.PeerConnection != nil {
		switch c.Role {
		case "viewer":
			log.Printf("[Manager] Client %s was a viewer. Attempting to clean viewer state.", c.ClientAddr)
			c.PendingCandidates = nil
			if err := c.PeerConnection.Close(); err != nil {
				return ErrCloseClientState
			}
		case "broadcast":
			log.Printf("[Manager] Client %s was a broadcaster. Attempting to clean broadcaster state.", c.ClientAddr)
			c.PendingCandidates = nil
			Manager.Clean(c.PeerConnection)
		default:
			return ErrRole
		}
	}

	return c.Conn.Close()
}
