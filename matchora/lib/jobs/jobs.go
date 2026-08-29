package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
	"github.com/alyshmahell/matchora/lib/ingest"
	"github.com/alyshmahell/matchora/lib/match"
)

var ErrInvalidSession = errors.New("invalid session")

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dataDir string) *Store {
	return &Store{dir: dataDir}
}

func FromRows(rows []ingest.Row, source string) []match.Job {
	out := make([]match.Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, match.Job{
			ID:      newID(),
			Source:  source,
			Title:   row.Title,
			Year:    row.Year,
			Type:    row.Type,
			Season:  row.Season,
			Episode: row.Episode,
			IMDB:    row.IMDB,
			Status:  "pending",
		})
	}
	return out
}

func (s *Store) Create(session string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidSessionID(session) {
		return ErrInvalidSession
	}
	return s.write(session, []match.Job{})
}

func (s *Store) Has(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ValidSessionID(session) && fileExists(jobsFile(s.dir, session))
}

func (s *Store) PurgeExpired(ttl time.Duration) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.purgeUnlocked(ttl)
}

func (s *Store) Sessions(ttl time.Duration) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeUnlocked(ttl)
	return s.listIDsUnlocked(), nil
}

func (s *Store) List(session string) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return nil, err
	}
	return s.read(session)
}

func (s *Store) ListAll(ttl time.Duration) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeUnlocked(ttl)
	var all []match.Job
	for _, id := range s.listIDsUnlocked() {
		cur, err := s.read(id)
		if err != nil {
			return nil, err
		}
		all = append(all, cur...)
	}
	if all == nil {
		all = []match.Job{}
	}
	return all, nil
}

func (s *Store) Append(session string, extra []match.Job) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return nil, err
	}
	cur, err := s.read(session)
	if err != nil {
		return nil, err
	}
	cur = append(cur, extra...)
	if err := s.write(session, cur); err != nil {
		return nil, err
	}
	return extra, nil
}

func (s *Store) ReplaceAll(session string, all []match.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidSessionID(session) {
		return ErrInvalidSession
	}
	return s.write(session, all)
}

func (s *Store) Clear(session string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidSessionID(session) {
		return ErrInvalidSession
	}
	err := os.Remove(jobsFile(s.dir, session))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) MarkPending(session string) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return nil, err
	}
	cur, err := s.read(session)
	if err != nil {
		return nil, err
	}
	for i := range cur {
		cur[i].Status = "pending"
		cur[i].Ranker = ""
		cur[i].Match = nil
		cur[i].Candidates = nil
		cur[i].Sub = nil
		cur[i].Catalog = nil
		cur[i].CatalogFor = ""
		cur[i].Error = ""
	}
	if err := s.write(session, cur); err != nil {
		return nil, err
	}
	return cur, nil
}

func (s *Store) MarkErrorsPending(session string) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return nil, err
	}
	cur, err := s.read(session)
	if err != nil {
		return nil, err
	}
	changed := make([]match.Job, 0)
	for i := range cur {
		if cur[i].Status != "error" && cur[i].Status != "unmatched" {
			continue
		}
		cur[i].Status = "pending"
		cur[i].Ranker = ""
		cur[i].Match = nil
		cur[i].Candidates = nil
		cur[i].Sub = nil
		cur[i].Catalog = nil
		cur[i].CatalogFor = ""
		cur[i].Error = ""
		changed = append(changed, cur[i])
	}
	if err := s.write(session, cur); err != nil {
		return nil, err
	}
	return changed, nil
}

func (s *Store) Get(session, id string) (match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return match.Job{}, err
	}
	cur, err := s.read(session)
	if err != nil {
		return match.Job{}, err
	}
	for _, j := range cur {
		if j.ID == id {
			return j, nil
		}
	}
	return match.Job{}, os.ErrNotExist
}

func (s *Store) Update(session string, ids map[string]match.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.require(session); err != nil {
		return err
	}
	cur, err := s.read(session)
	if err != nil {
		return err
	}
	for i, j := range cur {
		if next, ok := ids[j.ID]; ok {
			cur[i] = next
		}
	}
	return s.write(session, cur)
}

func (s *Store) UpdateAny(ttl time.Duration, ids map[string]match.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeUnlocked(ttl)
	for _, sess := range s.listIDsUnlocked() {
		cur, err := s.read(sess)
		if err != nil {
			return err
		}
		changed := false
		for i, j := range cur {
			if next, ok := ids[j.ID]; ok {
				cur[i] = next
				changed = true
			}
		}
		if changed {
			return s.write(sess, cur)
		}
	}
	return os.ErrNotExist
}

func (s *Store) Pinning(ttl time.Duration, cfg config.Config, provider, id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeUnlocked(ttl)
	var out []string
	for _, sess := range s.listIDsUnlocked() {
		cur, err := s.read(sess)
		if err != nil {
			return nil, err
		}
		for _, j := range cur {
			if jobPins(cfg, j, provider, id) {
				out = append(out, sess)
				break
			}
		}
	}
	return out, nil
}

func (s *Store) PinningAny(ttl time.Duration) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeUnlocked(ttl)
	var out []string
	for _, sess := range s.listIDsUnlocked() {
		cur, err := s.read(sess)
		if err != nil {
			return nil, err
		}
		for _, j := range cur {
			if j.Match != nil {
				out = append(out, sess)
				break
			}
		}
	}
	return out, nil
}

func (s *Store) require(session string) error {
	if !ValidSessionID(session) {
		return ErrInvalidSession
	}
	if !fileExists(jobsFile(s.dir, session)) {
		return os.ErrNotExist
	}
	return nil
}

func (s *Store) purgeUnlocked(ttl time.Duration) []string {
	now := time.Now().UTC()
	var gone []string
	for _, id := range s.listIDsUnlocked() {
		if !SessionExpired(id, now, ttl) {
			continue
		}
		_ = os.Remove(jobsFile(s.dir, id))
		gone = append(gone, id)
	}
	return gone
}

func (s *Store) listIDsUnlocked() []string {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		id, ok := sessionFromFile(e.Name())
		if !ok {
			continue
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

func (s *Store) read(session string) ([]match.Job, error) {
	b, err := os.ReadFile(jobsFile(s.dir, session))
	if err != nil {
		if os.IsNotExist(err) {
			return []match.Job{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return []match.Job{}, nil
	}
	var list []match.Job
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []match.Job{}
	}
	return list, nil
}

func (s *Store) write(session string, list []match.Job) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if list == nil {
		list = []match.Job{}
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	path := jobsFile(s.dir, session)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
