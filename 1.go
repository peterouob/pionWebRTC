package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"github.com/rs/cors"
)

// Signal 用於在客戶端和伺服器之間傳遞信令訊息
type Signal struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"` // 僅用於 "join" 類型
	SDP       *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit   `json:"candidate,omitempty"`
	Message   string                     `json:"message,omitempty"` // 用於錯誤或一般訊息
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true }, // 允許所有來源 (僅供演示)
	}
	peerConnectionConfig = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
	pionAPI *webrtc.API

	broadcasterTrackLock      sync.RWMutex
	broadcasterTrack          *webrtc.TrackLocalStaticRTP
	broadcasterPeerConnection *webrtc.PeerConnection // 儲存廣播者的 PeerConnection 以便管理
)

func main() {
	// --- Pion WebRTC API 初始化 ---
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		log.Fatal(err)
	}
	interceptorRegistry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		log.Fatal(err)
	}
	// 為從廣播者接收的軌道註冊 PLI 攔截器 (伺服器請求關鍵影格)
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor(intervalpli.GeneratorInterval(DefaultPLIInterval))
	if err != nil {
		log.Fatal(err)
	}
	interceptorRegistry.Add(intervalPliFactory)

	pionAPI = webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(interceptorRegistry))
	// --- Pion WebRTC API 初始化結束 ---
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWebSocket)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html") // 提供 HTML 檔案
	})
	log.Println("Server starting ...")
	handler := cors.Default().Handler(mux)

	log.Fatal(http.ListenAndServe(":8082", handler))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("無法升級 WebSocket 連接: %v", err)
		return
	}
	defer conn.Close()

	var currentPC *webrtc.PeerConnection
	var currentRole string // "broadcaster" 或 "viewer"

	// WebSocket 連接的讀取迴圈
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("讀取 WebSocket 訊息錯誤: %v", err)
			break // 客戶端斷開連接或發生錯誤
		}

		var signal Signal
		if err := json.Unmarshal(message, &signal); err != nil {
			log.Printf("無法解析 JSON 訊息: %v", err)
			continue
		}

		switch signal.Type {
		case "join":
			currentRole = signal.Role
			log.Printf("客戶端加入，角色: %s (來自 %s)", currentRole, conn.RemoteAddr())

			if currentRole == "broadcaster" {
				broadcasterTrackLock.Lock()
				if broadcasterPeerConnection != nil {
					log.Println("已有廣播者存在，拒絕新的廣播者。")
					payload, _ := json.Marshal(Signal{Type: "error", Message: "已有廣播者存在"})
					conn.WriteMessage(websocket.TextMessage, payload)
					broadcasterTrackLock.Unlock()
					return // 結束此 handler
				}
				// 在收到 offer 時才建立 PC
				broadcasterTrackLock.Unlock()
			}

		case "offer":
			log.Println("currentRole = ", currentRole)
			log.Printf("收到來自 %s (%s) 的 Offer", currentRole, conn.RemoteAddr())
			if signal.SDP == nil {
				log.Println("Offer SDP 為空")
				continue
			}
			log.Printf("原始 SDP Offer:\n%s", signal.SDP.SDP)
			currentPC, err = pionAPI.NewPeerConnection(peerConnectionConfig)
			if err != nil {
				log.Printf("無法為 %s 建立 PeerConnection: %v", currentRole, err)
				continue
			}
			// 設定 ICE Candidate 處理器
			currentPC.OnICECandidate(func(candidate *webrtc.ICECandidate) {
				if candidate == nil {
					return
				}
				candidateJSON := candidate.ToJSON()
				payload, err := json.Marshal(Signal{Type: "candidate", Candidate: &candidateJSON})
				if err != nil {
					log.Printf("無法序列化 Candidate: %v", err)
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					log.Printf("無法發送 Candidate 給 %s: %v", currentRole, err)
				}
			})

			if currentRole == "broadcaster" {
				broadcasterTrackLock.Lock()
				// 再次檢查，因為 join 和 offer 之間可能有競爭
				if broadcasterPeerConnection != nil {
					log.Println("已有廣播者存在 (offer 階段檢查)，拒絕新的廣播者。")
					payload, _ := json.Marshal(Signal{Type: "error", Message: "已有廣播者存在"})
					conn.WriteMessage(websocket.TextMessage, payload)
					broadcasterTrackLock.Unlock()
					currentPC.Close() // 關閉剛建立的 PC
					return            // 結束此 handler
				}
				broadcasterPeerConnection = currentPC // 儲存廣播者的 PC
				broadcasterTrackLock.Unlock()

				// 設定 OnTrack 以接收廣播者的視訊軌道
				currentPC.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
					log.Printf("收到廣播者軌道: ID=%s, StreamID=%s, Kind=%s, Codec=%s",
						remoteTrack.ID(), remoteTrack.StreamID(), remoteTrack.Kind(), remoteTrack.Codec().MimeType)

					// 建立一個本地軌道用於轉發
					localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
					if newTrackErr != nil {
						log.Printf("無法為廣播者建立本地軌道: %v", newTrackErr)
						return
					}

					broadcasterTrackLock.Lock()
					broadcasterTrack = localTrack
					broadcasterTrackLock.Unlock()
					log.Println("廣播者本地軌道已設定")

					// 從遠端軌道讀取 RTP 封包並寫入本地軌道
					rtpBuf := make([]byte, 1500)
					for {
						i, _, readErr := remoteTrack.Read(rtpBuf)
						if readErr != nil {
							if errors.Is(readErr, io.EOF) {
								log.Printf("廣播者軌道 %s 已結束 (EOF)", remoteTrack.ID())
							} else {
								log.Printf("讀取廣播者軌道 %s 錯誤: %v", remoteTrack.ID(), readErr)
							}
							// 清理廣播者資源
							broadcasterTrackLock.Lock()
							broadcasterTrack = nil
							// broadcasterPeerConnection = nil // 不要在這裡設為 nil，讓 defer currentPC.Close() 和外層的 cleanup 處理
							broadcasterTrackLock.Unlock()
							return
						}
						if _, writeErr := localTrack.Write(rtpBuf[:i]); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
							log.Printf("寫入本地軌道錯誤: %v", writeErr)
							return
						}
					}
				})

			}
			if currentRole == "viewer" {
				broadcasterTrackLock.RLock()
				if broadcasterTrack == nil {
					log.Printf("觀看者 %s 連接，但沒有可用的廣播軌道", conn.RemoteAddr())
					payload, _ := json.Marshal(Signal{Type: "error", Message: "目前沒有廣播"})
					conn.WriteMessage(websocket.TextMessage, payload)
					broadcasterTrackLock.RUnlock()
					currentPC.Close() // 關閉剛建立的 PC
					continue          // 繼續迴圈等待下一個訊息，或客戶端會斷開
				}
				// 將廣播者的軌道加入到觀看者的 PeerConnection
				_, err = currentPC.AddTrack(broadcasterTrack)
				broadcasterTrackLock.RUnlock() // 盡快釋放讀鎖
				if err != nil {
					log.Printf("無法將廣播軌道加入觀看者 %s 的 PeerConnection: %v", conn.RemoteAddr(), err)
					currentPC.Close()
					continue
				}
				log.Printf("已將廣播軌道提供給觀看者 %s", conn.RemoteAddr())
			}

			// 設定遠端描述 (Offer)
			if err := currentPC.SetRemoteDescription(*signal.SDP); err != nil {
				log.Printf("無法為 %s (%s) 設定遠端描述: %v", currentRole, conn.RemoteAddr(), err)
				currentPC.Close()
				continue
			}

			// 建立 Answer
			answer, err := currentPC.CreateAnswer(nil)
			if err != nil {
				log.Printf("無法為 %s (%s) 建立 Answer: %v", currentRole, conn.RemoteAddr(), err)
				currentPC.Close()
				continue
			}
			// 設定本地描述 (Answer)
			if err := currentPC.SetLocalDescription(answer); err != nil {
				log.Printf("無法為 %s (%s) 設定本地描述: %v", currentRole, conn.RemoteAddr(), err)
				currentPC.Close()
				continue
			}

			// 發送 Answer 給客戶端
			payload, err := json.Marshal(Signal{Type: "answer", SDP: &answer})
			if err != nil {
				log.Printf("無法序列化 Answer: %v", err)
				currentPC.Close()
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				log.Printf("無法發送 Answer 給 %s (%s): %v", currentRole, conn.RemoteAddr(), err)
			}
			log.Printf("已發送 Answer 給 %s (%s)", currentRole, conn.RemoteAddr())

		case "candidate":
			if signal.Candidate == nil {
				log.Printf("收到來自 %s (%s) 的空 Candidate", currentRole, conn.RemoteAddr())
				continue
			}
			if currentPC == nil {
				log.Printf("收到來自 %s (%s) 的 Candidate，但 PeerConnection 未初始化", currentRole, conn.RemoteAddr())
				continue
			}
			if err := currentPC.AddICECandidate(*signal.Candidate); err != nil {
				log.Printf("無法為 %s (%s) 加入 ICE Candidate: %v", currentRole, conn.RemoteAddr(), err)
			}
			// log.Printf("已為 %s (%s) 加入 ICE Candidate", currentRole, conn.RemoteAddr())

		default:
			log.Printf("收到未知信令類型: %s 來自 %s (%s)", signal.Type, currentRole, conn.RemoteAddr())
		}
	}

	// --- 清理 ---
	log.Printf("客戶端 %s (%s) 已斷開連接", currentRole, conn.RemoteAddr())
	if currentPC != nil {
		currentPC.Close()
	}

	if currentRole == "broadcaster" {
		broadcasterTrackLock.Lock()
		// 確保是同一個 PeerConnection 實例斷開
		if currentPC == broadcasterPeerConnection {
			log.Println("廣播者已斷開，正在清理廣播資源...")
			broadcasterTrack = nil
			broadcasterPeerConnection = nil
			// 此處可以考慮通知所有觀看者廣播已結束
		}
		broadcasterTrackLock.Unlock()
	}
}

const (
	// DefaultPLIInterval is the default interval for sending PLI requests.
	// Pion's default is 2s, which might be too frequent for some networks.
	DefaultPLIInterval = 3000 * time.Millisecond
)
