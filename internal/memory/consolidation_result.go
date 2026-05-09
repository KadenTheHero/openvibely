package memory

// ConsolidationResult summarizes files touched by a model-backed memory
// extraction or consolidation run. The model decides the semantic memory edits;
// deterministic code records touched paths from sandboxed memory file tools.
type ConsolidationResult struct {
	TouchedPaths []string
	Notes        []string
}
