package parity

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	ContractMissing       = "missing"
	ContractPartial       = "partial"
	ContractMatched       = "matched"
	ContractDivergent     = "divergent"
	ContractNotApplicable = "not-applicable"

	EvidenceMapped       = "mapped"
	EvidenceUnit         = "unit"
	EvidenceIntegration  = "integration"
	EvidenceDifferential = "differential"

	CertificationPending = "pending"
	CertificationPassed  = "passed"
)

var contractStatuses = map[string]bool{
	ContractMissing:       true,
	ContractPartial:       true,
	ContractMatched:       true,
	ContractDivergent:     true,
	ContractNotApplicable: true,
}

var evidenceLevels = map[string]bool{
	EvidenceMapped:       true,
	EvidenceUnit:         true,
	EvidenceIntegration:  true,
	EvidenceDifferential: true,
	// Docker's existing inventory calls real-daemon evidence "daemon".
	"daemon": true,
}

var certificationStatuses = map[string]bool{
	CertificationPending: true,
	CertificationPassed:  true,
}

type Assessment struct {
	ContractStatus      string `yaml:"contract_status"`
	EvidenceLevel       string `yaml:"evidence_level"`
	CertificationStatus string `yaml:"certification_status"`
	Blocker             string `yaml:"blocker,omitempty"`
}

type Issue struct {
	ID       string   `yaml:"id"`
	Priority string   `yaml:"priority"`
	Summary  string   `yaml:"summary"`
	Features []string `yaml:"features"`
}

type CertificationEvidence struct {
	UpstreamCases      []string `yaml:"upstream_cases,omitempty"`
	DibraCases         []string `yaml:"dibra_cases,omitempty"`
	CertificationLanes []string `yaml:"certification_lanes,omitempty"`
}

func IsContractStatus(value string) bool {
	return contractStatuses[value]
}

func IsEvidenceLevel(value string) bool {
	return evidenceLevels[value]
}

func IsCertificationStatus(value string) bool {
	return certificationStatuses[value]
}

func ValidateAssessment(label string, assessment Assessment) []string {
	var problems []string
	if !IsContractStatus(assessment.ContractStatus) {
		problems = append(problems, fmt.Sprintf("%s has invalid contract_status %q", label, assessment.ContractStatus))
	}
	if !IsEvidenceLevel(assessment.EvidenceLevel) {
		problems = append(problems, fmt.Sprintf("%s has invalid evidence_level %q", label, assessment.EvidenceLevel))
	}
	if !IsCertificationStatus(assessment.CertificationStatus) {
		problems = append(problems, fmt.Sprintf("%s has invalid certification_status %q", label, assessment.CertificationStatus))
	}
	return problems
}

// ValidStatusTransition accepts the common parity progression and the existing
// Docker inventory's "verified" terminal label. It deliberately rejects a
// direct missing-to-matched/verified jump so audited evidence cannot be skipped.
func ValidStatusTransition(previous, current string) bool {
	if previous == current {
		return true
	}
	allowed := map[string]map[string]bool{
		ContractMissing: {
			ContractPartial:       true,
			ContractDivergent:     true,
			ContractNotApplicable: true,
		},
		ContractPartial: {
			ContractMatched:       true,
			"verified":            true,
			ContractDivergent:     true,
			ContractNotApplicable: true,
		},
		ContractDivergent: {
			ContractPartial:       true,
			ContractMatched:       true,
			"verified":            true,
			ContractNotApplicable: true,
		},
		ContractMatched: {
			ContractPartial:   true,
			ContractDivergent: true,
		},
		"verified": {
			ContractPartial:   true,
			ContractDivergent: true,
		},
		ContractNotApplicable: {
			ContractMissing: true,
			ContractPartial: true,
		},
	}
	return allowed[previous][current]
}

func RepositoryRoot(start string) (string, error) {
	directory := start
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	directory = absolute
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("could not find repository root")
		}
		directory = parent
	}
}

func Resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func ValidateRelativePaths(root, label string, paths []string) []string {
	var problems []string
	for _, path := range paths {
		clean, valid := cleanRelativePath(path)
		if !valid {
			problems = append(problems, fmt.Sprintf("%s has invalid local path %q", label, path))
			continue
		}
		if _, err := os.Stat(filepath.Join(root, clean)); err != nil {
			problems = append(problems, fmt.Sprintf("%s references missing local path %q", label, path))
		}
	}
	return problems
}

