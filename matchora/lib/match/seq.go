package match

// SeqRatio is Python difflib.SequenceMatcher.ratio() without autojunk:
// 2 * matching / (len(a) + len(b)) using Ratcliff/Obershelp blocks.
func SeqRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	total := len(ra) + len(rb)
	if total == 0 {
		return 1
	}
	return 2 * float64(matchCount(ra, rb, 0, len(ra), 0, len(rb))) / float64(total)
}

func matchCount(a, b []rune, alo, ahi, blo, bhi int) int {
	i, j, n := longestMatch(a, b, alo, ahi, blo, bhi)
	if n == 0 {
		return 0
	}
	return n + matchCount(a, b, alo, i, blo, j) + matchCount(a, b, i+n, ahi, j+n, bhi)
}

func longestMatch(a, b []rune, alo, ahi, blo, bhi int) (int, int, int) {
	bestI, bestJ, best := alo, blo, 0
	for i := alo; i < ahi; i++ {
		for j := blo; j < bhi; j++ {
			k := 0
			for i+k < ahi && j+k < bhi && a[i+k] == b[j+k] {
				k++
			}
			if k > best {
				bestI, bestJ, best = i, j, k
			}
		}
	}
	return bestI, bestJ, best
}
