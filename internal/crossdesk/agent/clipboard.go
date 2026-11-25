package agent

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

// 剪贴板通道协议（web-clipboard，兼容 CrossDesk Web 客户端）。
//
// 16 字节头，全部小端：
//
//	0..3   kind：1 TEXT / 2 APPLIED / 3 REJECTED / 4 HELLO / 5 READY
//	4..7   request id（客户端发 TEXT 时非 0；主机发 TEXT 时为 0）
//	8..11  文本总长度或协议版本
//	12..15 分片偏移
const (
	clipKindText     = 1
	clipKindApplied  = 2
	clipKindRejected = 3
	clipKindHello    = 4
	clipKindReady    = 5

	clipVersion    = 1
	clipHeaderSize = 16
	clipChunkSize  = 16 * 1024
	clipTextLimit  = 128 * 1024
)

// Clipboard 是平台文本剪贴板实现。
type Clipboard interface {
	ReadText() (string, error)
	WriteText(string) error
	Close() error
}

func clipFrame(kind, id, total, offset uint32, payload []byte) []byte {
	buf := make([]byte, clipHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], kind)
	binary.LittleEndian.PutUint32(buf[4:8], id)
	binary.LittleEndian.PutUint32(buf[8:12], total)
	binary.LittleEndian.PutUint32(buf[12:16], offset)
	copy(buf[clipHeaderSize:], payload)
	return buf
}

func parseClipFrame(data []byte) (kind, id, total, offset uint32, ok bool) {
	if len(data) < clipHeaderSize || len(data) > clipHeaderSize+clipChunkSize {
		return 0, 0, 0, 0, false
	}
	kind = binary.LittleEndian.Uint32(data[0:4])
	id = binary.LittleEndian.Uint32(data[4:8])
	total = binary.LittleEndian.Uint32(data[8:12])
	offset = binary.LittleEndian.Uint32(data[12:16])
	return kind, id, total, offset, true
}

// clipboardLoop 轮询本地剪贴板，变化时广播给所有已就绪的 Guest。
func (a *Agent) clipboardLoop(ctx context.Context) {
	if a.clipboard == nil {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if a.clipboardViewers() == 0 {
			continue
		}
		text, err := a.clipboard.ReadText()
		if err != nil || text == "" {
			continue
		}
		a.clipMu.Lock()
		changed := text != a.clipText
		if changed {
			a.clipText = text
		}
		a.clipMu.Unlock()
		if changed {
			a.broadcastClipboard(text)
		}
	}
}

func (a *Agent) clipboardViewers() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clipSessions
}

// setLocalClipboard 记录最近一次本地/远端写入，避免回环广播。
func (a *Agent) setLocalClipboard(text string) {
	a.clipMu.Lock()
	a.clipText = text
	a.clipMu.Unlock()
}

func (a *Agent) currentClipboard() string {
	a.clipMu.Lock()
	text := a.clipText
	a.clipMu.Unlock()
	if text != "" || a.clipboard == nil {
		return text
	}
	if text, err := a.clipboard.ReadText(); err == nil {
		a.setLocalClipboard(text)
		return text
	}
	return ""
}

func (a *Agent) broadcastClipboard(text string) {
	data := []byte(text)
	if len(data) == 0 || len(data) > clipTextLimit || containsNUL(data) {
		return
	}
	a.mu.Lock()
	list := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		list = append(list, s)
	}
	a.mu.Unlock()
	for _, s := range list {
		s.sendClipboard(data)
	}
}

func (a *Agent) clipboardReady(s *session, ready bool) {
	a.mu.Lock()
	if ready {
		a.clipSessions++
	} else if a.clipSessions > 0 {
		a.clipSessions--
	}
	a.mu.Unlock()
	if ready {
		go func() {
			if text := a.currentClipboard(); text != "" {
				s.sendClipboard([]byte(text))
			}
		}()
	}
}

type clipboardSessionState struct {
	mu       sync.Mutex
	ready    bool
	incoming []byte
	total    uint32
	next     uint32
	id       uint32
}

func (s *session) onClipboardMessage(msg []byte) {
	kind, id, total, offset, ok := parseClipFrame(msg)
	if !ok {
		return
	}
	switch kind {
	case clipKindHello:
		if id != 0 || total != clipVersion || offset != 0 {
			return
		}
		if s.clipboardChannel == nil {
			return
		}
		_ = s.clipboardChannel.Send(clipFrame(clipKindReady, 0, clipVersion, 0, nil))
		s.clipState.mu.Lock()
		already := s.clipState.ready
		s.clipState.ready = true
		s.clipState.mu.Unlock()
		if !already {
			s.agent.clipboardReady(s, true)
		}
	case clipKindText:
		if id == 0 {
			return
		}
		if total == 0 || total > clipTextLimit || offset > total || len(msg) < clipHeaderSize {
			s.rejectClipboard(id)
			return
		}
		payload := msg[clipHeaderSize:]
		if len(payload) == 0 || uint32(len(payload)) > total-offset {
			s.rejectClipboard(id)
			return
		}
		s.clipState.mu.Lock()
		if offset == 0 || s.clipState.id != id || len(s.clipState.incoming) != int(total) {
			s.clipState.id = id
			s.clipState.incoming = make([]byte, total)
			s.clipState.next = 0
			s.clipState.total = total
		}
		if s.clipState.id != id || s.clipState.next != offset || int(offset) > len(s.clipState.incoming) {
			s.clipState.incoming = nil
			s.clipState.mu.Unlock()
			s.rejectClipboard(id)
			return
		}
		copy(s.clipState.incoming[offset:], payload)
		s.clipState.next = offset + uint32(len(payload))
		complete := s.clipState.next == s.clipState.total
		var data []byte
		if complete {
			data = s.clipState.incoming
			s.clipState.incoming = nil
		}
		s.clipState.mu.Unlock()
		if !complete {
			return
		}
		if !utf8.Valid(data) || containsNUL(data) {
			s.rejectClipboard(id)
			return
		}
		if s.agent.clipboard == nil {
			s.rejectClipboard(id)
			return
		}
		if err := s.agent.clipboard.WriteText(string(data)); err != nil {
			s.rejectClipboard(id)
			return
		}
		s.agent.setLocalClipboard(string(data))
		if s.clipboardChannel != nil {
			_ = s.clipboardChannel.Send(clipFrame(clipKindApplied, id, 0, 0, nil))
		}
	}
}

func (s *session) rejectClipboard(id uint32) {
	if s.clipboardChannel != nil {
		_ = s.clipboardChannel.Send(clipFrame(clipKindRejected, id, 0, 0, nil))
	}
}

func (s *session) sendClipboard(data []byte) {
	if len(data) == 0 || len(data) > clipTextLimit {
		return
	}
	s.clipState.mu.Lock()
	ready := s.clipState.ready
	s.clipState.mu.Unlock()
	if !ready || s.clipboardChannel == nil {
		return
	}
	total := uint32(len(data))
	for offset := 0; offset < len(data); offset += clipChunkSize {
		end := offset + clipChunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := s.clipboardChannel.Send(clipFrame(clipKindText, 0, total, uint32(offset), data[offset:end])); err != nil {
			return
		}
	}
}

func (s *session) closeClipboard() {
	s.clipState.mu.Lock()
	wasReady := s.clipState.ready
	s.clipState.ready = false
	s.clipState.incoming = nil
	s.clipState.mu.Unlock()
	if wasReady {
		s.agent.clipboardReady(s, false)
	}
}

func containsNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

var errClipboardUnavailable = errors.New("剪贴板不可用")
