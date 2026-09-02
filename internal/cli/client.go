package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ranauzair/myshare/internal/config"
)

// baseURL returns the local server URL from resolved config.
func baseURL(cfg config.Config) string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	host := cfg.Host
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}
	return scheme + "://" + net_JoinHostPort(host, strconv.Itoa(cfg.Port))
}

func net_JoinHostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func loadCfgFor(cmd *cobra.Command) (config.Config, error) {
	var ov config.Overrides
	if cmd.Flags().Changed("data-dir") {
		v, _ := cmd.Flags().GetString("data-dir")
		ov.DataDir = &v
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		ov.ConfigFile = &v
	}
	if cmd.Flags().Changed("port") {
		v, _ := cmd.Flags().GetInt("port")
		ov.Port = &v
	}
	if cmd.Flags().Changed("url") {
		// handled by caller via --url
	}
	return config.Load(ov)
}

func newUploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload <file>...",
		Short: "Upload one or more files to a running MyShare server",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runUpload,
	}
	cmd.Flags().String("url", "", "server URL (default derived from config, e.g. http://127.0.0.1:8787)")
	cmd.Flags().String("data-dir", "", "data directory (to derive the URL)")
	cmd.Flags().String("config", "", "config file")
	cmd.Flags().Int("port", 0, "server port (to derive the URL)")
	return cmd
}

func runUpload(cmd *cobra.Command, args []string) error {
	base, err := resolveURL(cmd)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 0}
	for _, path := range args {
		if err := uploadOne(cmd.Context(), client, base, path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func uploadOne(ctx context.Context, client *http.Client, base, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/files", pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server said %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out struct {
		File struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"file"`
	}
	_ = json.Unmarshal(body, &out)
	fmt.Printf("uploaded %s (%d bytes) -> id %s\n", out.File.Name, st.Size(), out.File.ID)
	return nil
}

func newClipboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clipboard [text]",
		Short: "Push stdin/args to the shared clipboard, or print the latest entry",
		Long: `With no input, prints the most recent clipboard entry.
With arguments or piped stdin, adds a new clipboard entry.

Examples:
  echo "npm run build" | myshare clipboard
  myshare clipboard "quick note"
  myshare clipboard get`,
		Args: cobra.ArbitraryArgs,
		RunE: runClipboard,
	}
	cmd.Flags().String("url", "", "server URL")
	cmd.Flags().String("data-dir", "", "data directory (to derive the URL)")
	cmd.Flags().String("config", "", "config file")
	cmd.Flags().Int("port", 0, "server port (to derive the URL)")
	return cmd
}

func runClipboard(cmd *cobra.Command, args []string) error {
	base, err := resolveURL(cmd)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 20 * time.Second}

	// "get" or no input -> read latest.
	stdinHasData := false
	if fi, _ := os.Stdin.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		stdinHasData = true
	}
	if (len(args) == 1 && args[0] == "get") || (len(args) == 0 && !stdinHasData) {
		return clipboardGet(cmd.Context(), client, base)
	}

	var content string
	if stdinHasData {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
		content = string(b)
	} else {
		content = strings.Join(args, " ")
	}
	content = strings.TrimRight(content, "\n")
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("nothing to send")
	}

	payload, _ := json.Marshal(map[string]string{"content": content})
	req, _ := http.NewRequestWithContext(cmd.Context(), http.MethodPost, base+"/api/clipboard", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("server said %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	fmt.Println("added to clipboard")
	return nil
}

func clipboardGet(ctx context.Context, client *http.Client, base string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/clipboard?limit=1", nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server said %s", resp.Status)
	}
	var page struct {
		Items []struct {
			Content string `json:"content"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		return fmt.Errorf("clipboard is empty")
	}
	fmt.Print(page.Items[0].Content)
	if !strings.HasSuffix(page.Items[0].Content, "\n") {
		fmt.Println()
	}
	return nil
}

func resolveURL(cmd *cobra.Command) (string, error) {
	if u, _ := cmd.Flags().GetString("url"); u != "" {
		return strings.TrimRight(u, "/"), nil
	}
	cfg, err := loadCfgFor(cmd)
	if err != nil {
		return "", err
	}
	return baseURL(cfg), nil
}