func ValidateCheckoutPaths(checkout, label string, paths []string) []string {
	var problems []string
	for _, path := range paths {
		referencePath, _, _ := strings.Cut(path, "::")
		clean, valid := cleanRelativePath(filepath.FromSlash(referencePath))
		if !valid {
			problems = append(problems, fmt.Sprintf("%s has invalid upstream path %q", label, path))
			continue
		}
		if _, err := os.Stat(filepath.Join(checkout, clean)); err != nil {
			problems = append(problems, fmt.Sprintf("%s references missing upstream path %q", label, path))
		}
	}
	return problems
}

func cleanRelativePath(path string) (string, bool) {
	clean := filepath.Clean(path)
	if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func CheckoutCommit(checkout string) (string, error) {
	gitDirectory := filepath.Join(checkout, ".git")
	info, err := os.Stat(gitDirectory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(gitDirectory)
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(data))
		if !strings.HasPrefix(line, "gitdir: ") {
			return "", errors.New("invalid .git file")
		}
		gitDirectory = strings.TrimPrefix(line, "gitdir: ")
		if !filepath.IsAbs(gitDirectory) {
			gitDirectory = filepath.Join(checkout, gitDirectory)
		}
	}

	headData, err := os.ReadFile(filepath.Join(gitDirectory, "HEAD"))
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(string(headData))
	if !strings.HasPrefix(head, "ref: ") {
		return head, nil
	}
	reference := strings.TrimPrefix(head, "ref: ")
	if data, err := os.ReadFile(filepath.Join(gitDirectory, filepath.FromSlash(reference))); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	packed, err := os.ReadFile(filepath.Join(gitDirectory, "packed-refs"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == reference {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("cannot resolve Git reference %s", reference)
}

func ValidateCheckoutCommit(checkout, expected string) []string {
	commit, err := CheckoutCommit(checkout)
	if err != nil {
		return []string{fmt.Sprintf("read upstream checkout %s: %v", checkout, err)}
	}
	if commit != expected {
		return []string{fmt.Sprintf("upstream checkout is at %s, expected %s", commit, expected)}
	}
	return nil
}

func ValidateTextCaseReferences(root, label string, references []string) []string {
	var problems []string
	for _, reference := range references {
		path, selector, found := strings.Cut(reference, "::")
		if !found || path == "" || selector == "" {
			problems = append(problems, fmt.Sprintf("%s has invalid case %q; expected path::selector", label, reference))
			continue
		}
		pathProblems := ValidateCheckoutPaths(root, label+" case", []string{path})
		problems = append(problems, pathProblems...)
		if len(pathProblems) > 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s cannot read case file %q", label, path))
			continue
		}
		for _, component := range strings.Split(selector, "::") {
			if !strings.Contains(string(data), component) {
				problems = append(problems, fmt.Sprintf("%s references missing case %q in %s", label, selector, path))
				break
			}
		}
	}
	return problems
}

func ValidateGoTestReferences(root, label string, references []string) []string {
	var problems []string
	for _, reference := range references {
		path, testCase, found := strings.Cut(reference, "::")
		if !found || path == "" || testCase == "" {
			problems = append(problems, fmt.Sprintf("%s has invalid Go test case %q; expected path::TestName[/subtest]", label, reference))
			continue
		}
		pathProblems := ValidateRelativePaths(root, label+" test", []string{path})
		problems = append(problems, pathProblems...)
		if len(pathProblems) > 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s cannot read Go test file %q", label, path))
			continue
		}
		topLevel, subtest, _ := strings.Cut(testCase, "/")
		if !strings.Contains(string(data), "func "+topLevel+"(") {
			problems = append(problems, fmt.Sprintf("%s references missing Go test %s in %s", label, topLevel, path))
		}
		sourceSubtest := strings.ReplaceAll(subtest, "_", " ")
		if subtest != "" && !strings.Contains(string(data), subtest) && !strings.Contains(string(data), sourceSubtest) {
			problems = append(problems, fmt.Sprintf("%s references missing Go subtest %s in %s", label, subtest, path))
		}
	}
	return problems
}

func MakeTargetContent(makefile, target string) string {
	lines := strings.Split(makefile, "\n")
	var content strings.Builder
	for index, line := range lines {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		content.WriteString(line)
		content.WriteByte('\n')
		for next := index + 1; next < len(lines); next++ {
			if lines[next] != "" && !strings.HasPrefix(lines[next], "\t") {
				break
			}
			content.WriteString(lines[next])
			content.WriteByte('\n')
		}
		break
	}
	if content.Len() == 0 {
		return ""
	}
	variablePattern := regexp.MustCompile(`\$\(([^)]+)\)`)
	for _, match := range variablePattern.FindAllStringSubmatch(content.String(), -1) {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, match[1]+" :=") || strings.HasPrefix(trimmed, match[1]+" =") {
				content.WriteString(line)
				content.WriteByte('\n')
				break
			}
		}
	}
	return content.String()
}

