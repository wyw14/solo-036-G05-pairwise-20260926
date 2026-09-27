package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// ResidueComposition describes the makeup of a recovered residue sample
// as mass percentages. The four components must sum to 100 (within a
// small tolerance to absorb laboratory rounding).
type ResidueComposition struct {
	WaxPercent    float64 `json:"wax_percent"`
	SandPercent   float64 `json:"sand_percent"`
	DebrisPercent float64 `json:"debris_percent"`
	OtherPercent  float64 `json:"other_percent"`
}

const compositionTolerance = 0.5

func (c ResidueComposition) Validate() error {
	components := []struct {
		name  string
		value float64
	}{
		{"wax", c.WaxPercent},
		{"sand", c.SandPercent},
		{"debris", c.DebrisPercent},
		{"other", c.OtherPercent},
	}
	total := 0.0
	for _, component := range components {
		if component.value < 0 || component.value > 100 {
			return fmt.Errorf("%s percent must be within 0 and 100", component.name)
		}
		total += component.value
	}
	if math.Abs(total-100) > compositionTolerance {
		return fmt.Errorf("composition must sum to 100 percent, got %.2f", total)
	}
	return nil
}

// ResidueSample is one independent physical sample recovered from the
// receiver. SampleKey is the operator-facing identity of the physical
// sample: distinct keys are independent samples, while multiple
// weighings may reference the same key (repeat weighings).
type ResidueSample struct {
	ID          string             `json:"id"`
	RunID       string             `json:"run_id"`
	SampleKey   string             `json:"sample_key"`
	StationID   string             `json:"station_id"`
	ChainageKM  float64            `json:"chainage_km"`
	Composition ResidueComposition `json:"composition"`
	CollectedAt time.Time          `json:"collected_at"`
	Note        string             `json:"note,omitempty"`
}

func (s ResidueSample) Validate() error {
	if s.RunID == "" || strings.TrimSpace(s.SampleKey) == "" {
		return errors.New("sample identity is incomplete")
	}
	if s.StationID == "" {
		return errors.New("sample station is required")
	}
	if s.ChainageKM < 0 {
		return errors.New("sample chainage must be non-negative")
	}
	return s.Composition.Validate()
}

// ResidueWeighing is a single weighing act. Several weighings sharing a
// SampleKey are repeat weighings of the same physical sample.
type ResidueWeighing struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	SampleKey string    `json:"sample_key"`
	MassKg    float64   `json:"mass_kg"`
	ScaleID   string    `json:"scale_id"`
	WeighedAt time.Time `json:"weighed_at"`
}

func (w ResidueWeighing) Validate() error {
	if w.RunID == "" || strings.TrimSpace(w.SampleKey) == "" {
		return errors.New("weighing identity is incomplete")
	}
	if w.MassKg <= 0 {
		return errors.New("weighing mass must be positive")
	}
	if strings.TrimSpace(w.ScaleID) == "" {
		return errors.New("weighing scale is required")
	}
	return nil
}

// SegmentDeposition is the computed deposition profile of one pipeline
// segment for a single run.
type SegmentDeposition struct {
	SegmentID     string  `json:"segment_id"`
	FromStation   string  `json:"from_station"`
	ToStation     string  `json:"to_station"`
	SampleCount   int     `json:"sample_count"`
	TotalMassKg   float64 `json:"total_mass_kg"`
	KgPerKm       float64 `json:"kg_per_km"`
	WaxPercent    float64 `json:"wax_percent"`
	SandPercent   float64 `json:"sand_percent"`
	DebrisPercent float64 `json:"debris_percent"`
	Direction     string  `json:"direction"`
}

// ResidueAnalysis is a traceable conclusion derived from the archive at
// a point in time. Generation increases per run; SampleIDs records
// exactly which samples the conclusion was drawn from.
type ResidueAnalysis struct {
	ID         string              `json:"id"`
	RunID      string              `json:"run_id"`
	Generation int64               `json:"generation"`
	SampleIDs  []string            `json:"sample_ids"`
	Segments   []SegmentDeposition `json:"segments"`
	CreatedAt  time.Time           `json:"created_at"`
}

// ResidueArchive is the per-run residue dossier: raw samples, raw
// weighings, and every analysis derived from them.
type ResidueArchive struct {
	RunID     string            `json:"run_id"`
	Samples   []ResidueSample   `json:"samples"`
	Weighings []ResidueWeighing `json:"weighings"`
	Analyses  []ResidueAnalysis `json:"analyses"`
	UpdatedAt time.Time         `json:"updated_at"`
}
