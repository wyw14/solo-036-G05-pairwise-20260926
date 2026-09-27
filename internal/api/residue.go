package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/service"
)

type residueSampleRequest struct {
	SampleKey   string                   `json:"sample_key"`
	StationID   string                   `json:"station_id"`
	ChainageKM  float64                  `json:"chainage_km"`
	Composition model.ResidueComposition `json:"composition"`
	CollectedAt string                   `json:"collected_at"`
	Note        string                   `json:"note"`
}

type residueWeighingRequest struct {
	SampleKey string  `json:"sample_key"`
	MassKg    float64 `json:"mass_kg"`
	ScaleID   string  `json:"scale_id"`
	WeighedAt string  `json:"weighed_at"`
}

func (h *Handler) RecordResidueSample(w http.ResponseWriter, r *http.Request) {
	var request residueSampleRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	collectedAt, err := parseObservedAt(request.CollectedAt)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	archive, err := h.runtime.RecordResidueSample(chi.URLParam(r, "runID"), service.ResidueSampleRequest{
		SampleKey:   request.SampleKey,
		StationID:   request.StationID,
		ChainageKM:  request.ChainageKM,
		Composition: request.Composition,
		CollectedAt: collectedAt,
		Note:        request.Note,
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, archive)
}

func (h *Handler) RecordResidueWeighing(w http.ResponseWriter, r *http.Request) {
	var request residueWeighingRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	weighedAt, err := parseObservedAt(request.WeighedAt)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	archive, err := h.runtime.RecordResidueWeighing(chi.URLParam(r, "runID"), service.ResidueWeighingRequest{
		SampleKey: request.SampleKey,
		MassKg:    request.MassKg,
		ScaleID:   request.ScaleID,
		WeighedAt: weighedAt,
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, archive)
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
