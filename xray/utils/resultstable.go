package utils

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jfrog/gofrog/datastructures"
	"golang.org/x/exp/maps"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/jfrog/jfrog-cli-core/v2/xray/formats"

	"github.com/gookit/color"
	"github.com/jfrog/jfrog-cli-core/v2/utils/coreutils"
	"github.com/jfrog/jfrog-client-go/utils/errorutils"
	"github.com/jfrog/jfrog-client-go/utils/log"
	"github.com/jfrog/jfrog-client-go/xray/services"
)

const (
	rootIndex                  = 0
	directDependencyIndex      = 1
	directDependencyPathLength = 2
)

// PrintViolationsTable prints the violations in 4 tables: security violations, license compliance violations, operational risk violations and ignore rule URLs.
// Set multipleRoots to true in case the given violations array contains (or may contain) results of several projects or files (like in binary scan).
// In case multipleRoots is true, the field Component will show the root of each impact path, otherwise it will show the root's child.
// In case one (or more) of the violations contains the field FailBuild set to true, CliError with exit code 3 will be returned.
// Set printExtended to true to print fields with 'extended' tag.
// If the scan argument is set to true, print the scan tables.
func PrintViolationsTable(violations []services.Violation, extendedResults *ExtendedScanResults, multipleRoots, printExtended, isBinaryScan bool) error {
	securityViolationsRows, licenseViolationsRows, operationalRiskViolationsRows, err := prepareViolations(violations, extendedResults, multipleRoots, true, true)
	if err != nil {
		return err
	}
	// Print tables, if scan is true; print the scan tables.
	if isBinaryScan {
		err = coreutils.PrintTable(formats.ConvertToVulnerabilityScanTableRow(securityViolationsRows), "Security Violations", "No security violations were found", printExtended)
		if err != nil {
			return err
		}
		err = coreutils.PrintTable(formats.ConvertToLicenseViolationScanTableRow(licenseViolationsRows), "License Compliance Violations", "No license compliance violations were found", printExtended)
		if err != nil {
			return err
		}
		if len(operationalRiskViolationsRows) > 0 {
			return coreutils.PrintTable(formats.ConvertToOperationalRiskViolationScanTableRow(operationalRiskViolationsRows), "Operational Risk Violations", "No operational risk violations were found", printExtended)
		}
	} else {
		err = coreutils.PrintTable(formats.ConvertToVulnerabilityTableRow(securityViolationsRows), "Security Violations", "No security violations were found", printExtended)
		if err != nil {
			return err
		}
		err = coreutils.PrintTable(formats.ConvertToLicenseViolationTableRow(licenseViolationsRows), "License Compliance Violations", "No license compliance violations were found", printExtended)
		if err != nil {
			return err
		}
		if len(operationalRiskViolationsRows) > 0 {
			return coreutils.PrintTable(formats.ConvertToOperationalRiskViolationTableRow(operationalRiskViolationsRows), "Operational Risk Violations", "No operational risk violations were found", printExtended)
		}
	}
	return nil
}

// Prepare violations for all non-table formats (without style or emoji)
func PrepareViolations(violations []services.Violation, extendedResults *ExtendedScanResults, multipleRoots, simplifiedOutput bool) ([]formats.VulnerabilityOrViolationRow, []formats.LicenseRow, []formats.OperationalRiskViolationRow, error) {
	return prepareViolations(violations, extendedResults, multipleRoots, false, simplifiedOutput)
}

