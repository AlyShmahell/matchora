package library

import (
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

type Title struct {
	Provider string   `json:"provider"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Year     string   `json:"year,omitempty"`
	Type     string   `json:"type"`
	Dir      string   `json:"dir"`
	Synopsis string   `json:"synopsis,omitempty"`
	Poster   string   `json:"poster,omitempty"`
	Seasons  []Season `json:"seasons,omitempty"`
}

type Season struct {
	ID       string    `json:"id,omitempty"`
	Number   string    `json:"number,omitempty"`
	Title    string    `json:"title"`
	Synopsis string    `json:"synopsis,omitempty"`
	Poster   string    `json:"poster,omitempty"`
	Year     string    `json:"year,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
}

type Episode struct {
	ID       string `json:"id,omitempty"`
	Number   string `json:"number,omitempty"`
	Title    string `json:"title"`
	Synopsis string `json:"synopsis,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Year     string `json:"year,omitempty"`
}

func List(dataDir string) ([]Title, error) {
	root := Root(dataDir)
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Title{}, nil
		}
		return nil, err
	}
	out := make([]Title, 0)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		t, err := loadTitle(root, e.Name(), false)
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func Get(cfg config.Config, provider, id string) (Title, error) {
	root := Root(cfg.DataDir)
	dir, err := FindDir(cfg, root, provider, id)
	if err != nil {
		return Title{}, err
	}
	return loadTitle(root, filepath.Base(dir), true)
}

func PosterFile(cfg config.Config, provider, id, season, episode string) (string, error) {
	root := Root(cfg.DataDir)
	dir, err := FindDir(cfg, root, provider, id)
	if err != nil {
		return "", err
	}
	if season == "" {
		name := findPoster(dir, "poster")
		if name == "" {
			return "", os.ErrNotExist
		}
		return filepath.Join(dir, name), nil
	}
	sdir, err := findSeasonDir(dir, season)
	if err != nil {
		return "", err
	}
	if episode == "" {
		name := findPoster(sdir, "poster")
		if name == "" {
			return "", os.ErrNotExist
		}
		return filepath.Join(sdir, name), nil
	}
	base, err := findEpisodeBase(sdir, episode)
	if err != nil {
		return "", err
	}
	name := findPoster(sdir, base)
	if name == "" {
		return "", os.ErrNotExist
	}
	return filepath.Join(sdir, name), nil
}

func loadTitle(root, name string, full bool) (Title, error) {
	dir := filepath.Join(root, name)
	kind, title, year, plot, uidType, uid, jobType, err := titleFromNFO(dir)
	provider, id := uidType, uid
	if p, i, ok := parsePrefix(name); ok {
		provider, id = p, i
	}
	if err != nil {
		if provider == "" || id == "" {
			return Title{}, err
		}
		kind = "tvshow"
		if _, e := os.Stat(filepath.Join(dir, "movie.nfo")); e == nil {
			kind = "movie"
		}
		title = strings.TrimSpace(strings.TrimPrefix(name, Prefix(config.Config{}, provider, id)))
		title = strings.TrimSpace(strings.TrimPrefix(title, " "))
	}
	if provider == "" || id == "" {
		p, i, ok := parsePrefix(name)
		if ok {
			provider, id = p, i
		}
	}
	t := Title{
		Provider: provider,
		ID:       id,
		Title:    title,
		Year:     year,
		Type:     kind,
		Dir:      name,
		Synopsis: plot,
	}
	if jobType != "" {
		t.Type = jobType
	}
	if p := findPoster(dir, "poster"); p != "" {
		t.Poster = posterURL(provider, id, "", "", extOf(p))
	}
	if full && kind != "movie" {
		t.Seasons, err = loadSeasons(dir, provider, id)
		if err != nil {
			return Title{}, err
		}
	}
	return t, nil
}

func loadSeasons(dir, provider, id string) ([]Season, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Season
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		num, ok := parseSeasonDir(e.Name())
		if !ok {
			continue
		}
		sdir := filepath.Join(dir, e.Name())
		s := Season{Number: num, Title: "Season " + num}
		var n seasonNFO
		if err := readNFO(filepath.Join(sdir, "season.nfo"), &n); err == nil {
			if n.Title != "" {
				s.Title = n.Title
			}
			s.Synopsis = n.Plot
			s.Year = n.Year
			s.ID = n.UniqueID.Value
			if n.SeasonNumber != "" {
				s.Number = padSeason(n.SeasonNumber)
			}
		}
		if p := findPoster(sdir, "poster"); p != "" {
			s.Poster = posterURL(provider, id, s.Number, "", extOf(p))
		}
		s.Episodes, err = loadEpisodes(sdir, provider, id, s.Number)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return numLess(out[i].Number, out[j].Number)
	})
	return out, nil
}

func loadEpisodes(sdir, provider, id, season string) ([]Episode, error) {
	ents, err := os.ReadDir(sdir)
	if err != nil {
		return nil, err
	}
	var out []Episode
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".nfo") {
			continue
		}
		if e.Name() == "season.nfo" {
			continue
		}
		var n episodeNFO
		if err := readNFO(filepath.Join(sdir, e.Name()), &n); err != nil {
			continue
		}
		ep := Episode{
			ID:       n.UniqueID.Value,
			Number:   n.Episode,
			Title:    n.Title,
			Synopsis: n.Plot,
			Year:     n.Year,
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if p := findPoster(sdir, base); p != "" {
			num := ep.Number
			if num == "" {
				num = episodeNumFromBase(base)
			}
			ep.Poster = posterURL(provider, id, season, num, extOf(p))
		}
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		return numLess(out[i].Number, out[j].Number)
	})
	return out, nil
}

func findSeasonDir(showDir, season string) (string, error) {
	want := padSeason(season)
	ents, err := os.ReadDir(showDir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		num, ok := parseSeasonDir(e.Name())
		if ok && padSeason(num) == want {
			return filepath.Join(showDir, e.Name()), nil
		}
	}
	return "", os.ErrNotExist
}

func findEpisodeBase(sdir, episode string) (string, error) {
	want := padSeason(episode)
	ents, err := os.ReadDir(sdir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".nfo") || e.Name() == "season.nfo" {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		var n episodeNFO
		if err := readNFO(filepath.Join(sdir, e.Name()), &n); err == nil && padSeason(n.Episode) == want {
			return base, nil
		}
		if padSeason(episodeNumFromBase(base)) == want {
			return base, nil
		}
	}
	return "", os.ErrNotExist
}

func episodeNumFromBase(base string) string {
	i := strings.Index(base, "E")
	if i < 0 || i+1 >= len(base) {
		return ""
	}
	rest := base[i+1:]
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 && !strings.HasPrefix(rest, "0") {
		return ""
	}
	return strconv.Itoa(n)
}

func posterURL(provider, id, season, episode, ext string) string {
	p := "/v1/catalog/" + url.PathEscape(provider) + "/" + url.PathEscape(id)
	if season == "" {
		return p + "/poster" + ext
	}
	p += "/seasons/" + url.PathEscape(season)
	if episode == "" {
		return p + "/poster" + ext
	}
	return p + "/episodes/" + url.PathEscape(episode) + "/poster" + ext
}

func extOf(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpeg" {
		return ".jpg"
	}
	if ext == "" {
		return ".jpg"
	}
	return ext
}

func numLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	if aerr == nil {
		return true
	}
	if berr == nil {
		return false
	}
	return a < b
}
