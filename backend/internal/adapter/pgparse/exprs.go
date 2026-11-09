package pgparse

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/enchang1017/avior/backend/internal/platform/lineage"
)

func (c *collector) exprs(nodes []*pg.Node, role lineage.Role, scope int) {
	for _, n := range nodes {
		c.expr(n, role, scope)
	}
}

// expr walks an expression tree. Every node it does not name is descended
// into field by field, so a reference inside a construct nobody listed is
// still found.
func (c *collector) expr(m proto.Message, role lineage.Role, scope int) {
	if m == nil || !m.ProtoReflect().IsValid() {
		return
	}
	switch n := m.(type) {
	case *pg.Node:
		if isStatement(n) {
			c.query(n, scope)
			return
		}
		c.fields(n, role, scope)
	case *pg.ColumnRef:
		c.column(n, role, scope)
	case *pg.FuncCall:
		c.funcCall(n, role, scope)
	case *pg.SubLink:
		c.expr(n.GetTestexpr(), role, scope)
		c.query(n.GetSubselect(), scope)
	default:
		c.fields(m, role, scope)
	}
}

func isStatement(n *pg.Node) bool {
	switch n.GetNode().(type) {
	case *pg.Node_SelectStmt, *pg.Node_InsertStmt, *pg.Node_UpdateStmt, *pg.Node_DeleteStmt, *pg.Node_MergeStmt:
		return true
	default:
		return false
	}
}

func (c *collector) fields(m proto.Message, role lineage.Role, scope int) {
	m.ProtoReflect().Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.Message() == nil || field.IsMap():
		case field.IsList():
			list := value.List()
			for i := range list.Len() {
				c.expr(list.Get(i).Message().Interface(), role, scope)
			}
		default:
			c.expr(value.Message().Interface(), role, scope)
		}
		return true
	})
}

func (c *collector) column(ref *pg.ColumnRef, role lineage.Role, scope int) {
	var names []string
	star := false
	for _, field := range ref.GetFields() {
		if s := field.GetString_(); s != nil {
			names = append(names, s.GetSval())
		} else if field.GetAStar() != nil {
			star = true
		}
	}
	if star {
		names = append(names, "*")
	}
	out := lineage.Reference{Role: role, Scope: scope}
	switch len(names) {
	case 1:
		out.Column = names[0]
	case 2:
		out.Name, out.Column = names[0], names[1]
		out.Role = c.recordRole(names[0], role, scope)
	case 3:
		out.Qualifier, out.Name, out.Column = names[0], names[1], names[2]
	default:
		out.Qualifier, out.Name, out.Column = names[len(names)-3], names[len(names)-2], names[len(names)-1]
	}
	c.reference(out, ref.GetLocation())
}

// recordRole tells a trigger's NEW and OLD from a table aliased the same way.
func (c *collector) recordRole(qualifier string, role lineage.Role, scope int) lineage.Role {
	if c.hasAlias(scope, qualifier) {
		return role
	}
	switch qualifier {
	case "new":
		return lineage.RoleNewRead
	case "old":
		return lineage.RoleOldRead
	default:
		return role
	}
}

// sequenceRoles are the sequence functions, by whether they advance it.
var sequenceRoles = map[string]lineage.Role{
	"nextval": lineage.RoleSequence,
	"setval":  lineage.RoleSequence,
	"currval": lineage.RoleRelation,
}

func (c *collector) funcCall(f *pg.FuncCall, role lineage.Role, scope int) {
	schema, name := "", lastName(f.GetFuncname())
	if parts := f.GetFuncname(); len(parts) > 1 {
		schema = parts[len(parts)-2].GetString_().GetSval()
	}
	if sequenceRole, isSequence := sequenceRoles[name]; isSequence && (schema == "" || schema == "pg_catalog") {
		c.sequence(f, sequenceRole, scope)
	} else if schema != "pg_catalog" {
		c.call(schema, name, len(f.GetArgs()), f.GetLocation())
	}
	c.exprs(f.GetArgs(), role, scope)
	c.exprs(f.GetAggOrder(), role, scope)
	c.expr(f.GetAggFilter(), role, scope)
	c.expr(f.GetOver(), role, scope)
}

func (c *collector) sequence(f *pg.FuncCall, role lineage.Role, scope int) {
	if len(f.GetArgs()) == 0 {
		return
	}
	arg := f.GetArgs()[0]
	if cast := arg.GetTypeCast(); cast != nil {
		arg = cast.GetArg()
	}
	literal := arg.GetAConst().GetSval()
	if literal == nil {
		return
	}
	parts := splitIdentifier(literal.GetSval())
	ref := lineage.Reference{Name: parts[len(parts)-1], Role: role, Scope: scope}
	if len(parts) > 1 {
		ref.Qualifier = parts[len(parts)-2]
	}
	c.reference(ref, f.GetLocation())
}

// splitIdentifier reads a regclass literal: dotted, each part folded to lower
// case unless it is double-quoted.
func splitIdentifier(text string) []string {
	var parts []string
	var part strings.Builder
	quoted, wasQuoted := false, false
	flush := func() {
		value := part.String()
		if !wasQuoted {
			value = strings.ToLower(strings.TrimSpace(value))
		}
		parts = append(parts, value)
		part.Reset()
		wasQuoted = false
	}
	for i := 0; i < len(text); i++ {
		switch ch := text[i]; {
		case ch == '"' && quoted && i+1 < len(text) && text[i+1] == '"':
			part.WriteByte('"')
			i++
		case ch == '"':
			quoted, wasQuoted = !quoted, true
		case ch == '.' && !quoted:
			flush()
		default:
			part.WriteByte(ch)
		}
	}
	flush()
	return parts
}
