// Package crossdesk 实现 CrossDesk 原生信令与设备管理（Phase 1）。
package crossdesk

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/iflyelf/iflygo/internal/store"
)

// Device 一个 CrossDesk 设备身份。
type Device struct {
	ID   string
	Kind string
}

// DeviceStore 设备身份存储抽象；生产使用 SQLStore，测试可用内存实现。
type DeviceStore interface {
	Register(kind string) (Device, string, error)
	Verify(id, password string) (Device, bool)
	MarkOnline(id string, online bool) error
	Stats() (total, online int64, err error)
}

// SQLStore 基于 iFlyGo 数据库的 CrossDesk 设备库。
type SQLStore struct {
	s *store.Store
}

func NewSQLStore(s *store.Store) *SQLStore { return &SQLStore{s: s} }

func (s *SQLStore) Register(kind string) (Device, string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "desktop"
	}
	for attempt := 0; attempt < 20; attempt++ {
		id, err := newDeviceID()
		if err != nil {
			return Device{}, "", err
		}
		if kind == "web" || kind == "cli" {
			id = kind + "-" + id
		}
		password := ""
		hash := ""
		if kind == "desktop" {
			password, err = newPassword()
			if err != nil {
				return Device{}, "", err
			}
			b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return Device{}, "", err
			}
			hash = string(b)
		}
		_, err = s.s.DB.Exec(s.s.Rebind(`INSERT INTO crossdesk_devices
			(device_id,password_hash,kind,online,last_seen,created_at) VALUES ($1,$2,$3,0,$4,$4)`),
			id, hash, kind, time.Now().UTC().Format(time.RFC3339))
		if err == nil {
			return Device{ID: id, Kind: kind}, password, nil
		}
		if !isUniqueViolation(err) {
			return Device{}, "", err
		}
	}
	return Device{}, "", fmt.Errorf("生成设备 ID 失败")
}

func (s *SQLStore) Verify(id, password string) (Device, bool) {
	var hash, kind string
	err := s.s.DB.QueryRow(s.s.Rebind(`SELECT COALESCE(password_hash,''), COALESCE(kind,'desktop') FROM crossdesk_devices WHERE device_id = $1`), id).
		Scan(&hash, &kind)
	if err != nil {
		if err == sql.ErrNoRows {
			// 临时控制器别名 web-/cli-/C- 不需要持久化记录。
			if isTemporaryID(id) {
				return Device{ID: id, Kind: "controller"}, true
			}
		}
		return Device{}, false
	}
	if hash == "" {
		return Device{ID: id, Kind: kind}, isTemporaryID(id)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return Device{}, false
	}
	return Device{ID: id, Kind: kind}, true
}

func (s *SQLStore) MarkOnline(id string, online bool) error {
	v := 0
	if online {
		v = 1
	}
	_, err := s.s.DB.Exec(s.s.Rebind(`UPDATE crossdesk_devices SET online = $1, last_seen = $2 WHERE device_id = $3`),
		v, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLStore) Stats() (int64, int64, error) {
	var total, online int64
	err := s.s.DB.QueryRow(s.s.Rebind(`SELECT COUNT(*), COALESCE(SUM(online),0) FROM crossdesk_devices`)).Scan(&total, &online)
	return total, online, err
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "constraint")
}

func isTemporaryID(id string) bool {
	return strings.HasPrefix(id, "web-") || strings.HasPrefix(id, "cli-") || strings.HasPrefix(id, "C-")
}

func newDeviceID() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000000))
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(100000000+n.Int64(), 10), nil
}

func newPassword() (string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 6)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}
