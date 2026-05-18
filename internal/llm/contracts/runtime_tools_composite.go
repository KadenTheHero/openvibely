package contracts

import (
	"context"
	"encoding/json"
)

// CompositeRuntimeTools merges multiple request-scoped RuntimeTools into one
// value that can be attached to a context. WithRuntimeTools intentionally stores
// a single RuntimeTools pointer, so callers that need skills + mutation tools +
// scoped-file tools must compose them before attaching.
func CompositeRuntimeTools(tools ...*RuntimeTools) *RuntimeTools {
	var filtered []*RuntimeTools
	for _, rt := range tools {
		if rt != nil {
			filtered = append(filtered, rt)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	out := &RuntimeTools{}
	for _, rt := range filtered {
		out.Definitions = append(out.Definitions, rt.Definitions...)
		out.SkipDefaultTools = out.SkipDefaultTools || rt.SkipDefaultTools
	}
	out.Executor = func(ctx context.Context, name string, input json.RawMessage) (string, bool, bool, error) {
		for _, rt := range filtered {
			if rt.Executor == nil {
				continue
			}
			output, handled, isError, err := rt.Executor(ctx, name, input)
			if handled || err != nil {
				return output, handled, isError, err
			}
		}
		return "", false, false, nil
	}
	out.Filter = func(name string) (bool, bool) {
		for _, rt := range filtered {
			if rt.Filter == nil {
				continue
			}
			allow, handled := rt.Filter(name)
			if handled {
				return allow, true
			}
		}
		return false, false
	}
	return out
}