func prepareViolations(violations []services.Violation, extendedResults *ExtendedScanResults, multipleRoots, isTable, simplifiedOutput bool) ([]formats.VulnerabilityOrViolationRow, []formats.LicenseRow, []formats.OperationalRiskViolationRow, error) {
	if simplifiedOutput {
		violations = simplifyViolations(violations, multipleRoots)
	}
	var securityViolationsRows []formats.VulnerabilityOrViolationRow
	var licenseViolationsRows []formats.LicenseRow
	var operationalRiskViolationsRows []formats.OperationalRiskViolationRow
	for _, violation := range violations {
		impactedPackagesNames, impactedPackagesVersions, impactedPackagesTypes, fixedVersions, components, impactPaths, err := splitComponents(violation.Components)
		if err != nil {
			return nil, nil, nil, err
		}
		switch violation.ViolationType {
		case "security":
			cves := convertCves(violation.Cves)
			applicableValue := getApplicableCveValue(extendedResults, cves)
			currSeverity := GetSeverity(violation.Severity, applicableValue)
			jfrogResearchInfo := convertJfrogResearchInformation(violation.ExtendedInformation)
			for compIndex := 0; compIndex < len(impactedPackagesNames); compIndex++ {
				securityViolationsRows = append(securityViolationsRows,
					formats.VulnerabilityOrViolationRow{
						Summary: violation.Summary,
						ImpactedDependencyDetails: formats.ImpactedDependencyDetails{
							SeverityDetails:           formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
							ImpactedDependencyName:    impactedPackagesNames[compIndex],
							ImpactedDependencyVersion: impactedPackagesVersions[compIndex],
							ImpactedDependencyType:    impactedPackagesTypes[compIndex],
							Components:                components[compIndex],
						},
						FixedVersions:            fixedVersions[compIndex],
						Cves:                     cves,
						IssueId:                  violation.IssueId,
						References:               violation.References,
						JfrogResearchInformation: jfrogResearchInfo,
						ImpactPaths:              impactPaths[compIndex],
						Technology:               coreutils.Technology(violation.Technology),
						Applicable:               printApplicableCveValue(applicableValue, isTable),
					},
				)
			}
		case "license":
			currSeverity := GetSeverity(violation.Severity, ApplicabilityUndetermined)
			for compIndex := 0; compIndex < len(impactedPackagesNames); compIndex++ {
				licenseViolationsRows = append(licenseViolationsRows,
					formats.LicenseRow{
						LicenseKey: violation.LicenseKey,
						ImpactedDependencyDetails: formats.ImpactedDependencyDetails{
							SeverityDetails:           formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
							ImpactedDependencyName:    impactedPackagesNames[compIndex],
							ImpactedDependencyVersion: impactedPackagesVersions[compIndex],
							ImpactedDependencyType:    impactedPackagesTypes[compIndex],
							Components:                components[compIndex],
						},
					},
				)
			}
		case "operational_risk":
			currSeverity := GetSeverity(violation.Severity, ApplicabilityUndetermined)
			violationOpRiskData := getOperationalRiskViolationReadableData(violation)
			for compIndex := 0; compIndex < len(impactedPackagesNames); compIndex++ {
				operationalRiskViolationsRow := &formats.OperationalRiskViolationRow{
					ImpactedDependencyDetails: formats.ImpactedDependencyDetails{
						SeverityDetails:           formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
						ImpactedDependencyName:    impactedPackagesNames[compIndex],
						ImpactedDependencyVersion: impactedPackagesVersions[compIndex],
						ImpactedDependencyType:    impactedPackagesTypes[compIndex],
						Components:                components[compIndex],
					},
					IsEol:         violationOpRiskData.isEol,
					Cadence:       violationOpRiskData.cadence,
					Commits:       violationOpRiskData.commits,
					Committers:    violationOpRiskData.committers,
					NewerVersions: violationOpRiskData.newerVersions,
					LatestVersion: violationOpRiskData.latestVersion,
					RiskReason:    violationOpRiskData.riskReason,
					EolMessage:    violationOpRiskData.eolMessage,
				}
				operationalRiskViolationsRows = append(operationalRiskViolationsRows, *operationalRiskViolationsRow)
			}
		default:
			// Unsupported type, ignore
		}
	}

	// Sort the rows by severity and whether the row contains fixed versions
	sortVulnerabilityOrViolationRows(securityViolationsRows)
	sort.Slice(licenseViolationsRows, func(i, j int) bool {
		return licenseViolationsRows[i].SeverityNumValue > licenseViolationsRows[j].SeverityNumValue
	})
	sort.Slice(operationalRiskViolationsRows, func(i, j int) bool {
		return operationalRiskViolationsRows[i].SeverityNumValue > operationalRiskViolationsRows[j].SeverityNumValue
	})

	return securityViolationsRows, licenseViolationsRows, operationalRiskViolationsRows, nil
}

// PrintVulnerabilitiesTable prints the vulnerabilities in a table.
// Set multipleRoots to true in case the given vulnerabilities array contains (or may contain) results of several projects or files (like in binary scan).
// In case multipleRoots is true, the field Component will show the root of each impact path, otherwise it will show the root's child.
// Set printExtended to true to print fields with 'extended' tag.
// If the scan argument is set to true, print the scan tables.
func PrintVulnerabilitiesTable(vulnerabilities []services.Vulnerability, extendedResults *ExtendedScanResults, multipleRoots, printExtended, isBinaryScan bool) error {
	vulnerabilitiesRows, err := prepareVulnerabilities(vulnerabilities, extendedResults, multipleRoots, true, true)
	if err != nil {
		return err
	}

	if isBinaryScan {
		return coreutils.PrintTable(formats.ConvertToVulnerabilityScanTableRow(vulnerabilitiesRows), "Vulnerable Components", "✨ No vulnerable components were found ✨", printExtended)
	}
	var emptyTableMessage string
	if len(extendedResults.ScannedTechnologies) > 0 {
		emptyTableMessage = "✨ No vulnerable dependencies were found ✨"
	} else {
		emptyTableMessage = coreutils.PrintYellow("🔧 Couldn't determine a package manager or build tool used by this project 🔧")
	}
	return coreutils.PrintTable(formats.ConvertToVulnerabilityTableRow(vulnerabilitiesRows), "Vulnerable Dependencies", emptyTableMessage, printExtended)
}

// Prepare vulnerabilities for all non-table formats (without style or emoji)
func PrepareVulnerabilities(vulnerabilities []services.Vulnerability, extendedResults *ExtendedScanResults, multipleRoots, simplifiedOutput bool) ([]formats.VulnerabilityOrViolationRow, error) {
	return prepareVulnerabilities(vulnerabilities, extendedResults, multipleRoots, false, simplifiedOutput)
}

