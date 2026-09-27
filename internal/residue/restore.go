package residue

import (
	"encoding/json"
	"fmt"

	"github.com/wyw14/solo-036/internal/model"
)

// Journal event kinds that keep residue records durable and traceable.
const (
	EventArchive  = "residue_archive"
	EventSpecimen = "residue_specimen"
	EventWeighing = "residue_weighing"
	EventAnalysis = "residue_analysis"
)

// Restore rebuilds the residue store from durable journal events, in the
// same order they were recorded.
func Restore(s *Store, events []model.Event) error {
	for _, event := range events {
		switch event.Kind {
		case EventArchive:
			var archive Archive
			if err := json.Unmarshal([]byte(event.Payload), &archive); err != nil {
				return fmt.Errorf("restore residue archive: %w", err)
			}
			s.EnsureArchive(archive)
		case EventSpecimen:
			var specimen Specimen
			if err := json.Unmarshal([]byte(event.Payload), &specimen); err != nil {
				return fmt.Errorf("restore residue specimen: %w", err)
			}
			s.restoreSpecimen(specimen)
		case EventWeighing:
			var weighing Weighing
			if err := json.Unmarshal([]byte(event.Payload), &weighing); err != nil {
				return fmt.Errorf("restore residue weighing: %w", err)
			}
			s.restoreWeighing(weighing)
		case EventAnalysis:
			var analysis Analysis
			if err := json.Unmarshal([]byte(event.Payload), &analysis); err != nil {
				return fmt.Errorf("restore residue analysis: %w", err)
			}
			s.RecordAnalysis(analysis)
		}
	}
	return nil
}

// restoreSpecimen re-inserts a specimen exactly as recorded, without
// re-validating against the archive spans.
func (s *Store) restoreSpecimen(specimen Specimen) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.specimens[specimen.ID] = specimen
}

// restoreWeighing appends a weighing with its recorded identity and
// sequence preserved.
func (s *Store) restoreWeighing(weighing Weighing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.weighings[weighing.SpecimenID] = append(s.weighings[weighing.SpecimenID], weighing)
}
