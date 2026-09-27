package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/residue"
	"github.com/wyw14/solo-036/internal/route"
)

type ResidueSpecimenRequest struct {
	Label       string             `json:"label"`
	Kilometer   float64            `json:"kilometer"`
	Components  map[string]float64 `json:"components"`
	MassGrams   *float64           `json:"mass_grams,omitempty"`
	CollectedAt time.Time          `json:"collected_at"`
}

type ResidueWeighingRequest struct {
	MassGrams  float64   `json:"mass_grams"`
	ObservedAt time.Time `json:"observed_at"`
}

type ResidueSpecimenView struct {
	Specimen            residue.Specimen   `json:"specimen"`
	Weighings           []residue.Weighing `json:"weighings"`
	RepresentativeMassG float64            `json:"representative_mass_g"`
}

type ResidueArchiveView struct {
	Archive   residue.Archive       `json:"archive"`
	Specimens []ResidueSpecimenView `json:"specimens"`
	Analyses  []residue.Analysis    `json:"analyses"`
}

// openResidueArchive creates the residue dossier when a pig is received,
// snapshotting the route segments so later residue records stay traceable
// to the line configuration of this run. It is best-effort: a failure
// here must not roll back a completed receiving operation.
func (r *Runtime) openResidueArchive(runID, stationID string) {
	control := r.ensureControl()
	r.mu.Lock()
	routeState, found := model.RouteState{}, false
	for _, candidate := range control.Routes {
		if candidate.RunID == runID {
			routeState = candidate
			found = true
			break
		}
	}
	r.mu.Unlock()
	archive := residue.Archive{
		ID:              model.NewRevision(),
		RunID:           runID,
		ReceiverStation: stationID,
		Segments:        make([]residue.SegmentSpan, 0),
		CreatedAt:       time.Now().UTC(),
	}
	if !found {
		r.recordIncident(runID, "warning", "residue archive: no route lease found for run")
	} else {
		archive.RouteID = routeState.ID
		from := control.Stations[routeState.FromStation]
		to := control.Stations[routeState.ToStation]
		limit := from.PressureLimit
		if to.PressureLimit < limit {
			limit = to.PressureLimit
		}
		path, err := control.Network.Path(from.ID, to.ID, limit)
		if err != nil {
			r.recordIncident(runID, "warning", "residue archive: "+err.Error())
		} else {
			segments := make(map[string]route.Segment)
			for _, segment := range control.Network.Segments() {
				segments[segment.ID] = segment
			}
			for _, segmentID := range path.Segments {
				segment := segments[segmentID]
				archive.Segments = append(archive.Segments, residue.SegmentSpan{
					SegmentID: segment.ID,
					FromKM:    control.Stations[segment.From].Kilometer,
					ToKM:      control.Stations[segment.To].Kilometer,
					LengthKM:  segment.Length,
				})
			}
		}
	}
	archive = r.Residue.EnsureArchive(archive)
	payload, err := json.Marshal(archive)
	if err != nil {
		r.recordIncident(runID, "warning", "residue archive: "+err.Error())
		return
	}
	if err := r.appendEvent(model.Event{
		ID:      archive.ID,
		RunID:   runID,
		Kind:    residue.EventArchive,
		Payload: string(payload),
	}); err != nil {
		r.recordIncident(runID, "warning", "residue archive journal: "+err.Error())
	}
}

func (r *Runtime) residueArchiveForRun(runID string) (residue.Archive, error) {
	if _, ok := r.Runs.Get(runID); !ok {
		return residue.Archive{}, fmt.Errorf("run %s not found", runID)
	}
	archive, ok := r.Residue.ArchiveForRun(runID)
	if !ok {
		return residue.Archive{}, errors.New("run has no residue archive; pig is not received yet")
	}
	return archive, nil
}