func prepareVulnerabilities(vulnerabilities []services.Vulnerability, extendedResults *ExtendedScanResults, multipleRoots, isTable, simplifiedOutput bool) ([]formats.VulnerabilityOrViolationRow, error) {
	if simplifiedOutput {
		vulnerabilities = simplifyVulnerabilities(vulnerabilities, multipleRoots)
	}
	var vulnerabilitiesRows []formats.VulnerabilityOrViolationRow
	for _, vulnerability := range vulnerabilities {
		impactedPackagesNames, impactedPackagesVersions, impactedPackagesTypes, fixedVersions, components, impactPaths, err := splitComponents(vulnerability.Components)
		if err != nil {
			return nil, err
		}
		cves := convertCves(vulnerability.Cves)
		applicableValue := getApplicableCveValue(extendedResults, cves)
		currSeverity := GetSeverity(vulnerability.Severity, applicableValue)
		jfrogResearchInfo := convertJfrogResearchInformation(vulnerability.ExtendedInformation)
		for compIndex := 0; compIndex < len(impactedPackagesNames); compIndex++ {
			vulnerabilitiesRows = append(vulnerabilitiesRows,
				formats.VulnerabilityOrViolationRow{
					Summary: vulnerability.Summary,
					ImpactedDependencyDetails: formats.ImpactedDependencyDetails{
						SeverityDetails:           formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
						ImpactedDependencyName:    impactedPackagesNames[compIndex],
						ImpactedDependencyVersion: impactedPackagesVersions[compIndex],
						ImpactedDependencyType:    impactedPackagesTypes[compIndex],
						Components:                components[compIndex],
					},
					FixedVersions:            fixedVersions[compIndex],
					Cves:                     cves,
					IssueId:                  vulnerability.IssueId,
					References:               vulnerability.References,
					JfrogResearchInformation: jfrogResearchInfo,
					ImpactPaths:              impactPaths[compIndex],
					Technology:               coreutils.Technology(vulnerability.Technology),
					Applicable:               printApplicableCveValue(applicableValue, isTable),
				},
			)
		}
	}

	sortVulnerabilityOrViolationRows(vulnerabilitiesRows)
	return vulnerabilitiesRows, nil
}

func sortVulnerabilityOrViolationRows(rows []formats.VulnerabilityOrViolationRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SeverityNumValue != rows[j].SeverityNumValue {
			return rows[i].SeverityNumValue > rows[j].SeverityNumValue
		}
		return len(rows[i].FixedVersions) > 0 && len(rows[j].FixedVersions) > 0
	})
}

// PrintLicensesTable prints the licenses in a table.
// Set multipleRoots to true in case the given licenses array contains (or may contain) results of several projects or files (like in binary scan).
// In case multipleRoots is true, the field Component will show the root of each impact path, otherwise it will show the root's child.
// Set printExtended to true to print fields with 'extended' tag.
// If the scan argument is set to true, print the scan tables.
func PrintLicensesTable(licenses []services.License, printExtended, isBinaryScan bool) error {
	licensesRows, err := PrepareLicenses(licenses)
	if err != nil {
		return err
	}
	if isBinaryScan {
		return coreutils.PrintTable(formats.ConvertToLicenseScanTableRow(licensesRows), "Licenses", "No licenses were found", printExtended)
	}
	return coreutils.PrintTable(formats.ConvertToLicenseTableRow(licensesRows), "Licenses", "No licenses were found", printExtended)
}

func PrepareLicenses(licenses []services.License) ([]formats.LicenseRow, error) {
	var licensesRows []formats.LicenseRow
	for _, license := range licenses {
		impactedPackagesNames, impactedPackagesVersions, impactedPackagesTypes, _, components, impactPaths, err := splitComponents(license.Components)
		if err != nil {
			return nil, err
		}
		for compIndex := 0; compIndex < len(impactedPackagesNames); compIndex++ {
			licensesRows = append(licensesRows,
				formats.LicenseRow{
					LicenseKey:  license.Key,
					ImpactPaths: impactPaths[compIndex],
					ImpactedDependencyDetails: formats.ImpactedDependencyDetails{
						ImpactedDependencyName:    impactedPackagesNames[compIndex],
						ImpactedDependencyVersion: impactedPackagesVersions[compIndex],
						ImpactedDependencyType:    impactedPackagesTypes[compIndex],
						Components:                components[compIndex],
					},
				},
			)
		}
	}

	return licensesRows, nil
}

// Prepare secrets for all non-table formats (without style or emoji)
func PrepareSecrets(secrets []SourceCodeScanResult) []formats.SourceCodeRow {
	return prepareSecrets(secrets, false)
}

func prepareSecrets(secrets []SourceCodeScanResult, isTable bool) []formats.SourceCodeRow {
	var secretsRows []formats.SourceCodeRow
	for _, secret := range secrets {
		currSeverity := GetSeverity(secret.Severity, Applicable)
		secretsRows = append(secretsRows,
			formats.SourceCodeRow{
				SeverityDetails: formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
				SourceCodeLocationRow: formats.SourceCodeLocationRow{
					File:       secret.File,
					LineColumn: secret.LineColumn,
					Text:       secret.Text,
				},
				Type: secret.Type,
			},
		)
	}

	sort.Slice(secretsRows, func(i, j int) bool {
		return secretsRows[i].SeverityNumValue > secretsRows[j].SeverityNumValue
	})

	return secretsRows
}

