package agent

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type iceServer = webrtc.ICEServer

type session struct {
	agent *Agent
	guest string
	pc    *webrtc.PeerConnection

	control          *webrtc.DataChannel
	motion           *webrtc.DataChannel
	clipboardChannel *webrtc.DataChannel
	clipState        clipboardSessionState

	fileChannel     *webrtc.DataChannel
	feedbackChannel *webrtc.DataChannel
	rpcMu           sync.Mutex
	pendingRPC      map[int64]chan map[string]any
	nextRPCID       int64
	fileMu          sync.Mutex
	uploads         map[uint32]string
	incoming        map[uint32]*incomingUpload
	cancelled       map[uint32]bool
	nextFileID      uint32
	fileDone        chan struct{}
	fileCloseOnce   sync.Once

	mu        sync.Mutex
	pending   []webrtc.ICECandidateInit
	remoteSet bool

	errMu     sync.Mutex
	lastErr   time.Time
	closeOnce sync.Once
}

type mouseMessage struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	S    int     `json:"s"`
	Flag int     `json:"flag"`
}

type keyboardMessage struct {
	KeyValue int `json:"key_value"`
	Flag     int `json:"flag"`
}

type videoSettingsMessage struct {
	Quality    int    `json:"quality"`
	FrameRate  int    `json:"frame_rate"`
	Preference int    `json:"preference"`
	RequestID  uint32 `json:"request_id"`
}

type controlMessage struct {
	Type          int                   `json:"type"`
	Mouse         *mouseMessage         `json:"mouse"`
	Keyboard      *keyboardMessage      `json:"keyboard"`
	VideoSettings *videoSettingsMessage `json:"video_settings"`
	DisplayID     *int                  `json:"display_id"`
}

func newSession(a *Agent, guest string) (*session, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: a.rtcConfiguration()})
	if err != nil {
		return nil, err
	}
	s := &session{
		agent: a, guest: guest, pc: pc,
		uploads: map[uint32]string{}, incoming: map[uint32]*incomingUpload{},
		cancelled: map[uint32]bool{}, fileDone: make(chan struct{}),
		pendingRPC: map[int64]chan map[string]any{},
	}

	ordered, unordered := true, false
	zeroRetransmits := uint16(0)
	protocol := "crossdesk-control-v1"
	control, err := pc.CreateDataChannel("data-stream", &webrtc.DataChannelInit{
		Ordered: &ordered, Protocol: &protocol,
	})
	if err != nil {
		pc.Close()
		return nil, err
	}
	motion, err := pc.CreateDataChannel("crossdesk-input-v1", &webrtc.DataChannelInit{
		Ordered: &unordered, MaxRetransmits: &zeroRetransmits,
	})
	if err != nil {
		pc.Close()
		return nil, err
	}
	s.control, s.motion = control, motion
	control.OnOpen(func() {
		a.log.Printf("📺 画面通道已建立: %s", guest)
		displays, active := a.displaySnapshot()
		hostInfo, _ := json.Marshal(map[string]any{
			"type": 3,
			"host_info": map[string]any{
				"agent": "iflygo-go", "platform": runtime.GOOS,
				"supports_video_settings": true, "supports_clipboard": a.clipboard != nil,
				"supports_file_browser": a.files != nil, "supports_web_file_browser": a.files != nil,
				"host_name": func() string {
					if a.files != nil {
						return a.files.hostname
					}
					return ""
				}(),
				"displays": displays, "active_display": active,
			},
		})
		_ = control.SendText(string(hostInfo))
	})
	control.OnMessage(s.onMessage)
	motion.OnMessage(s.onMessage)
	if a.clipboard != nil {
		clip, err := pc.CreateDataChannel("web-clipboard", &webrtc.DataChannelInit{Ordered: &ordered})
		if err == nil {
			s.clipboardChannel = clip
			clip.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					s.onClipboardMessage(m.Data)
				}
			})
			clip.OnClose(func() { s.closeClipboard() })
		}
	}
	if a.files != nil {
		proto := fileProtocol
		file, err := pc.CreateDataChannel("file", &webrtc.DataChannelInit{Ordered: &ordered, Protocol: &proto})
		if err == nil {
			s.fileChannel = file
			file.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					s.onFileChunk(m.Data)
				}
			})
		}
		feedback, err := pc.CreateDataChannel("file_feedback", &webrtc.DataChannelInit{Ordered: &ordered, Protocol: &proto})
		if err == nil {
			s.feedbackChannel = feedback
			feedback.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					s.onFileAck(m.Data)
				}
			})
		}
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := map[string]any{
			"type": "new_candidate_mid", "transmission_id": a.deviceID, "user_id": a.deviceID,
			"remote_user_id": guest, "candidate": init.Candidate,
		}
		if init.SDPMid != nil {
			msg["mid"] = *init.SDPMid
		}
		if init.SDPMLineIndex != nil {
			msg["sdpMLineIndex"] = *init.SDPMLineIndex
		}
		a.sendAsync(msg)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			s.close()
		case webrtc.PeerConnectionStateDisconnected:
			go func() {
				time.Sleep(30 * time.Second)
				if s.pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					s.close()
				}
			}()
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		s.close()
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		_ = pc.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.sendJSON(ctx, map[string]any{
		"type": "offer", "transmission_id": a.deviceID, "user_id": a.deviceID,
		"remote_user_id": guest, "sdp": pc.LocalDescription().SDP,
	}); err != nil {
		_ = pc.Close()
		return nil, err
	}
	return s, nil
}

