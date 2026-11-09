package lineage

// relationEntry is one drawn relation and what resolution needs of it.
type relationEntry struct {
	Relation
	id      NodeID
	columns map[string]bool
}

func (r *relationEntry) has(column string) bool { return r.columns[column] }

// routineEntry is one routine and its node id.
type routineEntry struct {
	Routine
	id NodeID
}

// catalogueIndex answers the lookups resolution makes, partitions folded.
type catalogueIndex struct {
	byOID       map[uint32]*relationEntry
	byName      map[string]*relationEntry
	routines    map[uint32]*routineEntry
	routineName map[string][]*routineEntry
	triggers    map[uint32][]Trigger
	views       map[NodeID]ViewMap
}

func qualifiedKey(schema, name string) string { return schema + "\x00" + name }

func indexCatalogue(cat Catalogue) *catalogueIndex {
	idx := &catalogueIndex{
		byOID: map[uint32]*relationEntry{}, byName: map[string]*relationEntry{},
		routines: map[uint32]*routineEntry{}, routineName: map[string][]*routineEntry{},
		triggers: map[uint32][]Trigger{}, views: cat.Views,
	}
	roots, owner := Fold(cat.Relations)
	for _, relation := range roots {
		entry := &relationEntry{Relation: relation, id: RelationID(relation.Kind, relation.Schema, relation.Name), columns: map[string]bool{}}
		for _, column := range relation.Columns {
			entry.columns[column] = true
		}
		idx.byOID[relation.OID] = entry
	}
	for _, relation := range cat.Relations {
		if entry := idx.byOID[owner[relation.OID]]; entry != nil {
			idx.byOID[relation.OID] = entry
			idx.byName[qualifiedKey(relation.Schema, relation.Name)] = entry
		}
	}
	for _, routine := range cat.Routines {
		entry := &routineEntry{Routine: routine, id: RoutineID(routine.Kind, routine.Schema, routine.Name, routine.Arguments)}
		idx.routines[routine.OID] = entry
		idx.routineName[routine.Name] = append(idx.routineName[routine.Name], entry)
	}
	for _, trigger := range cat.Triggers {
		idx.triggers[trigger.Function] = append(idx.triggers[trigger.Function], trigger)
	}
	return idx
}

func (idx *catalogueIndex) relation(schema, name string) *relationEntry {
	return idx.byName[qualifiedKey(schema, name)]
}

// triggerTables answers the tables whose triggers run a function, which are
// the tables its NEW and OLD are rows of.
func (idx *catalogueIndex) triggerTables(function uint32) []*relationEntry {
	var out []*relationEntry
	seen := map[*relationEntry]bool{}
	for _, trigger := range idx.triggers[function] {
		if table := idx.byOID[trigger.Table]; table != nil && !seen[table] {
			seen[table] = true
			out = append(out, table)
		}
	}
	return out
}