func PrintSecretsTable(secrets []SourceCodeScanResult, entitledForSecretsScan bool) error {
	if entitledForSecretsScan {
		secretsRows := prepareSecrets(secrets, true)
		log.Output()
		return coreutils.PrintTable(formats.ConvertToSecretsTableRow(secretsRows), "Secret Detection",
			"✨ No secrets were found ✨", false)
	}
	return nil
}

// Prepare iacs for all non-table formats (without style or emoji)
func PrepareIacs(iacs []SourceCodeScanResult) []formats.SourceCodeRow {
	return prepareIacs(iacs, false)
}

func prepareIacs(iacs []SourceCodeScanResult, isTable bool) []formats.SourceCodeRow {
	var iacRows []formats.SourceCodeRow
	for _, iac := range iacs {
		currSeverity := GetSeverity(iac.Severity, Applicable)
		iacRows = append(iacRows,
			formats.SourceCodeRow{
				SeverityDetails: formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
				SourceCodeLocationRow: formats.SourceCodeLocationRow{
					File:       iac.File,
					LineColumn: iac.LineColumn,
					Text:       iac.Text,
				},
				Type: iac.Type,
			},
		)
	}

	sort.Slice(iacRows, func(i, j int) bool {
		return iacRows[i].SeverityNumValue > iacRows[j].SeverityNumValue
	})

	return iacRows
}

func PrintIacTable(iacs []SourceCodeScanResult, entitledForIacScan bool) error {
	if entitledForIacScan {
		iacRows := prepareIacs(iacs, true)
		log.Output()
		return coreutils.PrintTable(formats.ConvertToIacTableRow(iacRows), "Infrastructure as Code Vulnerabilities",
			"✨ No Infrastructure as Code vulnerabilities were found ✨", false)
	}
	return nil
}

func PrepareSast(sasts []SourceCodeScanResult) []formats.SourceCodeRow {
	return prepareSast(sasts, false)
}

func prepareSast(sasts []SourceCodeScanResult, isTable bool) []formats.SourceCodeRow {
	var sastRows []formats.SourceCodeRow
	for _, sast := range sasts {
		currSeverity := GetSeverity(sast.Severity, Applicable)
		sastRows = append(sastRows,
			formats.SourceCodeRow{
				SeverityDetails: formats.SeverityDetails{Severity: currSeverity.printableTitle(isTable), SeverityNumValue: currSeverity.NumValue()},
				SourceCodeLocationRow: formats.SourceCodeLocationRow{
					File:       sast.File,
					LineColumn: sast.LineColumn,
					Text:       sast.Text,
				},
				Type:     sast.Type,
				CodeFlow: toSourceCodeCodeFlowRow(sast, isTable),
			},
		)
	}

	sort.Slice(sastRows, func(i, j int) bool {
		return sastRows[i].SeverityNumValue > sastRows[j].SeverityNumValue
	})

	return sastRows
}

func toSourceCodeCodeFlowRow(result SourceCodeScanResult, isTable bool) (flows [][]formats.SourceCodeLocationRow) {
	if isTable {
		// Not displaying in table
		return
	}
	for _, flowStack := range result.CodeFlow {
		rowFlow := []formats.SourceCodeLocationRow{}
		for _, location := range *flowStack {
			rowFlow = append(rowFlow, formats.SourceCodeLocationRow{
				File:       location.File,
				LineColumn: location.LineColumn,
				Text:       location.Text,
			})
		}
		flows = append(flows, rowFlow)
	}
	return
}

func PrintSastTable(sast []SourceCodeScanResult, entitledForSastScan bool) error {
	if entitledForSastScan {
		sastRows := prepareSast(sast, true)
		log.Output()
		return coreutils.PrintTable(formats.ConvertToSastTableRow(sastRows), "Static Application Security Testing (SAST)",
			"✨ No Static Application Security Testing vulnerabilities were found ✨", false)
	}
	return nil
}

func convertCves(cves []services.Cve) []formats.CveRow {
	var cveRows []formats.CveRow
	for _, cveObj := range cves {
		cveRows = append(cveRows, formats.CveRow{Id: cveObj.Id, CvssV2: cveObj.CvssV2Score, CvssV3: cveObj.CvssV3Score})
	}
	return cveRows
}

func convertJfrogResearchInformation(extendedInfo *services.ExtendedInformation) *formats.JfrogResearchInformation {
	if extendedInfo == nil {
		return nil
	}
	var severityReasons []formats.JfrogResearchSeverityReason
	for _, severityReason := range extendedInfo.JfrogResearchSeverityReasons {
		severityReasons = append(severityReasons, formats.JfrogResearchSeverityReason{
			Name:        severityReason.Name,
			Description: severityReason.Description,
			IsPositive:  severityReason.IsPositive,
		})
	}
	return &formats.JfrogResearchInformation{
		Summary:         extendedInfo.ShortDescription,
		Details:         extendedInfo.FullDescription,
		SeverityDetails: formats.SeverityDetails{Severity: extendedInfo.JfrogResearchSeverity},
		SeverityReasons: severityReasons,
		Remediation:     extendedInfo.Remediation,
	}
}

