package residue

import (
	"errors"
	"fmt"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/route"
	"sort"
)

// directionThreshold is the relative kg/km change versus the upstream
// segment beyond which a segment is labelled increasing or decreasing.
const directionThreshold = 0.10

const (
	DirectionIncreasing = "increasing"
	DirectionDecreasing = "decreasing"
	DirectionFlat       = "flat"
	DirectionIsolated   = "isolated"
)

// SegmentForChainage finds the enabled segment whose station window
// contains the chainage. The final segment's downstream boundary is
// inclusive so samples collected at the receiver still attribute.
func SegmentForChainage(segments []route.Segment, stations map[string]model.Station, chainageKM float64) (route.Segment, error) {
	ordered := append([]route.Segment(nil), segments...)
	sort.Slice(ordered, func(i, j int) bool {
		return stations[ordered[i].From].Kilometer < stations[ordered[j].From].Kilometer
	})
	for index, segment := range ordered {
		if !segment.Enabled {
			continue
		}
		from, fromOK := stations[segment.From]
		to, toOK := stations[segment.To]
		if !fromOK || !toOK {
			continue
		}
		if chainageKM < from.Kilometer {
			continue
		}
		if chainageKM < to.Kilometer || (index == len(ordered)-1 && chainageKM == to.Kilometer) {
			return segment, nil
		}
	}
	return route.Segment{}, fmt.Errorf("chainage %.3f km is outside the enabled network", chainageKM)
}

// Analyze derives the deposition profile along the line for one run.
// Samples without any weighing contribute no mass and are excluded;
// repeat weighings collapse to the sample's effective (median) mass.
func Analyze(archive model.ResidueArchive, segments []route.Segment, stations map[string]model.Station) (model.ResidueAnalysis, error) {
	type segmentTotal struct {
		mass    float64
		samples int
		wax     float64
		sand    float64
		debris  float64
	}
	totals := make(map[string]*segmentTotal)
	sampleIDs := make([]string, 0, len(archive.Samples))
	for _, sample := range archive.Samples {
		mass, ok := EffectiveMass(archive, sample.SampleKey)
		if !ok {
			continue
		}
		segment, err := SegmentForChainage(segments, stations, sample.ChainageKM)
		if err != nil {
			return model.ResidueAnalysis{}, fmt.Errorf("sample %s: %w", sample.SampleKey, err)
		}
		total, ok := totals[segment.ID]
		if !ok {
			total = &segmentTotal{}
			totals[segment.ID] = total
		}
		total.mass += mass
		total.samples++
		total.wax += sample.Composition.WaxPercent * mass
		total.sand += sample.Composition.SandPercent * mass
		total.debris += sample.Composition.DebrisPercent * mass
		sampleIDs = append(sampleIDs, sample.ID)
	}
	if len(sampleIDs) == 0 {
		return model.ResidueAnalysis{}, errors.New("archive has no weighed samples to analyze")
	}
	sort.Strings(sampleIDs)
	ordered := append([]route.Segment(nil), segments...)
	sort.Slice(ordered, func(i, j int) bool {
		return stations[ordered[i].From].Kilometer < stations[ordered[j].From].Kilometer
	})
	profile := make([]model.SegmentDeposition, 0, len(totals))
	upstream := 0.0
	for _, segment := range ordered {
		total, ok := totals[segment.ID]
		if !ok {
			continue
		}
		deposition := model.SegmentDeposition{
			SegmentID:     segment.ID,
			FromStation:   segment.From,
			ToStation:     segment.To,
			SampleCount:   total.samples,
			TotalMassKg:   total.mass,
			KgPerKm:       total.mass / segment.Length,
			WaxPercent:    total.wax / total.mass,
			SandPercent:   total.sand / total.mass,
			DebrisPercent: total.debris / total.mass,
			Direction:     DirectionIsolated,
		}
		if upstream > 0 {
			switch change := (deposition.KgPerKm - upstream) / upstream; {
			case change > directionThreshold:
				deposition.Direction = DirectionIncreasing
			case change < -directionThreshold:
				deposition.Direction = DirectionDecreasing
			default:
				deposition.Direction = DirectionFlat
			}
		}
		upstream = deposition.KgPerKm
		profile = append(profile, deposition)
	}
	return model.ResidueAnalysis{RunID: archive.RunID, SampleIDs: sampleIDs, Segments: profile}, nil
}
