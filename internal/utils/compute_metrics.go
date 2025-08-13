package utils

import (
	"regexp"
	"strings"
)

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
	if len(Aset) == 0 {
		containAinB = 1
	} else {
		containAinB = float64(inter) / float64(len(Aset))
	}
	if len(Bset) == 0 {
		containBinA = 1
	} else {
		containBinA = float64(inter) / float64(len(Bset))
	}
	if uni == 0 {
		jaccard = 1
	} else {
		jaccard = float64(inter) / float64(uni)
	}
	if len(Bset) == 0 {
		novel = 0
	} else {
		novel = float64(len(Bset)-inter) / float64(len(Bset))
	}

	return Metrics{
		ContainmentAinB: containAinB,
		ContainmentBinA: containBinA,
		Jaccard:         jaccard,
		NovelBminusA:    novel,
		DeltaTokens:     len(Btok) - len(Atok),
		DeltaChars:      len(B) - len(A),
		DeltaSent:       sentenceCount(B) - sentenceCount(A),
		DupRatioA:       duplicationRatio(Alist),
		DupRatioB:       duplicationRatio(Blist),
		AShingles:       len(Aset),
		BShingles:       len(Bset),
		Intersection:    inter,
		Union:           uni,
	}
}

var wordReCompare = regexp.MustCompile(`\p{L}+|\p{N}+`)

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
func tokenize(s string) []string {
	return wordReCompare.FindAllString(strings.ToLower(s), -1)
}
func shingleHashes(tokens []string, k int) ([]uint64, map[uint64]struct{}) {
	if k <= 0 {
		panic("k must be >= 1")
	}
	if len(tokens) < k {
		return nil, map[uint64]struct{}{}
	}
	list := make([]uint64, 0, len(tokens)-k+1)
	set := make(map[uint64]struct{}, cap(list))
	for i := 0; i <= len(tokens)-k; i++ {
		h := fnv64a(strings.Join(tokens[i:i+k], " "))
		list = append(list, h)
		set[h] = struct{}{}
	}
	return list, set
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
func sentenceCount(s string) int {
	re := regexp.MustCompile(`[.!?]+|\n+`)
	parts := re.Split(s, -1)
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}
