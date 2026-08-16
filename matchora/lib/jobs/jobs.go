package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/alyshmahell/matchora/lib/ingest"
	"github.com/alyshmahell/matchora/lib/match"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "jobs.json")}
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

func (s *Store) List() ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Store) Append(extra []match.Job) ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
	if err != nil {
		return nil, err
	}
	cur = append(cur, extra...)
	if err := s.write(cur); err != nil {
		return nil, err
	}
	return extra, nil
}

func (s *Store) ReplaceAll(all []match.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(all)
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write([]match.Job{})
}

func (s *Store) MarkPending() ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
	if err != nil {
		return nil, err
	}
	for i := range cur {
		cur[i].Status = "pending"
		cur[i].Ranker = ""
		cur[i].Match = nil
		cur[i].Candidates = nil
		cur[i].Sub = nil
		cur[i].Error = ""
	}
	if err := s.write(cur); err != nil {
		return nil, err
	}
	return cur, nil
}

func (s *Store) MarkErrorsPending() ([]match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
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
		cur[i].Error = ""
		changed = append(changed, cur[i])
	}
	if err := s.write(cur); err != nil {
		return nil, err
	}
	return changed, nil
}

func (s *Store) Get(id string) (match.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
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

func (s *Store) Update(ids map[string]match.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.read()
	if err != nil {
		return err
	}
	for i, j := range cur {
		if next, ok := ids[j.ID]; ok {
			cur[i] = next
		}
	}
	return s.write(cur)
}

func (s *Store) read() ([]match.Job, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []match.Job{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return []match.Job{}, nil
	}
	var jobs []match.Job
	if err := json.Unmarshal(b, &jobs); err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []match.Job{}
	}
	return jobs, nil
}

func (s *Store) write(jobs []match.Job) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
