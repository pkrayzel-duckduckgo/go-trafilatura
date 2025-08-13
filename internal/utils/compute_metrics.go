package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// -----------------------------
// Bucketing (exported)
// -----------------------------

// Thresholds (exported so callers can keep CSV summaries consistent)
const (
	ThContainMuchBetter   = 0.95
	ThContainSlightBetter = 0.90
	ThContainSlightWorseL = 0.70
)

// Labels (exported for CSV & summaries)
const (
	LMuchBetter   = "much_better"
	LSlightBetter = "slightly_better"
	LSame         = "same"
	LSlightWorse  = "slightly_worse"
	LMuchWorse    = "much_worse"
	LError        = "error"
)

// Bucket assigns a coarse label and reason for an A/B extraction delta, using Metrics.
// This mirrors the previous CLI-local logic so downstream tooling can rely on a single source of truth.

// labels

const (
	thContainMuchBetter   = 0.95
	thContainSlightBetter = 0.90
	thContainSlightWorseL = 0.70
)

const (
	lMuchBetter   = "much_better"
	lSlightBetter = "slightly_better"
	lSame         = "same"
	lSlightWorse  = "slightly_worse"
	lMuchWorse    = "much_worse"
	lError        = "error"
)

func Bucket(m Metrics) (label string, reason string) {
	dupDelta := m.DupRatioB - m.DupRatioA

	// much worse
	if m.ContainmentAinB < thContainSlightWorseL || m.Jaccard < 0.70 ||
		m.DeltaTokens <= -100 || m.DeltaSent <= -3 || dupDelta > 0.10 ||
		(m.AShingles > 0 && m.BShingles == 0) {
		return lMuchWorse, "drop/empty/large_dup_increase"
	}

	// slightly worse
	if (m.ContainmentAinB >= thContainSlightWorseL && m.ContainmentAinB < thContainSlightBetter) ||
		m.DeltaTokens <= -30 || m.DeltaSent <= -1 ||
		(dupDelta > 0.03 && dupDelta <= 0.10) {
		return lSlightWorse, "partial_drop_or_dup_increase"
	}

	// same
	if m.Jaccard >= 0.98 || (m.ContainmentAinB >= 0.98 && m.ContainmentBinA >= 0.98) {
		if m.AShingles == 0 && m.BShingles == 0 {
			return lSame, "both_empty"
		}
		return lSame, "sameish"
	}

	// slightly better
	if m.ContainmentAinB >= thContainSlightBetter &&
		m.NovelBminusA >= 0.05 && m.NovelBminusA < 0.15 &&
		dupDelta <= 0.03 &&
		(m.DeltaTokens >= 10 || m.DeltaSent >= 1) {
		return lSlightBetter, "minor_gain_low_dup"
	}

	// much better
	if m.ContainmentAinB >= thContainMuchBetter &&
		m.NovelBminusA >= 0.15 &&
		dupDelta <= 0.02 &&
		(m.DeltaTokens >= 50 || m.DeltaSent >= 2) {
		return lMuchBetter, "clear_gain_low_dup"
	}

	return lSame, "default_same"
}

type Metrics struct {
	ContainmentAinB float64 `json:"containment_A_in_B"`
	ContainmentBinA float64 `json:"containment_B_in_A"`
	Jaccard         float64 `json:"jaccard"`
	NovelBminusA    float64 `json:"novel_B_minus_A"`

	DeltaTokens int `json:"delta_tokens"`
	DeltaChars  int `json:"delta_chars"`
	DeltaSent   int `json:"delta_sentences"`

	DupRatioA float64 `json:"dup_ratio_A"`
	DupRatioB float64 `json:"dup_ratio_B"`

	AShingles    int `json:"A_shingles"`
	BShingles    int `json:"B_shingles"`
	Intersection int `json:"intersection"`
	Union        int `json:"union"`
}

