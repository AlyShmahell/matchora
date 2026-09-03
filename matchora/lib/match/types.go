package match

type Cleaned struct {
	Title   string `json:"title"`
	Year    string `json:"year"`
	Type    string `json:"type"`
	Season  string `json:"season"`
	Episode string `json:"episode"`
}

type Grouped struct {
	Cleaned
	Path   string
	Parent string
}

type Candidate struct {
	Provider string            `json:"provider"`
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Year     string            `json:"year,omitempty"`
	Score    float64           `json:"score,omitempty"`
	URL      string            `json:"url,omitempty"`
	Synopsis string            `json:"synopsis,omitempty"`
	Poster   string            `json:"poster,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	QueryCov float64           `json:"-"`
	Jaccard  float64           `json:"-"`
}

func (c Candidate) Key() string {
	return c.Provider + ":" + c.ID
}

type CatalogEpisode struct {
	ID       string   `json:"id,omitempty"`
	Number   string   `json:"number,omitempty"`
	Title    string   `json:"title"`
	Synopsis string   `json:"synopsis,omitempty"`
	Poster   string   `json:"poster,omitempty"`
	URL      string   `json:"url,omitempty"`
	Year     string   `json:"year,omitempty"`
	Path     string   `json:"path,omitempty"`
	Paths    []string `json:"paths,omitempty"`
}

type CatalogSeason struct {
	ID       string           `json:"id,omitempty"`
	Number   string           `json:"number,omitempty"`
	Title    string           `json:"title"`
	Synopsis string           `json:"synopsis,omitempty"`
	Poster   string           `json:"poster,omitempty"`
	URL      string           `json:"url,omitempty"`
	Year     string           `json:"year,omitempty"`
	Episodes []CatalogEpisode `json:"episodes,omitempty"`
}

type JobFile struct {
	Path    string `json:"path"`
	Season  string `json:"season,omitempty"`
	Episode string `json:"episode,omitempty"`
}

type Job struct {
	ID         string          `json:"id"`
	Source     string          `json:"source"`
	Title      string          `json:"title"`
	Year       string          `json:"year,omitempty"`
	Parent     string          `json:"parent,omitempty"`
	Type       string          `json:"type,omitempty"`
	Season     string          `json:"season,omitempty"`
	Episode    string          `json:"episode,omitempty"`
	IMDB       string          `json:"imdb,omitempty"`
	Path       string          `json:"path,omitempty"`
	Files      []JobFile       `json:"files,omitempty"`
	Status     string          `json:"status"`
	Ranker     string          `json:"ranker,omitempty"`
	Match      *Candidate      `json:"match,omitempty"`
	Candidates []Candidate     `json:"candidates,omitempty"`
	Sub        *Candidate      `json:"sub,omitempty"`
	Catalog    []CatalogSeason `json:"catalog"`
	CatalogFor string          `json:"catalog_for,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func (j Job) QueryText() string {
	if j.Year != "" {
		return j.Title + " (" + j.Year + ")"
	}
	return j.Title
}
