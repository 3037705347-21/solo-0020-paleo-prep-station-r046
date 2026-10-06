package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) recordRemoval(w http.ResponseWriter, r *http.Request) {
	var request contracts.RecordRemovalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RecordRemoval(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/subsamples/"+value.Subsample.ID)
	writeJSON(w, http.StatusCreated, value)
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
	values, err := api.service.ListSubsamples(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) listRemovals(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListRemovals(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}
