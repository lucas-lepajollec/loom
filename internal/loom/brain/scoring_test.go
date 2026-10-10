package brain

import (
	"math"
	"testing"
)

// The extraction must preserve floating-point arithmetic, including zero tf.
func TestBM25Compatibility(t *testing.T) {
	for _, n := range []int{1, 7, 100, MaxChunks} {
		for _, df := range []int{0, 1, n / 2, n} {
			idf := math.Log(1 + (float64(n-df)+0.5)/(float64(df)+0.5))
			if bm25IDF(n, df) != idf {
				t.Fatal("inverse document frequency changed")
			}
			for _, tf := range []float64{0, 1, 2, 17, 1200} {
				for _, length := range []int{1, 32, 1200} {
					for _, average := range []float64{1, 7.5, 1200} {
						want := idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(length)/average))
						if got := bm25Term(idf, tf, length, average); got != want {
							t.Fatalf("n=%d df=%d tf=%g length=%d average=%g: %g != %g", n, df, tf, length, average, got, want)
						}
					}
				}
			}
		}
	}
}
