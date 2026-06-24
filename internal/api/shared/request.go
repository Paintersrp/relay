package shared

import (
	"encoding/json"
	"net/http"
)

func DecodeJSON(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
