package httpx

import (
	_ "embed"
	"net/http"
)

// openapiSpec, docs/openapi.yaml ile birebir ayni olmalidir; test/faz27.js
// iki dosyanin ayni oldugunu denetler.
//
//go:embed openapi.yaml
var openapiSpec []byte

func openapiHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapiSpec)
	}
}