func splitComponents(impactedPackages map[string]services.Component) (impactedPackagesNames, impactedPackagesVersions, impactedPackagesTypes []string, fixedVersions [][]string, directComponents [][]formats.ComponentRow, impactPaths [][][]formats.ComponentRow, err error) {
	if len(impactedPackages) == 0 {
		err = errorutils.CheckErrorf("failed while parsing the response from Xray: violation doesn't have any components")
		return
	}
	for currCompId, currComp := range impactedPackages {
		currCompName, currCompVersion, currCompType := SplitComponentId(currCompId)
		impactedPackagesNames = append(impactedPackagesNames, currCompName)
		impactedPackagesVersions = append(impactedPackagesVersions, currCompVersion)
		impactedPackagesTypes = append(impactedPackagesTypes, currCompType)
		fixedVersions = append(fixedVersions, currComp.FixedVersions)
		currDirectComponents, currImpactPaths := getDirectComponentsAndImpactPaths(currComp.ImpactPaths)
		directComponents = append(directComponents, currDirectComponents)
		impactPaths = append(impactPaths, currImpactPaths)
	}
	return
}

var packageTypes = map[string]string{
	"gav":      "Maven",
	"docker":   "Docker",
	"rpm":      "RPM",
	"deb":      "Debian",
	"nuget":    "NuGet",
	"generic":  "Generic",
	"npm":      "npm",
	"pip":      "Python",
	"pypi":     "Python",
	"composer": "Composer",
	"go":       "Go",
	"alpine":   "Alpine",
}

// SplitComponentId splits a Xray component ID to the component name, version and package type.
// In case componentId doesn't contain a version, the returned version will be an empty string.
// In case componentId's format is invalid, it will be returned as the component name
// and empty strings will be returned instead of the version and the package type.
// Examples:
//  1. componentId: "gav://antparent:ant:1.6.5"
//     Returned values:
//     Component name: "antparent:ant"
//     Component version: "1.6.5"
//     Package type: "Maven"
//  2. componentId: "generic://sha256:244fd47e07d1004f0aed9c156aa09083c82bf8944eceb67c946ff7430510a77b/foo.jar"
//     Returned values:
//     Component name: "foo.jar"
//     Component version: ""
//     Package type: "Generic"
//  3. componentId: "invalid-comp-id"
//     Returned values:
//     Component name: "invalid-comp-id"
//     Component version: ""
//     Package type: ""
func SplitComponentId(componentId string) (string, string, string) {
	compIdParts := strings.Split(componentId, "://")
	// Invalid component ID
	if len(compIdParts) != 2 {
		return componentId, "", ""
	}

	packageType := compIdParts[0]
	packageId := compIdParts[1]

	// Generic identifier structure: generic://sha256:<Checksum>/name
	if packageType == "generic" {
		lastSlashIndex := strings.LastIndex(packageId, "/")
		return packageId[lastSlashIndex+1:], "", packageTypes[packageType]
	}

	var compName, compVersion string
	switch packageType {
	case "rpm":
		// RPM identifier structure: rpm://os-version:package:epoch-version:version
		// os-version is optional.
		splitCompId := strings.Split(packageId, ":")
		if len(splitCompId) >= 3 {
			compName = splitCompId[len(splitCompId)-3]
			compVersion = fmt.Sprintf("%s:%s", splitCompId[len(splitCompId)-2], splitCompId[len(splitCompId)-1])
		}
	default:
		// All other identifiers look like this: package-type://package-name:version.
		// Sometimes there's a namespace or a group before the package name, separated by a '/' or a ':'.
		lastColonIndex := strings.LastIndex(packageId, ":")

		if lastColonIndex != -1 {
			compName = packageId[:lastColonIndex]
			compVersion = packageId[lastColonIndex+1:]
		}
	}

	// If there's an error while parsing the component ID
	if compName == "" {
		compName = packageId
	}

	return compName, compVersion, packageTypes[packageType]
}

// Gets a slice of the direct dependencies or packages of the scanned component, that depends on the vulnerable package, and converts the impact paths.
func getDirectComponentsAndImpactPaths(impactPaths [][]services.ImpactPathNode) (components []formats.ComponentRow, impactPathsRows [][]formats.ComponentRow) {
	componentsMap := make(map[string]formats.ComponentRow)

	// The first node in the impact path is the scanned component itself. The second one is the direct dependency.
	impactPathLevel := 1
	for _, impactPath := range impactPaths {
		impactPathIndex := impactPathLevel
		if len(impactPath) <= impactPathLevel {
			impactPathIndex = len(impactPath) - 1
		}
		componentId := impactPath[impactPathIndex].ComponentId
		if _, exist := componentsMap[componentId]; !exist {
			compName, compVersion, _ := SplitComponentId(componentId)
			componentsMap[componentId] = formats.ComponentRow{Name: compName, Version: compVersion}
		}

		// Convert the impact path
		var compImpactPathRows []formats.ComponentRow
		for _, pathNode := range impactPath {
			nodeCompName, nodeCompVersion, _ := SplitComponentId(pathNode.ComponentId)
			compImpactPathRows = append(compImpactPathRows, formats.ComponentRow{
				Name:    nodeCompName,
				Version: nodeCompVersion,
			})
		}
		impactPathsRows = append(impactPathsRows, compImpactPathRows)
	}

	for _, row := range componentsMap {
		components = append(components, row)
	}
	return
}

type TableSeverity struct {
	formats.SeverityDetails
	style color.Style
	emoji string
}

