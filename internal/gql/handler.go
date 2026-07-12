package gql

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/graphql-go/graphql"
)

// request is the standard GraphQL-over-HTTP POST body.
type request struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

// Handler serves GraphQL over HTTP POST with permissive CORS, so the
// static UI in web/ can call it from file:// or another origin.
func Handler(schema graphql.Schema, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		switch r.Method {
		case http.MethodOptions: // CORS preflight
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodPost:
		default:
			http.Error(w, "GraphQL endpoint: POST a JSON body {query, variables}", http.StatusMethodNotAllowed)
			return
		}

		var req request
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]string{{"message": "invalid JSON body: " + err.Error()}},
			})
			return
		}

		start := time.Now()
		result := graphql.Do(graphql.Params{
			Schema:         schema,
			RequestString:  req.Query,
			OperationName:  req.OperationName,
			VariableValues: req.Variables,
			Context:        r.Context(),
		})

		log.Info("graphql",
			slog.String("operation", req.OperationName),
			slog.Duration("took", time.Since(start)),
			slog.Int("errors", len(result.Errors)),
		)

		// GraphQL-over-HTTP convention: resolver errors still return 200
		// with an errors array; only transport problems get non-200.
		writeJSON(w, http.StatusOK, result)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}