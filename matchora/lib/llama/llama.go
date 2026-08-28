package llama

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

var spawned *exec.Cmd

func Start(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	probe := cfg.LlamaProbeURL()
	llm := cfg.Llama.LLMBaseURL
	if healthy(probe) {
		cfg.Llama.BaseURL = probe
	} else {
		binDir := cfg.LlamaBinDir()
		modelsDir := cfg.LlamaModelsDir()
		if err := os.MkdirAll(modelsDir, 0o755); err != nil {
			return err
		}
		if err := ensureBin(*cfg, binDir); err != nil {
			return err
		}
		if err := stageModel(cfg.Llama.EmbedFile, cfg.Llama.EmbedURL, modelsDir); err != nil {
			return fmt.Errorf("embed model: %w", err)
		}
		if cfg.LocalInstruct() {
			if err := stageModel(cfg.Llama.InstructFile, cfg.Llama.InstructURL, modelsDir); err != nil {
				return fmt.Errorf("instruct model: %w", err)
			}
		}
		ngl := nglOf(*cfg)
		if cfg.Llama.Port < 1 {
			cfg.Llama.Port = 8080
		}
		if err := spawn(filepath.Join(binDir, "llama-server"), binDir, cfg.Llama.Port, ngl,
			"--metrics", "--embeddings", "--pooling", "mean", "--jinja",
			"--ctx-size", "8192",
			"--chat-template-kwargs", `{"enable_thinking": false}`,
			"--models-dir", modelsDir,
		); err != nil {
			return err
		}
		cfg.Llama.BaseURL = cfg.LlamaVendorURL()
		if err := waitHealth(cfg.Llama.BaseURL, 120*time.Second); err != nil {
			return fmt.Errorf("llama-server: %w", err)
		}
	}
	if config.InstructFollowsListen(llm, probe) {
		cfg.Llama.LLMBaseURL = cfg.Llama.BaseURL
	}
	if err := ensureListed(*cfg, cfg.Llama.EmbedFile, cfg.Llama.EmbedURL); err != nil {
		return fmt.Errorf("embed model: %w", err)
	}
	if cfg.LocalInstruct() {
		if err := ensureListed(*cfg, cfg.Llama.InstructFile, cfg.Llama.InstructURL); err != nil {
			return fmt.Errorf("instruct model: %w", err)
		}
	}
	return nil
}

func stageModel(file, rawURL, modelsDir string) error {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil
	}
	return ensureFile(rawURL, filepath.Join(modelsDir, filepath.Base(file)))
}

func ensureListed(cfg config.Config, file, rawURL string) error {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil
	}
	ids, err := listModels(cfg.Llama.BaseURL, false)
	if err != nil {
		log.Printf("llama: list models: %v", err)
		ids = nil
	}
	if modelListed(ids, file) {
		return nil
	}
	dest := filepath.Join(cfg.LlamaModelsDir(), filepath.Base(file))
	if err := ensureFile(rawURL, dest); err != nil {
		return err
	}
	ids, err = listModels(cfg.Llama.BaseURL, true)
	if err == nil && modelListed(ids, file) {
		return nil
	}
	var loadErr error
	stem := strings.TrimSuffix(filepath.Base(file), ".gguf")
	for _, name := range []string{filepath.Base(file), stem, dest} {
		if name == "" {
			continue
		}
		loadErr = loadModel(cfg.Llama.BaseURL, name)
		if loadErr == nil {
			break
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ids, err := listModels(cfg.Llama.BaseURL, loadErr != nil)
		if err == nil && modelListed(ids, file) {
			return nil
		}
		time.Sleep(time.Second)
	}
	if loadErr != nil {
		return loadErr
	}
	return fmt.Errorf("%s not in llama-server model list", file)
}

func healthy(base string) bool {
	u := originOf(base)
	if u == "" {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/health", "/v1/models"} {
		resp, err := client.Get(u + path)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 500 {
			return true
		}
	}
	return false
}

