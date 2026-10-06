package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) createSubsample(w http.ResponseWriter, r *http.Request) {
	var request contracts.SubsampleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.CreateSubsample(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	location := "/removals/" + value.Removal.ID
	if value.Subsample != nil {
		location = "/subsamples/" + value.Subsample.ID
	}
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) recordRemoval(w http.ResponseWriter, r *http.Request) {
	var request contracts.MaterialRemovalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RecordMaterialRemoval(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/removals/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) getRemoval(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetRemoval(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) listRemovals(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListRemovals(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) getSubsample(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetSubsample(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) listSubsamples(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListSubsamplesForParent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) terminateMaterial(w http.ResponseWriter, r *http.Request) {
	var request contracts.TerminateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := api.service.TerminateMaterial(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
		w.Header().Set("Location", "/materials/"+result.Record.TargetID+"/termination")
	}
	writeJSON(w, status, result.Record)
}
