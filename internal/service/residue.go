package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/residue"
)

type ResidueSampleRequest struct {
	SampleKey   string                 `json:"sample_key"`
	StationID   string                 `json:"station_id"`
	ChainageKM  float64                `json:"chainage_km"`
	Composition model.ResidueComposition `json:"composition"`
	CollectedAt time.Time              `json:"collected_at"`
	Note        string                 `json:"note"`
}

type ResidueWeighingRequest struct {
	SampleKey string    `json:"sample_key"`
	MassKg    float64   `json:"mass_kg"`
	ScaleID   string    `json:"scale_id"`
	WeighedAt time.Time `json:"weighed_at"`
}

// ResidueSampleView enriches a raw sample with its resolved mass and
// repeat-weighing count so independent samples and repeat weighings are
// distinguishable without walking the weighing log.
type ResidueSampleView struct {
	model.ResidueSample
	EffectiveMassKg  float64 `json:"effective_mass_kg,omitempty"`
	WeighingCount    int     `json:"weighing_count"`
	RepeatWeighed    bool    `json:"repeat_weighed"`
}

type ResidueArchiveView struct {
	model.ResidueArchive
	SampleViews      []ResidueSampleView `json:"sample_views"`
	IndependentCount int                 `json:"independent_sample_count"`
}

// residueRecordingOpen reports whether the run has entered the receiver
// far enough that residue recovery can be documented.
func residueRecordingOpen(state model.RunState) bool {
	return state == model.StateReceiving || state == model.StateDepressurizing || state == model.StateComplete
}

func (r *Runtime) residueRun(runID string) (model.Run, error) {
	current, ok := r.Runs.Get(runID)
	if !ok {
		return model.Run{}, fmt.Errorf("run %s not found", runID)
	}
	if !residueRecordingOpen(current.State) {
		return model.Run{}, fmt.Errorf("run state %s cannot record residue", current.State)
	}
	return current, nil
}

func (r *Runtime) RecordResidueSample(runID string, request ResidueSampleRequest) (model.ResidueArchive, error) {
	if _, err := r.residueRun(runID); err != nil {
		return model.ResidueArchive{}, err
	}
	control := r.ensureControl()
	station, ok := control.Stations[request.StationID]
	if !ok {
		return model.ResidueArchive{}, fmt.Errorf("unknown sample station %s", request.StationID)
	}
	if request.ChainageKM > station.Kilometer+1000 {
		return model.ResidueArchive{}, fmt.Errorf("chainage %.3f km is outside the station window", request.ChainageKM)
	}
	if request.CollectedAt.IsZero() {
		request.CollectedAt = time.Now().UTC()
	}
	sample := model.ResidueSample{
		ID:          model.NewRevision(),
		RunID:       runID,
		SampleKey:   request.SampleKey,
		StationID:   request.StationID,
		ChainageKM:  request.ChainageKM,
		Composition: request.Composition,
		CollectedAt: request.CollectedAt,
		Note:        request.Note,
	}
	if err := r.Residue.AddSample(sample); err != nil {
		return model.ResidueArchive{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   runID,
		Kind:    "residue_sample",
		Payload: fmt.Sprintf("%s/%s/%.3f", sample.SampleKey, sample.StationID, sample.ChainageKM),
	}); err != nil {
		return model.ResidueArchive{}, err
	}
	archive, _ := r.Residue.Snapshot(runID)
	return archive, nil
}

func (r *Runtime) RecordResidueWeighing(runID string, request ResidueWeighingRequest) (model.ResidueArchive, error) {
	if _, err := r.residueRun(runID); err != nil {
		return model.ResidueArchive{}, err
	}
	if request.WeighedAt.IsZero() {
		request.WeighedAt = time.Now().UTC()
	}
	weighing := model.ResidueWeighing{
		ID:        model.NewRevision(),
		RunID:     runID,
		SampleKey: request.SampleKey,
		MassKg:    request.MassKg,
		ScaleID:   request.ScaleID,
		WeighedAt: request.WeighedAt,
	}
	if err := r.Residue.AddWeighing(weighing); err != nil {
		return model.ResidueArchive{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   runID,
		Kind:    "residue_weighing",
		Payload: fmt.Sprintf("%s/%.3f/%s", weighing.SampleKey, weighing.MassKg, weighing.ScaleID),
	}); err != nil {
		return model.ResidueArchive{}, err
	}
	archive, _ := r.Residue.Snapshot(runID)
	return archive, nil
}

func (r *Runtime) AnalyzeResidue(runID string) (model.ResidueAnalysis, error) {
	if _, err := r.residueRun(runID); err != nil {
		return model.ResidueAnalysis{}, err
	}
	archive, ok := r.Residue.Snapshot(runID)
	if !ok {
		return model.ResidueAnalysis{}, errors.New("run has no residue archive")
	}
	control := r.ensureControl()
	analysis, err := residue.Analyze(archive, control.Network.Segments(), control.Stations)
	if err != nil {
		return model.ResidueAnalysis{}, err
	}
	analysis.ID = model.NewRevision()
	analysis.CreatedAt = time.Now().UTC()
	analysis = r.Residue.AppendAnalysis(runID, analysis)
	if err := r.appendEvent(model.Event{
		RunID:   runID,
		Kind:    "residue_analysis",
		Epoch:   analysis.Generation,
		Payload: analysis.ID,
	}); err != nil {
		return model.ResidueAnalysis{}, err
	}
	r.mu.Lock()
	r.control.Revision = model.NewRevision()
	r.mu.Unlock()
	return analysis, nil
}

func (r *Runtime) ResidueArchive(runID string) (ResidueArchiveView, error) {
	if _, ok := r.Runs.Get(runID); !ok {
		return ResidueArchiveView{}, fmt.Errorf("run %s not found", runID)
	}
	archive, ok := r.Residue.Snapshot(runID)
	if !ok {
		return ResidueArchiveView{}, errors.New("run has no residue archive")
	}
	repeats := residue.RepeatWeighingKeys(archive)
	views := make([]ResidueSampleView, 0, len(archive.Samples))
	for _, sample := range archive.Samples {
		view := ResidueSampleView{ResidueSample: sample}
		if mass, ok := residue.EffectiveMass(archive, sample.SampleKey); ok {
			view.EffectiveMassKg = mass
		}
		for _, weighing := range archive.Weighings {
			if weighing.SampleKey == sample.SampleKey {
				view.WeighingCount++
			}
		}
		_, view.RepeatWeighed = repeats[sample.SampleKey]
		views = append(views, view)
	}
	return ResidueArchiveView{
		ResidueArchive:   archive,
		SampleViews:      views,
		IndependentCount: len(archive.Samples),
	}, nil
}
