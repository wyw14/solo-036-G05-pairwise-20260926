package residue

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// SegmentSpan captures one line section of the route as it was when the
// pig was received, so residue records stay traceable to the line
// configuration of that run even after the route lease is released.
type SegmentSpan struct {
	SegmentID string  `json:"segment_id"`
	FromKM    float64 `json:"from_km"`
	ToKM      float64 `json:"to_km"`
	LengthKM  float64 `json:"length_km"`
}

// Archive is the residue dossier for one receiving operation.
type Archive struct {
	ID              string        `json:"id"`
	RunID           string        `json:"run_id"`
	RouteID         string        `json:"route_id"`
	ReceiverStation string        `json:"receiver_station"`
	Segments        []SegmentSpan `json:"segments"`
	CreatedAt       time.Time     `json:"created_at"`
}

// Specimen is one independent physical sample taken from the receiver.
type Specimen struct {
	ID          string             `json:"id"`
	ArchiveID   string             `json:"archive_id"`
	RunID       string             `json:"run_id"`
	Label       string             `json:"label"`
	Kilometer   float64            `json:"kilometer"`
	SegmentID   string             `json:"segment_id"`
	Components  map[string]float64 `json:"components"`
	CollectedAt time.Time          `json:"collected_at"`
	CreatedAt   time.Time          `json:"created_at"`
}

// Weighing is one mass measurement. Several weighings on the same
// specimen are repeat weighings of one sample; separate specimens are
// independent samples.
type Weighing struct {
	ID         string    `json:"id"`
	SpecimenID string    `json:"specimen_id"`
	Sequence   int       `json:"sequence"`
	MassGrams  float64   `json:"mass_grams"`
	ObservedAt time.Time `json:"observed_at"`
}

type Store struct {
	mu        sync.Mutex
	archives  map[string]Archive
	byRun     map[string]string
	specimens map[string]Specimen
	weighings map[string][]Weighing
	analyses  map[string][]Analysis
}

func NewStore() *Store {
	return &Store{
		archives:  make(map[string]Archive),
		byRun:     make(map[string]string),
		specimens: make(map[string]Specimen),
		weighings: make(map[string][]Weighing),
		analyses:  make(map[string][]Analysis),
	}
}

func (s *Store) EnsureArchive(archive Archive) Archive {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byRun[archive.RunID]; ok {
		return s.archives[id]
	}
	s.archives[archive.ID] = archive
	s.byRun[archive.RunID] = archive.ID
	return archive
}

func (s *Store) ArchiveForRun(runID string) (Archive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byRun[runID]
	if !ok {
		return Archive{}, false
	}
	archive, ok := s.archives[id]
	return archive, ok
}

func (s *Store) ArchiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.archives)
}

// LocateSegment maps a sampling kilometer post onto the archived spans.
// The final span includes its downstream boundary.
func LocateSegment(spans []SegmentSpan, kilometer float64) (SegmentSpan, bool) {
	for i, span := range spans {
		if kilometer < span.FromKM || kilometer > span.ToKM {
			continue
		}
		if kilometer == span.ToKM && i != len(spans)-1 {
			continue
		}
		return span, true
	}
	return SegmentSpan{}, false
}

func ValidateComponents(components map[string]float64) error {
	if len(components) == 0 {
		return errors.New("at least one component is required")
	}
	total := 0.0
	for name, fraction := range components {
		if strings.TrimSpace(name) == "" {
			return errors.New("component name is required")
		}
		if fraction < 0 || fraction > 100 {
			return fmt.Errorf("component %s fraction must be within 0 and 100", name)
		}
		total += fraction
	}
	if total > 100 {
		return fmt.Errorf("component fractions total %.2f exceeds 100", total)
	}
	return nil
}

func (s *Store) AddSpecimen(specimen Specimen) (Specimen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	archiveID, ok := s.byRun[specimen.RunID]
	if !ok {
		return Specimen{}, fmt.Errorf("no residue archive for run %s", specimen.RunID)
	}
	span, ok := LocateSegment(s.archives[archiveID].Segments, specimen.Kilometer)
	if !ok {
		return Specimen{}, fmt.Errorf("kilometer %.3f is outside the archived route segments", specimen.Kilometer)
	}
	specimen.ArchiveID = archiveID
	specimen.SegmentID = span.SegmentID
	s.specimens[specimen.ID] = specimen
	return specimen, nil
}

func (s *Store) Specimen(specimenID string) (Specimen, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	specimen, ok := s.specimens[specimenID]
	return specimen, ok
}

func (s *Store) Specimens(archiveID string) []Specimen {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Specimen, 0)
	for _, specimen := range s.specimens {
		if specimen.ArchiveID == archiveID {
			out = append(out, specimen)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kilometer == out[j].Kilometer {
			return out[i].Label < out[j].Label
		}
		return out[i].Kilometer < out[j].Kilometer
	})
	return out
}

func (s *Store) AddWeighing(specimenID string, mass float64, at time.Time) (Weighing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.specimens[specimenID]; !ok {
		return Weighing{}, fmt.Errorf("specimen %s not found", specimenID)
	}
	if mass <= 0 {
		return Weighing{}, errors.New("mass must be positive")
	}
	weighing := Weighing{
		ID:         model.NewRevision(),
		SpecimenID: specimenID,
		Sequence:   len(s.weighings[specimenID]) + 1,
		MassGrams:  mass,
		ObservedAt: at,
	}
	s.weighings[specimenID] = append(s.weighings[specimenID], weighing)
	return weighing, nil
}

func (s *Store) Weighings(specimenID string) []Weighing {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Weighing(nil), s.weighings[specimenID]...)
}

func (s *Store) RecordAnalysis(analysis Analysis) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.analyses[analysis.ArchiveID] = append(s.analyses[analysis.ArchiveID], analysis)
}

func (s *Store) Analyses(archiveID string) []Analysis {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Analysis(nil), s.analyses[archiveID]...)
}

// LatestAnalysisForRoute returns the most recent analysis produced by any
// other archive on the same route, which anchors the deposition trend.
func (s *Store) LatestAnalysisForRoute(routeID, excludeArchiveID string) (Analysis, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest Analysis
	found := false
	for _, archive := range s.archives {
		if archive.RouteID == "" || archive.RouteID != routeID || archive.ID == excludeArchiveID {
			continue
		}
		for _, analysis := range s.analyses[archive.ID] {
			if !found || analysis.CreatedAt.After(latest.CreatedAt) {
				latest = analysis
				found = true
			}
		}
	}
	return latest, found
}
