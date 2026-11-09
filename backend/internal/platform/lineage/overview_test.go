package lineage

import (
	"slices"
	"testing"
)

func TestOverviewFoldsEachSchemaIntoOneClusterAndEveryCrossingIntoOneLink(t *testing.T) {
	g := build(t, smallCatalogue(), nil)
	galaxy := Overview(g, false)
	schemas := make([]string, 0, len(galaxy.Schemas))
	for _, cluster := range galaxy.Schemas {
		schemas = append(schemas, cluster.Schema)
		if cluster.Objects < cluster.Tables+cluster.Views+cluster.Routines {
			t.Errorf("%s counts %d objects but %d of its kinds", cluster.Schema, cluster.Objects, cluster.Tables+cluster.Views+cluster.Routines)
		}
		if cluster.Anchor == "" {
			t.Errorf("%s has no anchor", cluster.Schema)
		}
	}
	if !slices.IsSorted(schemas) || !slices.Contains(schemas, "public") || !slices.Contains(schemas, "promo") {
		t.Fatalf("clusters %v, want public and promo in order", schemas)
	}
	for _, link := range galaxy.Links {
		if link.From >= link.To || link.Edges < link.Reads+link.Writes {
			t.Errorf("malformed link %+v", link)
		}
	}
}

func TestOverviewAnchorsAClusterOnItsMostConnectedObject(t *testing.T) {
	cat := tables(4)
	link(&cat, 1, 0)
	link(&cat, 2, 0)
	link(&cat, 3, 1)
	galaxy := Overview(build(t, cat, nil), false)
	if len(galaxy.Schemas) != 1 || galaxy.Schemas[0].Anchor != tableID(0) || galaxy.Schemas[0].Tables != 4 {
		t.Errorf("galaxy %+v, want one cluster of four tables anchored on t0", galaxy)
	}
	if len(galaxy.Links) != 0 {
		t.Errorf("edges within one schema became links: %+v", galaxy.Links)
	}
}

func TestOverviewLeavesExtensionRoutinesOutUnlessAsked(t *testing.T) {
	cat := smallCatalogue()
	cat.Routines = append(cat.Routines, Routine{
		OID: 9001, Schema: "public", Name: "soundex", Arguments: "text", ArgCount: 1,
		Kind: KindFunction, Language: "c", Extension: true,
	})
	g := build(t, cat, map[NodeID]Body{RoutineID(KindFunction, "public", "soundex", "text"): {Status: StatusOpaque}})
	with, without := Overview(g, true), Overview(g, false)
	total := func(galaxy Galaxy) (sum int) {
		for _, cluster := range galaxy.Schemas {
			sum += cluster.Objects
		}
		return sum
	}
	extension := 0
	for _, node := range g.Nodes {
		if node.Extension {
			extension++
		}
	}
	if extension == 0 || total(with)-total(without) != extension {
		t.Errorf("with extensions %d, without %d, and %d extension routines", total(with), total(without), extension)
	}
}
