package agent

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// CrossDesk 文件浏览与传输协议（兼容内置 Web 客户端）。
//
// 目录 RPC 走控制通道 JSON：{"type":"file_browser","version":1,...}。
// 文件数据走 "file" / "file_feedback" 两个可靠通道，packed little-endian：
//
//	FileChunkHeader (31B): magic u32, id u32, offset u64, total u64,
//	                       size u32, nameSize u16, flags u8
//	FileTransferAck (28B): magic u32, id u32, bytes u64, total u64, flags u32
//
// flags: 1=first, 2=last, 4=cancel；ACK flags: 1=complete, 2=error/cancel。
const (
	fileProtocol   = "crossdesk-file-v1"
	fileChunkMagic = 0x4a4e544d
	fileAckMagic   = 0x4a4e5443
	fileHeaderSize = 31
	fileAckSize    = 28
	fileChunkSize  = 16 * 1024
	filePageSize   = 512
	filePageBytes  = 45000
	fileBufferHigh = 1024 * 1024
)

type fileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified"`
	Link      bool   `json:"link"`
}

type directorySnapshot struct {
	path    string
	parent  string
	entries []fileEntry
	expires time.Time
}

type fileBrowser struct {
	roots     []string
	hostname  string
	mu        sync.Mutex
	snapshots map[string]*directorySnapshot
}

type fileRPC struct {
	Type        string   `json:"type"`
	Version     int      `json:"version"`
	ID          int64    `json:"id"`
	Op          string   `json:"op"`
	Path        string   `json:"path"`
	Cursor      string   `json:"cursor"`
	Pagination  int      `json:"pagination"`
	Destination string   `json:"destination"`
	TransferID  string   `json:"transfer_id"`
	Paths       []string `json:"paths"`
	Items       []struct {
		FileID    uint32 `json:"file_id"`
		Directory string `json:"directory"`
	} `json:"items"`
}

func newFileBrowser(roots []string) *fileBrowser {
	var resolved []string
	seen := map[string]bool{}
	for _, raw := range roots {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			continue
		}
		if !seen[real] {
			seen[real] = true
			resolved = append(resolved, real)
		}
	}
	if len(resolved) == 0 {
		return nil
	}
	host, _ := os.Hostname()
	return &fileBrowser{roots: resolved, hostname: host, snapshots: map[string]*directorySnapshot{}}
}

