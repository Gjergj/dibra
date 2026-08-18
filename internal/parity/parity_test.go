package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidStatusTransitionRequiresIntermediateAudit(t *testing.T) {
	t.Parallel()

	if ValidStatusTransition(ContractMissing, ContractMatched) {
		t.Fatal("missing -> matched unexpectedly accepted")
	}
	if !ValidStatusTransition(ContractMissing, ContractPartial) {
		t.Fatal("missing -> partial unexpectedly rejected")
	}
	if !ValidStatusTransition(ContractPartial, "verified") {
		t.Fatal("partial -> verified must remain valid for the Docker inventory")
	}
}

func TestCheckoutCommitResolvesLooseReference(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, root, ".git/HEAD", "ref: refs/heads/devel\n")
	writeFixture(t, root, ".git/refs/heads/devel", "0123456789012345678901234567890123456789\n")

	commit, err := CheckoutCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if commit != "0123456789012345678901234567890123456789" {
		t.Fatalf("commit = %q", commit)
	}
}

func TestValidateCheckoutCommitRejectsWrongSHA(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, root, ".git/HEAD", "0123456789012345678901234567890123456789\n")
	problems := ValidateCheckoutCommit(root, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if len(problems) != 1 || !strings.Contains(problems[0], "expected aaaaaaaaaa") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestValidatePathsRejectsTraversalAndMissingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixture(t, root, "present.go", "package fixture\n")
	problems := ValidateRelativePaths(root, "feature example", []string{"present.go", "../escape", "missing.go"})
	joined := strings.Join(problems, "\n")
	for _, expected := range []string{"invalid local path", "missing local path"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("problems %q do not contain %q", joined, expected)
		}
	}
}

func TestValidatePassedCertificationRequiresExactEvidence(t *testing.T) {
	t.Parallel()

	assessment := Assessment{
		ContractStatus:      ContractMatched,
		EvidenceLevel:       EvidenceIntegration,
		CertificationStatus: CertificationPassed,
	}
	problems := ValidatePassedCertification(t.TempDir(), t.TempDir(), "feature example", assessment, CertificationEvidence{})
	if len(problems) != 1 || !strings.Contains(problems[0], "without exact upstream cases") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestValidatePassedCertificationAcceptsRealSelectorsAndLane(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	upstream := t.TempDir()
	writeFixture(t, root, "Makefile", "test-core-integration:\n\tgo test -run 'TestCoreCase'\n")
	writeFixture(t, root, "test/integration/core_test.go", "package integration\nfunc TestCoreCase(t *testing.T) {}\n")
	writeFixture(t, upstream, "test/integration/targets/core/tasks/main.yml", "- name: core_case\n")

	assessment := Assessment{
		ContractStatus:      ContractMatched,
		EvidenceLevel:       EvidenceIntegration,
		CertificationStatus: CertificationPassed,
	}
	evidence := CertificationEvidence{
		UpstreamCases:      []string{"test/integration/targets/core/tasks/main.yml::core_case"},
		DibraCases:         []string{"test/integration/core_test.go::TestCoreCase"},
		CertificationLanes: []string{"test-core-integration"},
	}
	if problems := ValidatePassedCertification(root, upstream, "feature example", assessment, evidence); len(problems) != 0 {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestPythonRawStringSupportsRawAndPlainTripleQuotes(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"DOCUMENTATION = r\"\"\"module: apt\n\"\"\"\n",
		"DOCUMENTATION = '''module: apt\n'''\n",
	} {
		value, err := PythonRawString([]byte(source), "DOCUMENTATION")
		if err != nil {
			t.Fatal(err)
		}
		if string(value) != "module: apt\n" {
			t.Fatalf("value = %q", value)
		}
	}
}

func TestCheckGeneratedRejectsStaleOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckGenerated(path, []byte("new")); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("CheckGenerated() error = %v", err)
	}
}

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
