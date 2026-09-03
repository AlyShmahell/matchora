package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/library"
	"github.com/alyshmahell/matchora/lib/match"
)

const (
	sessionTimeLayout = "20060102T150405Z"
	jobsFilePrefix    = "jobs-"
	jobsFileSuffix    = ".json"
)

var sessionIDRe = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{16}$`)

func NewSessionID(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format(sessionTimeLayout) + "-" + hex.EncodeToString(b[:])
}

func ValidSessionID(id string) bool {
	return sessionIDRe.MatchString(strings.TrimSpace(id))
}

func SessionCreated(id string) (time.Time, bool) {
	id = strings.TrimSpace(id)
	if !ValidSessionID(id) {
		return time.Time{}, false
	}
	stamp, _, ok := strings.Cut(id, "-")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(sessionTimeLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func SessionExpired(id string, now time.Time, ttl time.Duration) bool {
	created, ok := SessionCreated(id)
	if !ok {
		return true
	}
	if ttl <= 0 {
		return false
	}
	return now.UTC().After(created.Add(ttl))
}

func jobsFile(dir, session string) string {
	return filepath.Join(dir, jobsFilePrefix+session+jobsFileSuffix)
}

func sessionFromFile(name string) (string, bool) {
	if !strings.HasPrefix(name, jobsFilePrefix) || !strings.HasSuffix(name, jobsFileSuffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, jobsFilePrefix), jobsFileSuffix)
	if !ValidSessionID(id) {
		return "", false
	}
	return id, true
}

func jobPins(cfg config.Config, job match.Job, provider, id string) bool {
	if job.Match == nil {
		return false
	}
	return library.SameTitle(cfg, job.Match.Provider, job.Match.ID, provider, id)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
