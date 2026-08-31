package match

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alyshmahell/matchora/lib/config"
)

var (
	yearRe        = regexp.MustCompile(`\((\d{4})\)|\.(\d{4})\b`)
	yearSuffixRe  = regexp.MustCompile(`\s*\((\d{4})\)\s*$`)
	bareYearRe    = regexp.MustCompile(`\s+(\d{4})\s*$`)
	episodeRe     = regexp.MustCompile(`(?i)s\d{1,2}e\d{1,3}|\bepisode\s*\d+`)
	seasonHeadRe  = regexp.MustCompile(`(?i)^season\s*\d+`)
	sxxRe         = regexp.MustCompile(`(?i)^s\d{1,2}$`)
	arcTailRe     = regexp.MustCompile(`(?i)\barc\b`)
	junkTitleRe   = regexp.MustCompile(`(?i)\b(recap|offline|omake|preview|pv)\b`)
	romanRe       = regexp.MustCompile(`(?i)^(ii|iii|iv|v|vi)$`)
	tagRe         = regexp.MustCompile(`^\[[^\]]+\]\s*`)
	leadParenRe   = regexp.MustCompile(`^\([^)]{1,16}\)\s*`)
	bracketsRe    = regexp.MustCompile(`\[[^\]]*\]`)
	trailingType  = regexp.MustCompile(`(?i)\s*\((?:anime|tv|movie)\)\s*$`)
	dashRe        = regexp.MustCompile(`\s+[-–—]\s+`)
	spaceRe       = regexp.MustCompile(`\s+`)
	resRe         = regexp.MustCompile(`(?i)^\d{3,4}p$`)
	indexRe       = regexp.MustCompile(`^\d{1,2}\.\s*`)
	dashSplitRe   = regexp.MustCompile(`[-–—]`)
	nonAlnumRe    = regexp.MustCompile(`[^a-z0-9]+`)
	foldSxxRe     = regexp.MustCompile(`\s+s\d{1,2}$`)
	foldSeasonRe  = regexp.MustCompile(`\s+season\s+\d+$`)
	oneTwoDigitRe = regexp.MustCompile(`^\d{1,2}$`)
	oneThreeDigRe = regexp.MustCompile(`^\d{1,3}$`)
	seasonRestRe  = regexp.MustCompile(`^season\s+\d+(\s+.*)?$`)
	epTailRe      = regexp.MustCompile(`(?i)\s+E\d{1,3}$`)
	numTailRe     = regexp.MustCompile(`\s+\d{2,3}$`)
	partRe        = regexp.MustCompile(`(?i)\s*\(part\s*\d+\)\s*$`)
	hex8Re        = regexp.MustCompile(`^[0-9A-Fa-f]{8}$`)
	shortQualRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)
	yearOnlyRe    = regexp.MustCompile(`^\d{4}$`)
	epAbsorbRe    = regexp.MustCompile(`^(?:(ii|iii|iv|v)\s+)?(\d{1,3})(?:\s+(.*))?$`)
	qualSplitRe  = regexp.MustCompile(`[\s/,+]+`)
	slashSpaceRe = regexp.MustCompile(`[\s/]+`)
)

