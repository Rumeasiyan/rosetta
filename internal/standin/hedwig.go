package standin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// HedwigLiveKey and HedwigTestKey are the keys HEDWIG's stand-in takes.
const (
	HedwigLiveKey = "hw_live_standin"
	HedwigTestKey = "hw_test_standin"
)

// Modes after normal are the refusals in API.md a caller must handle.
var hedwigModes = []string{"normal", "unauthorized", "unknown_notice_type", "missing_detail", "rate_limited", "internal", "slow", "silent"}

const hedwigMaxPeople = 500

type hedwigState struct {
	nextRequest int64
	nextNotice  int64
	byKey       map[string]json.RawMessage
	notices     map[int64]json.RawMessage
}

func (s *Server) mountHedwig(mux *http.ServeMux) {
	mux.HandleFunc("POST /hedwig/v1/notices", s.serve("hedwig", s.hedwigSend))
	mux.HandleFunc("GET /hedwig/v1/notices/{id}", s.serve("hedwig", s.hedwigRead))
}

func hedwigError(w http.ResponseWriter, status int, code, message string, person int) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "person": person}})
}

// hedwigKey reports the key's kind: "live", "test", or "" after refusing it.
func (s *Server) hedwigKey(w http.ResponseWriter, r *http.Request, mode string) string {
	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if mode == "unauthorized" || (key != HedwigLiveKey && key != HedwigTestKey) {
		hedwigError(w, http.StatusUnauthorized, "unauthorized", "the key is not accepted", -1)
		return ""
	}
	if key == HedwigTestKey {
		return "test"
	}
	return "live"
}

func (s *Server) hedwigSend(w http.ResponseWriter, r *http.Request) {
	mode, _ := s.mode("hedwig")
	kind := s.hedwigKey(w, r, mode)
	if kind == "" {
		return
	}
	switch mode {
	case "unknown_notice_type":
		hedwigError(w, http.StatusNotFound, "unknown_notice_type", "no template for this", -1)
		return
	case "missing_detail":
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
			"code": "missing_detail", "message": "missing details: date", "person": 0, "missing": []string{"date"}}})
		return
	case "rate_limited":
		w.Header().Set("Retry-After", "60")
		hedwigError(w, http.StatusTooManyRequests, "rate_limited", "over this key's limit", -1)
		return
	case "internal":
		hedwigError(w, http.StatusInternalServerError, "internal", "nothing was queued", -1)
		return
	}
	var in struct {
		NoticeType string `json:"notice_type"`
		Priority   string `json:"priority"`
		RequestKey string `json:"request_key"`
		People     []struct {
			Ref      string            `json:"ref"`
			Email    string            `json:"email"`
			Phone    string            `json:"phone"`
			Language string            `json:"language"`
			Channels []string          `json:"channels"`
			Details  map[string]string `json:"details"`
		} `json:"people"`
		Details map[string]string `json:"details"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // API.md: a misspelt field is refused
	if err := dec.Decode(&in); err != nil {
		hedwigError(w, http.StatusBadRequest, "bad_request", "the body is not valid", -1)
		return
	}
	if in.NoticeType == "" || len(in.People) == 0 || len(in.People) > hedwigMaxPeople || len(in.RequestKey) > 200 ||
		(in.Priority != "" && in.Priority != "urgent" && in.Priority != "normal") {
		hedwigError(w, http.StatusBadRequest, "bad_request", "a field is not valid", -1)
		return
	}
	for i, p := range in.People {
		if p.Ref == "" || len(p.Ref) > 200 {
			hedwigError(w, http.StatusBadRequest, "bad_request", "ref is not valid", i)
			return
		}
		if p.Email == "" {
			hedwigError(w, http.StatusUnprocessableEntity, "missing_address", "no address for this person", i)
			return
		}
		if !strings.Contains(p.Email, "@") {
			hedwigError(w, http.StatusUnprocessableEntity, "bad_address", "the email is not valid", i)
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.hedwig
	dedupe := kind + ":" + in.RequestKey
	if first, again := h.byKey[dedupe]; in.RequestKey != "" && again {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write(first)
		return
	}
	status := "queued"
	if kind == "test" {
		status = "test"
	}
	h.nextRequest++
	notices := []map[string]any{}
	for _, p := range in.People {
		h.nextNotice++
		notices = append(notices, map[string]any{"id": h.nextNotice, "ref": p.Ref, "status": status})
		h.notices[h.nextNotice], _ = json.Marshal(map[string]any{"id": h.nextNotice, "request_id": h.nextRequest, "ref": p.Ref,
			"notice_type": in.NoticeType, "status": status, "deliveries": []any{}})
	}
	answer, _ := json.Marshal(map[string]any{"request_id": h.nextRequest, "notices": notices})
	if in.RequestKey != "" {
		h.byKey[dedupe] = answer
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write(answer)
}

func (s *Server) hedwigRead(w http.ResponseWriter, r *http.Request) {
	mode, _ := s.mode("hedwig")
	if s.hedwigKey(w, r, mode) == "" {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	raw, ok := s.hedwig.notices[id]
	s.mu.Unlock()
	if err != nil || !ok {
		hedwigError(w, http.StatusNotFound, "not_found", "no such notice", -1)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