func ComputeMetrics(bodyA, bodyB string, k int) Metrics {
	A := normalize(bodyA)
	B := normalize(bodyB)

	Atok := tokenize(A)
	Btok := tokenize(B)

	Alist, Aset := shingleHashes(Atok, k)
	Blist, Bset := shingleHashes(Btok, k)

	inter := intersectCount(Aset, Bset)
	uni := len(Aset) + len(Bset) - inter

	var containAinB, containBinA, jaccard, novel float64
	switch {
	case len(Aset) == 0 && len(Bset) == 0:
		// Both empty => identical
		containAinB, containBinA, jaccard, novel = 1.0, 1.0, 1.0, 0.0
	case len(Aset) == 0 && len(Bset) > 0:
		// Empty contained in anything; Jaccard 0, all of B is novel
		containAinB, containBinA, jaccard, novel = 1.0, 0.0, 0.0, 1.0
	case len(Bset) == 0 && len(Aset) > 0:
		// Symmetric case
		containAinB, containBinA, jaccard, novel = 0.0, 1.0, 0.0, 0.0
	default:
		containAinB = float64(inter) / float64(len(Aset))
		containBinA = float64(inter) / float64(len(Bset))
		if uni == 0 {
			jaccard = 1.0
		} else {
			jaccard = float64(inter) / float64(uni)
		}
		novel = float64(len(Bset)-inter) / float64(len(Bset))
	}

	return Metrics{
		ContainmentAinB: containAinB,
		ContainmentBinA: containBinA,
		Jaccard:         jaccard,
		NovelBminusA:    novel,

		DeltaTokens: len(Btok) - len(Atok),
		DeltaChars:  len(B) - len(A),
		DeltaSent:   sentenceCount(B) - sentenceCount(A),

		DupRatioA: duplicationRatio(Alist),
		DupRatioB: duplicationRatio(Blist),

		AShingles:    len(Aset),
		BShingles:    len(Bset),
		Intersection: inter,
		Union:        uni,
	}
}

// --- Text processing helpers ---

var wordReCompare = regexp.MustCompile(`\p{L}+|\p{N}+`)

func normalize(s string) string {
	// Normalize newlines, collapse all whitespace runs to single spaces, and trim
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func tokenize(s string) []string {
	return wordReCompare.FindAllString(strings.ToLower(s), -1)
}

// shingleHashes returns both the sequential list of shingle hashes (for duplication ratio)
// and a set of unique shingle hashes (for set metrics).
//
// FIX: When len(tokens) < k but > 0, emit a single shingle from all tokens.
// This prevents empty-shingle edge cases from producing misleading similarity.
func shingleHashes(tokens []string, k int) ([]uint64, map[uint64]struct{}) {
	if k <= 0 {
		panic("k must be >= 1")
	}
	switch {
	case len(tokens) == 0:
		return nil, map[uint64]struct{}{}
	case len(tokens) < k:
		joined := strings.Join(tokens, " ")
		h := fnv64a(joined)
		return []uint64{h}, map[uint64]struct{}{h: {}}
	default:
		list := make([]uint64, 0, len(tokens)-k+1)
		set := make(map[uint64]struct{}, cap(list))
		for i := 0; i <= len(tokens)-k; i++ {
			h := fnv64a(strings.Join(tokens[i:i+k], " "))
			list = append(list, h)
			set[h] = struct{}{}
		}
		return list, set
	}
}

func fnv64a(s string) uint64 {
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	var hash uint64 = offset64
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	return hash
}

func intersectCount(a, b map[uint64]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// iterate smaller set
	if len(a) > len(b) {
		a, b = b, a
	}
	c := 0
	for h := range a {
		if _, ok := b[h]; ok {
			c++
		}
	}
	return c
}

func duplicationRatio(list []uint64) float64 {
	if len(list) == 0 {
		return 0
	}
	seen := make(map[uint64]struct{}, len(list))
	for _, h := range list {
		seen[h] = struct{}{}
	}
	return float64(len(list)-len(seen)) / float64(len(list))
}

// sentenceCount counts sequences of terminal punctuation ('.', '!', '?')
// as a single sentence end. So "what?!" and "Really???" each count as 1.
//
// Examples:
//
//	"Wait... what?! Really???" -> 3
//	"Paragraph one.\n\nParagraph two.\nSame paragraph continued." -> 2
func sentenceCount(s string) int {
	count := 0
	inTermRun := false
	for _, r := range s {
		switch r {
		case '.', '!', '?':
			if !inTermRun {
				count++
				inTermRun = true
			}
		default:
			inTermRun = false
		}
	}
	return count
}

// small helper kept here to avoid duplicating tiny formatters in callers
func fmtf(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", f), "0"), ".")
}