func (s *TableSeverity) printableTitle(isTable bool) string {
	if isTable && (log.IsStdOutTerminal() && log.IsColorsSupported() || os.Getenv("GITLAB_CI") != "") {
		return s.style.Render(s.emoji + s.Severity)
	}
	return s.Severity
}

var Severities = map[string]map[ApplicabilityStatus]*TableSeverity{
	"Critical": {
		Applicable:                {SeverityDetails: formats.SeverityDetails{Severity: "Critical", SeverityNumValue: 15}, emoji: "💀", style: color.New(color.BgLightRed, color.LightWhite)},
		ApplicabilityUndetermined: {SeverityDetails: formats.SeverityDetails{Severity: "Critical", SeverityNumValue: 14}, emoji: "💀", style: color.New(color.BgLightRed, color.LightWhite)},
		NotApplicable:             {SeverityDetails: formats.SeverityDetails{Severity: "Critical", SeverityNumValue: 5}, emoji: "💀", style: color.New(color.Gray)},
	},
	"High": {
		Applicable:                {SeverityDetails: formats.SeverityDetails{Severity: "High", SeverityNumValue: 13}, emoji: "🔥", style: color.New(color.Red)},
		ApplicabilityUndetermined: {SeverityDetails: formats.SeverityDetails{Severity: "High", SeverityNumValue: 12}, emoji: "🔥", style: color.New(color.Red)},
		NotApplicable:             {SeverityDetails: formats.SeverityDetails{Severity: "High", SeverityNumValue: 4}, emoji: "🔥", style: color.New(color.Gray)},
	},
	"Medium": {
		Applicable:                {SeverityDetails: formats.SeverityDetails{Severity: "Medium", SeverityNumValue: 11}, emoji: "🎃", style: color.New(color.Yellow)},
		ApplicabilityUndetermined: {SeverityDetails: formats.SeverityDetails{Severity: "Medium", SeverityNumValue: 10}, emoji: "🎃", style: color.New(color.Yellow)},
		NotApplicable:             {SeverityDetails: formats.SeverityDetails{Severity: "Medium", SeverityNumValue: 3}, emoji: "🎃", style: color.New(color.Gray)},
	},
	"Low": {
		Applicable:                {SeverityDetails: formats.SeverityDetails{Severity: "Low", SeverityNumValue: 9}, emoji: "👻"},
		ApplicabilityUndetermined: {SeverityDetails: formats.SeverityDetails{Severity: "Low", SeverityNumValue: 8}, emoji: "👻"},
		NotApplicable:             {SeverityDetails: formats.SeverityDetails{Severity: "Low", SeverityNumValue: 2}, emoji: "👻", style: color.New(color.Gray)},
	},
	"Unknown": {
		Applicable:                {SeverityDetails: formats.SeverityDetails{Severity: "Unknown", SeverityNumValue: 7}, emoji: "😐"},
		ApplicabilityUndetermined: {SeverityDetails: formats.SeverityDetails{Severity: "Unknown", SeverityNumValue: 6}, emoji: "😐"},
		NotApplicable:             {SeverityDetails: formats.SeverityDetails{Severity: "Unknown", SeverityNumValue: 1}, emoji: "😐", style: color.New(color.Gray)},
	},
}

func (s *TableSeverity) NumValue() int {
	return s.SeverityNumValue
}

func (s *TableSeverity) Emoji() string {
	return s.emoji
}

func GetSeveritiesFormat(severity string) (string, error) {
	formattedSeverity := cases.Title(language.Und).String(severity)
	if formattedSeverity != "" && Severities[formattedSeverity][Applicable] == nil {
		return "", errorutils.CheckErrorf("only the following severities are supported: " + coreutils.ListToText(maps.Keys(Severities)))
	}

	return formattedSeverity, nil
}

func GetSeverity(severityTitle string, applicable ApplicabilityStatus) *TableSeverity {
	if Severities[severityTitle] == nil {
		return &TableSeverity{SeverityDetails: formats.SeverityDetails{Severity: severityTitle}}
	}

	switch applicable {
	case NotApplicable:
		return Severities[severityTitle][NotApplicable]
	case Applicable:
		return Severities[severityTitle][Applicable]
	default:
		return Severities[severityTitle][ApplicabilityUndetermined]
	}
}

type operationalRiskViolationReadableData struct {
	isEol         string
	cadence       string
	commits       string
	committers    string
	eolMessage    string
	riskReason    string
	latestVersion string
	newerVersions string
}

func getOperationalRiskViolationReadableData(violation services.Violation) *operationalRiskViolationReadableData {
	isEol, cadence, commits, committers, newerVersions, latestVersion := "N/A", "N/A", "N/A", "N/A", "N/A", "N/A"
	if violation.IsEol != nil {
		isEol = strconv.FormatBool(*violation.IsEol)
	}
	if violation.Cadence != nil {
		cadence = strconv.FormatFloat(*violation.Cadence, 'f', -1, 64)
	}
	if violation.Committers != nil {
		committers = strconv.FormatInt(int64(*violation.Committers), 10)
	}
	if violation.Commits != nil {
		commits = strconv.FormatInt(*violation.Commits, 10)
	}
	if violation.NewerVersions != nil {
		newerVersions = strconv.FormatInt(int64(*violation.NewerVersions), 10)
	}
	if violation.LatestVersion != "" {
		latestVersion = violation.LatestVersion
	}
	return &operationalRiskViolationReadableData{
		isEol:         isEol,
		cadence:       cadence,
		commits:       commits,
		committers:    committers,
		eolMessage:    violation.EolMessage,
		riskReason:    violation.RiskReason,
		latestVersion: latestVersion,
		newerVersions: newerVersions,
	}
}

