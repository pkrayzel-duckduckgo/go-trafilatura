package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/markusmobius/go-trafilatura"
	"github.com/markusmobius/go-trafilatura/internal/utils"
	"github.com/spf13/cobra"
)

// worker result
type res struct {
	url, file    string
	bodyA, bodyB string
	m            utils.Metrics
	err          error
}

func compareCmd() *cobra.Command {
	var (
		inDir       string
		outCSV      string
		k           int
		concurrency int
		urlFrom     string // canonical|filename|none
		emitBodies  bool
		limit       int
	)

	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Run A/B extraction on a directory of HTML and export quality metrics to CSV",
		RunE: func(cmd *cobra.Command, args []string) error {
			if inDir == "" {
				return errors.New("-in is required")
			}
			files, err := walkHTML(inDir, limit)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return fmt.Errorf("no HTML-like files under %s", inDir)
			}

			// -------- Define your two option sets here (hard-coded) --------
			// A: mirrors your production-ish defaults
			optsA := trafilatura.Options{
				Config: &trafilatura.Config{
					CacheSize:                    4096,
					MinDuplicateCheckSize:        100,
					MaxDuplicateCount:            2,
					MinExtractedSize:             250,
					MinExtractedCommentSize:      1,
					MinOutputSize:                0,
					MinOutputCommentSize:         0,
					MinExtractedParagraphPercent: 0.25, // fork heuristic
				},
				ExcludeComments:     false,
				IncludeImages:       false,
				IncludeLinks:        false,
				EnableLog:           false,
				EnableFallback:      false,
				HtmlDateMode:        trafilatura.Disabled, // perf leak avoided
				FilterCookieBanners: true,
				// OriginalURL set per-document below
			}

			// B: tweak only what you're testing (edit as needed)
			optsB := trafilatura.Options{
				Config: &trafilatura.Config{
					CacheSize:                    4096,
					MinDuplicateCheckSize:        100,
					MaxDuplicateCount:            2,
					MinExtractedSize:             250,
					MinExtractedCommentSize:      1,
					MinOutputSize:                0,
					MinOutputCommentSize:         0,
					MinExtractedParagraphPercent: 0.25,
				},
				ExcludeComments:     false,
				IncludeImages:       false,
				IncludeLinks:        false,
				EnableLog:           false,
				EnableFallback:      false,
				HtmlDateMode:        trafilatura.Disabled,
				FilterCookieBanners: true,
				// Example toggle under test:
				// Deduplicate: true,
			}

			// Worker pool
			type job struct{ path string }
			jobs := make(chan job)
			out := make(chan res)
			var wg sync.WaitGroup

			// Force writeOutput to produce TXT like the main command would
			txtCmd := &cobra.Command{}
			txtCmd.Flags().String("format", "txt", "")
			_ = txtCmd.Flags().Set("format", "txt")

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range jobs {
						htmlBytes, err := os.ReadFile(j.path)
						if err != nil {
							out <- res{file: j.path, err: err}
							continue
						}

						// Derive URL string, set OriginalURL on both option sets
						uStr := deriveURL(urlFrom, j.path, htmlBytes)
						var parsed *url.URL
						if uStr != "" {
							if pu, perr := url.Parse(uStr); perr == nil {
								parsed = pu
							}
						}

						// Copy per doc (avoid sharing pointers across goroutines)
						a := optsA
						b := optsB
						a.OriginalURL = parsed
						b.OriginalURL = parsed

						// Extract A
						rA, errA := processFile(j.path, a)
						if errA != nil || rA == nil {
							out <- res{file: j.path, url: uStr, err: fmt.Errorf("A: %v", errA)}
							continue
						}
						// Extract B
						rB, errB := processFile(j.path, b)
						if errB != nil || rB == nil {
							out <- res{file: j.path, url: uStr, err: fmt.Errorf("B: %v", errB)}
							continue
						}

						// Render body text via the same pipeline the CLI uses
						var bufA, bufB bytes.Buffer
						if err := writeOutput(&bufA, rA, txtCmd); err != nil {
							out <- res{file: j.path, url: uStr, err: fmt.Errorf("render A: %v", err)}
							continue
						}
						if err := writeOutput(&bufB, rB, txtCmd); err != nil {
							out <- res{file: j.path, url: uStr, err: fmt.Errorf("render B: %v", err)}
							continue
						}
						bodyA := bufA.String()
						bodyB := bufB.String()

						// Metrics
						m := utils.ComputeMetrics(bodyA, bodyB, k)

						out <- res{
							url: uStr, file: j.path, bodyA: bodyA, bodyB: bodyB, m: m,
						}
					}
				}()
			}

			go func() {
				for _, f := range files {
					jobs <- job{path: f}
				}
				close(jobs)
				wg.Wait()
				close(out)
			}()

			// Write CSV
			if outCSV == "" {
				outCSV = "metrics.csv"
			}
			if err := writeCSV(outCSV, out, emitBodies); err != nil {
				return err
			}
			log.Info().Msgf("wrote %s", outCSV)
			summarizeCSV(outCSV)
			return nil
		},
	}

	// Basic args only (A/B options are hard-coded above)
	cmd.Flags().StringVar(&inDir, "in", "", "Directory with HTML files (recursed)")
	cmd.Flags().StringVar(&outCSV, "out", "metrics.csv", "Output CSV path")
	cmd.Flags().IntVar(&k, "k", 5, "Shingle size k (default 5)")
	cmd.Flags().IntVar(&concurrency, "concurrency", runtime.NumCPU(), "Parallel workers")
	cmd.Flags().StringVar(&urlFrom, "url-from", "canonical", "URL source: canonical|filename|none")
	cmd.Flags().BoolVar(&emitBodies, "emit-bodies", false, "Include body_a/body_b in CSV")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max HTML files to process (0=all)")

	return cmd
}

