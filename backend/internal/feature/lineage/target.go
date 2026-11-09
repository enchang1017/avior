package lineage

import (
	"errors"
	"fmt"

	domain "github.com/enchang1017/avior/backend/internal/platform/lineage"
)

// ErrTargetForm reports an object given both by id and by name, or by neither.
var ErrTargetForm = errors.New("lineage: give the object by id or by name, not both")

// ErrRoutineAmbiguous reports a routine named without a signature whose name
// more than one overload shares.
var ErrRoutineAmbiguous = errors.New("lineage: more than one routine has that name")

// KindRoutine names a function or a procedure when an object is given by name.
const KindRoutine = "routine"

// Target is an object a request names: by its opaque id, or by schema, name
// and kind. The server builds the id, so no client reproduces its quoting.
type Target struct {
	ID     domain.NodeID
	Schema string
	Name   string
	Kind   string
	// Signature is a routine's identity arguments, nil when not given. An empty
	// signature is a routine that takes none.
	Signature *string
}

func (t Target) byName() bool {
	return t.Schema != "" || t.Name != "" || t.Kind != "" || t.Signature != nil
}

// Validate refuses a target given both ways, or neither, or by an incomplete
// name.
func (t Target) Validate() error {
	switch {
	case t.ID != "" && t.byName():
		return fmt.Errorf("%w: both were given", ErrTargetForm)
	case t.ID == "" && (t.Schema == "" || t.Name == "" || t.Kind == ""):
		return fmt.Errorf("%w: an id, or a schema, a name and a kind, are needed", ErrTargetForm)
	case t.Signature != nil && t.Kind != KindRoutine:
		return fmt.Errorf("%w: only a routine takes a signature", ErrTargetForm)
	}
	return nil
}

// Label names the target in a refusal the way the operator gave it.
func (t Target) Label() string {
	if t.ID != "" {
		return string(t.ID)
	}
	label := t.Schema + "." + t.Name
	if t.Signature != nil {
		label += "(" + *t.Signature + ")"
	}
	return label
}

// resolve turns a target into the node id the snapshot holds it under. A name
// that resolves to nothing still yields an id, so the domain answers it as an
// object the database does not hold.
func (b Built) resolve(t Target) (domain.NodeID, error) {
	if t.ID != "" {
		return t.ID, nil
	}
	if t.Kind == string(domain.KindView) || t.Kind == string(domain.KindMatview) {
		return b.viewLike(t), nil
	}
	if t.Kind != KindRoutine {
		return domain.RelationID(domain.NodeKind(t.Kind), t.Schema, t.Name), nil
	}
	if t.Signature != nil {
		for _, kind := range []domain.NodeKind{domain.KindFunction, domain.KindProcedure} {
			if id := domain.RoutineID(kind, t.Schema, t.Name, *t.Signature); b.nodes[id].ID != "" {
				return id, nil
			}
		}
		return domain.RoutineID(domain.KindFunction, t.Schema, t.Name, *t.Signature), nil
	}
	candidates := b.routines[routineKey(t.Schema, t.Name)]
	switch len(candidates) {
	case 0:
		return domain.RoutineID(domain.KindFunction, t.Schema, t.Name, ""), nil
	case 1:
		return candidates[0], nil
	}
	return "", fmt.Errorf("%w: %s.%s has %d overloads; give its signature", ErrRoutineAmbiguous, t.Schema, t.Name, len(candidates))
}

// viewLike resolves a view and a materialised view by name alike, because
// FEAT-067's chart draws both as a view and cannot say which it is. A table
// stays strict: nothing names a table as anything else.
func (b Built) viewLike(t Target) domain.NodeID {
	asked := domain.RelationID(domain.NodeKind(t.Kind), t.Schema, t.Name)
	if b.nodes[asked].ID != "" {
		return asked
	}
	other := domain.KindMatview
	if t.Kind == string(domain.KindMatview) {
		other = domain.KindView
	}
	if id := domain.RelationID(other, t.Schema, t.Name); b.nodes[id].ID != "" {
		return id
	}
	return asked
}

func routineKey(schema, name string) string { return schema + "\x00" + name }
