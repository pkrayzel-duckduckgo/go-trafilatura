package utils

import (
	"math"
	"strings"
	"testing"
)

func floatEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestComputeMetrics(t *testing.T) {
	tests := []struct {
		name     string
		bodyA    string
		bodyB    string
		k        int
		expected Metrics
		desc     string
	}{
		{
			name:  "identical_texts",
			bodyA: "The quick brown fox jumps over the lazy dog",
			bodyB: "The quick brown fox jumps over the lazy dog",
			k:     5,
			expected: Metrics{
				ContainmentAinB: 1.0,
				ContainmentBinA: 1.0,
				Jaccard:         1.0,
				NovelBminusA:    0.0,
				DeltaTokens:     0,
				DeltaChars:      0,
				DeltaSent:       0,
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       5, // 9 tokens -> 9-5+1 = 5
				BShingles:       5,
				Intersection:    5,
				Union:           5,
			},
			desc: "Identical texts should have perfect similarity metrics",
		},
		{
			name:  "short_text_under_k_tokens",
			bodyA: "Hello world",
			bodyB: "Hello there",
			k:     5,
			expected: Metrics{
				ContainmentAinB: 0.0,
				ContainmentBinA: 0.0,
				Jaccard:         0.0,
				NovelBminusA:    1.0, // B is entirely novel vs A
				DeltaTokens:     0,
				DeltaChars:      0,
				DeltaSent:       0,
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       1, // fixed behavior: 1 shingle per side when len(tokens) < k
				BShingles:       1,
				Intersection:    0,
				Union:           2,
			},
			desc: "Short texts (<k) now produce one shingle each and proper set metrics",
		},
		{
			name:  "short_text_under_k_tokens_identical",
			bodyA: "Hello world",
			bodyB: "hello   world!!",
			k:     5,
			expected: Metrics{
				ContainmentAinB: 1.0,
				ContainmentBinA: 1.0,
				Jaccard:         1.0,
				NovelBminusA:    0.0,
				DeltaTokens:     0, // tokenization ignores case/punct for tokens
				DeltaChars:      2, // "hello world!!" (13) - "Hello world" (11) = +2
				DeltaSent:       1, // B has terminal "!!", A has none
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       1,
				BShingles:       1,
				Intersection:    1,
				Union:           1,
			},
			desc: "Short identical texts (<k) yield a single matching shingle and perfect similarity",
		},
		{
			name:  "empty_vs_non_empty",
			bodyA: "",
			bodyB: "Some content here",
			k:     5,
			expected: Metrics{
				ContainmentAinB: 1.0, // empty is contained in anything
				ContainmentBinA: 0.0,
				Jaccard:         0.0,
				NovelBminusA:    1.0, // all of B is novel
				DeltaTokens:     3,
				DeltaChars:      17,
				DeltaSent:       0, // no terminal punctuation
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       0,
				BShingles:       1, // fixed behavior for <k
				Intersection:    0,
				Union:           1,
			},
			desc: "Empty vs non-empty edge case with <k shingles",
		},
		{
			name:  "both_empty",
			bodyA: "",
			bodyB: "",
			k:     5,
			expected: Metrics{
				ContainmentAinB: 1.0,
				ContainmentBinA: 1.0,
				Jaccard:         1.0,
				NovelBminusA:    0.0,
				DeltaTokens:     0,
				DeltaChars:      0,
				DeltaSent:       0,
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       0,
				BShingles:       0,
				Intersection:    0,
				Union:           0,
			},
			desc: "Both empty are identical",
		},
		{
			name:  "duplication_detection",
			bodyA: "word word word different",
			bodyB: "word word word word different different",
			k:     2,
			expected: Metrics{
				ContainmentAinB: 1.0,       // set-based: 2/2
				ContainmentBinA: 2.0 / 3.0, // set-based: 2/3
				Jaccard:         2.0 / 3.0, // 2 / (2+3-2)
				NovelBminusA:    1.0 / 3.0, // (3-2)/3
				DeltaTokens:     2,         // 6-4
				DeltaChars:      15,        // with current normalize()
				DeltaSent:       0,
				DupRatioA:       1.0 / 3.0, // (3-2)/3 using list (bigrams: 3 total, 2 unique)
				DupRatioB:       0.4,       // (5-3)/5
				AShingles:       2,
				BShingles:       3,
				Intersection:    2,
				Union:           3,
			},
			desc: "Duplication ratio and set overlaps line up on repeated n-grams",
		},
		{
			name:  "normalization_test",
			bodyA: "Text   with\r\nextra\twhitespace\r\n",
			bodyB: "Text with extra whitespace",
			k:     3,
			expected: Metrics{
				ContainmentAinB: 1.0,
				ContainmentBinA: 1.0,
				Jaccard:         1.0,
				NovelBminusA:    0.0,
				DeltaTokens:     0,
				DeltaChars:      0, // strings.Fields collapse makes them identical
				DeltaSent:       0,
				DupRatioA:       0.0,
				DupRatioB:       0.0,
				AShingles:       2, // 4 tokens -> 4-3+1 = 2
				BShingles:       2,
				Intersection:    2,
				Union:           2,
			},
			desc: "Whitespace normalization is consistent",
		},
	}

	// NOTE: two tests above (sentence_counting_equalized, duplication_detection) include
	//       more aggressive expectations. If you later change tokenization/normalization rules,
	//       you may need to adjust their exact char deltas or shingle counts.

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMetrics(tt.bodyA, tt.bodyB, tt.k)

			if !floatEqual(got.ContainmentAinB, tt.expected.ContainmentAinB) {
				t.Errorf("ContainmentAinB: got %.10f, want %.10f (%s)", got.ContainmentAinB, tt.expected.ContainmentAinB, tt.desc)
			}
			if !floatEqual(got.ContainmentBinA, tt.expected.ContainmentBinA) {
				t.Errorf("ContainmentBinA: got %.10f, want %.10f (%s)", got.ContainmentBinA, tt.expected.ContainmentBinA, tt.desc)
			}
			if !floatEqual(got.Jaccard, tt.expected.Jaccard) {
				t.Errorf("Jaccard: got %.10f, want %.10f (%s)", got.Jaccard, tt.expected.Jaccard, tt.desc)
			}
			if !floatEqual(got.NovelBminusA, tt.expected.NovelBminusA) {
				t.Errorf("NovelBminusA: got %.10f, want %.10f (%s)", got.NovelBminusA, tt.expected.NovelBminusA, tt.desc)
			}

			if got.DeltaTokens != tt.expected.DeltaTokens {
				t.Errorf("DeltaTokens: got %d, want %d (%s)", got.DeltaTokens, tt.expected.DeltaTokens, tt.desc)
			}
			if got.DeltaChars != tt.expected.DeltaChars {
				t.Errorf("DeltaChars: got %d, want %d (%s)", got.DeltaChars, tt.expected.DeltaChars, tt.desc)
			}
			if got.DeltaSent != tt.expected.DeltaSent {
				t.Errorf("DeltaSent: got %d, want %d (%s)", got.DeltaSent, tt.expected.DeltaSent, tt.desc)
			}

			if got.DupRatioA != 0 || got.DupRatioB != 0 || tt.expected.DupRatioA != 0 || tt.expected.DupRatioB != 0 {
				// compare with tolerance
				if !floatEqual(got.DupRatioA, tt.expected.DupRatioA) {
					t.Errorf("DupRatioA: got %.6f, want %.6f (%s)", got.DupRatioA, tt.expected.DupRatioA, tt.desc)
				}
				if !floatEqual(got.DupRatioB, tt.expected.DupRatioB) {
					t.Errorf("DupRatioB: got %.6f, want %.6f (%s)", got.DupRatioB, tt.expected.DupRatioB, tt.desc)
				}
			}

			if got.AShingles != tt.expected.AShingles {
				t.Errorf("AShingles: got %d, want %d (%s)", got.AShingles, tt.expected.AShingles, tt.desc)
			}
			if got.BShingles != tt.expected.BShingles {
				t.Errorf("BShingles: got %d, want %d (%s)", got.BShingles, tt.expected.BShingles, tt.desc)
			}
			if got.Intersection != tt.expected.Intersection {
				t.Errorf("Intersection: got %d, want %d (%s)", got.Intersection, tt.expected.Intersection, tt.desc)
			}
			if got.Union != tt.expected.Union {
				t.Errorf("Union: got %d, want %d (%s)", got.Union, tt.expected.Union, tt.desc)
			}
		})
	}
}