var tails = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b.*$`),
	regexp.MustCompile(`(?i)\bSeason(?:\s+\d+)?\b.*$`),
	regexp.MustCompile(`(?i)\bEpisode(?:\s+\d+)?\b.*$`),
	regexp.MustCompile(`(?i)\bS\d{1,2}\b.*$`),
}

var parenRe = regexp.MustCompile(`\(([^)]*)\)`)

var qualityExtra = map[string]struct{}{
	"av1": {}, "10bit": {}, "10-bit": {}, "8bit": {}, "opus": {}, "bd": {}, "uhd": {}, "hdr10+": {},
}

type groupedRow struct {
	title, year, path string
}

type preprocessor struct {
	extras  map[string]struct{}
	release map[string]struct{}
}

type postprocessor struct {
	extras  map[string]struct{}
	release map[string]struct{}
	kinds   map[string]struct{}
}

type classifier struct {
	pre  *preprocessor
	post *postprocessor
}

type grouper struct {
	pre       *preprocessor
	post      *postprocessor
	cls       *classifier
	videoExt  map[string]struct{}
	threshold float64
	library   string
}

func Group(cfg config.Config, root, child string) []Grouped {
	if strings.TrimSpace(child) == "" {
		return nil
	}
	g := newGrouper(cfg, root)
	rows := g.group(child)
	out := make([]Grouped, 0, len(rows))
	for _, r := range rows {
		out = append(out, Grouped{Cleaned: Cleaned{Title: r.title, Year: r.year}, Path: r.path})
	}
	return out
}

func newGrouper(cfg config.Config, library string) *grouper {
	pre := &preprocessor{extras: cfg.GroupExtras(), release: cfg.GroupRelease()}
	post := &postprocessor{extras: pre.extras, release: pre.release, kinds: cfg.GroupKinds()}
	return &grouper{
		pre:       pre,
		post:      post,
		cls:       &classifier{pre: pre, post: post},
		videoExt:  cfg.GroupVideoExt(),
		threshold: cfg.SeqThreshold(),
		library:   library,
	}
}

func (p *preprocessor) extrasName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(strings.TrimRight(name, "/")))
	_, ok := p.extras[n]
	return ok
}

func setToken(set map[string]struct{}, t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return false
	}
	if _, ok := set[t]; ok {
		return true
	}
	i := len(t)
	for i > 0 && t[i-1] >= '0' && t[i-1] <= '9' {
		i--
	}
	if i == 0 || i == len(t) {
		return false
	}
	_, ok := set[t[:i]]
	return ok
}

func (p *preprocessor) extrasPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." {
			continue
		}
		if p.extrasName(part) {
			return true
		}
	}
	return false
}

func (p *preprocessor) segment(raw string, stripExt bool) string {
	if p.extrasName(raw) {
		return ""
	}
	name := raw
	if stripExt {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = tagRe.ReplaceAllString(name, "")
	name = leadParenRe.ReplaceAllString(name, "")
	name = bracketsRe.ReplaceAllString(name, "")
	name = indexRe.ReplaceAllString(name, "")
	name = trailingType.ReplaceAllString(name, "")
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = dashRe.ReplaceAllString(name, " ")
	var tokens []string
	for _, t := range strings.Fields(name) {
		if setToken(p.extras, t) {
			continue
		}
		parts := dashSplitRe.Split(t, -1)
		var kept []string
		for _, part := range parts {
			if part != "" {
				kept = append(kept, part)
			}
		}
		if len(kept) == 0 {
			continue
		}
		skip := false
		for _, part := range kept {
			if _, ok := p.release[strings.ToLower(part)]; ok || resRe.MatchString(part) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		tokens = append(tokens, t)
	}
	cleaned := strings.TrimSpace(spaceRe.ReplaceAllString(strings.Join(tokens, " "), " "))
	if p.extrasName(cleaned) {
		return ""
	}
	return cleaned
}

func (p *preprocessor) rel(rel string) string {
	parts := []string{}
	for _, part := range strings.Split(rel, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	var out []string
	for i, part := range parts {
		if p.extrasName(part) {
			continue
		}
		cleaned := p.segment(part, i == len(parts)-1)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	if len(out) > 0 {
		return strings.Join(out, "/")
	}
	return p.segment(filepath.Base(rel), true)
}

func (p *postprocessor) extrasName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(strings.TrimRight(name, "/")))
	_, ok := p.extras[n]
	return ok
}

func (p *postprocessor) stripTails(s string) string {
	for _, rx := range tails {
		s = rx.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

func (p *postprocessor) seasonJunk(s string) bool {
	return p.stripTails(s) == ""
}

func (p *postprocessor) kindFolder(s string) bool {
	t := strings.TrimSpace(spaceRe.ReplaceAllString(p.stripKindPrefix(s), " "))
	if t == "" {
		return true
	}
	_, ok := p.kinds[strings.ToLower(strings.Trim(t, ":,"))]
	return ok
}

func (p *postprocessor) stripKindPrefix(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	for k := range p.kinds {
		if !strings.HasPrefix(low, k) {
			continue
		}
		rest := strings.TrimSpace(s[len(k):])
		if rest == "" {
			return ""
		}
		if r := rest[0]; r == ' ' || r == '-' || r == ':' {
			return strings.TrimSpace(rest)
		}
	}
	return s
}

func (p *postprocessor) kindToken(t string) bool {
	return setToken(p.kinds, t)
}

func (p *postprocessor) segmentJunk(s string) bool {
	if p.seasonJunk(s) || p.extrasName(s) {
		return true
	}
	return p.kindFolder(s)
}

func dedupeTokens(toks []string) []string {
	var out []string
	for _, t := range toks {
		if len(out) > 0 && strings.EqualFold(t, out[len(out)-1]) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func titleNorm(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, ";", " "))
	return strings.TrimSpace(nonAlnumRe.ReplaceAllString(s, " "))
}

func (p *postprocessor) dropRootParens(s, root string) string {
	nr := titleNorm(root)
	return parenRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := parenRe.FindStringSubmatch(m)
		if len(inner) == 2 && titleNorm(inner[1]) == nr {
			return ""
		}
		return m
	})
}

func (p *postprocessor) aliasToRoot(s, root string) string {
	if root == "" {
		return s
	}
	nr := titleNorm(root)
	if titleNorm(s) == nr {
		return root
	}
	for _, m := range parenRe.FindAllStringSubmatch(s, -1) {
		if titleNorm(m[1]) == nr {
			return root
		}
	}
	return s
}

func (p *postprocessor) kindAbsorb(s, root string) string {
	toks := strings.Fields(s)
	var leftover []string
	for i, t := range toks {
		if p.kindToken(t) {
			if len(leftover) > 0 && strings.EqualFold(leftover[len(leftover)-1], "the") {
				leftover = append(leftover, t)
				continue
			}
			continue
		}
		prevKind := i > 0 && p.kindToken(toks[i-1])
		nextKind := i+1 < len(toks) && p.kindToken(toks[i+1])
		if oneTwoDigitRe.MatchString(t) && (prevKind || nextKind) {
			continue
		}
		leftover = append(leftover, t)
	}
	leftover = dedupeTokens(leftover)
	rest := strings.TrimSpace(strings.Join(leftover, " "))
	if rest == "" || titleNorm(rest) == titleNorm(root) {
		return root
	}
	rest = strings.TrimSpace(epTailRe.ReplaceAllString(rest, ""))
	rest = strings.TrimSpace(numTailRe.ReplaceAllString(rest, ""))
	if rest == "" || titleNorm(rest) == titleNorm(root) {
		return root
	}
	return rest
}

func (p *postprocessor) episodeAbsorb(s, root string) string {
	ns, nr := titleNorm(s), titleNorm(root)
	if nr == "" || !strings.HasPrefix(ns, nr) {
		return s
	}
	rest := strings.TrimSpace(ns[len(nr):])
	if rest == "" {
		return s
	}
	m := epAbsorbRe.FindStringSubmatch(rest)
	if m == nil {
		return s
	}
	roman, num, after := m[1], m[2], strings.TrimSpace(m[3])
	if num == "0" && roman == "" && after == "" {
		return s
	}
	return root
}

func (p *postprocessor) qualityParen(inner string) bool {
	inner = strings.TrimSpace(inner)
	if yearOnlyRe.MatchString(inner) {
		return false
	}
	if hex8Re.MatchString(inner) {
		return true
	}
	if len(inner) <= 6 && shortQualRe.MatchString(inner) {
		return true
	}
	toks := []string{}
	for _, t := range qualSplitRe.Split(strings.ToLower(inner), -1) {
		if t != "" {
			toks = append(toks, t)
		}
	}
	if len(toks) == 0 {
		return true
	}
	n := 0
	for _, t := range toks {
		if _, ok := p.release[t]; ok {
			n++
			continue
		}
		if _, ok := qualityExtra[t]; ok || resRe.MatchString(t) {
			n++
		}
	}
	return n*2 >= len(toks)
}

func (p *postprocessor) dropQualityParens(s string) string {
	return parenRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := parenRe.FindStringSubmatch(m)
		if len(inner) == 2 && p.qualityParen(inner[1]) {
			return ""
		}
		return m
	})
}

func (p *postprocessor) label(raw, root string) string {
	alias := p.aliasToRoot(strings.ReplaceAll(raw, "/", " "), root)
	if alias == root {
		return root
	}
	flat := strings.TrimSpace(slashSpaceRe.ReplaceAllString(raw, " "))
	for _, rx := range tails {
		flat = rx.ReplaceAllString(flat, "")
	}
	flat = strings.TrimSpace(flat)
	if flat == "" {
		return root
	}
	var parts []string
	for _, part := range strings.Split(raw, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	s := ""
	for _, part := range parts {
		if strings.EqualFold(part, flat) {
			s = part
			break
		}
	}
	if s == "" {
		for i := len(parts) - 1; i >= 0; i-- {
			if !p.segmentJunk(parts[i]) {
				s = parts[i]
				break
			}
		}
	}
	if s == "" {
		if len(parts) > 0 {
			s = parts[len(parts)-1]
		} else {
			s = raw
		}
	}
	for _, rx := range tails {
		s = rx.ReplaceAllString(s, "")
	}
	s = p.stripKindPrefix(s)
	s = trailingType.ReplaceAllString(s, "")
	s = dashRe.ReplaceAllString(s, ": ")
	s = partRe.ReplaceAllString(s, "")
	s = p.dropQualityParens(s)
	s = p.dropRootParens(s, root)
	s = strings.TrimSpace(slashSpaceRe.ReplaceAllString(s, " "))
	if s == "" || p.extrasName(s) {
		return root
	}
	s = strings.Join(dedupeTokens(strings.Fields(s)), " ")
	s = p.kindAbsorb(s, root)
	s = p.episodeAbsorb(s, root)
	return p.aliasToRoot(s, root)
}

func (p *postprocessor) apply(rows []labelCount, root string) []labelCount {
	type acc struct {
		label string
		n     int
	}
	merged := map[string]*acc{}
	var order []string
	for _, row := range rows {
		cleaned := p.label(row.label, root)
		key := titleNorm(cleaned)
		if cur, ok := merged[key]; ok {
			cur.n += row.n
			continue
		}
		merged[key] = &acc{label: cleaned, n: row.n}
		order = append(order, key)
	}
	out := make([]labelCount, 0, len(order))
	for _, k := range order {
		out = append(out, labelCount{label: merged[k].label, n: merged[k].n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return strings.ToLower(out[i].label) < strings.ToLower(out[j].label)
	})
	return out
}

type labelCount struct {
	label string
	n     int
}

func (g *grouper) isVideo(name string) bool {
	_, ok := g.videoExt[strings.ToLower(filepath.Ext(name))]
	return ok
}

func (g *grouper) collectVideos(directory string) []string {
	var out []string
	var walk func(string)
	walk = func(cur string) {
		ents, err := os.ReadDir(cur)
		if err != nil {
			return
		}
		for _, e := range ents {
			p := filepath.Join(cur, e.Name())
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if st.IsDir() {
				walk(p)
				continue
			}
			if g.isVideo(e.Name()) {
				out = append(out, p)
			}
		}
	}
	walk(directory)
	sort.Strings(out)
	return out
}

func (g *grouper) videosFor(child string) []string {
	st, err := os.Stat(child)
	if err != nil {
		return nil
	}
	if st.IsDir() {
		return g.collectVideos(child)
	}
	if g.isVideo(filepath.Base(child)) {
		return []string{child}
	}
	return nil
}

func relFromChild(child, path string) string {
	rel, err := filepath.Rel(child, path)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

func (g *grouper) libRel(path string) string {
	rel, err := filepath.Rel(g.library, path)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

func (g *grouper) keepVideo(rootPath, path string) bool {
	rel := relFromChild(rootPath, path)
	if g.pre.extrasPath(rel) {
		return false
	}
	if g.pre.segment(filepath.Base(path), true) == "" {
		return false
	}
	return g.pre.rel(rel) != ""
}

func (g *grouper) listingYears(child string) map[string]bool {
	years := map[string]bool{}
	add := func(text string) {
		for _, m := range yearRe.FindAllStringSubmatch(text, -1) {
			y := m[1]
			if y == "" {
				y = m[2]
			}
			if y != "" {
				years[y] = true
			}
		}
	}
	add(filepath.Base(child))
	add(child)
	st, err := os.Stat(child)
	if err != nil {
		return years
	}
	if st.IsDir() {
		ents, err := os.ReadDir(child)
		if err == nil {
			for _, e := range ents {
				add(e.Name())
			}
		}
	}
	for _, p := range g.videosFor(child) {
		add(filepath.Base(p))
		add(p)
	}
	return years
}

func splitYear(title string, allowed map[string]bool) (string, string) {
	if m := yearSuffixRe.FindStringSubmatchIndex(title); m != nil {
		y := title[m[2]:m[3]]
		if allowed[y] {
			return strings.TrimSpace(title[:m[0]]), y
		}
	}
	if m := bareYearRe.FindStringSubmatchIndex(title); m != nil {
		y := title[m[2]:m[3]]
		if allowed[y] {
			return strings.TrimSpace(title[:m[0]]), y
		}
	}
	return title, ""
}

func (g *grouper) emit(title string, years map[string]bool) (string, string) {
	return splitYear(g.post.stripTails(title), years)
}

func titleKey(s string) string {
	return titleNorm(s)
}

func clusterIndices(n int, similar func(a, b int) bool) [][]int {
	if n == 0 {
		return nil
	}
	if n == 1 {
		return [][]int{{0}}
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			if similar(a, b) {
				ra, rb := find(a), find(b)
				if ra != rb {
					parent[rb] = ra
				}
			}
		}
	}
	groups := map[int][]int{}
	var order []int
	for i := 0; i < n; i++ {
		r := find(i)
		if _, ok := groups[r]; !ok {
			order = append(order, r)
		}
		groups[r] = append(groups[r], i)
	}
	out := make([][]int, 0, len(order))
	for _, r := range order {
		out = append(out, groups[r])
	}
	return out
}

func clusterLabel(names []string) string {
	var partsList [][]string
	for _, n := range names {
		if n == "" {
			continue
		}
		var parts []string
		for _, p := range strings.Split(n, "/") {
			if p != "" {
				parts = append(parts, p)
			}
		}
		partsList = append(partsList, parts)
	}
	if len(partsList) == 0 {
		if len(names) > 0 {
			return names[0]
		}
		return ""
	}
	prefix := append([]string(nil), partsList[0]...)
	for _, other := range partsList[1:] {
		i := 0
		for i < len(prefix) && i < len(other) && strings.EqualFold(prefix[i], other[i]) {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			break
		}
	}
	if len(prefix) > 0 {
		return strings.Join(prefix, "/")
	}
	return names[0]
}

func seqScore(names []string, i, j int) float64 {
	a, b := strings.ToLower(names[i]), strings.ToLower(names[j])
	if a == b {
		return 1
	}
	return SeqRatio(a, b)
}

func (g *grouper) seqClusters(names []string, root string) []labelCount {
	similar := func(i, j int) bool {
		return seqScore(names, i, j) >= g.threshold
	}
	var rows []labelCount
	for _, grp := range clusterIndices(len(names), similar) {
		picked := make([]string, 0, len(grp))
		for _, i := range grp {
			picked = append(picked, names[i])
		}
		rows = append(rows, labelCount{label: clusterLabel(picked), n: len(grp)})
	}
	return g.post.apply(rows, root)
}

type entry struct {
	abs  string
	name string
	dir  bool
}

func (g *grouper) immediate(path string) []entry {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return []entry{{abs: path, name: filepath.Base(path), dir: false}}
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	var out []entry
	for _, e := range ents {
		p := filepath.Join(path, e.Name())
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			out = append(out, entry{abs: p, name: e.Name(), dir: true})
		} else if g.isVideo(e.Name()) {
			out = append(out, entry{abs: p, name: e.Name(), dir: false})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

func (c *classifier) fold(s string) string {
	n := titleNorm(s)
	n = foldSxxRe.ReplaceAllString(n, "")
	n = foldSeasonRe.ReplaceAllString(n, "")
	if strings.HasPrefix(n, "the ") {
		n = n[4:]
	}
	return n
}

func (c *classifier) isAlias(cleaned, root string) bool {
	if cleaned == "" || root == "" {
		return false
	}
	if c.fold(cleaned) == c.fold(root) {
		return true
	}
	for _, m := range parenRe.FindAllStringSubmatch(cleaned, -1) {
		if c.fold(m[1]) == c.fold(root) {
			return true
		}
	}
	for _, m := range parenRe.FindAllStringSubmatch(root, -1) {
		if c.fold(m[1]) == c.fold(cleaned) {
			return true
		}
	}
	return false
}

func (c *classifier) isRootPlusYear(cleaned, root string) bool {
	t := strings.TrimSpace(yearSuffixRe.ReplaceAllString(cleaned, ""))
	return titleNorm(t) == titleNorm(root)
}

func (c *classifier) isSeasonName(raw, cleaned, root string) bool {
	if seasonHeadRe.MatchString(strings.TrimSpace(raw)) || seasonHeadRe.MatchString(cleaned) {
		return true
	}
	if sxxRe.MatchString(strings.TrimSpace(raw)) || sxxRe.MatchString(cleaned) {
		return true
	}
	if arcTailRe.MatchString(raw) && !strings.Contains(strings.ToLower(raw), "movie") {
		return true
	}
	nc, nr := titleNorm(cleaned), titleNorm(root)
	if nr != "" && nc == nr {
		return true
	}
	if nr != "" && strings.HasPrefix(nc, nr+" ") {
		rest := strings.TrimSpace(nc[len(nr):])
		if seasonRestRe.MatchString(rest) || sxxRe.MatchString(rest) || romanRe.MatchString(rest) {
			return true
		}
	}
	return false
}

func (c *classifier) isKindName(raw, cleaned, root string) bool {
	low := strings.ToLower(strings.TrimSpace(strings.TrimRight(raw, "/")))
	if _, ok := c.post.kinds[low]; ok {
		return true
	}
	if _, ok := c.post.kinds[strings.ToLower(cleaned)]; ok {
		return true
	}
	toks := strings.Fields(cleaned)
	var leftover []string
	for i, t := range toks {
		if c.post.kindToken(t) {
			continue
		}
		prevKind := i > 0 && c.post.kindToken(toks[i-1])
		nextKind := i+1 < len(toks) && c.post.kindToken(toks[i+1])
		if oneTwoDigitRe.MatchString(t) && (prevKind || nextKind) {
			continue
		}
		leftover = append(leftover, t)
	}
	rest := strings.TrimSpace(strings.Join(leftover, " "))
	if rest == "" {
		return true
	}
	if root != "" && titleNorm(rest) == titleNorm(root) {
		return true
	}
	return false
}

func (c *classifier) belongs(cleaned, raw, root string) bool {
	if cleaned == "" {
		return true
	}
	if episodeRe.MatchString(raw) {
		return true
	}
	if junkTitleRe.MatchString(cleaned) && strings.Contains(titleNorm(cleaned), titleNorm(root)) {
		return true
	}
	if c.isRootPlusYear(cleaned, root) || c.isAlias(cleaned, root) {
		return true
	}
	nr, nc := titleNorm(root), titleNorm(cleaned)
	if nr != "" && strings.HasPrefix(nc, nr) {
		rest := strings.TrimSpace(nc[len(nr):])
		if rest == "" {
			return true
		}
		if seasonRestRe.MatchString(rest) || sxxRe.MatchString(rest) || romanRe.MatchString(rest) {
			return true
		}
		if oneThreeDigRe.MatchString(rest) && rest != "0" {
			return true
		}
		return false
	}
	if nr != "" && strings.Contains(nc, nr) {
		rx := regexp.MustCompile(`\b` + regexp.QuoteMeta(nr) + `\b`)
		rest := strings.TrimSpace(spaceRe.ReplaceAllString(rx.ReplaceAllString(nc, " "), " "))
		if oneThreeDigRe.MatchString(rest) && rest != "0" {
			return true
		}
	}
	return false
}

func (c *classifier) classify(raw string, isDir bool, root string) string {
	if c.pre.extrasName(raw) {
		return "extras"
	}
	if !isDir {
		return "loose"
	}
	tagged := tagRe.MatchString(raw)
	cleaned := c.pre.segment(raw, false)
	if c.isSeasonName(raw, cleaned, root) {
		return "season"
	}
	if tagged {
		return "release"
	}
	if c.isKindName(raw, cleaned, root) {
		return "kind"
	}
	if c.isAlias(cleaned, root) || c.isRootPlusYear(cleaned, root) {
		return "season"
	}
	return "named"
}

func uniqueTitles(rows []groupedRow) []groupedRow {
	seen := map[string]groupedRow{}
	var order []string
	for _, r := range rows {
		r.title = strings.TrimSpace(r.title)
		if r.title == "" {
			continue
		}
		key := titleKey(r.title)
		if key == "" {
			continue
		}
		if old, ok := seen[key]; ok {
			if old.year == "" && r.year != "" {
				old.year = r.year
				seen[key] = old
			}
			continue
		}
		seen[key] = r
		order = append(order, key)
	}
	out := make([]groupedRow, 0, len(order))
	for _, k := range order {
		out = append(out, seen[k])
	}
	return out
}

func (g *grouper) group(child string) []groupedRow {
	root := g.pre.segment(filepath.Base(child), true)
	if root == "" {
		root = filepath.Base(child)
	}
	years := g.listingYears(child)
	st, err := os.Stat(child)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return g.looseFile(child, root, years, g.libRel(child))
	}
	return uniqueTitles(g.node(child, root, years, g.libRel(child)))
}

func (g *grouper) looseFile(path, root string, years map[string]bool, pathRel string) []groupedRow {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = path
	}
	if !g.keepVideo(dir, path) {
		return nil
	}
	cleaned := g.pre.segment(filepath.Base(path), true)
	label := root
	if cleaned != "" {
		label = g.post.label(cleaned, root)
	}
	if label == "" {
		label = root
	}
	title, year := g.emit(label, years)
	return []groupedRow{{title: title, year: year, path: pathRel}}
}

func (g *grouper) node(path, root string, years map[string]bool, pathRel string) []groupedRow {
	entries := g.immediate(path)
	if len(entries) == 0 {
		return nil
	}
	var out []groupedRow
	series := false
	var looseOther [][2]string
	for _, e := range entries {
		kind := g.cls.classify(e.name, e.dir, root)
		switch kind {
		case "extras":
			continue
		case "season":
			series = true
			continue
		case "release":
			cleaned := g.pre.segment(e.name, false)
			if g.cls.isAlias(cleaned, root) || g.cls.isSeasonName(e.name, cleaned, root) || g.cls.isRootPlusYear(cleaned, root) {
				series = true
			} else if g.cls.isKindName(e.name, cleaned, root) {
				inner := g.peek(e.abs, root, years, pathRel+"/"+e.name)
				if len(inner) > 0 {
					out = append(out, inner...)
				} else {
					series = true
				}
			} else {
				out = append(out, g.namedDir(e.abs, e.name, root, years, pathRel+"/"+e.name)...)
			}
			continue
		case "kind":
			inner := g.peek(e.abs, root, years, pathRel+"/"+e.name)
			if len(inner) > 0 {
				out = append(out, inner...)
			} else {
				series = true
			}
			continue
		case "named":
			out = append(out, g.namedDir(e.abs, e.name, root, years, pathRel+"/"+e.name)...)
			continue
		}
		if !g.keepVideo(path, e.abs) {
			continue
		}
		cleaned := g.pre.segment(e.name, true)
		if g.cls.belongs(cleaned, e.name, root) {
			series = true
		} else {
			looseOther = append(looseOther, [2]string{e.abs, cleaned})
		}
	}
	if len(looseOther) > 0 {
		names := make([]string, len(looseOther))
		for i, pair := range looseOther {
			names[i] = pair[1]
		}
		clustered := g.seqClusters(names, root)
		if !series && len(out) == 0 && len(clustered) == 1 {
			series = true
		} else {
			for _, row := range clustered {
				if g.cls.isAlias(row.label, root) || g.cls.isRootPlusYear(row.label, root) {
					series = true
					continue
				}
				title, year := g.emit(row.label, years)
				match := looseOther[0][0]
				for _, pair := range looseOther {
					if titleKey(pair[1]) == titleKey(row.label) {
						match = pair[0]
						break
					}
				}
				out = append(out, groupedRow{title: title, year: year, path: g.libRel(match)})
			}
		}
	}
	if series || (len(out) == 0 && g.hasKeptVideos(path)) {
		title, year := g.emit(root, years)
		out = append([]groupedRow{{title: title, year: year, path: pathRel}}, out...)
	}
	return out
}

func (g *grouper) hasKeptVideos(path string) bool {
	for _, p := range g.videosFor(path) {
		if g.keepVideo(path, p) {
			return true
		}
	}
	return false
}

func (g *grouper) namedDir(absP, name, walkRoot string, years map[string]bool, pathRel string) []groupedRow {
	local := g.pre.segment(name, false)
	if local == "" {
		local = name
	}
	local = g.post.label(local, walkRoot)
	if g.cls.isAlias(local, walkRoot) || g.cls.isSeasonName(name, local, walkRoot) {
		return g.peek(absP, walkRoot, years, pathRel)
	}
	return g.node(absP, local, years, pathRel)
}

func (g *grouper) peek(absP, root string, years map[string]bool, pathRel string) []groupedRow {
	var paths []string
	for _, p := range g.videosFor(absP) {
		if g.keepVideo(absP, p) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	var names []string
	for _, p := range paths {
		if n := g.pre.rel(relFromChild(absP, p)); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	var out []groupedRow
	for _, row := range g.seqClusters(names, root) {
		if row.label == "" || g.cls.isAlias(row.label, root) || g.cls.isRootPlusYear(row.label, root) {
			continue
		}
		title, year := g.emit(row.label, years)
		out = append(out, groupedRow{title: title, year: year, path: pathRel})
	}
	return uniqueTitles(out)
}