// simplifyVulnerabilities returns a new slice of services.Vulnerability that contains only the unique vulnerabilities from the input slice
// The uniqueness of the vulnerabilities is determined by the GetUniqueKey function
func simplifyVulnerabilities(scanVulnerabilities []services.Vulnerability, multipleRoots bool) []services.Vulnerability {
	var uniqueVulnerabilities = make(map[string]*services.Vulnerability)
	for _, vulnerability := range scanVulnerabilities {
		for vulnerableComponentId := range vulnerability.Components {
			vulnerableDependency, vulnerableVersion, _ := SplitComponentId(vulnerableComponentId)
			packageKey := GetUniqueKey(vulnerableDependency, vulnerableVersion, vulnerability.IssueId, len(vulnerability.Components[vulnerableComponentId].FixedVersions) > 0)
			if uniqueVulnerability, exist := uniqueVulnerabilities[packageKey]; exist {
				fixedVersions := appendUniqueFixVersions(uniqueVulnerability.Components[vulnerableComponentId].FixedVersions, vulnerability.Components[vulnerableComponentId].FixedVersions...)
				impactPaths := appendUniqueImpactPaths(uniqueVulnerability.Components[vulnerableComponentId].ImpactPaths, vulnerability.Components[vulnerableComponentId].ImpactPaths, multipleRoots)
				uniqueVulnerabilities[packageKey].Components[vulnerableComponentId] = services.Component{
					FixedVersions: fixedVersions,
					ImpactPaths:   impactPaths,
				}
				continue
			}
			uniqueVulnerabilities[packageKey] = &services.Vulnerability{
				Cves:                vulnerability.Cves,
				Severity:            vulnerability.Severity,
				Components:          map[string]services.Component{vulnerableComponentId: vulnerability.Components[vulnerableComponentId]},
				IssueId:             vulnerability.IssueId,
				Technology:          vulnerability.Technology,
				ExtendedInformation: vulnerability.ExtendedInformation,
				Summary:             vulnerability.Summary,
			}
		}
	}
	// convert map to slice
	result := make([]services.Vulnerability, 0, len(uniqueVulnerabilities))
	for _, v := range uniqueVulnerabilities {
		result = append(result, *v)
	}
	return result
}

// simplifyViolations returns a new slice of services.Violations that contains only the unique violations from the input slice
// The uniqueness of the violations is determined by the GetUniqueKey function
func simplifyViolations(scanViolations []services.Violation, multipleRoots bool) []services.Violation {
	var uniqueViolations = make(map[string]*services.Violation)
	for _, violation := range scanViolations {
		for vulnerableComponentId := range violation.Components {
			vulnerableDependency, vulnerableVersion, _ := SplitComponentId(vulnerableComponentId)
			packageKey := GetUniqueKey(vulnerableDependency, vulnerableVersion, violation.IssueId, len(violation.Components[vulnerableComponentId].FixedVersions) > 0)
			if uniqueVulnerability, exist := uniqueViolations[packageKey]; exist {
				fixedVersions := appendUniqueFixVersions(uniqueVulnerability.Components[vulnerableComponentId].FixedVersions, violation.Components[vulnerableComponentId].FixedVersions...)
				impactPaths := appendUniqueImpactPaths(uniqueVulnerability.Components[vulnerableComponentId].ImpactPaths, violation.Components[vulnerableComponentId].ImpactPaths, multipleRoots)
				uniqueViolations[packageKey].Components[vulnerableComponentId] = services.Component{
					FixedVersions: fixedVersions,
					ImpactPaths:   impactPaths,
				}
				continue
			}
			uniqueViolations[packageKey] = &services.Violation{
				Severity:      violation.Severity,
				ViolationType: violation.ViolationType,
				Components:    map[string]services.Component{vulnerableComponentId: violation.Components[vulnerableComponentId]},
				WatchName:     violation.WatchName,
				IssueId:       violation.IssueId,
				Cves:          violation.Cves,
				LicenseKey:    violation.LicenseKey,
				LicenseName:   violation.LicenseName,
				Technology:    violation.Technology,
			}
		}
	}
	// convert map to slice
	result := make([]services.Violation, 0, len(uniqueViolations))
	for _, v := range uniqueViolations {
		result = append(result, *v)
	}
	return result
}

// appendImpactPathsWithoutDuplicates appends the elements of a source [][]ImpactPathNode struct to a target [][]ImpactPathNode, without adding any duplicate elements.
// This implementation uses the ComponentId field of the ImpactPathNode struct to check for duplicates, as it is guaranteed to be unique.
func appendUniqueImpactPaths(target [][]services.ImpactPathNode, source [][]services.ImpactPathNode, multipleRoots bool) [][]services.ImpactPathNode {
	if multipleRoots {
		return appendUniqueImpactPathsForMultipleRoots(target, source)
	}
	impactPathMap := make(map[string][]services.ImpactPathNode)
	for _, path := range target {
		// The first node component id is the key and the value is the whole path
		key := getImpactPathKey(path)
		impactPathMap[key] = path
	}

	for _, path := range source {
		key := getImpactPathKey(path)
		if _, exists := impactPathMap[key]; !exists {
			impactPathMap[key] = path
			target = append(target, path)
		}
	}
	return target
}

