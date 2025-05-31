package wbc

import (
	"errors"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"log"
	"sync"
	"time"
)

type WebRTCManager struct {
	pionAPI        *webrtc.API
	pcConfig       webrtc.Configuration
	broadcastMU    sync.RWMutex
	broadcastPeer  *webrtc.PeerConnection
	broadcastTrack *webrtc.TrackLocalStaticRTP
}

var (
	Manager     *WebRTCManager
	ContinueErr = errors.New("continue")
)

const DefaultPLITimeout = 3000 * time.Millisecond

func InitManager() error {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		log.Println("[webrtc] mediaEngine register default codec err:", err.Error())
		return err
	}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, i); err != nil {
		log.Println("[webrtc] register default interceptors err:", err.Error())
		return err
	}

	intervalPli, err := intervalpli.NewReceiverInterceptor(
		intervalpli.GeneratorInterval(DefaultPLITimeout),
	)
	if err != nil {
		log.Println("[webrtc] new interval pli err:", err.Error())
		return err
	}

	i.Add(intervalPli)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(i))

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	Manager = &WebRTCManager{
		pionAPI:  api,
		pcConfig: config,
	}
	log.Println("[webrtc] WebRTC Manager initialized successfully")
	return nil
}

func (m *WebRTCManager) GetPionAPI() *webrtc.API {
	return m.pionAPI
}

func (m *WebRTCManager) GetPeerConnectionConfig() webrtc.Configuration {
	return m.pcConfig
}

func (m *WebRTCManager) SetBroadcaster(pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticRTP) {
	m.broadcastMU.Lock()
	defer m.broadcastMU.Unlock()
	m.broadcastPeer = pc
	m.broadcastTrack = track
	log.Println("[Manager] Broadcaster set")
}

func (m *WebRTCManager) Clean(pc *webrtc.PeerConnection) {
	m.broadcastMU.Lock()
	defer m.broadcastMU.Unlock()
	if m.broadcastPeer == pc {
		m.broadcastPeer = nil
		m.broadcastTrack = nil
		log.Println("[Manager] Broadcaster cleared")
	}
}

func (m *WebRTCManager) GetBroadcastTrack() (*webrtc.TrackLocalStaticRTP, bool) {
	m.broadcastMU.RLock()
	defer m.broadcastMU.RUnlock()
	if m.broadcastTrack == nil {
		return nil, false
	}
	return m.broadcastTrack, true
}

func (m *WebRTCManager) HasActiveBroadcast() bool {
	m.broadcastMU.RLock()
	defer m.broadcastMU.RUnlock()
	return m.broadcastPeer != nil && m.broadcastTrack != nil
}
