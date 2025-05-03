package codemap

import "fmt"

type transformation struct {
	cutStart, cutEnd uint
	addEllipsis      bool
	prependText      string
}

func collapseTransformations(cutRanges []transformation) ([]transformation, error) {
	var filteredRanges []transformation
	for i, r := range cutRanges {
		isSubset := false
		for j, other := range cutRanges {
			if i != j {
				if r.cutStart >= other.cutStart && r.cutEnd <= other.cutEnd {
					isSubset = true
					break
				}
				if (r.cutStart < other.cutStart && r.cutEnd > other.cutStart && r.cutEnd < other.cutEnd) ||
					(r.cutStart > other.cutStart && r.cutStart < other.cutEnd && r.cutEnd > other.cutEnd) {
					return nil, fmt.Errorf("overlapping cut ranges detected: %v and %v", r, other)
				}
			}
		}
		if !isSubset {
			filteredRanges = append(filteredRanges, r)
		}
	}
	return filteredRanges, nil
}
