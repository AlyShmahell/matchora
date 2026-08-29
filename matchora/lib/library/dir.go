package library

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/match"
)

const catalogDir = "catalog"

func Root(dataDir string) string {
	return filepath.Join(dataDir, catalogDir)
}

func ident(cfg config.Config, provider string) string {
	if spec, ok := cfg.Providers[provider]; ok {
		if s := strings.TrimSpace(spec.UniqueID); s != "" {
			return s
		}
	}
	return provider
}

func Prefix(cfg config.Config, provider, id string) string {
	return "[" + ident(cfg, provider) + "-" + id + "]"
}

func Prefixes(cfg config.Config, provider, id string) []string {
	want := Prefix(cfg, provider, id)
	raw := "[" + provider + "-" + id + "]"
	if raw == want {
		return []string{want}
	}
	return []string{want, raw}
}

func DirName(cfg config.Config, cand match.Candidate) string {
	name := Prefix(cfg, cand.Provider, cand.ID)
	title := sanitize(cand.Title)
	if title != "" {
		name += " " + title
	}
	if y := sanitize(cand.Year); y != "" {
		name += " (" + y + ")"
	}
	return name
}

func SameTitle(cfg config.Config, providerA, idA, providerB, idB string) bool {
	if idA == "" || idA != idB {
		return false
	}
	if providerA == providerB {
		return true
	}
	ia, ib := ident(cfg, providerA), ident(cfg, providerB)
	return ia == ib || ia == providerB || ib == providerA
}

func Remove(cfg config.Config, provider, id string) error {
	dir, err := FindDir(cfg, Root(cfg.DataDir), provider, id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func RemoveAll(dataDir string) error {
	err := os.RemoveAll(Root(dataDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func FindDir(cfg config.Config, root, provider, id string) (string, error) {
	prefixes := Prefixes(cfg, provider, id)
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", err
	}
	for _, prefix := range prefixes {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			n := e.Name()
			if n == prefix || strings.HasPrefix(n, prefix+" ") {
				return filepath.Join(root, n), nil
			}
		}
	}
	return "", os.ErrNotExist
}

func parsePrefix(name string) (provider, id string, ok bool) {
	if !strings.HasPrefix(name, "[") {
		return "", "", false
	}
	end := strings.Index(name, "]")
	if end < 2 {
		return "", "", false
	}
	inner := name[1:end]
	i := strings.LastIndex(inner, "-")
	if i <= 0 || i == len(inner)-1 {
		return "", "", false
	}
	return inner[:i], inner[i+1:], true
}

func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			continue
		}
		if r < 32 || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func seasonDirName(number string) string {
	if n, err := strconv.Atoi(number); err == nil {
		return "Season " + pad2(n)
	}
	s := sanitize(number)
	if s == "" {
		return "Season 00"
	}
	return "Season " + s
}

func parseSeasonDir(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, "Season ")
	if !ok {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if n, err := strconv.Atoi(rest); err == nil {
		return strconv.Itoa(n), true
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

func episodeBase(season, number, title string) string {
	sn, _ := strconv.Atoi(season)
	en, _ := strconv.Atoi(number)
	base := "S" + pad2(sn) + "E" + pad2(en)
	t := sanitize(title)
	if t != "" {
		base += " " + t
	}
	return base
}

func pad2(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
