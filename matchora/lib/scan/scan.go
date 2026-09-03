package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	matchfs "github.com/alyshmahell/matchora/lib/fs"
)

type Item struct {
	Raw    string
	Path   string
	Parent string
}

type Child struct {
	Path    string
	Listing string
	Videos  int
}

func Walk(root, target string) ([]Item, error) {
	root = filepath.Clean(root)
	if target == "" {
		target = root
	}
	target = filepath.Clean(target)
	if !matchfs.Within(root, target) {
		return nil, os.ErrPermission
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	parent := filepath.Base(target)
	var out []Item
	for _, e := range ents {
		p := filepath.Join(target, e.Name())
		if !matchfs.Within(root, p) {
			continue
		}
		st, err := os.Stat(p)
		if err == nil && st.IsDir() {
			if skipDir(e.Name()) {
				continue
			}
			out = append(out, Item{Raw: e.Name(), Path: p, Parent: parent})
			continue
		}
		if isVideo(e.Name()) {
			out = append(out, Item{Raw: e.Name(), Path: p, Parent: parent})
		}
	}
	return out, nil
}

func ListVideos(root, target string) ([]string, error) {
	root, target, err := resolve(root, target)
	if err != nil {
		return nil, err
	}
	var out []string
	if err := collectVideos(root, target, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// collectVideos uses ReadDir + Stat so symlink directories (library folders) are followed.
// filepath.WalkDir does not enter a symlink-to-dir start or child.
func collectVideos(root, dir string, out *[]string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if !matchfs.Within(root, p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			if err := collectVideos(root, p, out); err != nil {
				return err
			}
			continue
		}
		if isVideo(e.Name()) {
			*out = append(*out, p)
		}
	}
	return nil
}

func Children(root, target string, sampleVideos int) ([]Child, error) {
	root, target, err := resolve(root, target)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	parent := filepath.Base(target)
	var out []Child
	for _, e := range ents {
		p := filepath.Join(target, e.Name())
		if !matchfs.Within(root, p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			listing, n := formatDir(e.Name(), p, parent, sampleVideos)
			out = append(out, Child{Path: p, Listing: listing, Videos: n})
			continue
		}
		if isVideo(e.Name()) {
			out = append(out, Child{Path: p, Listing: formatFile(e.Name(), parent), Videos: 1})
		}
	}
	return out, nil
}

func formatFile(name, parent string) string {
	var b strings.Builder
	b.WriteString("File: ")
	b.WriteString(name)
	b.WriteByte('\n')
	if parent != "" {
		b.WriteString("Parent: ")
		b.WriteString(parent)
		b.WriteByte('\n')
	}
	return b.String()
}

func formatDir(name, path, parent string, sampleVideos int) (string, int) {
	var b strings.Builder
	b.WriteString("Folder: ")
	b.WriteString(name)
	b.WriteByte('\n')
	if parent != "" && parent != name {
		b.WriteString("Parent: ")
		b.WriteString(parent)
		b.WriteByte('\n')
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return b.String(), 0
	}
	total := 0
	for _, e := range ents {
		p := filepath.Join(path, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			names, n := videoSample(p, sampleVideos)
			total += n
			fmt.Fprintf(&b, "  - %s/ (%d videos", e.Name(), n)
			if len(names) > 0 {
				b.WriteString(": ")
				b.WriteString(strings.Join(names, ", "))
				if n > len(names) {
					b.WriteString(", ...")
				}
			}
			b.WriteString(")\n")
			continue
		}
		if isVideo(e.Name()) {
			total++
			b.WriteString("  - ")
			b.WriteString(e.Name())
			b.WriteByte('\n')
		}
	}
	return b.String(), total
}

func videoSample(dir string, max int) ([]string, int) {
	var names []string
	n := 0
	var walk func(string) error
	walk = func(cur string) error {
		ents, err := os.ReadDir(cur)
		if err != nil {
			return nil
		}
		for _, e := range ents {
			p := filepath.Join(cur, e.Name())
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if st.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if !isVideo(e.Name()) {
				continue
			}
			n++
			if len(names) < max {
				names = append(names, e.Name())
			}
		}
		return nil
	}
	_ = walk(dir)
	return names, n
}

func resolve(root, target string) (string, string, error) {
	root = filepath.Clean(root)
	if target == "" {
		target = root
	}
	target = filepath.Clean(target)
	if !matchfs.Within(root, target) {
		return "", "", os.ErrPermission
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", os.ErrInvalid
	}
	return root, target, nil
}

func skipExtras(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "behind the scenes", "deleted scenes", "trailers", "interviews",
		"scenes", "featurettes", "shorts", "other", "extras":
		return true
	default:
		return false
	}
}

func skipDir(name string) bool {
	if skipExtras(name) {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "season") {
		rest := strings.TrimLeft(n[len("season"):], " ._-")
		return rest != "" && isDigits(rest)
	}
	if len(n) >= 2 && (n[0] == 's' || n[0] == 'S') && isDigits(n[1:]) {
		return true
	}
	return false
}

func isVideo(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".ts", ".m2ts":
		return true
	default:
		return false
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
