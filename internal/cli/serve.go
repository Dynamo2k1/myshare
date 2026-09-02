package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dynamo2k1/myshare/internal/app"
	"github.com/dynamo2k1/myshare/internal/config"
	"github.com/dynamo2k1/myshare/internal/diskusage"
	"github.com/dynamo2k1/myshare/internal/netinfo"
)

func addServeFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("host", "", "address to bind (default 127.0.0.1; use 0.0.0.0 for LAN access)")
	f.Int("port", 0, "TCP port to listen on (default 8787)")
	f.String("data-dir", "", "directory for the database and file storage (default ~/MyShare)")
	f.String("max-file-size", "", "reject uploads larger than this (e.g. 5GB; default unlimited)")
	f.String("max-storage", "", "cap total stored bytes (e.g. 100GB; default unlimited)")
	f.Bool("auth", false, "require a password (set it with: myshare set-password)")
	f.String("log-level", "", "debug | info | warn | error (default info)")
	f.Bool("tls", false, "serve HTTPS with a self-signed certificate (enables full clipboard support on LAN)")
	f.String("access", "", "who may connect: local | lan | public  (default: local, or lan when --host is 0.0.0.0)")
	f.String("dir", "", "serve this real folder in the Files tab (browse, upload, delete real files)")
	f.Bool("ephemeral", false, "keep MyShare's own state in a temp dir and delete it on exit (never touches served files)")
	f.String("cleanup-interval", "", "how often to run background cleanup (e.g. 1h; default 1h)")
	f.String("config", "", "path to a TOML config file")
	f.String("dev-proxy", "", "proxy non-API routes to a Vite dev server (development only)")
	f.MarkHidden("dev-proxy") //nolint:errcheck
}

func overridesFromFlags(cmd *cobra.Command) config.Overrides {
	var ov config.Overrides
	f := cmd.Flags()
	if f.Changed("host") {
		v, _ := f.GetString("host")
		ov.Host = &v
	}
	if f.Changed("port") {
		v, _ := f.GetInt("port")
		ov.Port = &v
	}
	if f.Changed("data-dir") {
		v, _ := f.GetString("data-dir")
		ov.DataDir = &v
	}
	if f.Changed("max-file-size") {
		v, _ := f.GetString("max-file-size")
		ov.MaxFileSize = &v
	}
	if f.Changed("max-storage") {
		v, _ := f.GetString("max-storage")
		ov.MaxStorage = &v
	}
	if f.Changed("auth") {
		v, _ := f.GetBool("auth")
		ov.Auth = &v
	}
	if f.Changed("log-level") {
		v, _ := f.GetString("log-level")
		ov.LogLevel = &v
	}
	if f.Changed("tls") {
		v, _ := f.GetBool("tls")
		ov.TLS = &v
	}
	if f.Changed("access") {
		v, _ := f.GetString("access")
		ov.Access = &v
	}
	if f.Changed("dir") {
		v, _ := f.GetString("dir")
		ov.ServeDir = &v
	}
	if f.Changed("ephemeral") {
		v, _ := f.GetBool("ephemeral")
		ov.Ephemeral = &v
	}
	if f.Changed("cleanup-interval") {
		v, _ := f.GetString("cleanup-interval")
		ov.CleanupInterval = &v
	}
	if f.Changed("config") {
		v, _ := f.GetString("config")
		ov.ConfigFile = &v
	}
	return ov
}

func runServe(cmd *cobra.Command, args []string) error {
	ov := overridesFromFlags(cmd)
	// A positional argument is shorthand for --dir.
	if len(args) == 1 && ov.ServeDir == nil {
		ov.ServeDir = &args[0]
	}
	cfg, err := config.Load(ov)
	if err != nil {
		return err
	}
	devProxy, _ := cmd.Flags().GetString("dev-proxy")

	log := newLogger(cfg.LogLevel)

	// --- Bind the listener FIRST, before any other init, so a port conflict is
	// reported immediately and clearly (spec §38: never silently switch ports).
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return portError(err, cfg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, log, devProxy)
	if err != nil {
		ln.Close()
		return err
	}

	printBanner(cfg, application.Disk())

	switch cfg.Access {
	case "public":
		log.Warn("access=public — this server accepts connections from ANY address, including the internet. " +
			"Only do this behind a VPN or a trusted reverse proxy, and with --auth enabled.")
		if !cfg.Auth {
			log.Warn("authentication is OFF while access=public. Run 'myshare set-password' and restart with --auth.")
		}
	case "lan":
		log.Info("access=lan — only devices on your local network can connect (loopback + private IP ranges).")
		if cfg.PublicHost() && !cfg.Auth {
			log.Warn("authentication is OFF — anyone on your LAN can read and upload files. " +
				"Enable it with --auth after running: myshare set-password")
		}
	default: // local
		log.Info("access=local — only this machine can connect.")
	}

	if err := application.Serve(ctx, ln); err != nil {
		return err
	}
	log.Info("stopped")
	return nil
}

func portError(err error, cfg config.Config) error {
	if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") {
		next := freePortNear(cfg.Host, cfg.Port)
		msg := fmt.Sprintf("port %d on %s is already in use.", cfg.Port, cfg.Host)
		if next > 0 {
			msg += fmt.Sprintf("\nTry another port, for example:\n    myshare --port %d", next)
		}
		return errors.New(msg)
	}
	if errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("permission denied binding %s:%d (ports below 1024 need elevated privileges; pick a higher port)", cfg.Host, cfg.Port)
	}
	return fmt.Errorf("cannot listen on %s:%d: %w", cfg.Host, cfg.Port, err)
}

func freePortNear(host string, start int) int {
	for p := start + 1; p < start+20 && p < 65536; p++ {
		l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err == nil {
			l.Close()
			return p
		}
	}
	return 0
}

func printBanner(cfg config.Config, du diskusage.Usage) {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	fmt.Println()
	fmt.Println("  MyShare is running")
	fmt.Println()
	fmt.Printf("  Local:    %s://localhost:%d\n", scheme, cfg.Port)
	if cfg.PublicHost() {
		for _, ip := range netinfo.LANIPs() {
			fmt.Printf("  LAN:      %s://%s:%d\n", scheme, ip, cfg.Port)
		}
	} else if ip := netinfo.PrimaryLANIP(); ip != "" {
		fmt.Printf("  LAN:      (disabled — start with --host 0.0.0.0 to reach this from %s)\n", ip)
	}
	fmt.Println()
	if cfg.DirectoryMode() {
		fmt.Printf("  Serving:  %s   (real folder — the Files tab browses it)\n", cfg.ServeDir)
	}
	if cfg.Ephemeral {
		fmt.Println("  State:    temporary — MyShare's database is deleted on exit (served files are kept)")
	} else {
		fmt.Printf("  Data:     %s\n", cfg.DataDir)
	}
	if du.Total > 0 {
		fmt.Printf("  Storage:  %s used, %s free (%s)\n",
			human(int64(du.Used)), human(int64(du.Free)), du.FSType)
	}
	fmt.Printf("  Access:   %s%s\n", cfg.Access, accessNote(cfg.Access))
	if cfg.Auth {
		fmt.Println("  Auth:     enabled")
	}
	fmt.Println()
	fmt.Println("  Press Ctrl+C to stop.")
	fmt.Println()
}

func accessNote(mode string) string {
	switch mode {
	case "local":
		return "  (this machine only)"
	case "lan":
		return "  (local network only)"
	case "public":
		return "  (⚠ reachable from anywhere)"
	default:
		return ""
	}
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

func human(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
