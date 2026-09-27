package residue

import (
	"fmt"
	"github.com/wyw14/solo-036/internal/model"
	"sort"
	"time"
)

func (s *Store) AddSample(sample model.ResidueSample) error {
	if err := sample.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	archive := s.archive(sample.RunID)
	for _, existing := range archive.samples {
		if existing.SampleKey == sample.SampleKey {
			return fmt.Errorf("sample %s is already registered for run %s", sample.SampleKey, sample.RunID)
		}
	}
	archive.samples = append(archive.samples, sample)
	return nil
}

func (s *Store) AddWeighing(weighing model.ResidueWeighing) error {
	if err := weighing.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	archive := s.archive(weighing.RunID)
	for _, sample := range archive.samples {
		if sample.SampleKey == weighing.SampleKey {
			archive.weighings = append(archive.weighings, weighing)
			return nil
		}
	}
	return fmt.Errorf("sample %s is not registered for run %s", weighing.SampleKey, weighing.RunID)
}

func (s *Store) HasSample(runID, sampleKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	archive, ok := s.archives[runID]
	if !ok {
		return false
	}
	for _, sample := range archive.samples {
		if sample.SampleKey == sampleKey {
			return true
		}
	}
	return false
}

// EffectiveMass resolves a sample's mass from its weighings. Repeat
// weighings of the same physical sample are collapsed by median so a
// single outlier scale reading does not shift the archive.
func EffectiveMass(archive model.ResidueArchive, sampleKey string) (float64, bool) {
	masses := make([]float64, 0)
	for _, weighing := range archive.Weighings {
		if weighing.SampleKey == sampleKey {
			masses = append(masses, weighing.MassKg)
		}
	}
	if len(masses) == 0 {
		return 0, false
	}
	sort.Float64s(masses)
	middle := len(masses) / 2
	if len(masses)%2 == 1 {
		return masses[middle], true
	}
	return (masses[middle-1] + masses[middle]) / 2, true
}

// RepeatWeighingKeys reports which physical samples were weighed more
// than once; everything else in the archive is an independent sample
// with at most a single weighing.
func RepeatWeighingKeys(archive model.ResidueArchive) map[string]int {
	counts := make(map[string]int)
	for _, weighing := range archive.Weighings {
		counts[weighing.SampleKey]++
	}
	repeated := make(map[string]int)
	for key, count := range counts {
		if count > 1 {
			repeated[key] = count
		}
	}
	return repeated
}

func (s *Store) AppendAnalysis(runID string, analysis model.ResidueAnalysis) model.ResidueAnalysis {
	s.mu.Lock()
	defer s.mu.Unlock()
	archive := s.archive(runID)
	archive.generation++
	analysis.Generation = archive.generation
	archive.analyses = append(archive.analyses, analysis)
	return analysis
}

func (s *Store) Snapshot(runID string) (model.ResidueArchive, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	archive, ok := s.archives[runID]
	if !ok {
		return model.ResidueArchive{}, false
	}
	return model.ResidueArchive{
		RunID:     runID,
		Samples:   append([]model.ResidueSample(nil), archive.samples...),
		Weighings: append([]model.ResidueWeighing(nil), archive.weighings...),
		Analyses:  append([]model.ResidueAnalysis(nil), archive.analyses...),
		UpdatedAt: latestActivity(archive),
	}, true
}

func (s *Store) SnapshotAll() []model.ResidueArchive {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.ResidueArchive, 0, len(s.archives))
	for runID, archive := range s.archives {
		out = append(out, model.ResidueArchive{
			RunID:     runID,
			Samples:   append([]model.ResidueSample(nil), archive.samples...),
			Weighings: append([]model.ResidueWeighing(nil), archive.weighings...),
			Analyses:  append([]model.ResidueAnalysis(nil), archive.analyses...),
			UpdatedAt: latestActivity(archive),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}

// Restore replaces all archives, e.g. when recovering a persisted
// snapshot. Generations resume after the highest stored analysis.
func (s *Store) Restore(archives []model.ResidueArchive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archives = make(map[string]*runArchive, len(archives))
	for _, item := range archives {
		archive := &runArchive{
			samples:   append([]model.ResidueSample(nil), item.Samples...),
			weighings: append([]model.ResidueWeighing(nil), item.Weighings...),
			analyses:  append([]model.ResidueAnalysis(nil), item.Analyses...),
		}
		for _, analysis := range item.Analyses {
			if analysis.Generation > archive.generation {
				archive.generation = analysis.Generation
			}
		}
		s.archives[item.RunID] = archive
	}
}

func latestActivity(archive *runArchive) time.Time {
	latest := time.Time{}
	consider := func(at time.Time) {
		if at.After(latest) {
			latest = at
		}
	}
	for _, sample := range archive.samples {
		consider(sample.CollectedAt)
	}
	for _, weighing := range archive.weighings {
		consider(weighing.WeighedAt)
	}
	for _, analysis := range archive.analyses {
		consider(analysis.CreatedAt)
	}
	return latest
}
