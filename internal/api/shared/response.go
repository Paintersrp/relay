package shared

import (
	"encoding/json"
	"net/http"
)

type ErrorShape struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Code    string                 `json:"code,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, errStr, msg string) {
	JSON(w, status, ErrorShape{
		Error:   errStr,
		Message: msg,
	})
}
