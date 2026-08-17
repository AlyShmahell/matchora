package match

type Candidate struct {
	Provider  string            `json:"provider"`
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Year      string            `json:"year,omitempty"`
	Score     float64           `json:"score,omitempty"`
	URL       string            `json:"url,omitempty"`
	Synopsis  string            `json:"synopsis,omitempty"`
	Poster    string            `json:"poster,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

func (c Candidate) Key() string {
	return c.Provider + ":" + c.ID
}

type Job struct {
	ID         string      `json:"id"`
	Source     string      `json:"source"`
	Title      string      `json:"title"`
	Year       string      `json:"year,omitempty"`
	Type       string      `json:"type,omitempty"`
	Season     string      `json:"season,omitempty"`
	Episode    string      `json:"episode,omitempty"`
	IMDB       string      `json:"imdb,omitempty"`
	Path       string      `json:"path,omitempty"`
	Status     string      `json:"status"`
	Ranker     string      `json:"ranker,omitempty"`
	Match      *Candidate  `json:"match,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Sub        *Candidate  `json:"sub,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func (j Job) QueryText() string {
	if j.Year != "" {
		return j.Title + " (" + j.Year + ")"
	}
	return j.Title
}
