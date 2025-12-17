package middleware

import (
	"net/http"

	"github.com/velmie/idempo"
)

func writeHTTPResponse(w http.ResponseWriter, resp *idempo.Response) {
	if resp == nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	for k, vals := range resp.Metadata {
		cp := make([]string, len(vals))
		copy(cp, vals)
		w.Header()[k] = cp
	}
	if resp.Truncated {
		w.Header().Set("X-Idempotent-Truncated", "true")
	}
	status := resp.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if !resp.Truncated && len(resp.Body) > 0 {
		_, _ = w.Write(resp.Body)
	}
}
