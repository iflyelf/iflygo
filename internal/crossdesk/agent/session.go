package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

type iceServer = webrtc.ICEServer

type session struct {
	agent *Agent
	guest string
	pc    *webrtc.PeerConnection

	control *webrtc.DataChannel
	motion  *webrtc.DataChannel

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

type controlMessage struct {
	Type     int              `json:"type"`
	Mouse    *mouseMessage    `json:"mouse"`
	Keyboard *keyboardMessage `json:"keyboard"`
}

func newSession(a *Agent, guest string) (*session, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: a.rtcConfiguration()})
	if err != nil {
		return nil, err
	}
	s := &session{agent: a, guest: guest, pc: pc}

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
	control.OnOpen(func() { a.log.Printf("📺 画面通道已建立: %s", guest) })
	control.OnMessage(s.onMessage)
	motion.OnMessage(s.onMessage)

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
	}
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

func (s *session) close() {
	s.closeOnce.Do(func() {
		_ = s.pc.Close()
		s.agent.dropSession(s)
	})
}
