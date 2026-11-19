package brain

import "math"

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Keep scoring shared while each caller owns its corpus, term counts and policy.
func bm25IDF(documents, frequency int) float64 {
	return math.Log(1 + (float64(documents-frequency)+0.5)/(float64(frequency)+0.5))
}

func bm25Term(idf, frequency float64, length int, average float64) float64 {
	return idf * frequency * (bm25K1 + 1) / (frequency + bm25K1*((1-bm25B)+bm25B*float64(length)/average))
}
