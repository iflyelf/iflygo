package edge_test

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/edge"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestTCPForwarding(t *testing.T) {
	// 后端 echo 服务
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_, _ = io.Copy(c, c)
				c.Close()
			}(c)
		}
	}()
	echoAddr := echoLn.Addr().String()

	// 公网端口
	pubPort := freePort(t)
	router := edge.NewTCPRouter([]edge.Route{{Port: pubPort, Target: echoAddr}})
	proxy := edge.NewTCPProxy(router, "127.0.0.1")
	if err := proxy.Sync(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	// 通过公网端口访问
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(pubPort)), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello-tcp")
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("读取 echo 失败: %v", err)
	}
	conn.Close()
	if string(buf) != string(msg) {
		t.Fatalf("echo 不符: got %q want %q", buf, msg)
	}
}