func (r *Runtime) RecordResidueSpecimen(runID string, request ResidueSpecimenRequest) (residue.Specimen, error) {
	current, ok := r.Runs.Get(runID)
	if !ok {
		return residue.Specimen{}, fmt.Errorf("run %s not found", runID)
	}
	switch current.State {
	case model.StateReceiving, model.StateDepressurizing, model.StateComplete:
	default:
		return residue.Specimen{}, fmt.Errorf("run state %s cannot record residue", current.State)
	}
	label := strings.TrimSpace(request.Label)
	if label == "" {
		return residue.Specimen{}, errors.New("specimen label is required")
	}
	if err := residue.ValidateComponents(request.Components); err != nil {
		return residue.Specimen{}, err
	}
	if request.MassGrams != nil && *request.MassGrams <= 0 {
		return residue.Specimen{}, errors.New("mass must be positive")
	}
	if request.CollectedAt.IsZero() {
		request.CollectedAt = time.Now().UTC()
	}
	specimen, err := r.Residue.AddSpecimen(residue.Specimen{
		ID:          model.NewRevision(),
		RunID:       runID,
		Label:       label,
		Kilometer:   request.Kilometer,
		Components:  request.Components,
		CollectedAt: request.CollectedAt,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return residue.Specimen{}, err
	}
	payload, err := json.Marshal(specimen)
	if err != nil {
		return residue.Specimen{}, err
	}
	if err := r.appendEvent(model.Event{
		ID:      specimen.ID,
		RunID:   runID,
		Kind:    residue.EventSpecimen,
		Payload: string(payload),
	}); err != nil {
		return residue.Specimen{}, fmt.Errorf("journal: %w", err)
	}
	if request.MassGrams != nil {
		if _, err := r.RecordResidueWeighing(runID, specimen.ID, ResidueWeighingRequest{
			MassGrams:  *request.MassGrams,
			ObservedAt: request.CollectedAt,
		}); err != nil {
			return residue.Specimen{}, err
		}
	}
	return specimen, nil
}

func (r *Runtime) RecordResidueWeighing(runID, specimenID string, request ResidueWeighingRequest) (residue.Weighing, error) {
	if _, err := r.residueArchiveForRun(runID); err != nil {
		return residue.Weighing{}, err
	}
	specimen, ok := r.Residue.Specimen(specimenID)
	if !ok || specimen.RunID != runID {
		return residue.Weighing{}, fmt.Errorf("specimen %s not found for run %s", specimenID, runID)
	}
	if request.MassGrams <= 0 {
		return residue.Weighing{}, errors.New("mass must be positive")
	}
	if request.ObservedAt.IsZero() {
		request.ObservedAt = time.Now().UTC()
	}
	weighing, err := r.Residue.AddWeighing(specimenID, request.MassGrams, request.ObservedAt)
	if err != nil {
		return residue.Weighing{}, err
	}
	payload, err := json.Marshal(weighing)
	if err != nil {
		return residue.Weighing{}, err
	}
	if err := r.appendEvent(model.Event{
		ID:      weighing.ID,
		RunID:   runID,
		Kind:    residue.EventWeighing,
		Payload: string(payload),
	}); err != nil {
		return residue.Weighing{}, fmt.Errorf("journal: %w", err)
	}
	return weighing, nil
}

func (r *Runtime) AnalyzeResidue(runID string) (residue.Analysis, error) {
	archive, err := r.residueArchiveForRun(runID)
	if err != nil {
		return residue.Analysis{}, err
	}
	specimens := r.Residue.Specimens(archive.ID)
	if len(specimens) == 0 {
		return residue.Analysis{}, errors.New("no residue specimens recorded")
	}
	weighings := make(map[string][]residue.Weighing, len(specimens))
	weighed := 0
	for _, specimen := range specimens {
		weighings[specimen.ID] = r.Residue.Weighings(specimen.ID)
		if len(weighings[specimen.ID]) > 0 {
			weighed++
		}
	}
	if weighed == 0 {
		return residue.Analysis{}, errors.New("no residue weighings recorded")
	}
	var previous *residue.Analysis
	if latest, ok := r.Residue.LatestAnalysisForRoute(archive.RouteID, archive.ID); ok {
		previous = &latest
	}
	generation := len(r.Residue.Analyses(archive.ID)) + 1
	analysis := residue.Compute(archive, specimens, weighings, previous, model.NewRevision(), generation, time.Now().UTC())
	r.Residue.RecordAnalysis(analysis)
	payload, err := json.Marshal(analysis)
	if err != nil {
		return residue.Analysis{}, err
	}
	if err := r.appendEvent(model.Event{
		ID:      analysis.ID,
		RunID:   runID,
		Kind:    residue.EventAnalysis,
		Epoch:   int64(generation),
		Payload: string(payload),
	}); err != nil {
		return residue.Analysis{}, fmt.Errorf("journal: %w", err)
	}
	return analysis, nil
}

func (r *Runtime) ResidueArchive(runID string) (ResidueArchiveView, error) {
	archive, err := r.residueArchiveForRun(runID)
	if err != nil {
		return ResidueArchiveView{}, err
	}
	view := ResidueArchiveView{
		Archive:   archive,
		Specimens: make([]ResidueSpecimenView, 0),
		Analyses:  r.Residue.Analyses(archive.ID),
	}
	for _, specimen := range r.Residue.Specimens(archive.ID) {
		weighings := r.Residue.Weighings(specimen.ID)
		mass, _ := residue.RepresentativeMass(weighings)
		view.Specimens = append(view.Specimens, ResidueSpecimenView{
			Specimen:            specimen,
			Weighings:           weighings,
			RepresentativeMassG: mass,
		})
	}
	return view, nil
}
