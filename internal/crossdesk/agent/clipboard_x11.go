//go:build linux && !android

package agent

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
)

// latin1ToUTF8 兼容只提供 STRING（Latin-1）目标的旧应用。
func latin1ToUTF8(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return b.String()
}

// x11Clipboard 通过 X11 selection 协议读写 CLIPBOARD，纯 Go 实现。
//
// 读：ConvertSelection -> SelectionNotify -> GetProperty。
// 写：SetSelectionOwner -> 响应 SelectionRequest。
type x11Clipboard struct {
	conn *xgb.Conn
	win  xproto.Window

	clipboard xproto.Atom
	utf8      xproto.Atom
	text      xproto.Atom
	str       xproto.Atom
	targets   xproto.Atom
	incr      xproto.Atom
	property  xproto.Atom

	cmds chan func()
	done chan struct{}
	once sync.Once

	// 以下字段只在事件循环 goroutine 中访问。
	textValue string
	pending   chan clipReadResult
	deadline  time.Time
}

type clipReadResult struct {
	text string
	err  error
}

func newClipboard() (Clipboard, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("无法连接 X11: %w", err)
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	win, err := xproto.NewWindowId(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	xproto.CreateWindow(conn, 0, win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, 0, nil)
	if _, err := xproto.GetGeometry(conn, xproto.Drawable(win)).Reply(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("创建 X11 剪贴板窗口失败: %w", err)
	}
	c := &x11Clipboard{
		conn: conn, win: win,
		cmds: make(chan func(), 8), done: make(chan struct{}),
	}
	if c.clipboard, err = internAtom(conn, "CLIPBOARD"); err != nil {
		return nil, c.fail(err)
	}
	if c.utf8, err = internAtom(conn, "UTF8_STRING"); err != nil {
		return nil, c.fail(err)
	}
	if c.text, err = internAtom(conn, "TEXT"); err != nil {
		return nil, c.fail(err)
	}
	if c.str, err = internAtom(conn, "STRING"); err != nil {
		return nil, c.fail(err)
	}
	if c.targets, err = internAtom(conn, "TARGETS"); err != nil {
		return nil, c.fail(err)
	}
	if c.incr, err = internAtom(conn, "INCR"); err != nil {
		return nil, c.fail(err)
	}
	if c.property, err = internAtom(conn, "IFLYGO_CLIPBOARD"); err != nil {
		return nil, c.fail(err)
	}
	go c.loop()
	return c, nil
}

func (c *x11Clipboard) fail(err error) error {
	c.conn.Close()
	close(c.done)
	return err
}

func internAtom(conn *xgb.Conn, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	return reply.Atom, nil
}

func (c *x11Clipboard) loop() {
	for {
		select {
		case <-c.done:
			return
		case fn := <-c.cmds:
			fn()
		default:
		}
		ev, _ := c.conn.PollForEvent()
		if ev == nil {
			if c.pending != nil && time.Now().After(c.deadline) {
				ch := c.pending
				c.pending = nil
				ch <- clipReadResult{err: fmt.Errorf("X11 剪贴板读取超时")}
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		switch e := ev.(type) {
		case xproto.SelectionRequestEvent:
			c.handleSelectionRequest(e)
		case xproto.SelectionNotifyEvent:
			c.handleSelectionNotify(e)
		case xproto.SelectionClearEvent:
			// 其他应用接管了剪贴板；保持自身文本，等待下次写入。
		}
	}
}

func (c *x11Clipboard) handleSelectionRequest(e xproto.SelectionRequestEvent) {
	if e.Selection != c.clipboard {
		return
	}
	property := e.Property
	if property == 0 {
		property = e.Target
	}
	switch e.Target {
	case c.targets:
		atoms := []xproto.Atom{c.utf8, c.text, c.str}
		data := make([]byte, len(atoms)*4)
		for i, atom := range atoms {
			binary.LittleEndian.PutUint32(data[i*4:], uint32(atom))
		}
		xproto.ChangeProperty(c.conn, xproto.PropModeReplace, e.Requestor, property,
			xproto.AtomAtom, 32, uint32(len(atoms)), data)
	case c.utf8, c.text, c.str:
		if c.textValue == "" {
			property = 0
			break
		}
		xproto.ChangeProperty(c.conn, xproto.PropModeReplace, e.Requestor, property,
			e.Target, 8, uint32(len(c.textValue)), []byte(c.textValue))
	default:
		property = 0
	}
	notify := xproto.SelectionNotifyEvent{
		Time: e.Time, Requestor: e.Requestor, Selection: e.Selection,
		Target: e.Target, Property: property,
	}
	xproto.SendEvent(c.conn, false, e.Requestor, 0, string(notify.Bytes()))
}

func (c *x11Clipboard) handleSelectionNotify(e xproto.SelectionNotifyEvent) {
	if c.pending == nil || e.Requestor != c.win || e.Selection != c.clipboard {
		return
	}
	ch := c.pending
	c.pending = nil
	if e.Property == 0 {
		ch <- clipReadResult{err: fmt.Errorf("X11 剪贴板为空或目标格式不支持")}
		return
	}
	reply, err := xproto.GetProperty(c.conn, false, c.win, e.Property, 0,
		0, uint32(clipTextLimit/4+1)).Reply()
	if err != nil {
		ch <- clipReadResult{err: err}
		return
	}
	xproto.DeleteProperty(c.conn, c.win, e.Property)
	if reply.Type == c.incr {
		ch <- clipReadResult{err: fmt.Errorf("X11 剪贴板使用了 INCR 传输，暂不支持")}
		return
	}
	if reply.Format != 8 {
		ch <- clipReadResult{err: fmt.Errorf("X11 剪贴板格式不支持: %d", reply.Format)}
		return
	}
	if reply.BytesAfter > 0 {
		ch <- clipReadResult{err: fmt.Errorf("X11 剪贴板文本超过 %d 字节", clipTextLimit)}
		return
	}
	text := string(reply.Value)
	if reply.Type == c.str && !utf8.ValidString(text) {
		text = latin1ToUTF8(reply.Value)
	}
	ch <- clipReadResult{text: text}
}

func (c *x11Clipboard) ReadText() (string, error) {
	result := make(chan clipReadResult, 1)
	select {
	case c.cmds <- func() {
		if c.pending != nil {
			result <- clipReadResult{err: fmt.Errorf("X11 剪贴板正在读取")}
			return
		}
		c.pending = result
		c.deadline = time.Now().Add(2 * time.Second)
		xproto.ConvertSelection(c.conn, c.win, c.clipboard, c.utf8, c.property, 0)
	}:
	case <-time.After(time.Second):
		return "", fmt.Errorf("X11 剪贴板命令超时")
	}
	select {
	case r := <-result:
		return r.text, r.err
	case <-time.After(3 * time.Second):
		return "", fmt.Errorf("X11 剪贴板读取超时")
	}
}

func (c *x11Clipboard) WriteText(text string) error {
	if len(text) > clipTextLimit {
		return fmt.Errorf("剪贴板文本超过 %d 字节", clipTextLimit)
	}
	done := make(chan error, 1)
	select {
	case c.cmds <- func() {
		c.textValue = text
		xproto.SetSelectionOwner(c.conn, c.win, c.clipboard, 0)
		reply, err := xproto.GetSelectionOwner(c.conn, c.clipboard).Reply()
		if err != nil {
			done <- err
			return
		}
		if reply.Owner != c.win {
			done <- fmt.Errorf("无法取得 X11 剪贴板所有权")
			return
		}
		done <- nil
	}:
	case <-time.After(time.Second):
		return fmt.Errorf("X11 剪贴板命令超时")
	}
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("X11 剪贴板写入超时")
	}
}

func (c *x11Clipboard) Close() error {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
	return nil
}
