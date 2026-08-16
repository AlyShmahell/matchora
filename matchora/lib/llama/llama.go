package llama

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

func Start(cfg config.Config) error {
	binDir := cfg.LlamaBinDir()
	modelsDir := cfg.LlamaModelsDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return err
	}
	if err := ensureBin(cfg, binDir); err != nil {
		return err
	}
	embed := filepath.Join(modelsDir, cfg.Llama.ModelFile)
	if err := ensureFile(cfg.Llama.ModelURL, embed); err != nil {
		return err
	}
	server := filepath.Join(binDir, "llama-server")
	ngl := nglOf(cfg)
	if err := spawn(server, binDir, portOf(cfg.Llama.BaseURL, 8080), ngl,
		"--metrics", "--ctx-size", "512", "--embeddings", "--pooling", "mean", "--model", embed); err != nil {
		return err
	}
	if err := waitHealth(cfg.Llama.BaseURL, 120*time.Second); err != nil {
		return fmt.Errorf("embed llama-server: %w", err)
	}
	if !cfg.LocalInstruct() {
		return nil
	}
	instruct := filepath.Join(modelsDir, cfg.Llama.InstructFile)
	if err := ensureFile(cfg.Llama.InstructURL, instruct); err != nil {
		return err
	}
	if err := spawn(server, binDir, portOf(cfg.Llama.LLMBaseURL, 8081), ngl,
		"--metrics", "--ctx-size", "8192", "--jinja",
		"--chat-template-kwargs", `{"enable_thinking": false}`,
		"--model", instruct); err != nil {
		return err
	}
	if err := waitHealth(cfg.Llama.LLMBaseURL, 180*time.Second); err != nil {
		return fmt.Errorf("instruct llama-server: %w", err)
	}
	return nil
}

func ensureBin(cfg config.Config, binDir string) error {
	server := filepath.Join(binDir, "llama-server")
	if st, err := os.Stat(server); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
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
	if _, err := os.Stat(server); err != nil {
		return fmt.Errorf("llama-server missing after extract: %w", err)
	}
	return os.Chmod(server, 0o755)
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

func spawn(bin, binDir string, port, ngl int, extra ...string) error {
	args := serverArgs(port, ngl, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = binDir
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+binDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	log.Printf("llama: starting :%d", port)
	return cmd.Start()
}

func waitHealth(base string, d time.Duration) error {
	u := strings.TrimRight(base, "/")
	if strings.HasSuffix(u, "/v1") {
		u = strings.TrimSuffix(u, "/v1")
	}
	deadline := time.Now().Add(d)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		for _, path := range []string{"/health", "/v1/models"} {
			resp, err := client.Get(u + path)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("not healthy at %s", base)
}

func portOf(raw string, fallback int) int {
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" {
		return fallback
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		return fallback
	}
	return p
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