func ValidateCertificationLanes(root, label string, lanes, dibraCases []string) []string {
	if len(lanes) == 0 {
		return nil
	}
	makefileData, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return []string{fmt.Sprintf("read Makefile for %s: %v", label, err)}
	}
	laneContents := make(map[string]string, len(lanes))
	var problems []string
	for _, lane := range lanes {
		content := MakeTargetContent(string(makefileData), lane)
		if content == "" {
			problems = append(problems, fmt.Sprintf("%s references missing certification lane %s", label, lane))
		}
		laneContents[lane] = content
	}
	for _, reference := range dibraCases {
		path, selector, found := strings.Cut(reference, "::")
		if !found {
			continue
		}
		slashed := filepath.ToSlash(path)
		if !strings.HasPrefix(slashed, "test/integration/") && !strings.HasPrefix(slashed, "test/deploy_integration/") {
			continue
		}
		topLevel, _, _ := strings.Cut(selector, "/")
		foundInLane := false
		for _, content := range laneContents {
			if strings.Contains(content, topLevel) {
				foundInLane = true
				break
			}
		}
		if !foundInLane {
			problems = append(problems, fmt.Sprintf("%s test %s is absent from certification lanes %s", label, topLevel, strings.Join(lanes, ", ")))
		}
	}
	return problems
}

func ValidatePassedCertification(root, upstreamRoot, label string, assessment Assessment, evidence CertificationEvidence) []string {
	if assessment.CertificationStatus != CertificationPassed {
		return nil
	}
	var problems []string
	if assessment.ContractStatus != ContractMatched && assessment.ContractStatus != ContractNotApplicable {
		problems = append(problems, label+" passed certification without a matched or not-applicable contract")
	}
	if assessment.EvidenceLevel != EvidenceIntegration && assessment.EvidenceLevel != EvidenceDifferential && assessment.EvidenceLevel != "daemon" {
		problems = append(problems, label+" passed certification without integration, daemon, or differential evidence")
	}
	if len(evidence.UpstreamCases) == 0 || len(evidence.DibraCases) == 0 || len(evidence.CertificationLanes) == 0 {
		problems = append(problems, label+" passed certification without exact upstream cases, Dibra cases, or a certification lane")
		return problems
	}
	problems = append(problems, ValidateTextCaseReferences(upstreamRoot, label+" upstream", evidence.UpstreamCases)...)
	problems = append(problems, ValidateGoTestReferences(root, label+" Dibra", evidence.DibraCases)...)
	problems = append(problems, ValidateCertificationLanes(root, label, evidence.CertificationLanes, evidence.DibraCases)...)
	return problems
}

func PythonRawString(data []byte, variable string) ([]byte, error) {
	pattern := regexp.MustCompile(`(?ms)^\s*` + regexp.QuoteMeta(variable) + `\s*=\s*[rRuUbBfF]{0,2}(?:"""(.*?)"""|'''(.*?)''')`)
	match := pattern.FindSubmatch(data)
	if len(match) != 3 {
		return nil, fmt.Errorf("could not find %s triple-quoted string", variable)
	}
	if match[1] != nil {
		return match[1], nil
	}
	return match[2], nil
}

func CheckGenerated(path string, expected []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read generated report: %w", err)
	}
	if !bytes.Equal(current, expected) {
		return fmt.Errorf("%s is stale", path)
	}
	return nil
}

func MarkdownCodeList(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, "`"+strings.ReplaceAll(MarkdownText(value), "`", "\\`")+"`")
	}
	return strings.Join(escaped, "<br>")
}

func MarkdownLink(url, label string) string {
	if url == "" {
		return "—"
	}
	return "[" + MarkdownText(label) + "](" + strings.ReplaceAll(url, ")", "%29") + ")"
}

func MarkdownText(value string) string {
	if value == "" {
		return "—"
	}
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}

func ShortSHA(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func SortedProblems(problems []string, heading string) error {
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%s:\n- %s", heading, strings.Join(problems, "\n- "))
}
