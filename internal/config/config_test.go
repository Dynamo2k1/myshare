package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"unlimited", 0, false},
		{"1024", 1024, false},
		{"1KB", 1000, false},
		{"1KiB", 1024, false},
		{"5GB", 5_000_000_000, false},
		{"5GiB", 5 * (1 << 30), false},
		{"1.5MiB", 1_572_864, false},
		{"2 tb", 2_000_000_000_000, false},
		{"garbage", 0, true},
		{"-5MB", 0, true},
		{"MB", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseSize(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPrecedence_CLIOverridesEnvOverridesDefault(t *testing.T) {
	t.Setenv("MYSHARE_PORT", "9000")
	t.Setenv("MYSHARE_LOG_LEVEL", "warn")
	t.Setenv("MYSHARE_CONFIG", "") // ensure no file picked up

	// env only
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 9000 {
		t.Errorf("env port not applied: got %d", cfg.Port)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("env log level not applied: got %q", cfg.LogLevel)
	}

	// CLI beats env
	p := 12345
	cfg, err = Load(Overrides{Port: &p})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 12345 {
		t.Errorf("CLI port did not override env: got %d", cfg.Port)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("env log level should still apply: got %q", cfg.LogLevel)
	}
}

func TestConfigFile_LowestButAboveDefaults(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.toml")
	body := `
port = 7777
log_level = "debug"
data_dir = "` + filepath.ToSlash(filepath.Join(dir, "data")) + `"
cleanup_interval = "2h"
`
	if err := os.WriteFile(cf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{ConfigFile: &cf})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 7777 {
		t.Errorf("file port: got %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("file log level: got %q", cfg.LogLevel)
	}
	if cfg.CleanupInterval != 2*time.Hour {
		t.Errorf("file cleanup interval: got %s", cfg.CleanupInterval)
	}

	// env beats file
	t.Setenv("MYSHARE_PORT", "8888")
	cfg, err = Load(Overrides{ConfigFile: &cf})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 8888 {
		t.Errorf("env should beat file: got %d", cfg.Port)
	}
}

func TestValidate(t *testing.T) {
	bad := []Overrides{
		{Port: intp(0)},
		{Port: intp(70000)},
		{LogLevel: strp("loud")},
	}
	for i, ov := range bad {
		if _, err := Load(ov); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestPublicHost(t *testing.T) {
	c := Config{Host: "127.0.0.1"}
	if c.PublicHost() {
		t.Error("127.0.0.1 should not be public")
	}
	c.Host = "0.0.0.0"
	if !c.PublicHost() {
		t.Error("0.0.0.0 should be public")
	}
}

func TestDataDirAbsoluteAndHomeExpanded(t *testing.T) {
	home, _ := os.UserHomeDir()
	d := "~/msharetest"
	cfg, err := Load(Overrides{DataDir: &d})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := filepath.Join(home, "msharetest")
	if cfg.DataDir != want {
		t.Errorf("data dir = %q, want %q", cfg.DataDir, want)
	}
}

func intp(i int) *int       { return &i }
func strp(s string) *string { return &s }
