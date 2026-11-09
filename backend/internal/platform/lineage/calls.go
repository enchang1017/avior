package lineage

// callee resolves a call by name and argument count. A name that is not a
// routine of this database is a built-in and draws nothing. An overload that
// the count does not settle reaches no callee and is badged.
func (r *resolver) callee(call Call) *routineEntry {
	candidates := r.candidates(call)
	var exact []*routineEntry
	for _, candidate := range candidates {
		if candidate.ArgCount == call.Args {
			exact = append(exact, candidate)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0]
	case len(exact) == 0 && len(candidates) == 1:
		return candidates[0]
	case len(candidates) == 0:
		return nil
	default:
		r.out.badges[BadgeOverloadAmbiguous] = true
		r.note("overloaded call", call.Name)
		return nil
	}
}

func (r *resolver) candidates(call Call) []*routineEntry {
	schemas := r.path
	if call.Schema != "" {
		schemas = []string{call.Schema}
	}
	for _, schema := range schemas {
		var found []*routineEntry
		for _, routine := range r.idx.routineName[call.Name] {
			if routine.Schema == schema {
				found = append(found, routine)
			}
		}
		if len(found) > 0 {
			return found
		}
	}
	return nil
}
