package library

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alyshmahell/matchora/lib/match"
)

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

type tvshowNFO struct {
	XMLName   xml.Name `xml:"tvshow"`
	Title     string   `xml:"title"`
	Type      string   `xml:"type,omitempty"`
	Plot      string   `xml:"plot,omitempty"`
	Year      string   `xml:"year,omitempty"`
	Premiered string   `xml:"premiered,omitempty"`
	UniqueID  uniqueID `xml:"uniqueid"`
}

type movieNFO struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Type      string   `xml:"type,omitempty"`
	Plot      string   `xml:"plot,omitempty"`
	Year      string   `xml:"year,omitempty"`
	Premiered string   `xml:"premiered,omitempty"`
	UniqueID  uniqueID `xml:"uniqueid"`
}

type seasonNFO struct {
	XMLName      xml.Name `xml:"season"`
	Title        string   `xml:"title"`
	Plot         string   `xml:"plot,omitempty"`
	Year         string   `xml:"year,omitempty"`
	Premiered    string   `xml:"premiered,omitempty"`
	SeasonNumber string   `xml:"seasonnumber,omitempty"`
	UniqueID     uniqueID `xml:"uniqueid"`
}

type episodeNFO struct {
	XMLName  xml.Name `xml:"episodedetails"`
	Title    string   `xml:"title"`
	Plot     string   `xml:"plot,omitempty"`
	Year     string   `xml:"year,omitempty"`
	Aired    string   `xml:"aired,omitempty"`
	Season   string   `xml:"season,omitempty"`
	Episode  string   `xml:"episode,omitempty"`
	UniqueID uniqueID `xml:"uniqueid"`
}

func makeID(provider, id string) uniqueID {
	return uniqueID{Type: provider, Default: "true", Value: id}
}

func writeNFO(path string, v any) error {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xmlHeader), b...), 0o644)
}

func readNFO(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return xml.Unmarshal(b, v)
}

func writeShowNFO(dir string, cand match.Candidate, uniqueType, jobType string) error {
	if uniqueType == "" {
		uniqueType = cand.Provider
	}
	return writeNFO(filepath.Join(dir, "tvshow.nfo"), tvshowNFO{
		Title:     cand.Title,
		Type:      jobType,
		Plot:      cand.Synopsis,
		Year:      cand.Year,
		Premiered: cand.Year,
		UniqueID:  makeID(uniqueType, cand.ID),
	})
}

func writeMovieNFO(dir string, cand match.Candidate, uniqueType, jobType string) error {
	if uniqueType == "" {
		uniqueType = cand.Provider
	}
	return writeNFO(filepath.Join(dir, "movie.nfo"), movieNFO{
		Title:     cand.Title,
		Type:      jobType,
		Plot:      cand.Synopsis,
		Year:      cand.Year,
		Premiered: cand.Year,
		UniqueID:  makeID(uniqueType, cand.ID),
	})
}

func writeSeasonNFO(dir string, provider string, s match.CatalogSeason) error {
	title := s.Title
	if title == "" && s.Number != "" {
		title = "Season " + s.Number
	}
	return writeNFO(filepath.Join(dir, "season.nfo"), seasonNFO{
		Title:        title,
		Plot:         s.Synopsis,
		Year:         s.Year,
		Premiered:    s.Year,
		SeasonNumber: s.Number,
		UniqueID:     makeID(provider, s.ID),
	})
}

func writeEpisodeNFO(path, provider, season string, e match.CatalogEpisode) error {
	return writeNFO(path, episodeNFO{
		Title:    e.Title,
		Plot:     e.Synopsis,
		Year:     e.Year,
		Aired:    e.Year,
		Season:   season,
		Episode:  e.Number,
		UniqueID: makeID(provider, e.ID),
	})
}

func titleFromNFO(dir string) (kind, title, year, plot, provider, id, jobType string, err error) {
	if _, e := os.Stat(filepath.Join(dir, "movie.nfo")); e == nil {
		var n movieNFO
		if err = readNFO(filepath.Join(dir, "movie.nfo"), &n); err != nil {
			return "", "", "", "", "", "", "", err
		}
		return "movie", n.Title, n.Year, n.Plot, n.UniqueID.Type, n.UniqueID.Value, n.Type, nil
	}
	var n tvshowNFO
	if err = readNFO(filepath.Join(dir, "tvshow.nfo"), &n); err != nil {
		return "", "", "", "", "", "", "", err
	}
	return "tvshow", n.Title, n.Year, n.Plot, n.UniqueID.Type, n.UniqueID.Value, n.Type, nil
}

func padSeason(number string) string {
	n, err := strconv.Atoi(strings.TrimSpace(number))
	if err != nil {
		return number
	}
	return strconv.Itoa(n)
}
