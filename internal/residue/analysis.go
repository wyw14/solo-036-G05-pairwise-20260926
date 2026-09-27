package residue

import (
	"time"
)

// Trend classifications for segment deposition compared with the
// previous analysis on the same route.
const (
	TrendInitial = "initial"
	TrendRising  = "rising"
	TrendFalling = "falling"
	TrendSteady  = "steady"
)

// trendBand is the relative change within which deposition is steady.
const trendBand = 0.05

// SegmentDeposition aggregates the weighed residue of one line section.
type SegmentDeposition struct {
	SegmentID     string   `json:"segment_id"`
	FromKM        float64  `json:"from_km"`
	ToKM          float64  `json:"to_km"`
	SpecimenCount int      `json:"specimen_count"`
	TotalMassG    float64  `json:"total_mass_g"`
	MeanMassG     float64  `json:"mean_mass_g"`
	KgPerKm       float64  `json:"kg_per_km"`
	PrevKgPerKm   *float64 `json:"prev_kg_per_km,omitempty"`
	Trend         string   `json:"trend"`
}

// Analysis is a computed deposition conclusion. It records every specimen
// and weighing it consumed so the result stays traceable to raw records.
type Analysis struct {
	ID          string              `json:"id"`
	ArchiveID   string              `json:"archive_id"`
	RunID       string              `json:"run_id"`
	Generation  int                 `json:"generation"`
	Segments    []SegmentDeposition `json:"segments"`
	SpecimenIDs []string            `json:"specimen_ids"`
	WeighingIDs []string            `json:"weighing_ids"`
	CreatedAt   time.Time           `json:"created_at"`
}

// RepresentativeMass reduces repeat weighings of one specimen to a single
// mass; independent specimens are never merged.
func RepresentativeMass(weighings []Weighing) (float64, bool) {
	if len(weighings) == 0 {
		return 0, false
	}
	total := 0.0
	for _, weighing := range weighings {
		total += weighing.MassGrams
	}
	return total / float64(len(weighings)), true
}

func classifyTrend(current, previous float64) string {
	if previous == 0 {
		if current > 0 {
			return TrendRising
		}
		return TrendSteady
	}
	delta := (current - previous) / previous
	switch {
	case delta > trendBand:
		return TrendRising
	case delta < -trendBand:
		return TrendFalling
	default:
		return TrendSteady
	}
}

// Compute aggregates specimen masses per archived segment and classifies
// the trend against the previous analysis on the same route, if any.
func Compute(archive Archive, specimens []Specimen, weighings map[string][]Weighing, previous *Analysis, id string, generation int, at time.Time) Analysis {
	previousBySegment := make(map[string]float64)
	if previous != nil {
		for _, segment := range previous.Segments {
			previousBySegment[segment.SegmentID] = segment.KgPerKm
		}
	}
	analysis := Analysis{
		ID:          id,
		ArchiveID:   archive.ID,
		RunID:       archive.RunID,
		Generation:  generation,
		Segments:    make([]SegmentDeposition, 0, len(archive.Segments)),
		SpecimenIDs: make([]string, 0, len(specimens)),
		WeighingIDs: make([]string, 0),
		CreatedAt:   at,
	}
	for _, span := range archive.Segments {
		deposition := SegmentDeposition{
			SegmentID: span.SegmentID,
			FromKM:    span.FromKM,
			ToKM:      span.ToKM,
			Trend:     TrendInitial,
		}
		for _, specimen := range specimens {
			if specimen.SegmentID != span.SegmentID {
				continue
			}
			mass, ok := RepresentativeMass(weighings[specimen.ID])
			if !ok {
				continue
			}
			deposition.SpecimenCount++
			deposition.TotalMassG += mass
			analysis.SpecimenIDs = append(analysis.SpecimenIDs, specimen.ID)
			for _, weighing := range weighings[specimen.ID] {
				analysis.WeighingIDs = append(analysis.WeighingIDs, weighing.ID)
			}
		}
		if deposition.SpecimenCount > 0 {
			deposition.MeanMassG = deposition.TotalMassG / float64(deposition.SpecimenCount)
			if span.LengthKM > 0 {
				deposition.KgPerKm = deposition.TotalMassG / 1000 / span.LengthKM
			}
		}
		if previous != nil {
			if prev, ok := previousBySegment[span.SegmentID]; ok {
				deposition.PrevKgPerKm = &prev
				deposition.Trend = classifyTrend(deposition.KgPerKm, prev)
			}
		}
		analysis.Segments = append(analysis.Segments, deposition)
	}
	return analysis
}