func TestShingleGenerationFixed(t *testing.T) {
	// explicit checks on list/set sizes for <k behavior
	list, set := shingleHashes([]string{"hello", "world"}, 5)
	if len(list) != 1 || len(set) != 1 {
		t.Fatalf("<k shingle fix broken: want 1/1, got list=%d set=%d", len(list), len(set))
	}
	list2, set2 := shingleHashes([]string{}, 5)
	if len(list2) != 0 || len(set2) != 0 {
		t.Fatalf("empty tokens should yield empty shingles: got list=%d set=%d", len(list2), len(set2))
	}
}

func TestContentExtractionScenarios(t *testing.T) {
	type scen struct {
		name       string
		original   string
		extracted  string
		k          int
		shouldPass bool
		desc       string
	}
	isGoodExtraction := func(m Metrics) bool {
		if m.ContainmentBinA < 0.95 {
			return false
		}
		if m.NovelBminusA > 0.15 {
			return false
		}
		if m.DeltaTokens > 0 {
			return false
		}
		if m.DupRatioB > m.DupRatioA+1e-6 {
			return false
		}
		return true
	}
	scenarios := []scen{
		{
			name: "good_extraction_removes_boilerplate",
			original: `Main article content here with important details.

Cookie banner: We use cookies to improve your experience.
Subscribe to our newsletter!
Privacy policy | Terms of service`,
			extracted:  "Main article content here with important details.",
			k:          3,
			shouldPass: true,
			desc:       "Keeps main content and removes boilerplate",
		},
		{
			name:       "over_aggressive_extraction_loses_recall",
			original:   "Important main content with details and context for understanding.",
			extracted:  "Important main",
			k:          3,
			shouldPass: false,
			desc:       "Drops too much content (low A→B containment)",
		},
		{
			name: "noisy_additions",
			original: `Core content paragraph that users care about.
Another relevant paragraph with context.`,
			extracted: `Core content paragraph that users care about.
Another relevant paragraph with context.
Sign up to our newsletter and accept all cookies!`,
			k:          3,
			shouldPass: false,
			desc:       "Adds boilerplate/noise (high novel B−A; longer)",
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			m := ComputeMetrics(sc.original, sc.extracted, sc.k)
			pass := isGoodExtraction(m)
			if pass != sc.shouldPass {
				t.Fatalf("%s\nHeuristic said pass=%v, want %v.\nMetrics: %+v", sc.desc, pass, sc.shouldPass, m)
			}
		})
	}
}

