package gql

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
)

// tracer is a graphql-go Extension that records how long each individual
// field resolver takes and, once execution finishes, emits a single
// structured Debug line listing the slowest resolvers for the request.
//
// It logs at Debug so normal runs stay quiet; set LOG_LEVEL=debug to see it.
// State is per-request: Init stashes a fresh accumulator in the context, and
// the resolve/execution hooks read it back from there.
type tracer struct {
	log *slog.Logger
}

// slowestN caps how many resolvers we report per request.
const slowestN = 8

// minReport filters out trivial scalar resolves (sub-microsecond field reads)
// so the breakdown highlights the resolvers that actually did work (DB calls).
const minReport = 100 * time.Microsecond

type traceKey struct{}

type fieldTiming struct {
	path string
	dur  time.Duration
}

type traceAcc struct {
	mu     sync.Mutex
	fields []fieldTiming
}

func (t *tracer) Name() string { return "slog-resolver-tracer" }

func (t *tracer) Init(ctx context.Context, _ *graphql.Params) context.Context {
	return context.WithValue(ctx, traceKey{}, &traceAcc{})
}

func (t *tracer) ResolveFieldDidStart(ctx context.Context, info *graphql.ResolveInfo) (context.Context, graphql.ResolveFieldFinishFunc) {
	start := time.Now()
	return ctx, func(_ interface{}, _ error) {
		acc, _ := ctx.Value(traceKey{}).(*traceAcc)
		if acc == nil {
			return
		}
		acc.mu.Lock()
		acc.fields = append(acc.fields, fieldTiming{path: pathString(info), dur: time.Since(start)})
		acc.mu.Unlock()
	}
}

func (t *tracer) ExecutionDidStart(ctx context.Context) (context.Context, graphql.ExecutionFinishFunc) {
	return ctx, func(_ *graphql.Result) {
		acc, _ := ctx.Value(traceKey{}).(*traceAcc)
		if acc == nil {
			return
		}
		acc.mu.Lock()
		fields := acc.fields
		acc.mu.Unlock()

		sort.Slice(fields, func(i, j int) bool { return fields[i].dur > fields[j].dur })

		slowest := make([]map[string]any, 0, slowestN)
		for _, f := range fields {
			if f.dur < minReport || len(slowest) >= slowestN {
				break
			}
			slowest = append(slowest, map[string]any{
				"path":   f.path,
				"micros": f.dur.Microseconds(),
			})
		}

		t.log.Debug("graphql.resolvers",
			slog.Int("fields", len(fields)),
			slog.Any("slowest", slowest),
		)
	}
}

// pathString renders a resolver's response path, e.g. "users.0.tasks".
func pathString(info *graphql.ResolveInfo) string {
	if info == nil || info.Path == nil {
		return "?"
	}
	parts := info.Path.AsArray()
	segs := make([]string, len(parts))
	for i, p := range parts {
		segs[i] = fmt.Sprint(p)
	}
	return strings.Join(segs, ".")
}

// The remaining hooks are no-ops: this extension only cares about resolver
// timing and adds nothing to the GraphQL response itself.
func (t *tracer) ParseDidStart(ctx context.Context) (context.Context, graphql.ParseFinishFunc) {
	return ctx, func(error) {}
}

func (t *tracer) ValidationDidStart(ctx context.Context) (context.Context, graphql.ValidationFinishFunc) {
	return ctx, func([]gqlerrors.FormattedError) {}
}

func (t *tracer) HasResult() bool                             { return false }
func (t *tracer) GetResult(context.Context) interface{}       { return nil }