func listModels(base string, reload bool) ([]string, error) {
	u := originOf(base)
	client := &http.Client{Timeout: 5 * time.Second}
	var last error
	paths := []string{"/v1/models", "/models"}
	if reload {
		paths = []string{"/models?reload=1", "/v1/models?reload=1", "/v1/models", "/models"}
	}
	var ids []string
	seen := map[string]bool{}
	ok := false
	for _, path := range paths {
		resp, err := client.Get(u + path)
		if err != nil {
			last = err
			continue
		}
		b, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if resp.StatusCode >= 400 {
			last = fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
			continue
		}
		ok = true
		for _, id := range parseModelIDs(b) {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if ok {
		return ids, nil
	}
	if last == nil {
		last = fmt.Errorf("no models endpoint at %s", u)
	}
	return nil, last
}

func parseModelIDs(raw []byte) []string {
	var ids []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			ids = append(ids, s)
		}
	}
	var wrapped struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		for _, m := range wrapped.Data {
			for _, s := range modelFields(m) {
				add(s)
			}
		}
		for _, m := range wrapped.Models {
			for _, s := range modelFields(m) {
				add(s)
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, m := range arr {
			for _, s := range modelFields(m) {
				add(s)
			}
		}
	}
	return ids
}

func modelFields(raw []byte) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return []string{s}
		}
		return nil
	}
	var m struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Model string `json:"model"`
		Path  string `json:"path"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	out := []string{m.ID, m.Name, m.Model}
	if strings.TrimSpace(m.Path) != "" {
		out = append(out, filepath.Base(m.Path))
	}
	return out
}

func modelListed(ids []string, file string) bool {
	want := []string{
		file,
		filepath.Base(file),
		strings.TrimSuffix(filepath.Base(file), ".gguf"),
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		base := filepath.Base(id)
		stem := strings.TrimSuffix(base, ".gguf")
		for _, w := range want {
			if strings.EqualFold(id, w) || strings.EqualFold(base, w) || strings.EqualFold(stem, w) {
				return true
			}
		}
	}
	return false
}

func loadModel(base, file string) error {
	u := originOf(base) + "/models/load"
	body, err := json.Marshal(map[string]string{"model": file})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /models/load: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func ensureBin(cfg config.Config, binDir string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	server := filepath.Join(binDir, "llama-server")
	if requireExec(server) == nil {
		return nil
	}
	if cfg.Llama.TarballURL == "" {
		return fmt.Errorf("llama.tarball_url is empty")
	}
	tmp, err := os.CreateTemp("", "llama-*.tar.gz")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	log.Printf("llama: downloading runtime")
	if err := download(cfg.Llama.TarballURL, tmpPath); err != nil {
		return err
	}
	if err := extractBin(tmpPath, binDir); err != nil {
		return err
	}
	if err := chmodRX(binDir); err != nil {
		return err
	}
	if err := os.Chmod(server, 0o755); err != nil {
		return fmt.Errorf("llama-server missing after extract: %w", err)
	}
	return requireExec(server)
}

func extractBin(tarball, binDir string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		base := filepath.Base(hdr.Name)
		if base == "." || base == ".." || !wantedBin(base) {
			continue
		}
		dst := filepath.Join(binDir, base)
		switch hdr.Typeflag {
		case tar.TypeSymlink:
			_ = os.Remove(dst)
			if err := os.Symlink(filepath.Base(hdr.Linkname), dst); err != nil {
				return err
			}
		case tar.TypeLink:
			src := filepath.Join(binDir, filepath.Base(hdr.Linkname))
			if err := copyFile(src, dst); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(dst, tr, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

func wantedBin(name string) bool {
	if name == "llama-server" {
		return true
	}
	return strings.Contains(name, ".so")
}

func ensureFile(rawURL, dest string) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return nil
	}
	if rawURL == "" {
		return fmt.Errorf("missing download url for %s", dest)
	}
	log.Printf("llama: downloading %s", filepath.Base(dest))
	return download(rawURL, dest)
}

func download(rawURL, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "matchora")
	client := &http.Client{Timeout: 20 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download %s: status %d", rawURL, resp.StatusCode)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func requireExec(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
		return fmt.Errorf("llama-server is not executable: %s", path)
	}
	return nil
}

func nglOf(cfg config.Config) int {
	if cfg.Llama.GPULayers == 0 {
		return 999
	}
	return cfg.Llama.GPULayers
}

func serverArgs(port, ngl int, extra ...string) []string {
	args := []string{
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-ngl", strconv.Itoa(ngl),
	}
	return append(args, extra...)
}

func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
}

func spawn(bin, binDir string, port, ngl int, extra ...string) error {
	args := serverArgs(port, ngl, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = binDir
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+binDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = procAttr()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	log.Printf("llama: starting :%d", port)
	if err := cmd.Start(); err != nil {
		return err
	}
	spawned = cmd
	return nil
}

func Stop() {
	cmd := spawned
	spawned = nil
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	_ = cmd.Wait()
}

func portOf(base string, fallback int) int {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Port() == "" {
		return fallback
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		return fallback
	}
	return p
}

func waitHealth(base string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if healthy(base) {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("not healthy at %s", base)
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	_ = os.Remove(dst)
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	return writeFile(dst, in, st.Mode())
}

func chmodRX(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		mode := info.Mode() | 0o444
		if d.IsDir() || mode&0o111 != 0 {
			mode |= 0o111
		}
		return os.Chmod(path, mode)
	})
}
