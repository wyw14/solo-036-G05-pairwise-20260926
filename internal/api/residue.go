package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/service"
)

type residueSpecimenRequest struct {
	Label       string             `json:"label"`
	Kilometer   float64            `json:"kilometer"`
	Components  map[string]float64 `json:"components"`
	MassGrams   *float64           `json:"mass_grams"`
	CollectedAt string             `json:"collected_at"`
}

type residueWeighingRequest struct {
	MassGrams  float64 `json:"mass_grams"`
	ObservedAt string  `json:"observed_at"`
}

func (h *Handler) RecordResidueSpecimen(w http.ResponseWriter, r *http.Request) {
	var request residueSpecimenRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	collectedAt, err := parseObservedAt(request.CollectedAt)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	specimen, err := h.runtime.RecordResidueSpecimen(chi.URLParam(r, "runID"), service.ResidueSpecimenRequest{
		Label:       request.Label,
		Kilometer:   request.Kilometer,
		Components:  request.Components,
		MassGrams:   request.MassGrams,
		CollectedAt: collectedAt,
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, specimen)
}

func (h *Handler) RecordResidueWeighing(w http.ResponseWriter, r *http.Request) {
	var request residueWeighingRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	observedAt, err := parseObservedAt(request.ObservedAt)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	weighing, err := h.runtime.RecordResidueWeighing(chi.URLParam(r, "runID"), chi.URLParam(r, "specimenID"), service.ResidueWeighingRequest{
		MassGrams:  request.MassGrams,
		ObservedAt: observedAt,
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, weighing)
}

func (h *Handler) AnalyzeResidue(w http.ResponseWriter, r *http.Request) {
	analysis, err := h.runtime.AnalyzeResidue(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, analysis)
}

func (h *Handler) ResidueArchive(w http.ResponseWriter, r *http.Request) {
	archive, err := h.runtime.ResidueArchive(chi.URLParam(r, "runID"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, archive)
}
