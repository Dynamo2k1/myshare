// Package config resolves MyShare's runtime configuration.
//
// Precedence, highest wins:
//
//	CLI flags  >  environment (MYSHARE_*)  >  config file (TOML)  >  built-in defaults
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the fully-resolved configuration used by the server.
type Config struct {
	Host    string `toml:"host"`
	Port    int    `toml:"port"`
	DataDir string `toml:"data_dir"`

	MaxFileSize int64 `toml:"max_file_size"` // bytes; 0 = unlimited
	MaxStorage  int64 `toml:"max_storage"`   // bytes; 0 = unlimited

	Auth     bool   `toml:"auth"`      // require login
	LogLevel string `toml:"log_level"` // debug|info|warn|error

	TLS bool `toml:"tls"` // serve HTTPS with a self-signed cert

	// ServeDir, when set, switches MyShare into "directory mode": the Files tab
	// browses this real folder on disk. Uploads land in it, deletes remove the
	// real file, subfolders are navigable, and external changes are picked up by
	// a periodic rescan. MyShare's own metadata (clipboard, snippets, notes,
	// share tokens, search) lives in <ServeDir>/.myshare/ — or a temp dir when
	// Ephemeral is set — and is never confused with the served files.
	ServeDir string `toml:"dir"`

	// Ephemeral puts MyShare's database and upload-staging area in a temp
	// directory that is deleted on a clean shutdown. It never touches a served
	// directory's files — only MyShare's own scratch state.
	Ephemeral bool `toml:"ephemeral"`

	// Access controls WHO may reach the server, by client IP:
	//   "local"  – loopback only (127.0.0.0/8, ::1)
	//   "lan"    – loopback + private networks (10/8, 172.16/12, 192.168/16,
	//              link-local, IPv6 ULA/link-local). The default when the server
	//              is bound to a non-loopback address.
	//   "public" – anyone. Requires an explicit choice; strongly pair with --auth.
	Access string `toml:"access"`

	CleanupInterval time.Duration `toml:"cleanup_interval"`

	// Derived / not from file.
	ConfigFilePath string `toml:"-"`
}

// Defaults returns a Config populated with built-in defaults for the current OS.
func Defaults() Config {
	return Config{
		Host:            "127.0.0.1",
		Port:            8787,
		DataDir:         defaultDataDir(),
		MaxFileSize:     0,
		MaxStorage:      0,
		Auth:            false,
		LogLevel:        "info",
		TLS:             false,
		Access:          "", // resolved in normalize() from Host
		CleanupInterval: time.Hour,
	}
}

// Overrides carries values explicitly set on the command line. A nil pointer
// means "not set on the CLI", so the lower-precedence sources are consulted.
type Overrides struct {
	Host            *string
	Port            *int
	DataDir         *string
	MaxFileSize     *string // human string, e.g. "5GB"
	MaxStorage      *string
	Auth            *bool
	LogLevel        *string
	TLS             *bool
	Access          *string
	ServeDir        *string
	Ephemeral       *bool
	CleanupInterval *string
	ConfigFile      *string // explicit config file path
}

// Load resolves configuration from all sources honouring precedence.
func Load(ov Overrides) (Config, error) {
	cfg := Defaults()

	// --- config file -------------------------------------------------------
	path := ""
	if ov.ConfigFile != nil && *ov.ConfigFile != "" {
		path = *ov.ConfigFile
	} else if env := os.Getenv("MYSHARE_CONFIG"); env != "" {
		path = env
	} else if p := defaultConfigFile(); fileExists(p) {
		path = p
	}
	if path != "" {
		if !fileExists(path) {
			return cfg, fmt.Errorf("config file not found: %s", path)
		}
		var fileCfg Config
		if _, err := toml.DecodeFile(path, &fileCfg); err != nil {
			return cfg, fmt.Errorf("parse config file %s: %w", path, err)
		}
		mergeFile(&cfg, fileCfg)
		cfg.ConfigFilePath = path
	}

	// --- environment -----------------------------------------------------
	if v := os.Getenv("MYSHARE_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("MYSHARE_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("MYSHARE_PORT: %w", err)
		}
		cfg.Port = n
	}
	if v := os.Getenv("MYSHARE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("MYSHARE_MAX_FILE_SIZE"); v != "" {
		n, err := ParseSize(v)
		if err != nil {
			return cfg, fmt.Errorf("MYSHARE_MAX_FILE_SIZE: %w", err)
		}
		cfg.MaxFileSize = n
	}
	if v := os.Getenv("MYSHARE_MAX_STORAGE"); v != "" {
		n, err := ParseSize(v)
		if err != nil {
			return cfg, fmt.Errorf("MYSHARE_MAX_STORAGE: %w", err)
		}
		cfg.MaxStorage = n
	}
	if v := os.Getenv("MYSHARE_AUTH"); v != "" {
		cfg.Auth = truthy(v)
	}
	if v := os.Getenv("MYSHARE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("MYSHARE_TLS"); v != "" {
		cfg.TLS = truthy(v)
	}
	if v := os.Getenv("MYSHARE_ACCESS"); v != "" {
		cfg.Access = v
	}
	if v := os.Getenv("MYSHARE_DIR"); v != "" {
		cfg.ServeDir = v
	}
	if v := os.Getenv("MYSHARE_EPHEMERAL"); v != "" {
		cfg.Ephemeral = truthy(v)
	}
	if v := os.Getenv("MYSHARE_CLEANUP_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("MYSHARE_CLEANUP_INTERVAL: %w", err)
		}
		cfg.CleanupInterval = d
	}

	// --- CLI flags (highest) -------------------------------------------
	if ov.Host != nil {
		cfg.Host = *ov.Host
	}
	if ov.Port != nil {
		cfg.Port = *ov.Port
	}
	if ov.DataDir != nil {
		cfg.DataDir = *ov.DataDir
	}
	if ov.MaxFileSize != nil {
		n, err := ParseSize(*ov.MaxFileSize)
		if err != nil {
			return cfg, fmt.Errorf("--max-file-size: %w", err)
		}
		cfg.MaxFileSize = n
	}
	if ov.MaxStorage != nil {
		n, err := ParseSize(*ov.MaxStorage)
		if err != nil {
			return cfg, fmt.Errorf("--max-storage: %w", err)
		}
		cfg.MaxStorage = n
	}
	if ov.Auth != nil {
		cfg.Auth = *ov.Auth
	}
	if ov.LogLevel != nil {
		cfg.LogLevel = *ov.LogLevel
	}
	if ov.TLS != nil {
		cfg.TLS = *ov.TLS
	}
	if ov.Access != nil {
		cfg.Access = *ov.Access
	}
	if ov.ServeDir != nil {
		cfg.ServeDir = *ov.ServeDir
	}
	if ov.Ephemeral != nil {
		cfg.Ephemeral = *ov.Ephemeral
	}
	if ov.CleanupInterval != nil {
		d, err := time.ParseDuration(*ov.CleanupInterval)
		if err != nil {
			return cfg, fmt.Errorf("--cleanup-interval: %w", err)
		}
		cfg.CleanupInterval = d
	}

	if err := cfg.normalize(); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c *Config) normalize() error {
	c.DataDir = expandHome(c.DataDir)
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	c.DataDir = abs
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.Host = strings.TrimSpace(c.Host)

	if c.ServeDir != "" {
		sd, err := filepath.Abs(expandHome(c.ServeDir))
		if err != nil {
			return fmt.Errorf("resolve --dir: %w", err)
		}
		c.ServeDir = sd
	}

	// Default access policy from the bind address: a loopback bind is
	// unreachable from elsewhere anyway ("local"); a wider bind defaults to
	// "lan" so a stray port-forward can't expose it to the internet.
	c.Access = strings.ToLower(strings.TrimSpace(c.Access))
	if c.Access == "" {
		if c.PublicHost() {
			c.Access = "lan"
		} else {
			c.Access = "local"
		}
	}
	return nil
}

