// Package config 加载 iflygo 控制面配置。
//
// 优先级：内置默认值 → YAML 文件 → 环境变量。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 顶层配置。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
}

// ServerConfig 控制面 HTTP 服务。
type ServerConfig struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	ExternalURL string `yaml:"external_url"`
	TLSCert     string `yaml:"tls_cert"`
	TLSKey      string `yaml:"tls_key"`
}

// DatabaseConfig 数据库。
type DatabaseConfig struct {
	Driver          string `yaml:"driver"` // sqlite | postgres
	DSN             string `yaml:"dsn"`
	SQLitePath      string `yaml:"sqlite_path"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	Name            string `yaml:"name"`
	SSLMode         string `yaml:"sslmode"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
}

// AuthConfig 认证与会话。
type AuthConfig struct {
	AdminUsername string `yaml:"admin_username"`
	AdminPassword string `yaml:"admin_password"`
	SessionSecret string `yaml:"session_secret"`
	CookieSecure  string `yaml:"cookie_secure"` // auto | true | false
	SessionTTL    string `yaml:"session_ttl"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Driver:          "sqlite",
			SQLitePath:      "data/iflygo.db",
			SSLMode:         "disable",
			MaxOpenConns:    50,
			MaxIdleConns:    10,
			ConnMaxLifetime: "3600s",
		},
		Auth: AuthConfig{
			AdminUsername: "admin",
			SessionSecret: "",
			CookieSecure:  "auto",
			SessionTTL:    "168h",
		},
	}
}

// Load 加载配置：默认值 → YAML → 环境变量。
func Load(path string) (*Config, error) {
	c := Default()

	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("读取配置文件: %w", err)
			}
		} else if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("解析配置文件: %w", err)
		}
	}

	applyEnv(c)
	return c, nil
}

func applyEnv(c *Config) {
	envStr(&c.Server.Host, "SERVER_HOST")
	envInt(&c.Server.Port, "SERVER_PORT")
	envStr(&c.Server.ExternalURL, "SERVER_EXTERNAL_URL")
	envStr(&c.Server.TLSCert, "SERVER_TLS_CERT")
	envStr(&c.Server.TLSKey, "SERVER_TLS_KEY")

	envStr(&c.Database.Driver, "DATABASE_DRIVER")
	envStr(&c.Database.DSN, "DATABASE_URL")
	envStr(&c.Database.SQLitePath, "SQLITE_PATH")
	envStr(&c.Database.Host, "DATABASE_HOST")
	envInt(&c.Database.Port, "DATABASE_PORT")
	envStr(&c.Database.User, "DATABASE_USER")
	envStr(&c.Database.Password, "DATABASE_PASSWORD")
	envStr(&c.Database.Name, "DATABASE_NAME")
	envStr(&c.Database.SSLMode, "DATABASE_SSLMODE")

	envStr(&c.Auth.AdminUsername, "ADMIN_USERNAME")
	envStr(&c.Auth.AdminPassword, "ADMIN_PASSWORD")
	envStr(&c.Auth.SessionSecret, "SESSION_SECRET")
	envStr(&c.Auth.CookieSecure, "COOKIE_SECURE")
	envStr(&c.Auth.SessionTTL, "SESSION_TTL")
}

func envStr(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func envInt(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

// Addr 返回 HTTP 监听地址。
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// DriverDSN 返回数据库驱动名与连接串。
func (c *Config) DriverDSN() (string, string, error) {
	d := &c.Database
	switch strings.ToLower(d.Driver) {
	case "sqlite", "":
		return "sqlite", d.SQLitePath, nil
	case "postgres", "postgresql":
		if d.DSN != "" {
			return "postgres", d.DSN, nil
		}
		return "postgres", buildPostgresDSN(d), nil
	default:
		return "", "", fmt.Errorf("不支持的数据库驱动: %q", d.Driver)
	}
}

func buildPostgresDSN(d *DatabaseConfig) string {
	host := d.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := d.Port
	if port == 0 {
		port = 5432
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + d.Name,
	}
	q := u.Query()
	q.Set("sslmode", d.SSLMode)
	u.RawQuery = q.Encode()
	if d.User != "" {
		u.User = url.UserPassword(d.User, d.Password)
	}
	return u.String()
}

// PoolSettings 返回连接池参数。
func (d *DatabaseConfig) PoolSettings() (maxOpen, maxIdle int, lifetime time.Duration) {
	maxOpen, maxIdle = d.MaxOpenConns, d.MaxIdleConns
	if maxOpen < 1 {
		maxOpen = 50
	}
	if maxIdle < 1 {
		maxIdle = 10
	}
	lifetime, _ = time.ParseDuration(d.ConnMaxLifetime)
	if lifetime <= 0 {
		lifetime = time.Hour
	}
	return
}

// ValidateRequired 校验必填项。
func (c *Config) ValidateRequired() error {
	if _, _, err := c.DriverDSN(); err != nil {
		return err
	}
	if c.Database.Driver != "" && strings.EqualFold(c.Database.Driver, "postgres") && c.Database.Name == "" && c.Database.DSN == "" {
		return errors.New("postgres 模式必须配置 database.name 或 DATABASE_URL")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return errors.New("server.port 非法")
	}
	return nil
}
