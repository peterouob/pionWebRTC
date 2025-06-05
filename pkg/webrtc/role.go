package wbc

import (
	"errors"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
)

func setupBroadcaster(client *ClientState) error {
	Manager.broadcastMU.Lock()
	if Manager.broadcastPeer != nil {
		Manager.broadcastMU.Unlock()
		_ = client.PeerConnection.Close()
		return ErrMoreBroadcast
	}
	client.PeerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Broadcaster got track: %s, SSRC: %d", remoteTrack.ID(), remoteTrack.SSRC())
		localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			remoteTrack.ID(),
			remoteTrack.StreamID())

		if newTrackErr != nil {
			log.Println("[webrtc][offer] new track local static rtp err:", newTrackErr.Error())
			Manager.Clean(client.PeerConnection)
			return
		}
		Manager.SetBroadcaster(client.PeerConnection, localTrack)

		rtpBuf := make([]byte, 1500)
		for {
			i, _, readErr := remoteTrack.Read(rtpBuf)
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					log.Printf("[webrtc][offer] broadcast track %s ended (EOF)\n", remoteTrack.ID())
				} else {
					log.Println("[webrtc][offer] read remote track err:", readErr.Error())
				}
				Manager.broadcastMU.Lock()
				if Manager.broadcastTrack == localTrack {
					Manager.broadcastTrack = nil
					Manager.broadcastPeer = nil
					log.Println("[Manager][offer] Broadcast track removed due to EOF/error")
				}
				Manager.broadcastMU.Unlock()
				return
			}
			if _, writeErr := localTrack.Write(rtpBuf[:i]); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
				log.Printf("[webrtc][offer] write to broadcastTrack err: %s\n", writeErr.Error())
				return
			}
		}
	})
	Manager.broadcastMU.Unlock()
	return nil
}

func setupViewer(client *ClientState) error {
	track, ok := Manager.GetBroadcastTrack()
	if !ok {
		_ = client.PeerConnection.Close()
		return ErrContinue
	}
	rtpSender, err := client.PeerConnection.AddTrack(track)
	if err != nil {
		log.Println("[webrtc][offer] viewer failed to add track:", err)
		_ = client.PeerConnection.Close()
		return ErrContinue
	}
	log.Println("[webrtc][offer] viewer added broadcast track to their peer connection")
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(rtcpBuf); err != nil {
				if !errors.Is(err, io.EOF) {
					log.Printf("[webrtc][viewer] RTCP read error: %v", err)
				}
				return
			}
		}
	}()

	return nil
}