// Validate checks the resolved configuration for obviously bad values.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d out of range (1-65535)", c.Port)
	}
	if c.Host == "" {
		return fmt.Errorf("host must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log level %q (want debug|info|warn|error)", c.LogLevel)
	}
	switch c.Access {
	case "local", "lan", "public":
	default:
		return fmt.Errorf("invalid access %q (want local|lan|public)", c.Access)
	}
	if c.MaxFileSize < 0 || c.MaxStorage < 0 {
		return fmt.Errorf("size limits must not be negative")
	}
	if c.MaxStorage > 0 && c.MaxFileSize > c.MaxStorage {
		return fmt.Errorf("--max-file-size (%d) exceeds --max-storage (%d)", c.MaxFileSize, c.MaxStorage)
	}
	if c.CleanupInterval > 0 && c.CleanupInterval < time.Minute {
		return fmt.Errorf("cleanup interval %s too small (min 1m)", c.CleanupInterval)
	}
	if c.ServeDir != "" {
		st, err := os.Stat(c.ServeDir)
		if err != nil {
			return fmt.Errorf("--dir %s: %w", c.ServeDir, err)
		}
		if !st.IsDir() {
			return fmt.Errorf("--dir %s is not a directory", c.ServeDir)
		}
	}
	return nil
}

// DirectoryMode reports whether MyShare is serving a real folder from disk.
func (c *Config) DirectoryMode() bool { return c.ServeDir != "" }

// PublicHost reports whether the server is bound beyond loopback.
func (c *Config) PublicHost() bool {
	switch c.Host {
	case "127.0.0.1", "::1", "localhost", "":
		return false
	default:
		return true
	}
}

// --- helpers ------------------------------------------------------------

func mergeFile(dst *Config, src Config) {
	if src.Host != "" {
		dst.Host = src.Host
	}
	if src.Port != 0 {
		dst.Port = src.Port
	}
	if src.DataDir != "" {
		dst.DataDir = src.DataDir
	}
	if src.MaxFileSize != 0 {
		dst.MaxFileSize = src.MaxFileSize
	}
	if src.MaxStorage != 0 {
		dst.MaxStorage = src.MaxStorage
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
	if src.Access != "" {
		dst.Access = src.Access
	}
	if src.ServeDir != "" {
		dst.ServeDir = src.ServeDir
	}
	dst.Ephemeral = src.Ephemeral
	if src.CleanupInterval != 0 {
		dst.CleanupInterval = src.CleanupInterval
	}
	// bools from a file always apply (no sentinel for false)
	dst.Auth = src.Auth
	dst.TLS = src.TLS
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), string(filepath.Separator)))
		}
	}
	return p
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "MyShare"
	}
	return filepath.Join(home, "MyShare")
}

func defaultConfigFile() string {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "myshare", "config.toml")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "myshare", "config.toml")
		}
	default:
		if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
			return filepath.Join(dir, "myshare", "config.toml")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "myshare", "config.toml")
		}
	}
	return ""
}

// DefaultConfigFile is the OS-appropriate path MyShare looks for a config file.
func DefaultConfigFile() string { return defaultConfigFile() }