func TestSentenceCountEqualized(t *testing.T) {
	a := "First sentence. Second sentence! Third sentence?"
	b := "First sentence...\n\nSecond sentence?!\nThird sentence!!!"

	gotA := sentenceCount(a)
	gotB := sentenceCount(b)

	if gotA != 3 {
		t.Fatalf("sentenceCount(A) = %d, want 3", gotA)
	}
	if gotB != 3 {
		t.Fatalf("sentenceCount(B) = %d, want 3", gotB)
	}
}

// -----------------------------
// Bucket tests
// -----------------------------
// -----------------------------
// Bucket tests
// -----------------------------

func TestBucket(t *testing.T) {
	type tc struct {
		name   string
		m      Metrics
		expect string
		reason string
	}
	cases := []tc{
		{
			name: "much_worse_low_containment",
			m: Metrics{
				ContainmentAinB: 0.65, // below ThContainSlightWorseL (0.70)
				Jaccard:         0.69, // also below 0.70 guard
				DeltaTokens:     -5,
				DeltaSent:       0,
				DupRatioA:       0.00, DupRatioB: 0.05,
				AShingles: 10, BShingles: 10,
			},
			expect: LMuchWorse, reason: "drop/empty/large_dup_increase",
		},
		{
			name: "slightly_worse_partial_drop_by_containment_band",
			m: Metrics{
				ContainmentAinB: 0.85, // in [0.70, 0.90)
				Jaccard:         0.90,
				DeltaTokens:     -10, // not large enough to trigger much_worse
				DeltaSent:       0,
				DupRatioA:       0.02, DupRatioB: 0.03, // dupDelta = 0.01
				AShingles: 10, BShingles: 10,
			},
			expect: LSlightWorse, reason: "partial_drop_or_dup_increase",
		},
		{
			name: "slightly_worse_due_to_dup_increase",
			m: Metrics{
				ContainmentAinB: 0.93,
				Jaccard:         0.93,
				DeltaTokens:     0,
				DeltaSent:       0,
				DupRatioA:       0.02, DupRatioB: 0.07, // dupDelta = 0.05 -> slightly_worse
				AShingles: 8, BShingles: 8,
			},
			expect: LSlightWorse, reason: "partial_drop_or_dup_increase",
		},
		{
			name: "same_both_empty",
			m: Metrics{
				ContainmentAinB: 1.0, ContainmentBinA: 1.0, Jaccard: 1.0,
				AShingles: 0, BShingles: 0,
			},
			expect: LSame, reason: "both_empty",
		},
		{
			name: "same_sameish_high_similarity",
			m: Metrics{
				ContainmentAinB: 0.99, ContainmentBinA: 0.985, Jaccard: 0.981,
				AShingles: 5, BShingles: 5,
			},
			expect: LSame, reason: "sameish",
		},
		{
			name: "slightly_better_minor_gain_low_dup_by_tokens",
			m: Metrics{
				ContainmentAinB: 0.92, // >= 0.90
				Jaccard:         0.92,
				NovelBminusA:    0.10,                  // in [0.05, 0.15)
				DupRatioA:       0.05, DupRatioB: 0.06, // dupDelta = 0.01 <= 0.03
				DeltaTokens: 12, // >= 10
				DeltaSent:   0,
			},
			expect: LSlightBetter, reason: "minor_gain_low_dup",
		},
		{
			name: "slightly_better_minor_gain_low_dup_by_sentences",
			m: Metrics{
				ContainmentAinB: 0.91,
				Jaccard:         0.91,
				NovelBminusA:    0.06,
				DupRatioA:       0.02, DupRatioB: 0.02, // dupDelta = 0.00
				DeltaTokens: 3,
				DeltaSent:   1, // >= 1
			},
			expect: LSlightBetter, reason: "minor_gain_low_dup",
		},
		{
			name: "much_better_clear_gain_low_dup_by_tokens",
			m: Metrics{
				ContainmentAinB: 0.97, // >= 0.95
				Jaccard:         0.97,
				NovelBminusA:    0.20,                  // >= 0.15
				DupRatioA:       0.10, DupRatioB: 0.11, // dupDelta = 0.01 <= 0.02
				DeltaTokens: 60, // >= 50
				DeltaSent:   0,
			},
			expect: LMuchBetter, reason: "clear_gain_low_dup",
		},
		{
			name: "much_better_clear_gain_low_dup_by_sentences",
			m: Metrics{
				ContainmentAinB: 0.96,
				Jaccard:         0.96,
				NovelBminusA:    0.18,
				DupRatioA:       0.03, DupRatioB: 0.03, // dupDelta = 0.00
				DeltaTokens: 8,
				DeltaSent:   2, // >= 2
			},
			expect: LMuchBetter, reason: "clear_gain_low_dup",
		},
		{
			name: "much_worse_empty_B_after_A_nonempty",
			m: Metrics{
				ContainmentAinB: 0.0, Jaccard: 0.0,
				AShingles: 5, BShingles: 0, // triggers worst case guard
			},
			expect: LMuchWorse, reason: "drop/empty/large_dup_increase",
		},
		{
			name: "default_same_catch_all",
			m: Metrics{
				ContainmentAinB: 0.91, // not in slightly_worse band, below 0.98 sameish
				ContainmentBinA: 0.91,
				Jaccard:         0.91,
				NovelBminusA:    0.02, // < 0.05 so not slightly_better
				DupRatioA:       0.02, DupRatioB: 0.02,
				DeltaTokens: 0,
				DeltaSent:   0,
				AShingles:   10, BShingles: 10,
			},
			expect: LSame, reason: "default_same",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, why := Bucket(tt.m)
			if got != tt.expect || why != tt.reason {
				t.Fatalf("Bucket(%+v) = (%s, %s), want (%s, %s)",
					tt.m, got, why, tt.expect, tt.reason)
			}
		})
	}
}

func BenchmarkComputeMetrics(b *testing.B) {
	bodyA := strings.Repeat("This is a sample sentence for benchmarking purposes. ", 100)
	bodyB := strings.Repeat("This is a modified sentence for benchmarking purposes. ", 95)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeMetrics(bodyA, bodyB, 5)
	}
}