// getImpactPathKey return a key that is used as a key to identify and deduplicate impact paths.
// If an impact path length is equal to directDependencyPathLength, then the direct dependency is the key, and it's in the directDependencyIndex place.
func getImpactPathKey(path []services.ImpactPathNode) string {
	key := path[rootIndex].ComponentId
	if len(path) == directDependencyPathLength {
		key = path[directDependencyIndex].ComponentId
	}
	return key
}

// appendUniqueImpactPathsForMultipleRoots appends the source impact path to the target impact path while avoiding duplicates.
// Specifically, it is designed for handling multiple root projects, such as Maven or Gradle, by comparing each pair of paths and identifying the path that is closest to the direct dependency.
func appendUniqueImpactPathsForMultipleRoots(target [][]services.ImpactPathNode, source [][]services.ImpactPathNode) [][]services.ImpactPathNode {
	for targetPathIndex, targetPath := range target {
		for sourcePathIndex, sourcePath := range source {
			var subset []services.ImpactPathNode
			if len(sourcePath) <= len(targetPath) {
				subset = isImpactPathIsSubset(targetPath, sourcePath)
				if len(subset) != 0 {
					target[targetPathIndex] = subset
				}
			} else {
				subset = isImpactPathIsSubset(sourcePath, targetPath)
				if len(subset) != 0 {
					source[sourcePathIndex] = subset
				}
			}
		}
	}

	return appendUniqueImpactPaths(target, source, false)
}

// isImpactPathIsSubset checks if targetPath is a subset of sourcePath, and returns the subset if exists
func isImpactPathIsSubset(target []services.ImpactPathNode, source []services.ImpactPathNode) []services.ImpactPathNode {
	var subsetImpactPath []services.ImpactPathNode
	impactPathNodesMap := make(map[string]bool)
	for _, node := range target {
		impactPathNodesMap[node.ComponentId] = true
	}

	for _, node := range source {
		if impactPathNodesMap[node.ComponentId] {
			subsetImpactPath = append(subsetImpactPath, node)
		}
	}

	if len(subsetImpactPath) == len(target) || len(subsetImpactPath) == len(source) {
		return subsetImpactPath
	}
	return []services.ImpactPathNode{}
}

// appendUniqueFixVersions returns a new slice of strings that contains elements from both input slices without duplicates
func appendUniqueFixVersions(targetFixVersions []string, sourceFixVersions ...string) []string {
	fixVersionsSet := datastructures.MakeSet[string]()
	var result []string
	for _, fixVersion := range sourceFixVersions {
		fixVersionsSet.Add(fixVersion)
		result = append(result, fixVersion)
	}

	for _, fixVersion := range targetFixVersions {
		if exist := fixVersionsSet.Exists(fixVersion); !exist {
			result = append(result, fixVersion)
		}
	}
	return result
}

// GetUniqueKey returns a unique string key of format "vulnerableDependency:vulnerableVersion:xrayID:fixVersionExist"
func GetUniqueKey(vulnerableDependency, vulnerableVersion, xrayID string, fixVersionExist bool) string {
	return strings.Join([]string{vulnerableDependency, vulnerableVersion, xrayID, strconv.FormatBool(fixVersionExist)}, ":")
}

// If at least one cve is applicable - final value is applicable
// Else if at least one cve is undetermined - final value is undetermined
// Else (case when all cves aren't applicable) -> final value is not applicable
func getApplicableCveValue(extendedResults *ExtendedScanResults, xrayCves []formats.CveRow) ApplicabilityStatus {
	if !extendedResults.EntitledForJas || len(extendedResults.ApplicabilityScanResults) == 0 {
		return NotScanned
	}

	if len(xrayCves) == 0 {
		return ApplicabilityUndetermined
	}
	cveExistsInResult := false
	finalApplicableValue := NotApplicable
	for _, cve := range xrayCves {
		if currentCveApplicableValue, exists := extendedResults.ApplicabilityScanResults[cve.Id]; exists {
			cveExistsInResult = true
			if currentCveApplicableValue == Applicable {
				return currentCveApplicableValue
			} else if currentCveApplicableValue == ApplicabilityUndetermined {
				finalApplicableValue = currentCveApplicableValue
			}
		}
	}
	if cveExistsInResult {
		return finalApplicableValue
	}
	return ApplicabilityUndetermined
}

func printApplicableCveValue(applicableValue ApplicabilityStatus, isTable bool) string {
	if applicableValue == NotScanned {
		return NotScanned.String()
	}

	if isTable && (log.IsStdOutTerminal() && log.IsColorsSupported() || os.Getenv("GITLAB_CI") != "") {
		if applicableValue == Applicable {
			return color.New(color.Red).Render(applicableValue)
		} else if applicableValue == NotApplicable {
			return color.New(color.Green).Render(applicableValue)
		}
	}
	return Applicable.String()
}
