package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent,omitempty"`
	Root    string  `json:"root"`
	Entries []Entry `json:"entries"`
}

func List(root, rel string) (Listing, error) {
	root = filepath.Clean(root)
	if root == "" {
		return Listing{}, fmt.Errorf("browse root is empty")
	}
	target := root
	if rel != "" {
		if filepath.IsAbs(rel) {
			target = rel
		} else {
			target = filepath.Join(root, rel)
		}
	}
	target = filepath.Clean(target)
	if !Within(root, target) {
		return Listing{}, fmt.Errorf("path %q is outside browse root", rel)
	}
	info, err := os.Stat(target)
	if err != nil {
		return Listing{}, err
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("not a directory: %s", target)
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Path: target, Root: root, Entries: []Entry{}}
	if target != root {
		out.Parent = filepath.Dir(target)
	}
	for _, e := range ents {
		p := filepath.Join(target, e.Name())
		if !Within(root, p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		out.Entries = append(out.Entries, Entry{Name: e.Name(), Path: p, Dir: true})
	}
	return out, nil
}

func Rel(root, path string) (string, error) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if !Within(root, path) {
		return "", fmt.Errorf("path %q is outside browse root", path)
	}
	if path == root {
		return "", nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return rel, nil
}

func Within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == string(os.PathSeparator) {
		return path == root || filepath.IsAbs(path)
	}
	if path == root {
		return true
	}
	prefix := root + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}