func (s *session) handleAnswer(msg map[string]any) {
	sdp, _ := msg["sdp"].(string)
	if sdp == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		s.logError("设置远端描述失败: %v", err)
		return
	}
	s.remoteSet = true
	pending := s.pending
	s.pending = nil
	for _, cand := range pending {
		if err := s.pc.AddICECandidate(cand); err != nil {
			s.logError("添加 ICE 候选失败: %v", err)
		}
	}
}

func (s *session) handleCandidate(msg map[string]any) {
	candidate, _ := msg["candidate"].(string)
	if candidate == "" {
		candidate, _ = msg["sdp"].(string)
	}
	if candidate == "" {
		return
	}
	init := webrtc.ICECandidateInit{Candidate: candidate}
	if mid, ok := msg["mid"].(string); ok && mid != "" {
		init.SDPMid = &mid
	}
	if v, ok := msg["sdpMLineIndex"].(float64); ok {
		idx := uint16(v)
		init.SDPMLineIndex = &idx
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.remoteSet {
		s.pending = append(s.pending, init)
		return
	}
	if err := s.pc.AddICECandidate(init); err != nil {
		s.logError("添加 ICE 候选失败: %v", err)
	}
}

func (s *session) onMessage(msg webrtc.DataChannelMessage) {
	if !msg.IsString {
		return
	}
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if json.Unmarshal(msg.Data, &probe) == nil && string(probe.Type) == `"file_browser"` {
		if s.agent.files != nil {
			s.agent.files.handleRPC(s, msg.Data)
		}
		return
	}
	var action controlMessage
	if json.Unmarshal(msg.Data, &action) != nil {
		return
	}
	switch action.Type {
	case 0:
		if action.Mouse == nil {
			return
		}
		if err := s.agent.input.Mouse(action.Mouse.X, action.Mouse.Y, action.Mouse.Flag, action.Mouse.S); err != nil {
			if action.Mouse.Flag != MouseMove {
				s.logError("注入鼠标事件失败: %v", err)
			}
		}
	case 1:
		if action.Keyboard == nil {
			return
		}
		if err := s.agent.input.Key(action.Keyboard.KeyValue, action.Keyboard.Flag == 0); err != nil {
			s.logError("注入键盘事件失败: %v", err)
		}
	case 11:
		s.applyVideoSettings(action.VideoSettings)
	case 4:
		if action.DisplayID != nil {
			s.agent.setDisplay(*action.DisplayID)
		}
	}
}

// applyVideoSettings 处理 Web 客户端的画质/帧率协商（type 11 -> type 12）。
func (s *session) applyVideoSettings(req *videoSettingsMessage) {
	if req == nil || req.Quality < 0 || req.Quality > 2 ||
		(req.FrameRate != 30 && req.FrameRate != 60) ||
		req.Preference < 0 || req.Preference > 2 {
		return
	}
	quality := [...]int32{45, 62, 78}[req.Quality]
	s.agent.videoQuality.Store(quality)
	s.agent.videoFPS.Store(int32(req.FrameRate))
	s.agent.videoPref.Store(int32(req.Preference))
	s.agent.log.Printf("🎛️ 画面设置: 质量=%d 帧率=%d 偏好=%d", req.Quality, req.FrameRate, req.Preference)
	reply, err := json.Marshal(map[string]any{
		"type": 12,
		"video_settings": map[string]any{
			"accepted": true, "quality": req.Quality, "frame_rate": req.FrameRate,
			"preference": req.Preference, "request_id": req.RequestID,
		},
	})
	if err != nil || s.control == nil {
		return
	}
	_ = s.control.SendText(string(reply))
}

func (s *session) send(chunks [][]byte) {
	dc := s.control
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	if dc.BufferedAmount() > 1<<20 {
		return
	}
	for _, chunk := range chunks {
		if err := dc.Send(chunk); err != nil {
			return
		}
	}
}

func (s *session) logError(format string, args ...any) {
	s.errMu.Lock()
	now := time.Now()
	allow := now.Sub(s.lastErr) > 3*time.Second
	if allow {
		s.lastErr = now
	}
	s.errMu.Unlock()
	if allow {
		s.agent.log.Printf("⚠️ ["+s.guest+"] "+format, args...)
	}
}

func (s *session) sendControlJSON(v any) bool {
	if s.control == nil {
		return false
	}
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return s.control.SendText(string(data)) == nil
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		s.closeClipboard()
		s.fileCloseOnce.Do(func() { close(s.fileDone) })
		s.closeFiles()
		_ = s.pc.Close()
		s.agent.dropSession(s)
	})
}
