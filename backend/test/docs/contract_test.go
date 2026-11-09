//go:build docs

package docs_test

import (
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/enchang1017/avior/backend/internal/platform/identity"
	"github.com/enchang1017/avior/backend/internal/sqlguard"
)

// The HTTP contract names the roles, and so does the role model. Two lists of
// the same fact drift, and this one drifts silently: a role added to the model
// and not to the contract makes CurrentUser.roles carry a value the schema
// forbids, which the generated TypeScript union then cannot express — so the
// page has no way to offer the role and nothing anywhere reports why.
//
// The frontend's own end of this is checked at build time in
// src/shared/roles.ts, which asserts its list against the generated union in
// both directions. That leaves exactly this gap: the contract against the Go
// model. This closes it.
const contractFile = repoRoot + "/api/openapi.yaml"

func TestRoleEnumInTheContractMatchesTheRoleModel(t *testing.T) {
	raw, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas struct {
				CurrentUser struct {
					Properties struct {
						Roles struct {
							Items struct {
								Enum []string `yaml:"enum"`
							} `yaml:"items"`
						} `yaml:"roles"`
					} `yaml:"properties"`
				} `yaml:"CurrentUser"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}

	inContract := doc.Components.Schemas.CurrentUser.Properties.Roles.Items.Enum
	if len(inContract) == 0 {
		t.Fatal("CurrentUser.roles names no enum; the schema shape changed and this " +
			"gate stopped checking anything")
	}

	inModel := make([]string, 0, len(inContract))
	for _, role := range identity.Catalogue() {
		inModel = append(inModel, string(role.Name))
	}

	sort.Strings(inContract)
	sort.Strings(inModel)

	if len(inContract) != len(inModel) {
		t.Fatalf("the contract names %v and the role model holds %v", inContract, inModel)
	}
	for i := range inModel {
		if inContract[i] != inModel[i] {
			t.Errorf("the contract names %v and the role model holds %v", inContract, inModel)
			break
		}
	}
}

// The guard's rules are named twice: as constants in sqlguard and as an enum in
// the contract. The generated Go type is what a handler assigns to, so a rule
// the contract has not heard of reaches the client as a value its own union
// cannot express — and the page renders a refusal it cannot describe.
//
// Reasons the contract's list may be longer are none: every value it offers has
// to be one something can actually produce, or the frontend is written against
// a case that never happens.
func TestGuardRuleEnumInTheContractMatchesTheGuard(t *testing.T) {
	raw, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas struct {
				GuardReason struct {
					Properties struct {
						Rule struct {
							Enum []string `yaml:"enum"`
						} `yaml:"rule"`
					} `yaml:"properties"`
				} `yaml:"GuardReason"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}

	inContract := doc.Components.Schemas.GuardReason.Properties.Rule.Enum
	if len(inContract) == 0 {
		t.Fatal("GuardReason.rule names no enum; the schema shape changed and this " +
			"gate stopped checking anything")
	}

	inGuard := make([]string, 0, len(inContract))
	for _, rule := range sqlguard.Rules() {
		inGuard = append(inGuard, string(rule))
	}

	sort.Strings(inContract)
	sort.Strings(inGuard)

	if len(inContract) != len(inGuard) {
		t.Fatalf("the contract names %v and the guard has %v", inContract, inGuard)
	}
	for i := range inGuard {
		if inContract[i] != inGuard[i] {
			t.Errorf("the contract names %v and the guard has %v", inContract, inGuard)
			break
		}
	}
}
