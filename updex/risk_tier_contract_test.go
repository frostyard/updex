package updex

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The risk-tier vocabulary is declared in three places that must agree:
// docs/risk-tiers.md (the canonical prose), .github/pull_request_template.md
// (the author-facing checkbox list), and .github/policies/ai-governance.json
// (the machine-readable allowed_tiers). policies/agent-governance.json fixes
// the org-wide tier names. Nothing else pins them together, so adding a tier
// to one file and forgetting another used to drift silently.

var (
	docsTierHeadingRE  = regexp.MustCompile(`(?m)^## Tier (\d+): (.+?)\s*$`)
	templateTierItemRE = regexp.MustCompile(`(?m)^- \[ \] Tier (\d+): (.+?)\s*$`)
)

type riskTier struct {
	number int
	name   string
}

func parseRiskTiers(t *testing.T, source, path string, re *regexp.Regexp) []riskTier {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("%s (%s) declares no risk tiers", source, path)
	}

	tiers := make([]riskTier, 0, len(matches))
	seen := make(map[int]bool, len(matches))
	for _, match := range matches {
		number, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("%s: parse tier number %q: %v", source, match[1], err)
		}
		if seen[number] {
			t.Fatalf("%s declares Tier %d more than once", source, number)
		}
		seen[number] = true
		tiers = append(tiers, riskTier{number: number, name: match[2]})
	}
	return tiers
}

func formatRiskTiers(tiers []riskTier) string {
	parts := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		parts = append(parts, fmt.Sprintf("Tier %d: %s", tier.number, tier.name))
	}
	return strings.Join(parts, ", ")
}

func tierNumbers(tiers []riskTier) []int {
	numbers := make([]int, 0, len(tiers))
	for _, tier := range tiers {
		numbers = append(numbers, tier.number)
	}
	return numbers
}

// TestRiskTierVocabularyIsConsistent fails when the tier list in the risk-tier
// spec, the pull-request template, and the AI governance policy diverge — for
// example when a Tier 5 is added to one file but not the other two.
func TestRiskTierVocabularyIsConsistent(t *testing.T) {
	docsTiers := parseRiskTiers(t, "docs/risk-tiers.md", "../docs/risk-tiers.md", docsTierHeadingRE)
	templateTiers := parseRiskTiers(t, ".github/pull_request_template.md", "../.github/pull_request_template.md", templateTierItemRE)

	data, err := os.ReadFile("../.github/policies/ai-governance.json")
	if err != nil {
		t.Fatalf("read .github/policies/ai-governance.json: %v", err)
	}
	var policy struct {
		Controls struct {
			RiskClassification struct {
				AllowedTiers []int `json:"allowed_tiers"`
			} `json:"risk_classification"`
		} `json:"controls"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatalf("parse .github/policies/ai-governance.json: %v", err)
	}
	allowedTiers := policy.Controls.RiskClassification.AllowedTiers
	if len(allowedTiers) == 0 {
		t.Fatal(".github/policies/ai-governance.json declares no allowed_tiers")
	}

	// The spec is canonical: tiers are numbered 1..N in ascending order.
	for i, tier := range docsTiers {
		if want := i + 1; tier.number != want {
			t.Fatalf("docs/risk-tiers.md heading %d is Tier %d, want Tier %d (tiers must be numbered 1..N in order)", i+1, tier.number, want)
		}
	}

	if got, want := formatRiskTiers(templateTiers), formatRiskTiers(docsTiers); got != want {
		t.Errorf(".github/pull_request_template.md offers %q, want %q (from docs/risk-tiers.md)", got, want)
	}

	if got, want := fmt.Sprint(allowedTiers), fmt.Sprint(tierNumbers(docsTiers)); got != want {
		t.Errorf(".github/policies/ai-governance.json allowed_tiers = %s, want %s (from docs/risk-tiers.md)", got, want)
	}
}

// TestRiskTierNamesMatchAgentGovernance pins the tier names in
// docs/risk-tiers.md to the org-fixed vocabulary in
// policies/agent-governance.json, so the two governance files cannot drift.
func TestRiskTierNamesMatchAgentGovernance(t *testing.T) {
	docsTiers := parseRiskTiers(t, "docs/risk-tiers.md", "../docs/risk-tiers.md", docsTierHeadingRE)

	data, err := os.ReadFile("../policies/agent-governance.json")
	if err != nil {
		t.Fatalf("read policies/agent-governance.json: %v", err)
	}
	var governance struct {
		RiskClassification struct {
			Tiers []string `json:"tiers"`
		} `json:"risk_classification"`
	}
	if err := json.Unmarshal(data, &governance); err != nil {
		t.Fatalf("parse policies/agent-governance.json: %v", err)
	}

	docsNames := make([]string, 0, len(docsTiers))
	for _, tier := range docsTiers {
		docsNames = append(docsNames, strings.ToLower(tier.name))
	}
	if got, want := fmt.Sprint(docsNames), fmt.Sprint(governance.RiskClassification.Tiers); got != want {
		t.Errorf("docs/risk-tiers.md tier names = %s, want %s (from policies/agent-governance.json)", got, want)
	}
}
