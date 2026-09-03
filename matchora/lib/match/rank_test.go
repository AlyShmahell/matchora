package match

import (
	"testing"

	"github.com/alyshmahell/matchora/lib/config"
)

var testPlotStop = []string{
	"a", "an", "the", "of", "to", "in", "on", "at", "is", "and", "or", "my", "i",
	"this", "that", "for", "with", "from", "as", "by",
}

func testRankCfg() config.Config {
	return config.Config{Match: config.Match{PlotStop: testPlotStop}}
}

func rankTitle(title string, cands []Candidate) []Candidate {
	return rank(testRankCfg(), Job{Title: title}, cands)
}

func TestRankSeqPrefersTitleMatch(t *testing.T) {
	got := rank(testRankCfg(), Job{Title: "Girls", Year: "2012"}, []Candidate{
		{Title: "Other", Year: "2012"},
		{Title: "Girls", Year: "2012"},
	})
	if len(got) != 2 || got[0].Title != "Girls" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Jaccard != 1 || got[0].Score != 1 {
		t.Fatalf("jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func TestRankSeqIgnoresSynopsis(t *testing.T) {
	got := rankTitle("Wednesday", []Candidate{
		{Title: "Wednesday Club", Year: "2023", Synopsis: "Wednesday gathers every week."},
		{Title: "Wednesday", Year: "2022", Synopsis: "A different show."},
	})
	if got[0].Title != "Wednesday" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Jaccard != 1 || got[0].Score != 1 {
		t.Fatalf("jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func TestRankSeqLimitlessBeatsWithSuffix(t *testing.T) {
	got := rankTitle("Limitless", []Candidate{
		{Title: "Limitless with Chris Hemsworth", Year: "2022", Synopsis: "Limitless challenges."},
		{Title: "Limitless", Year: "2015"},
	})
	if got[0].Title != "Limitless" || got[0].Year != "2015" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Jaccard != 1 || got[0].Score != 1 {
		t.Fatalf("jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func TestRankSeqDarkMatterYearsStayTied(t *testing.T) {
	got := rankTitle("Dark Matter", []Candidate{
		{Title: "Dark Matter", Year: "2024"},
		{Title: "Dark Matter", Year: "2015"},
	})
	if got[0].Score != got[1].Score {
		t.Fatalf("tied years should match: %v %v", got[0].Score, got[1].Score)
	}
	if got[0].Jaccard != 1 || got[0].Score != 1 {
		t.Fatalf("jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func TestRankSeqYearBonus(t *testing.T) {
	got := rank(testRankCfg(), Job{Title: "Cowboy Bebop", Year: "1998"}, []Candidate{
		{Title: "Cowboy Bebop", Year: "1998"},
		{Title: "Cowboy Bebop", Year: "2001"},
	})
	if got[0].Year != "1998" {
		t.Fatalf("got=%v", got)
	}
	if got[0].Score != 1 || got[1].Score != 1 {
		t.Fatalf("exact titles should both score 1: %v %v", got[0].Score, got[1].Score)
	}
	if got[0].Jaccard != got[1].Jaccard {
		t.Fatalf("jaccard should match: %v %v", got[0].Jaccard, got[1].Jaccard)
	}
}

func TestRankSidoniaExactWithParent(t *testing.T) {
	got := rank(testRankCfg(), Job{Title: "Sidonia no Kishi", Parent: "Knights of Sidonia"}, []Candidate{
		{Title: "Sidonia no Kishi", Year: "2014"},
	})
	if got[0].Jaccard != 1 || got[0].Score != 1 {
		t.Fatalf("jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func TestRankBanishedPlotDoesNotLeapfrog(t *testing.T) {
	got := rankTitle("Banished from the Hero's Party", []Candidate{
		{
			Title:    "Scooped Up by an S-Rank Adventurer!",
			Year:     "2023",
			Synopsis: "Banished from the hero party, a man lives a quiet countryside life until an S-rank adventurer scoops him up.",
		},
		{Title: "Banished from the Hero's Party, I Decided to Live a Quiet Life in the Countryside", Year: "2021"},
	})
	if got[0].Title != "Banished from the Hero's Party, I Decided to Live a Quiet Life in the Countryside" {
		t.Fatalf("got=%+v", titlesAndScores(got))
	}
}

func TestRankFrierenPrefixBeatsSpecial(t *testing.T) {
	got := rankTitle("Frieren", []Candidate{
		{Title: "Sousou no Frieren: xx no Mahou", Year: "2025", Synopsis: "Frieren studies a curious magic."},
		{Title: "Frieren: Beyond Journey's End", Year: "2023", Synopsis: "Frieren the elf remains after the hero's journey."},
	})
	if got[0].Title != "Frieren: Beyond Journey's End" {
		t.Fatalf("got=%+v", titlesAndScores(got))
	}
}

func TestRankFrierenBeatsFrieden(t *testing.T) {
	got := rankTitle("Frieren", []Candidate{
		{Title: "Frieden", Year: "2020", Attrs: map[string]string{"kind": "Scripted", "language": "German"}},
		{Title: "Frieren: Beyond Journey's End", Year: "2023", Attrs: map[string]string{"kind": "Animation", "language": "Japanese"}},
		{Title: "Sousou no Frieren", Year: "2023", Attrs: map[string]string{"kind": "Animation", "language": "Japanese"}},
	})
	if got[0].Title == "Frieden" {
		t.Fatalf("frieden won: %+v", got)
	}
	if got[0].QueryCov != 1 {
		t.Fatalf("queryCov=%v title=%q", got[0].QueryCov, got[0].Title)
	}
}

func TestRankErasedPrefersEnglishAlt(t *testing.T) {
	got := rankTitle("Erased", []Candidate{
		{Title: "Crashed", Year: "2017", Attrs: map[string]string{"kind": "Variety"}},
		{Title: "Epithet Erased", Year: "2021", Attrs: map[string]string{"kind": "Animation", "language": "English"}},
		{Title: "Boku Dake ga Inai Machi", Year: "2016", Attrs: map[string]string{"title_en": "Erased", "kind": "Animation", "language": "Japanese"}},
	})
	if got[0].Title != "Boku Dake ga Inai Machi" && got[0].Title != "Epithet Erased" {
		t.Fatalf("crashed or other won: %+v", got)
	}
	for _, c := range got {
		if c.Title == "Crashed" && c.Jaccard != 0 {
			t.Fatalf("crashed jaccard=%v", c.Jaccard)
		}
	}
}

func TestRankScarletBondUsesParent(t *testing.T) {
	got := rank(testRankCfg(), Job{
		Title:  "Scarlet Bond",
		Parent: "That Time I Got Reincarnated as a Slime",
	}, []Candidate{
		{Title: "Beauty's Bone, Scarlet Sleeves", Year: "2026"},
		{Title: "That Time I Got Reincarnated as a Slime the Movie: Scarlet Bond", Year: "2022", Attrs: map[string]string{"title_en": "Scarlet Bond"}},
	})
	if got[0].Title != "That Time I Got Reincarnated as a Slime the Movie: Scarlet Bond" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRankArifuretaCoverageAfterPrefer(t *testing.T) {
	got := rankTitle("Arifureta", []Candidate{
		{Title: "Arifureta: From Commonplace to World's Strongest", Year: "2019"},
	})
	if got[0].QueryCov != 1 {
		t.Fatalf("queryCov=%v", got[0].QueryCov)
	}
	if got[0].Jaccard >= 0.72 {
		t.Fatalf("long official title should stay below min_score: %v", got[0].Jaccard)
	}
}

func TestRankSinbadParentLiftsMagi(t *testing.T) {
	cands := []Candidate{
		{Title: "The Adventures of Sinbad", Year: "1996"},
		{Title: "Magi: Sinbad no Bouken", Year: "2016"},
	}
	withParent := rank(testRankCfg(), Job{Title: "adventure of sinbad", Parent: "Magi The Labyrinth of Magic"}, cands)
	var magi, advent float64
	for _, c := range withParent {
		switch c.Title {
		case "Magi: Sinbad no Bouken":
			magi = c.Score
		case "The Adventures of Sinbad":
			advent = c.Score
		}
	}
	if magi == 0 {
		t.Fatalf("missing magi: %+v", withParent)
	}
	without := rankTitle("adventure of sinbad", cands)
	var magiBare, adventBare float64
	for _, c := range without {
		switch c.Title {
		case "Magi: Sinbad no Bouken":
			magiBare = c.Score
		case "The Adventures of Sinbad":
			adventBare = c.Score
		}
	}
	if magi <= magiBare {
		t.Fatalf("parent should lift magi: with=%v without=%v advent=%v", magi, magiBare, advent)
	}
	if advent != adventBare {
		t.Fatalf("the/of must not lift adventures: with=%v without=%v", advent, adventBare)
	}
}

func TestRankExplosionPlotPrefersBakuen(t *testing.T) {
	got := rankTitle("An Explosion on This Wonderful World!", []Candidate{
		{
			Title:    "Kono Subarashii Sekai ni Shukufuku wo!",
			Year:     "2016",
			Synopsis: "The life of Satou Kazuma, a hikikomori who likes games, all too soon came to an end because of a traffic accident... It was supposed to, but when he woke up, a beautiful girl who called herself a goddess was in front of his eyes. \"Hey, I have got something a little nice for you. Wanna go to another world? You can take only one thing of your choice along with you.\"",
		},
		{
			Title:    "Kono Subarashii Sekai ni Bakuen wo!",
			Year:     "2023",
			Synopsis: "Crimson Magic Clan members Megumin and Yunyun are at the top of their class, but they still have a lot to learn. Yunyun's begun learning advanced magic, but Megumin has gone down a different path-the path of explosion magic! Despite being warned of its limited usefulness, Megumin believes explosion magic is the way for her to become a great, voluptuous wizard, and she won't be convinced otherwise!",
		},
	})
	if got[0].Title != "Kono Subarashii Sekai ni Bakuen wo!" {
		t.Fatalf("got=%+v", titlesAndScores(got))
	}
	if got[0].Jaccard != 0 || got[1].Jaccard != 0 {
		t.Fatalf("jaccard should be 0: %+v", got)
	}
	if got[0].Score <= got[1].Score || got[0].Score > 1 {
		t.Fatalf("plot residual missing: %v %v", got[0].Score, got[1].Score)
	}
}

func TestRankAverageAbilitiesPlotLifts(t *testing.T) {
	got := rankTitle("Didn't I Say to Make My Abilities Average in the Next Life", []Candidate{
		{
			Title:    "Watashi, Nouryoku wa Heikinchi de tte Itta yo ne!",
			Year:     "2019",
			Synopsis: "When she turns ten years old, Adele von Ascham is hit with a horrible headache–and memories of her previous life as an eighteen-year-old Japanese girl named Kurihara Misato. That life changed abruptly, however, when Misato died trying to aid a little girl and met god. During that meeting, she made an odd request and asked for average abilities in her next life. But few things – especially wishes –",
		},
	})
	if got[0].Jaccard != 0 {
		t.Fatalf("jaccard=%v", got[0].Jaccard)
	}
	if got[0].Score <= 0 || got[0].Score > 1 {
		t.Fatalf("plot should lift score: %v", got[0].Score)
	}
}

func TestRankErasedPlotStaysZero(t *testing.T) {
	got := rankTitle("Erased", []Candidate{
		{
			Title:    "Boku Dake ga Inai Machi",
			Year:     "2016",
			Synopsis: "Struggling manga author Satoru Fujinuma is beset by his fear to express himself. However, he has a supernatural ability of being forced to prevent deaths and catastrophes.",
		},
	})
	if got[0].Jaccard != 0 || got[0].Score != 0 {
		t.Fatalf("erased plot should not lift: jaccard=%v score=%v", got[0].Jaccard, got[0].Score)
	}
}

func titlesAndScores(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Title
	}
	return out
}
