package tracker

import (
	"testing"
)

func TestComputeEntropyScore(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		unique   int
		minScore float64
		maxScore float64
	}{
		{
			name:     "zero keys",
			total:    0,
			unique:   0,
			minScore: 0,
			maxScore: 0,
		},
		{
			name:     "repetitive key typing (aaaa)",
			total:    100,
			unique:   1,
			minScore: 0,
			maxScore: 10,
		},
		{
			name:     "high diversity typing",
			total:    100,
			unique:   40,
			minScore: 20,
			maxScore: 100,
		},
		{
			name:     "high volume and high unique keys",
			total:    1000,
			unique:   500,
			minScore: 40,
			maxScore: 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ComputeEntropyScore(tt.total, tt.unique)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ComputeEntropyScore(%d, %d) = %f; want score between %f and %f",
					tt.total, tt.unique, score, tt.minScore, tt.maxScore)
			}
		})
	}
}
