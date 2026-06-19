package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/tagcalcs"
)

// TagCalcHandlers holds dependencies for tag calc REST endpoints.
type TagCalcHandlers struct {
	DB     sqldb.DB
	Engine *tagcalcs.Engine
	GetOrg func(r *http.Request) string
}

// NewTagCalcHandlers creates a new TagCalcHandlers.
func NewTagCalcHandlers(db sqldb.DB, engine *tagcalcs.Engine, getOrg func(r *http.Request) string) *TagCalcHandlers {
	return &TagCalcHandlers{DB: db, Engine: engine, GetOrg: getOrg}
}

type testTagCalcRequest struct {
	Expression string `json:"expression"`
}

type testTagCalcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (h *TagCalcHandlers) HandleListWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleList, nil, []sqldb.TagCalc{}, "tagcalcs")
}

func (h *TagCalcHandlers) HandleList(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	scripts, err := h.DB.ListTagCalcs(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if scripts == nil {
		scripts = []sqldb.TagCalc{}
	}
	json.NewEncoder(w).Encode(scripts)
}

func (h *TagCalcHandlers) HandleGetWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGet, nil, sqldb.TagCalc{}, "tagcalcs")
}

func (h *TagCalcHandlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s, err := h.DB.GetTagCalc(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(s)
}

func (h *TagCalcHandlers) HandleCreateWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreate,
		RequestBody: openapischema.JSONRequestBody(sqldb.TagCalc{}),
		Responses:   openapischema.ResponseSchema(http.StatusCreated, sqldb.TagCalc{}),
		Tags:        []string{"tagcalcs"},
	}
}

func (h *TagCalcHandlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	var s sqldb.TagCalc
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if s.Name == "" || s.OutputTag == "" || s.Expression == "" {
		http.Error(w, "name, outputTag and expression are required", http.StatusBadRequest)
		return
	}
	if s.IntervalSeconds <= 0 {
		s.IntervalSeconds = 60
	}
	// Validate expression before saving.
	if _, err := h.Engine.EvaluateAny(org, s.Expression); err != nil {
		http.Error(w, "expression error: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.DB.CreateTagCalc(r.Context(), org, &s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Engine.Reload(r.Context(), org, s.ID)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

func (h *TagCalcHandlers) HandleUpdateWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUpdate, sqldb.TagCalc{}, sqldb.TagCalc{}, "tagcalcs")
}

func (h *TagCalcHandlers) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var s sqldb.TagCalc
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if s.IntervalSeconds <= 0 {
		s.IntervalSeconds = 60
	}
	// Validate expression before saving.
	if _, err := h.Engine.EvaluateAny(org, s.Expression); err != nil {
		http.Error(w, "expression error: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateTagCalc(r.Context(), org, id, &s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Engine.Reload(r.Context(), org, id)

	updated, _ := h.DB.GetTagCalc(r.Context(), org, id)
	if updated != nil {
		json.NewEncoder(w).Encode(updated)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *TagCalcHandlers) HandleDeleteWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleDelete, map[int]any{http.StatusNoContent: nil}, "tagcalcs")
}

func (h *TagCalcHandlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	h.Engine.Unschedule(id)
	if err := h.DB.DeleteTagCalc(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TagCalcHandlers) HandleTestWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleTest, testTagCalcRequest{}, testTagCalcResponse{}, "tagcalcs")
}

// HandleTest evaluates an expression against live data without writing to the tree.
func (h *TagCalcHandlers) HandleTest(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	var req testTagCalcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := h.Engine.EvaluateAny(org, req.Expression)
	if err != nil {
		json.NewEncoder(w).Encode(testTagCalcResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(testTagCalcResponse{Result: result})
}
