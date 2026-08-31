package match

import "testing"

func TestRankSeqPrefersTitleMatch(t *testing.T) {
	got := rank("Girls (2012)", []Candidate{
		{Title: "Other", Year: "2012"},
		{Title: "Girls", Year: "2012"},
	})
	if len(got) != 2 || got[0].Title != "Girls" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Score < 0.72 {
		t.Fatalf("score=%v", got[0].Score)
	}
}

func TestRankSeqIgnoresSynopsis(t *testing.T) {
	got := rank("Wednesday", []Candidate{
		{Title: "Wednesday Club", Year: "2023", Synopsis: "Wednesday gathers every week."},
		{Title: "Wednesday", Year: "2022", Synopsis: "A different show."},
	})
	if got[0].Title != "Wednesday" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Score-got[1].Score < 0.04 {
		t.Fatalf("margin=%v %v", got[0].Score, got[1].Score)
	}
}

func TestRankSeqLimitlessBeatsWithSuffix(t *testing.T) {
	got := rank("Limitless", []Candidate{
		{Title: "Limitless with Chris Hemsworth", Year: "2022", Synopsis: "Limitless challenges."},
		{Title: "Limitless", Year: "2015"},
	})
	if got[0].Title != "Limitless" || got[0].Year != "2015" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Score-got[1].Score < 0.04 {
		t.Fatalf("margin=%v %v", got[0].Score, got[1].Score)
	}
}

func TestRankSeqDarkMatterYearsStayTied(t *testing.T) {
	got := rank("Dark Matter", []Candidate{
		{Title: "Dark Matter", Year: "2024"},
		{Title: "Dark Matter", Year: "2015"},
	})
	if got[0].Score != got[1].Score {
		t.Fatalf("tied years should match: %v %v", got[0].Score, got[1].Score)
	}
	if got[0].Score != 1 {
		t.Fatalf("score=%v", got[0].Score)
	}
}

func TestRankSeqYearBonus(t *testing.T) {
	got := rank("Cowboy Bebop (1998)", []Candidate{
		{Title: "Cowboy Bebop", Year: "1998"},
		{Title: "Cowboy Bebop", Year: "2001"},
	})
	if got[0].Year != "1998" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("year bonus missing: %v %v", got[0].Score, got[1].Score)
	}
}