// resolveExisting 解析真实路径并确认位于允许的根目录内（符号链接会被解析）。
func (fb *fileBrowser) resolveExisting(raw string) (string, bool) {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", false
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	for _, root := range fb.roots {
		if withinRoot(root, real) {
			return real, true
		}
	}
	return "", false
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func (fb *fileBrowser) isRoot(path string) bool {
	for _, root := range fb.roots {
		if path == root {
			return true
		}
	}
	return false
}

func (fb *fileBrowser) handleRPC(s *session, data []byte) {
	var req fileRPC
	if json.Unmarshal(data, &req) != nil || req.Type != "file_browser" || req.Version != 1 || req.ID < 0 {
		return
	}
	switch req.Op {
	case "list":
		result := fb.list(req.Path, req.Cursor, req.Pagination)
		result["type"] = "file_browser"
		result["version"] = 1
		result["id"] = req.ID
		result["op"] = "reply"
		s.sendControlJSON(result)
	case "list_cancel":
		if token, _, ok := strings.Cut(req.Cursor, ":"); ok {
			fb.mu.Lock()
			delete(fb.snapshots, token)
			fb.mu.Unlock()
		}
		s.fileReply(req.ID, map[string]any{"ok": true})
	case "prepare":
		s.fileReply(req.ID, s.prepareUpload(&req))
	case "copy":
		s.fileReply(req.ID, s.startDownload(&req))
	default:
		s.fileReply(req.ID, map[string]any{"ok": false, "error": "unsupported"})
	}
}

func (s *session) fileReply(id int64, result map[string]any) {
	msg := map[string]any{
		"type": "file_browser", "version": 1, "id": id, "op": "reply",
	}
	for k, v := range result {
		msg[k] = v
	}
	s.sendControlJSON(msg)
}

func (s *session) fileNotice(transferID string, ok bool, reason string) {
	msg := map[string]any{
		"type": "file_browser", "version": 1, "id": time.Now().UnixNano() & 0x7fffffff,
		"op": "notice", "ok": ok, "transfer_id": transferID,
	}
	if !ok {
		msg["error"] = reason
	}
	s.sendControlJSON(msg)
}

func (fb *fileBrowser) list(path, cursor string, pagination int) map[string]any {
	if path == "" {
		path = "@roots"
	}
	if cursor != "" {
		token, rest, ok := strings.Cut(cursor, ":")
		offset, err := strconv.Atoi(rest)
		if !ok || err != nil || offset < 0 {
			return map[string]any{"ok": false, "error": "listing_expired"}
		}
		fb.mu.Lock()
		snap := fb.snapshots[token]
		if snap != nil && time.Now().After(snap.expires) {
			delete(fb.snapshots, token)
			snap = nil
		}
		fb.mu.Unlock()
		if snap == nil || offset > len(snap.entries) {
			return map[string]any{"ok": false, "error": "listing_expired"}
		}
		entries, next := pageEntries(snap.entries, offset)
		result := map[string]any{
			"ok": true, "path": snap.path, "parent": snap.parent, "entries": entries,
			"offset": offset, "total": len(snap.entries), "truncated": next < len(snap.entries),
		}
		if next < len(snap.entries) {
			result["next"] = fmt.Sprintf("%s:%d", token, next)
			fb.mu.Lock()
			snap.expires = time.Now().Add(30 * time.Second)
			fb.mu.Unlock()
		} else {
			fb.mu.Lock()
			delete(fb.snapshots, token)
			fb.mu.Unlock()
		}
		return result
	}

	var entries []fileEntry
	parent := ""
	if path == "@roots" {
		for i, root := range fb.roots {
			name := filepath.Base(root)
			if name == "" || name == "." || name == string(os.PathSeparator) {
				name = fmt.Sprintf("根目录 %d", i+1)
			}
			modified := int64(0)
			if info, err := os.Stat(root); err == nil {
				modified = info.ModTime().Unix()
			}
			entries = append(entries, fileEntry{Name: name, Path: root, Directory: true, Modified: modified})
		}
	} else {
		real, ok := fb.resolveExisting(path)
		if !ok {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		info, err := os.Stat(real)
		if err != nil {
			return map[string]any{"ok": false, "error": "directory_unavailable"}
		}
		if !info.IsDir() {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		items, err := os.ReadDir(real)
		if err != nil {
			return map[string]any{"ok": false, "error": "directory_unavailable"}
		}
		for _, item := range items {
			full := filepath.Join(real, item.Name())
			fi, err := os.Lstat(full)
			if err != nil {
				continue
			}
			entry := fileEntry{Name: item.Name(), Link: fi.Mode()&os.ModeSymlink != 0}
			if !entry.Link {
				entry.Directory = fi.IsDir()
				entry.Size = fi.Size()
				entry.Modified = fi.ModTime().Unix()
			}
			entries = append(entries, entry)
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Directory != entries[j].Directory {
				return entries[i].Directory
			}
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		})
		parent = filepath.Dir(real)
		if fb.isRoot(real) || !fb.withinAnyRoot(parent) {
			parent = "@roots"
		}
	}
	page, next := pageEntries(entries, 0)
	result := map[string]any{
		"ok": true, "path": func() string {
			if path == "@roots" {
				return "@roots"
			}
			if real, ok := fb.resolveExisting(path); ok {
				return real
			}
			return path
		}(), "parent": parent, "entries": page,
		"offset": 0, "total": len(entries), "truncated": next < len(entries),
	}
	if next < len(entries) {
		token := randomToken()
		fb.mu.Lock()
		if len(fb.snapshots) >= 16 {
			for key, old := range fb.snapshots {
				if time.Now().After(old.expires) {
					delete(fb.snapshots, key)
				}
			}
		}
		fb.snapshots[token] = &directorySnapshot{
			path: result["path"].(string), parent: parent, entries: entries,
			expires: time.Now().Add(30 * time.Second),
		}
		fb.mu.Unlock()
		result["next"] = fmt.Sprintf("%s:%d", token, next)
	}
	return result
}

func (fb *fileBrowser) withinAnyRoot(path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	for _, root := range fb.roots {
		if withinRoot(root, real) {
			return true
		}
	}
	return false
}

func pageEntries(entries []fileEntry, offset int) ([]fileEntry, int) {
	next := offset
	bytes := 0
	out := make([]fileEntry, 0, filePageSize)
	for next < len(entries) && len(out) < filePageSize {
		data, err := json.Marshal(entries[next])
		size := len(data) + 1
		if err != nil || bytes+size > filePageBytes {
			break
		}
		bytes += size
		out = append(out, entries[next])
		next++
	}
	return out, next
}

func randomToken() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func (s *session) prepareUpload(req *fileRPC) map[string]any {
	if len(req.Items) == 0 || len(req.Items) > 64 {
		return map[string]any{"ok": false, "error": "invalid_request"}
	}
	dest, ok := s.agent.files.resolveExisting(req.Destination)
	if !ok {
		return map[string]any{"ok": false, "error": "directory_not_writable"}
	}
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		return map[string]any{"ok": false, "error": "directory_not_writable"}
	}
	targets := make([]string, len(req.Items))
	for i, item := range req.Items {
		rel := strings.Trim(strings.ReplaceAll(item.Directory, "\\", "/"), "/")
		if rel != "" {
			for _, part := range strings.Split(rel, "/") {
				if part == "" || part == "." || part == ".." {
					return map[string]any{"ok": false, "error": "invalid_path"}
				}
			}
		}
		target := dest
		if rel != "" {
			target = filepath.Join(dest, filepath.FromSlash(rel))
		}
		if !withinRoot(dest, target) {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return map[string]any{"ok": false, "error": "directory_not_writable"}
		}
		if real, err := filepath.EvalSymlinks(target); err != nil || !withinRoot(dest, real) {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		targets[i] = target
	}
	s.fileMu.Lock()
	for i, item := range req.Items {
		if item.FileID != 0 {
			s.uploads[item.FileID] = targets[i]
		}
	}
	s.fileMu.Unlock()
	return map[string]any{"ok": true}
}

func (s *session) startDownload(req *fileRPC) map[string]any {
	if len(req.Paths) == 0 || len(req.Paths) > 256 {
		return map[string]any{"ok": false, "error": "invalid_request"}
	}
	files := make([]string, 0, len(req.Paths))
	for _, raw := range req.Paths {
		real, ok := s.agent.files.resolveExisting(raw)
		if !ok {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		info, err := os.Lstat(real)
		if err != nil {
			return map[string]any{"ok": false, "error": "invalid_path"}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return map[string]any{"ok": false, "error": "symbolic_link"}
		}
		if info.IsDir() {
			return map[string]any{"ok": false, "error": "unsupported"}
		}
		if !info.Mode().IsRegular() {
			return map[string]any{"ok": false, "error": "unsupported"}
		}
		files = append(files, real)
	}
	go s.sendFiles(req.TransferID, files)
	return map[string]any{"ok": true, "message": "queued"}
}

func (s *session) sendFiles(transferID string, paths []string) {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s.fileChannel != nil && s.fileChannel.ReadyState() == webrtc.DataChannelStateOpen &&
			s.feedbackChannel != nil && s.feedbackChannel.ReadyState() == webrtc.DataChannelStateOpen {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if s.fileChannel == nil || s.fileChannel.ReadyState() != webrtc.DataChannelStateOpen {
		s.fileNotice(transferID, false, "disconnected")
		return
	}
	for _, path := range paths {
		if err := s.sendFile(path); err != nil {
			s.fileNotice(transferID, false, "file_unreadable")
			return
		}
	}
	s.fileNotice(transferID, true, "")
}

func (s *session) sendFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	name := []byte(filepath.Base(path))
	if len(name) == 0 || len(name) > 65535 {
		return fmt.Errorf("invalid file name")
	}
	total := info.Size()
	id := s.nextFile()
	offset := int64(0)
	first := true
	for {
		select {
		case <-s.fileDone:
			return fmt.Errorf("session closed")
		default:
		}
		if s.fileCancelled(id) {
			return fmt.Errorf("cancelled")
		}
		payload := make([]byte, fileChunkSize)
		n, readErr := io.ReadFull(f, payload)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return readErr
		}
		if readErr == io.EOF && n == 0 && offset < total {
			return fmt.Errorf("file truncated during transfer")
		}
		payload = payload[:n]
		last := offset+int64(n) >= total
		flags := byte(0)
		if first {
			flags |= 1
		}
		if last {
			flags |= 2
		}
		packet := make([]byte, fileHeaderSize+len(name)+len(payload))
		binary.LittleEndian.PutUint32(packet[0:4], fileChunkMagic)
		binary.LittleEndian.PutUint32(packet[4:8], id)
		binary.LittleEndian.PutUint64(packet[8:16], uint64(offset))
		binary.LittleEndian.PutUint64(packet[16:24], uint64(total))
		binary.LittleEndian.PutUint32(packet[24:28], uint32(len(payload)))
		binary.LittleEndian.PutUint16(packet[28:30], uint16(len(name)))
		packet[30] = flags
		if first {
			copy(packet[fileHeaderSize:], name)
		}
		copy(packet[fileHeaderSize+len(name):], payload)
		if err := s.sendFilePacket(packet); err != nil {
			return err
		}
		offset += int64(n)
		first = false
		if last {
			return nil
		}
	}
}

func (s *session) sendFilePacket(packet []byte) error {
	deadline := time.Now().Add(30 * time.Second)
	for s.fileChannel.BufferedAmount() > fileBufferHigh {
		if time.Now().After(deadline) {
			return fmt.Errorf("file transfer stalled")
		}
		select {
		case <-s.fileDone:
			return fmt.Errorf("session closed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	return s.fileChannel.Send(packet)
}

func (s *session) onFileChunk(data []byte) {
	if len(data) < fileHeaderSize {
		return
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != fileChunkMagic {
		return
	}
	id := binary.LittleEndian.Uint32(data[4:8])
	offset := binary.LittleEndian.Uint64(data[8:16])
	total := binary.LittleEndian.Uint64(data[16:24])
	size := binary.LittleEndian.Uint32(data[24:28])
	nameSize := int(binary.LittleEndian.Uint16(data[28:30]))
	flags := data[30]
	if fileHeaderSize+nameSize+int(size) != len(data) {
		s.fileAck(id, 0, total, 2)
		return
	}
	if flags&4 != 0 {
		s.abortUpload(id)
		s.fileAck(id, 0, total, 2)
		return
	}
	if flags&1 != 0 {
		s.beginUpload(id, string(data[fileHeaderSize:fileHeaderSize+nameSize]), total)
	}
	s.fileMu.Lock()
	up := s.incoming[id]
	s.fileMu.Unlock()
	if up == nil || up.file == nil {
		s.fileAck(id, 0, total, 2)
		return
	}
	if offset != uint64(up.received) {
		s.abortUpload(id)
		s.fileAck(id, 0, total, 2)
		return
	}
	payload := data[fileHeaderSize+nameSize:]
	if len(payload) > 0 {
		if _, err := up.file.Write(payload); err != nil {
			s.abortUpload(id)
			s.fileAck(id, 0, total, 2)
			return
		}
		up.received += int64(len(payload))
	}
	if flags&2 != 0 {
		if up.received != up.total {
			s.abortUpload(id)
			s.fileAck(id, uint64(up.received), total, 2)
			return
		}
		_ = up.file.Close()
		up.file = nil
		s.fileMu.Lock()
		delete(s.incoming, id)
		delete(s.uploads, id)
		s.fileMu.Unlock()
		s.fileAck(id, uint64(up.received), total, 1)
		return
	}
	s.fileAck(id, uint64(up.received), total, 0)
}

type incomingUpload struct {
	file     *os.File
	path     string
	total    int64
	received int64
}

func (s *session) beginUpload(id uint32, name string, total uint64) {
	s.fileMu.Lock()
	dest, ok := s.uploads[id]
	s.fileMu.Unlock()
	if !ok {
		return
	}
	safe := safeUploadName(name)
	if safe == "" || total > math.MaxInt64 {
		return
	}
	path, file, err := createUnique(dest, safe)
	if err != nil {
		return
	}
	s.fileMu.Lock()
	s.incoming[id] = &incomingUpload{file: file, path: path, total: int64(total)}
	s.fileMu.Unlock()
}

func (s *session) abortUpload(id uint32) {
	s.fileMu.Lock()
	up := s.incoming[id]
	delete(s.incoming, id)
	delete(s.uploads, id)
	s.fileMu.Unlock()
	if up != nil {
		if up.file != nil {
			_ = up.file.Close()
		}
		if up.path != "" {
			_ = os.Remove(up.path)
		}
	}
}

func (s *session) fileAck(id uint32, bytes, total uint64, flags uint32) {
	if s.feedbackChannel == nil || s.feedbackChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	packet := make([]byte, fileAckSize)
	binary.LittleEndian.PutUint32(packet[0:4], fileAckMagic)
	binary.LittleEndian.PutUint32(packet[4:8], id)
	binary.LittleEndian.PutUint64(packet[8:16], bytes)
	binary.LittleEndian.PutUint64(packet[16:24], total)
	binary.LittleEndian.PutUint32(packet[24:28], flags)
	_ = s.feedbackChannel.Send(packet)
}

func (s *session) onFileAck(data []byte) {
	if len(data) != fileAckSize || binary.LittleEndian.Uint32(data[0:4]) != fileAckMagic {
		return
	}
	if binary.LittleEndian.Uint32(data[24:28])&2 == 0 {
		return
	}
	id := binary.LittleEndian.Uint32(data[4:8])
	s.fileMu.Lock()
	s.cancelled[id] = true
	s.fileMu.Unlock()
}

func (s *session) fileCancelled(id uint32) bool {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	return s.cancelled[id]
}

func (s *session) nextFile() uint32 {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	s.nextFileID++
	if s.nextFileID == 0 {
		s.nextFileID = 1
	}
	return s.nextFileID
}

func (s *session) closeFiles() {
	s.fileMu.Lock()
	incoming := make([]*incomingUpload, 0, len(s.incoming))
	for _, up := range s.incoming {
		incoming = append(incoming, up)
	}
	s.incoming = map[uint32]*incomingUpload{}
	s.uploads = map[uint32]string{}
	s.fileMu.Unlock()
	for _, up := range incoming {
		if up.file != nil {
			_ = up.file.Close()
		}
		if up.path != "" {
			_ = os.Remove(up.path)
		}
	}
}

func safeUploadName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case 0, '/', '\\', '<', '>', ':', '"', '|', '?', '*':
			return '_'
		}
		if r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

func createUnique(dest, name string) (string, *os.File, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		path := filepath.Join(dest, candidate)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return path, file, nil
		}
		if !os.IsExist(err) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("无法创建唯一文件名")
}