// -------- file walking (extension + sniff) ----------

func walkHTML(dir string, limit int) ([]string, error) {
	suffixes := []string{".html", ".htm", ".xhtml", ".shtml"}
	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		for _, sfx := range suffixes {
			if strings.HasSuffix(name, sfx) {
				files = append(files, path)
				return nil
			}
		}
		// quick content sniff for HTML-like files without a typical extension
		if likelyHTMLFile(path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

func likelyHTMLFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	b := strings.ToLower(string(buf[:n]))
	return strings.Contains(b, "<html") || strings.Contains(b, "<!doctype") ||
		strings.Contains(b, "<head") || strings.Contains(b, "<body")
}

// -------- bucket thresholds & function ----------

const (
	thContainMuchBetter   = 0.95
	thContainSlightBetter = 0.90
	thContainSlightWorseL = 0.70
)

// labels
const (
	lMuchBetter   = "much_better"
	lSlightBetter = "slightly_better"
	lSame         = "same"
	lSlightWorse  = "slightly_worse"
	lMuchWorse    = "much_worse"
	lError        = "error"
)

func bucket(m utils.Metrics) (label string, reason string) {
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

// -------- CSV writer + summary ----------

func writeCSV(path string, results <-chan res, emitBodies bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"url", "file",
		"containment_A_in_B", "containment_B_in_A", "jaccard", "novel_B_minus_A",
		"delta_tokens", "delta_chars", "delta_sentences",
		"dup_ratio_A", "dup_ratio_B",
		"A_shingles", "B_shingles", "intersection", "union",
		"bucket", "note",
	}
	if emitBodies {
		header = append(header, "body_a", "body_b")
	}
	if err := w.Write(header); err != nil {
		return err
	}

	var rows []res
	for r := range results {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].file < rows[j].file })

	for _, r := range rows {
		if r.err != nil {
			rec := []string{r.url, r.file}
			rec = append(rec, make([]string, 12)...) // metrics blanks
			rec = append(rec, lError, "error:"+r.err.Error())
			if emitBodies {
				rec = append(rec, "", "")
			}
			if err := w.Write(rec); err != nil {
				return err
			}
			continue
		}

		lab, note := bucket(r.m)
		rec := []string{
			r.url, r.file,
			fmtf(r.m.ContainmentAinB),
			fmtf(r.m.ContainmentBinA),
			fmtf(r.m.Jaccard),
			fmtf(r.m.NovelBminusA),
			fmt.Sprintf("%d", r.m.DeltaTokens),
			fmt.Sprintf("%d", r.m.DeltaChars),
			fmt.Sprintf("%d", r.m.DeltaSent),
			fmtf(r.m.DupRatioA),
			fmtf(r.m.DupRatioB),
			fmt.Sprintf("%d", r.m.AShingles),
			fmt.Sprintf("%d", r.m.BShingles),
			fmt.Sprintf("%d", r.m.Intersection),
			fmt.Sprintf("%d", r.m.Union),
			lab, note,
		}
		if emitBodies {
			rec = append(rec, r.bodyA, r.bodyB)
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

func summarizeCSV(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil || len(rows) <= 1 {
		return
	}
	// find bucket column
	hdr := rows[0]
	bi := -1
	for i, h := range hdr {
		if h == "bucket" {
			bi = i
			break
		}
	}
	if bi < 0 {
		return
	}
	counts := map[string]int{}
	total := 0
	for _, rec := range rows[1:] {
		if len(rec) > bi {
			counts[rec[bi]]++
			total++
		}
	}
	// stable order in summary
	labels := []string{lMuchBetter, lSlightBetter, lSame, lSlightWorse, lMuchWorse, lError}
	fmt.Fprintf(os.Stderr,
		"bucket summary: %s=%d %s=%d %s=%d %s=%d %s=%d %s=%d (n=%d)\n",
		labels[0], counts[labels[0]],
		labels[1], counts[labels[1]],
		labels[2], counts[labels[2]],
		labels[3], counts[labels[3]],
		labels[4], counts[labels[4]],
		labels[5], counts[labels[5]],
		total,
	)
}

// -------- URL derivation (for CSV & OriginalURL) ----------

var (
	reCanonical = regexp.MustCompile(`(?is)<link[^>]+rel=['"]canonical['"][^>]+href=['"]([^'"]+)`)
	reOGURL     = regexp.MustCompile(`(?is)<meta[^>]+property=['"]og:url['"][^>]+content=['"]([^'"]+)`)
)

func deriveURL(mode, htmlPath string, html []byte) string {
	switch mode {
	case "canonical":
		if m := reCanonical.FindSubmatch(html); len(m) == 2 {
			return string(m[1])
		}
		if m := reOGURL.FindSubmatch(html); len(m) == 2 {
			return string(m[1])
		}
		fallthrough
	case "filename":
		return "file://" + filepath.ToSlash(htmlPath)
	case "none":
		return ""
	default:
		return "file://" + filepath.ToSlash(htmlPath)
	}
}

// small float formatter
func fmtf(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", f), "0"), ".")
}
