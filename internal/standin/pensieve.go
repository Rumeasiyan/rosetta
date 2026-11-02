package standin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// PensieveKey is the key PENSIEVE's stand-in takes; PensieveOffKey is one it knows
// as turned off.
const (
	PensieveKey    = "pv_standin"
	PensieveOffKey = "pv_turned_off"
)

// Modes after normal are the answers in INTAKE.md that say nothing was kept.
var pensieveModes = []string{"normal", "not_kept", "unavailable", "too_many", "slow", "silent"}

const (
	pensieveBatchLimit = 500
	pensieveLateDays   = 30
	pensieveMaxDetails = 8 << 10
)

type pensieveKept struct {
	Seq      int64
	Received time.Time
}

type pensieveState struct {
	kept map[string]pensieveKept
	seq  int64
}

// entryLimits is each item the entry may hold and its longest length; required
// items are in entryRequired.
var entryLimits = map[string]int{
	"id": 200, "actor": 200, "actor_name": 200, "post": 200, "on_behalf_of": 200, "on_behalf_of_name": 200,
	"action": 100, "action_name": 100, "object_type": 100, "object_ref": 200, "result": 100, "reason": 1000,
	"visitor_address": 64,
}

// Order matters: a refusal names the first item at fault.
var entryRequired = []string{"id", "occurred_at", "actor", "post", "action", "object_type", "object_ref", "result"}

var entryActions = map[string]bool{"sign-in": true, "sign-out": true, "denial": true, "approval": true, "rejection": true,
	"export": true, "creation": true, "change": true, "deletion": true, "role change": true, "key change": true,
	"setting change": true, "break-glass": true, "other": true}

var entryResults = map[string]bool{"allowed": true, "denied": true, "done": true, "failed": true}

var (
	secretName = regexp.MustCompile(`password|passwd|secret|apikey|token|authorization|otp|credential|privatekey`)
	idName     = regexp.MustCompile(`^(nic|nicno|nicnumber|nationalid|nationalidnumber|idnumber|identitycard|identitycardnumber)$`)
	secretText = regexp.MustCompile(`(?i)\bpv_[a-z0-9_-]+|\bbearer\s+\S+|\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.|PRIVATE KEY|password=`)
	idText     = regexp.MustCompile(`(?i)(^|[^0-9a-z])([0-9]{9}[vx]|[0-9]{12})($|[^0-9a-z])`)
)

func (s *Server) mountPensieve(mux *http.ServeMux) {
	mux.HandleFunc("POST /pensieve/api/entries", s.serve("pensieve", s.pensieveEntries))
}

func (s *Server) pensieveEntries(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case !ok || key == "":
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no_key"})
		return
	case key == PensieveOffKey:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "key_turned_off"})
		return
	case key != PensieveKey:
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown_key"})
		return
	}
	mode, p := s.mode("pensieve")
	switch mode {
	case "unavailable":
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unavailable"})
		return
	case "too_many":
		wait := intParam(p, "retry_seconds", 60)
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too_many", "retry_after": wait})
		return
	}
	var in struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Entries == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable", "detail": "the body is not {\"entries\": [...]}"})
		return
	}
	limit := intParam(p, "limit", pensieveBatchLimit)
	if len(in.Entries) > limit {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "too_many_entries", "limit": limit})
		return
	}
	now := time.Now().UTC()
	results := []map[string]any{}
	kept, refused := 0, 0
	for i, e := range in.Entries {
		id, _ := e["id"].(string)
		res := map[string]any{"index": i, "id": id}
		reason, item := checkEntry(e, now)
		if reason == "" && mode == "not_kept" {
			reason = "not_kept"
		}
		if reason != "" {
			res["status"], res["reason"] = "refused", reason
			if item != "" {
				res["item"] = item
			}
			refused++
		} else {
			s.mu.Lock()
			k, repeat := s.pensieve.kept[id]
			if !repeat {
				s.pensieve.seq++
				k = pensieveKept{Seq: s.pensieve.seq, Received: now}
				s.pensieve.kept[id] = k
			}
			s.mu.Unlock()
			res["status"], res["seq"], res["received_at"] = "kept", k.Seq, k.Received.Format(time.RFC3339Nano)
			if repeat {
				res["repeat"] = true
			}
			if t, err := time.Parse(time.RFC3339, e["occurred_at"].(string)); err == nil && now.Sub(t) > 24*time.Hour {
				res["late"] = true
			}
			kept++
		}
		results = append(results, res)
	}
	status := http.StatusOK
	switch {
	case refused > 0 && kept > 0:
		status = http.StatusMultiStatus
	case refused > 0:
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]any{"kept": kept, "refused": refused, "results": results})
}

// checkEntry is INTAKE.md's refusals, as PENSIEVE gives them.
func checkEntry(e map[string]any, now time.Time) (reason, item string) {
	for name, v := range e {
		if name == "occurred_at" || name == "details" || name == "corrects" {
			continue
		}
		longest, known := entryLimits[name]
		if !known {
			return "unknown", name
		}
		text, isText := v.(string)
		if !isText {
			return "invalid", name
		}
		if utf8.RuneCountInString(text) > longest {
			return "too_long", name
		}
	}
	for _, name := range entryRequired {
		if text, _ := e[name].(string); strings.TrimSpace(text) == "" {
			return "missing", name
		}
	}
	if !entryActions[e["action"].(string)] {
		return "not_on_list", "action"
	}
	if !entryResults[e["result"].(string)] {
		return "not_on_list", "result"
	}
	if _, named := e["action_name"]; named != (e["action"] == "other") {
		if named {
			return "invalid", "action_name"
		}
		return "missing", "action_name"
	}
	at, err := time.Parse(time.RFC3339, e["occurred_at"].(string))
	if err != nil {
		return "invalid", "occurred_at"
	}
	if now.Sub(at) > pensieveLateDays*24*time.Hour {
		return "too_old", "occurred_at"
	}
	if at.Sub(now) > 5*time.Minute {
		return "in_future", "occurred_at"
	}
	if d, ok := e["details"]; ok {
		obj, isObject := d.(map[string]any)
		if !isObject {
			return "invalid", "details"
		}
		raw, _ := json.Marshal(obj)
		if len(raw) > pensieveMaxDetails {
			return "too_long", "details"
		}
		if r := scanDetails(obj); r != "" {
			return r, "details"
		}
	}
	return "", ""
}

// scanDetails looks at every name and value at every depth.
func scanDetails(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for name, inner := range x {
			flat := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(name))
			if secretName.MatchString(flat) {
				return "secret"
			}
			if idName.MatchString(flat) {
				return "id_number"
			}
			if r := scanDetails(inner); r != "" {
				return r
			}
		}
	case []any:
		for _, inner := range x {
			if r := scanDetails(inner); r != "" {
				return r
			}
		}
	case string:
		return scanText(x)
	case float64:
		return scanText(fmt.Sprintf("%.0f", x))
	}
	return ""
}

func scanText(text string) string {
	if secretText.MatchString(text) {
		return "secret"
	}
	if idText.MatchString(text) {
		return "id_number"
	}
	return ""
}
