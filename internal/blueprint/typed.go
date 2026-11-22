package blueprint

// blockProblems reports a typed block's diagnostics on the document; what
// names the block and item the kind of thing a node is ("invariant ").
func (d *Document) blockProblems(what, item string, errs, warns []TraceError) {
	for _, e := range append(append([]TraceError(nil), errs...), warns...) {
		d.problems = append(d.problems, e.problem(what, item))
	}
}
