package agentrefs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type FrontendAPICallEntry struct {
	FunctionName string `json:"functionName"`
	Method       string `json:"method"`
	PathTemplate string `json:"pathTemplate"`
	SourceFile   string `json:"sourceFile"`
	ResponseType string `json:"responseType,omitempty"`
	RequestType  string `json:"requestType,omitempty"`
	FeatureArea  string `json:"featureArea"`
}

type FrontendQueryBindingEntry struct {
	QueryKey      string `json:"queryKey"`
	QueryFunction string `json:"queryFunction"`
	SourceFile    string `json:"sourceFile"`
	FeatureArea   string `json:"featureArea"`
}

type TypeDTOAlignmentEntry struct {
	FrontendType      string   `json:"frontendType"`
	FrontendFile      string   `json:"frontendFile"`
	BackendType       string   `json:"backendType"`
	BackendFile       string   `json:"backendFile"`
	MatchedFields     []string `json:"matchedFields"`
	FrontendOnlyFields []string `json:"frontendOnlyFields"`
	BackendOnlyFields  []string `json:"backendOnlyFields"`
}

type RouteContractMatchEntry struct {
	FunctionName        string `json:"functionName"`
	Method              string `json:"method"`
	FrontendPathTemplate string `json:"frontendPathTemplate"`
	BackendPathTemplate  string `json:"backendPathTemplate"`
	BackendHandler      string `json:"backendHandler"`
	BackendSourceFile   string `json:"backendSourceFile"`
}

type RouteContractMismatchEntry struct {
	FunctionName   string `json:"functionName"`
	Method         string `json:"method"`
	FrontendPathTemplate string `json:"frontendPathTemplate"`
	SourceFile     string `json:"sourceFile"`
	Reason         string `json:"reason"`
}

type FrontendBackendContractInventory struct {
	FrontendCalls      []FrontendAPICallEntry       `json:"frontendCalls"`
	QueryBindings      []FrontendQueryBindingEntry  `json:"queryBindings"`
	TypeDTOAlignments  []TypeDTOAlignmentEntry      `json:"typeDTOAlignments"`
	RouteMatches       []RouteContractMatchEntry    `json:"routeMatches"`
	RouteMismatches    []RouteContractMismatchEntry `json:"routeMismatches"`
}

var frontendRunSourceFiles = []string{
	"apps/web/src/features/relay-runs/api.ts",
	"apps/web/src/features/relay-runs/types.ts",
	"apps/web/src/features/relay-runs/queries.ts",
}

var frontendPlanSourceFiles = []string{
	"apps/web/src/features/relay-plans/api.ts",
	"apps/web/src/features/relay-plans/types.ts",
	"apps/web/src/features/relay-plans/queries.ts",
}

var backendDTOSourceFiles = []string{
	"internal/api/runs/dto.go",
	"internal/api/plans/dto.go",
}

func allFrontendSourceFiles() []string {
	result := make([]string, 0, len(frontendRunSourceFiles)+len(frontendPlanSourceFiles))
	result = append(result, frontendRunSourceFiles...)
	result = append(result, frontendPlanSourceFiles...)
	return result
}

func ScanFrontendBackendContracts(repoRoot string) (*FrontendBackendContractInventory, error) {
	inv := &FrontendBackendContractInventory{}

	scanAPI := func(apiFile, featureArea string) error {
		absPath := filepath.Join(repoRoot, apiFile)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}
		content := string(data)
		relPath := apiFile

		calls := extractAPICalls(content, relPath, featureArea)
		inv.FrontendCalls = append(inv.FrontendCalls, calls...)
		return nil
	}

	scanQueries := func(queryFile, featureArea string) error {
		absPath := filepath.Join(repoRoot, queryFile)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}
		content := string(data)
		relPath := queryFile

		bindings := extractQueryBindings(content, relPath, featureArea)
		inv.QueryBindings = append(inv.QueryBindings, bindings...)
		return nil
	}

	scanAPI("apps/web/src/features/relay-runs/api.ts", "relay-runs")
	scanAPI("apps/web/src/features/relay-plans/api.ts", "relay-plans")

	scanQueries("apps/web/src/features/relay-runs/queries.ts", "relay-runs")
	scanQueries("apps/web/src/features/relay-plans/queries.ts", "relay-plans")

	sort.Slice(inv.FrontendCalls, func(i, j int) bool {
		if inv.FrontendCalls[i].FeatureArea != inv.FrontendCalls[j].FeatureArea {
			return inv.FrontendCalls[i].FeatureArea < inv.FrontendCalls[j].FeatureArea
		}
		return inv.FrontendCalls[i].FunctionName < inv.FrontendCalls[j].FunctionName
	})

	sort.Slice(inv.QueryBindings, func(i, j int) bool {
		if inv.QueryBindings[i].FeatureArea != inv.QueryBindings[j].FeatureArea {
			return inv.QueryBindings[i].FeatureArea < inv.QueryBindings[j].FeatureArea
		}
		return inv.QueryBindings[i].QueryKey < inv.QueryBindings[j].QueryKey
	})

	return inv, nil
}

func extractAPICalls(content, sourceFile, featureArea string) []FrontendAPICallEntry {
	var calls []FrontendAPICallEntry

	expFnRe := regexp.MustCompile(`export\s+(?:async\s+)?function\s+(\w+)`)
	lines := strings.Split(content, "\n")

	type segment struct {
		fnName string
		start  int
		end    int
	}
	var segments []segment
	for i, line := range lines {
		m := expFnRe.FindStringSubmatch(line)
		if m != nil {
			segments = append(segments, segment{fnName: m[1], start: i})
		}
	}
	for i := 0; i < len(segments); i++ {
		s := segments[i]
		if i+1 < len(segments) {
			s.end = segments[i+1].start
		} else {
			s.end = len(lines)
		}
		body := strings.Join(lines[s.start:s.end], "\n")

		call := scanSingleAPICall(s.fnName, body, sourceFile, featureArea)
		if call != nil {
			calls = append(calls, *call)
		}
	}

	return calls
}

func scanSingleAPICall(fnName, body, sourceFile, featureArea string) *FrontendAPICallEntry {
	getPathRe := regexp.MustCompile(`(?:getJson|getPlanJson)\s*<\s*\w+\s*>\s*\(\s*` + "`" + `([^` + "`" + `]*)` + "`" + ``)
	if m := getPathRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       "GET",
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	postPathRe := regexp.MustCompile(`(?:postJson|postPlanJson)\s*<\s*[\w,\s]*\s*>\s*\(\s*` + "`" + `([^` + "`" + `]*)` + "`" + ``)
	if m := postPathRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       "POST",
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	fetchPathRe := regexp.MustCompile(`fetch\s*\(\s*` + "`" + `([^` + "`" + `]*\/api\/[^` + "`" + `]*)` + "`" + ``)
	if m := fetchPathRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		method := "GET"
		lowerBody := strings.ToLower(body)
		if strings.Contains(lowerBody, `method:"post"`) || strings.Contains(lowerBody, `method: "post"`) {
			method = "POST"
		} else if strings.Contains(lowerBody, `method:"put"`) || strings.Contains(lowerBody, `method: "put"`) {
			method = "PUT"
		} else if strings.Contains(lowerBody, `method:"delete"`) || strings.Contains(lowerBody, `method: "delete"`) {
			method = "DELETE"
		}
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       method,
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	fetchStrRe := regexp.MustCompile(`fetch\s*\(\s*"([^"]*\/(api\/[^"]+))"`)
	if m := fetchStrRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		method := "GET"
		lowerBody := strings.ToLower(body)
		if strings.Contains(lowerBody, `method:"post"`) || strings.Contains(lowerBody, `method: "post"`) {
			method = "POST"
		} else if strings.Contains(lowerBody, `method:"put"`) || strings.Contains(lowerBody, `method: "put"`) {
			method = "PUT"
		} else if strings.Contains(lowerBody, `method:"delete"`) || strings.Contains(lowerBody, `method: "delete"`) {
			method = "DELETE"
		}
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       method,
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	getJsonStrRe := regexp.MustCompile(`(?:getJson|getPlanJson)\s*<\s*\w+\s*>\s*\(\s*"([^"]*)"\s*\)`)
	if m := getJsonStrRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       "GET",
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	postJsonStrRe := regexp.MustCompile(`(?:postJson|postPlanJson)\s*<\s*[\w,\s]*\s*>\s*\(\s*"([^"]*)"\s*`)
	if m := postJsonStrRe.FindStringSubmatch(body); m != nil {
		path := normalizePathTemplate(m[1])
		return &FrontendAPICallEntry{
			FunctionName: fnName,
			Method:       "POST",
			PathTemplate: path,
			SourceFile:   sourceFile,
			FeatureArea:  featureArea,
		}
	}

	return nil
}

func normalizePathTemplate(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	return normalizeTemplateParams(path)
}

func normalizeTemplateParams(path string) string {
	re := regexp.MustCompile(`\$\{[^}]+\}`)
	return re.ReplaceAllString(path, "{param}")
}

func extractQueryBindings(content, sourceFile, featureArea string) []FrontendQueryBindingEntry {
	var bindings []FrontendQueryBindingEntry

	keyFactoryRe := regexp.MustCompile(`export\s+const\s+(\w+Keys?)\s*=\s*\{`)
	if m := keyFactoryRe.FindStringSubmatch(content); m != nil {
		keyFactoryName := m[1]

		keyDefRe := regexp.MustCompile(`(\w+)\s*:\s*\([^)]*\)\s*=>\s*\[`)
		matches := keyDefRe.FindAllStringSubmatch(content, -1)
		seen := make(map[string]bool)
		for _, m := range matches {
			keyName := m[1]
			compositeKey := keyFactoryName + "." + keyName
			if !seen[compositeKey] {
				seen[compositeKey] = true
				bindings = append(bindings, FrontendQueryBindingEntry{
					QueryKey:      compositeKey,
					QueryFunction: "",
					SourceFile:    sourceFile,
					FeatureArea:   featureArea,
				})
			}
		}
	}

	queryFnRe := regexp.MustCompile(`queryOptions\s*\(\s*\{[^}]*queryFn\s*:\s*(\w+)`)
	matches := queryFnRe.FindAllStringSubmatch(content, -1)
	seenQF := make(map[string]bool)
	for _, m := range matches {
		apiFunc := m[1]
		if !seenQF[apiFunc] {
			seenQF[apiFunc] = true
			bindings = append(bindings, FrontendQueryBindingEntry{
				QueryKey:      "",
				QueryFunction: apiFunc,
				SourceFile:    sourceFile,
				FeatureArea:   featureArea,
			})
		}
	}

	for i := range bindings {
		if bindings[i].QueryKey == "" {
			continue
		}
		for _, b := range bindings {
			if b.QueryFunction != "" && bindings[i].QueryFunction == "" {
				bindings[i].QueryFunction = b.QueryFunction
			}
		}
	}

	var filtered []FrontendQueryBindingEntry
	for _, b := range bindings {
		if b.QueryKey != "" {
			filtered = append(filtered, b)
		}
	}

	return filtered
}

func scanDTOFields(sourceFile, repoRoot string) ([]structFieldEntry, error) {
	absPath := filepath.Join(repoRoot, sourceFile)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var entries []structFieldEntry

	structRe := regexp.MustCompile(`type\s+(\w+)\s+struct\s*\{([^}]+)\}`)
	matches := structRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		structName := m[1]
		body := m[2]

		fieldRe := regexp.MustCompile(`(\w+)\s+[^` + "`" + `]*` + "`" + `(?:json:"([^"]*)")`)
		fieldMatches := fieldRe.FindAllStringSubmatch(body, -1)
		seen := make(map[string]bool)
		var fields []string
		for _, fm := range fieldMatches {
			name := fm[2]
			if name != "" && name != "-" {
				if !seen[name] {
					seen[name] = true
					fields = append(fields, name)
				}
			}
		}
		if len(fields) > 0 {
			entries = append(entries, structFieldEntry{
				structName: structName,
				fields:     fields,
			})
		}
	}

	return entries, nil
}

type structFieldEntry struct {
	structName string
	fields     []string
}

func scanTSInterfaces(sourceFile, repoRoot string) ([]structFieldEntry, error) {
	absPath := filepath.Join(repoRoot, sourceFile)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var entries []structFieldEntry

	ifaceRe := regexp.MustCompile(`export\s+interface\s+(\w+)\s*\{([^}]*(?:\{[^}]*\}[^}]*)*)\}`)
	matches := ifaceRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		ifaceName := m[1]
		body := m[2]

		fieldRe := regexp.MustCompile(`(\w+)(?:\??\s*:\s*(?:[^;]+))`)
		fieldMatches := fieldRe.FindAllStringSubmatch(body, -1)
		seen := make(map[string]bool)
		var fields []string
		for _, fm := range fieldMatches {
			name := fm[1]
			if !seen[name] {
				seen[name] = true
				fields = append(fields, name)
			}
		}
		if len(fields) > 0 {
			entries = append(entries, structFieldEntry{
				structName: ifaceName,
				fields:     fields,
			})
		}
	}

	return entries, nil
}

type dtoTypePair struct {
	frontendType string
	backendType  string
	frontendFile string
	backendFile  string
}

func computeDTOAlignments(repoRoot string) []TypeDTOAlignmentEntry {
	var alignments []TypeDTOAlignmentEntry

	pairs := []dtoTypePair{
		{"RelayRun", "RelayRun", "apps/web/src/features/relay-runs/types.ts", "internal/api/runs/dto.go"},
		{"RelayArtifact", "RelayArtifact", "apps/web/src/features/relay-runs/types.ts", "internal/api/runs/dto.go"},
		{"RelayRunEvent", "RelayRunEvent", "apps/web/src/features/relay-runs/types.ts", "internal/api/runs/dto.go"},
		{"PlanAPIPlan", "PlanAPIPlan", "apps/web/src/features/relay-plans/types.ts", "internal/api/plans/dto.go"},
		{"PlanAPIPass", "PlanAPIPass", "apps/web/src/features/relay-plans/types.ts", "internal/api/plans/dto.go"},
		{"PlanAPIReadPlan", "PlanAPIReadPlan", "apps/web/src/features/relay-plans/types.ts", "internal/api/plans/dto.go"},
	}

	for _, pair := range pairs {
		alignment := computeSingleAlignment(pair, repoRoot)
		alignments = append(alignments, alignment)
	}

	planReadAPIResponse, _ := scanDTOFieldEntry("PlanReadAPIResponse", "internal/api/plans/dto.go", repoRoot)
	planDetailResponse, _ := scanTSIfaceEntry("PlanDetailResponse", "apps/web/src/features/relay-plans/types.ts", repoRoot)
	if planReadAPIResponse != nil && planDetailResponse != nil {
		alignment := compareFieldSets("PlanDetailResponse", "apps/web/src/features/relay-plans/types.ts",
			"PlanReadAPIResponse", "internal/api/plans/dto.go",
			planDetailResponse.fields, planReadAPIResponse.fields)
		alignments = append(alignments, alignment)
	}

	planListResponse, _ := scanTSIfaceEntry("PlanListResponse", "apps/web/src/features/relay-plans/types.ts", repoRoot)
	if planReadAPIResponse != nil && planListResponse != nil {
		alignment := compareFieldSets("PlanListResponse", "apps/web/src/features/relay-plans/types.ts",
			"PlanReadAPIResponse", "internal/api/plans/dto.go",
			planListResponse.fields, planReadAPIResponse.fields)
		alignments = append(alignments, alignment)
	}

	sort.Slice(alignments, func(i, j int) bool {
		return alignments[i].FrontendType < alignments[j].FrontendType
	})

	return alignments
}

func scanDTOFieldEntry(structName, sourceFile, repoRoot string) (*structFieldEntry, error) {
	absPath := filepath.Join(repoRoot, sourceFile)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	re := regexp.MustCompile(`type\s+` + regexp.QuoteMeta(structName) + `\s+struct\s*\{([^}]+)\}`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return nil, fmt.Errorf("struct %s not found in %s", structName, sourceFile)
	}

	body := m[1]
	fieldRe := regexp.MustCompile(`(\w+)\s+[^` + "`" + `]*` + "`" + `(?:json:"([^"]*)")`)
	fieldMatches := fieldRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var fields []string
	for _, fm := range fieldMatches {
		name := fm[2]
		if name != "" && name != "-" {
			if !seen[name] {
				seen[name] = true
				fields = append(fields, name)
			}
		}
	}

	return &structFieldEntry{structName: structName, fields: fields}, nil
}

func scanTSIfaceEntry(ifaceName, sourceFile, repoRoot string) (*structFieldEntry, error) {
	absPath := filepath.Join(repoRoot, sourceFile)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	re := regexp.MustCompile(`export\s+interface\s+` + regexp.QuoteMeta(ifaceName) + `\s*\{([^}]+)\}`)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return nil, fmt.Errorf("interface %s not found in %s", ifaceName, sourceFile)
	}

	body := m[1]
	fieldRe := regexp.MustCompile(`(\w+)(?:\??)\s*:\s*(?:[^;]+)`)
	fieldMatches := fieldRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var fields []string
	for _, fm := range fieldMatches {
		name := fm[1]
		if !seen[name] {
			seen[name] = true
			fields = append(fields, name)
		}
	}

	return &structFieldEntry{structName: ifaceName, fields: fields}, nil
}

func computeSingleAlignment(pair dtoTypePair, repoRoot string) TypeDTOAlignmentEntry {
	fe, _ := scanTSIfaceEntry(pair.frontendType, pair.frontendFile, repoRoot)
	be, _ := scanDTOFieldEntry(pair.backendType, pair.backendFile, repoRoot)

	if fe == nil {
		fe = &structFieldEntry{structName: pair.frontendType, fields: nil}
	}
	if be == nil {
		be = &structFieldEntry{structName: pair.backendType, fields: nil}
	}

	return compareFieldSets(pair.frontendType, pair.frontendFile, pair.backendType, pair.backendFile, fe.fields, be.fields)
}

func compareFieldSets(frontendType, frontendFile, backendType, backendFile string, frontendFields, backendFields []string) TypeDTOAlignmentEntry {
	feSet := make(map[string]bool)
	for _, f := range frontendFields {
		feSet[f] = true
	}
	beSet := make(map[string]bool)
	for _, f := range backendFields {
		beSet[f] = true
	}

	var matched []string
	var feOnly []string
	var beOnly []string

	for _, f := range frontendFields {
		if beSet[f] {
			matched = append(matched, f)
		} else {
			feOnly = append(feOnly, f)
		}
	}
	for _, f := range backendFields {
		if !feSet[f] {
			beOnly = append(beOnly, f)
		}
	}

	sort.Strings(matched)
	sort.Strings(feOnly)
	sort.Strings(beOnly)

	return TypeDTOAlignmentEntry{
		FrontendType:       frontendType,
		FrontendFile:       frontendFile,
		BackendType:        backendType,
		BackendFile:        backendFile,
		MatchedFields:      matched,
		FrontendOnlyFields: feOnly,
		BackendOnlyFields:  beOnly,
	}
}

func buildRouteContractMatches(inv *FrontendBackendContractInventory, httpInv *HTTPAPIInventory) {
	if httpInv == nil {
		for _, call := range inv.FrontendCalls {
			inv.RouteMismatches = append(inv.RouteMismatches, RouteContractMismatchEntry{
				FunctionName:        call.FunctionName,
				Method:              call.Method,
				FrontendPathTemplate: call.PathTemplate,
				SourceFile:          call.SourceFile,
				Reason:              "no_backend_route_match",
			})
		}
		return
	}

	backendIndex := buildBackendRouteIndex(httpInv)

	for _, call := range inv.FrontendCalls {
		key := normalizeMethod(call.Method) + " " + normalizeBackendPath(call.PathTemplate)
		if br, ok := backendIndex[key]; ok {
			inv.RouteMatches = append(inv.RouteMatches, RouteContractMatchEntry{
				FunctionName:        call.FunctionName,
				Method:              call.Method,
				FrontendPathTemplate: call.PathTemplate,
				BackendPathTemplate:  br.Path,
				BackendHandler:      br.Handler,
				BackendSourceFile:   br.SourceFile,
			})
		} else {
			inv.RouteMismatches = append(inv.RouteMismatches, RouteContractMismatchEntry{
				FunctionName:        call.FunctionName,
				Method:              call.Method,
				FrontendPathTemplate: call.PathTemplate,
				SourceFile:          call.SourceFile,
				Reason:              "no_backend_route_match",
			})
		}
	}

	sort.Slice(inv.RouteMatches, func(i, j int) bool {
		if inv.RouteMatches[i].Method != inv.RouteMatches[j].Method {
			return inv.RouteMatches[i].Method < inv.RouteMatches[j].Method
		}
		return inv.RouteMatches[i].FrontendPathTemplate < inv.RouteMatches[j].FrontendPathTemplate
	})

	sort.Slice(inv.RouteMismatches, func(i, j int) bool {
		if inv.RouteMismatches[i].Method != inv.RouteMismatches[j].Method {
			return inv.RouteMismatches[i].Method < inv.RouteMismatches[j].Method
		}
		return inv.RouteMismatches[i].FrontendPathTemplate < inv.RouteMismatches[j].FrontendPathTemplate
	})
}

func buildBackendRouteIndex(httpInv *HTTPAPIInventory) map[string]HTTPAPIRouteEntry {
	index := make(map[string]HTTPAPIRouteEntry)
	for _, r := range httpInv.Routes {
		prefix := ""
		if r.Group != "" {
			prefix = r.Group
		}
		fullPath := prefix + r.Path
		fullPath = normalizeBackendPath(fullPath)
		key := normalizeMethod(r.Method) + " " + fullPath
		if _, exists := index[key]; !exists {
			index[key] = r
		}
	}
	return index
}

func normalizeMethod(method string) string {
	return strings.ToUpper(method)
}

func normalizeBackendPath(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	re := regexp.MustCompile(`\{[^}]+\}`)
	return re.ReplaceAllString(path, "{param}")
}

func BuildFrontendBackendContractDoc(repoRoot string) (*ReferenceDocument, error) {
	inv, err := ScanFrontendBackendContracts(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan frontend backend contracts: %w", err)
	}

	inv.TypeDTOAlignments = computeDTOAlignments(repoRoot)

	httpInv, httpErr := ScanHTTPAPISurface(repoRoot)
	if httpErr == nil && len(httpInv.Routes) > 0 {
		buildRouteContractMatches(inv, httpInv)
	} else {
		buildRouteContractMatches(inv, nil)
	}

	var sourceInputs []SourceInput
	seenPaths := make(map[string]bool)

	addSourceInput := func(path string, role string) {
		if seenPaths[path] {
			return
		}
		seenPaths[path] = true
		hash, err := ComputeSHA256(filepath.Join(repoRoot, path))
		if err != nil {
			hash = "unavailable"
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   path,
			SHA256: hash,
			Role:   role,
		})
	}

	for _, sf := range allFrontendSourceFiles() {
		absPath := filepath.Join(repoRoot, sf)
		if _, statErr := os.Stat(absPath); statErr == nil {
			addSourceInput(sf, "frontend source")
		}
	}
	for _, dtoFile := range backendDTOSourceFiles {
		absPath := filepath.Join(repoRoot, dtoFile)
		if _, statErr := os.Stat(absPath); statErr == nil {
			addSourceInput(dtoFile, "backend DTO source")
		}
	}
	addSourceInput("internal/agentrefs/http_api.go", "backend route scanner source")

	httpAPIExists := false
	httpAPIJSONPath := filepath.Join(repoRoot, HTTPAPISurfaceJSONPath)
	if _, statErr := os.Stat(httpAPIJSONPath); statErr == nil {
		addSourceInput(HTTPAPISurfaceJSONPath, "generated HTTP API reference")
		httpAPIExists = true
	}

	var facts []Fact

	factOrdinal := 0
	apiCallCounters := make(map[string]int)

	for _, call := range inv.FrontendCalls {
		factOrdinal++
		slug := strings.ToLower(strings.ReplaceAll(call.FeatureArea+"-"+call.FunctionName, "_", "-"))
		if c, ok := apiCallCounters[slug]; ok {
			apiCallCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			apiCallCounters[slug] = 0
		}
		factID := fmt.Sprintf("frontend-api-call-%s", slug)

		statement := fmt.Sprintf("Frontend API call %s: %s %s in %s (%s)",
			call.FunctionName, call.Method, call.PathTemplate, call.FeatureArea, call.SourceFile)

		var evidence []Evidence
		if err := ValidateRepoRelativePath(call.SourceFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: call.SourceFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	queryKeyCounters := make(map[string]int)
	for _, qb := range inv.QueryBindings {
		factOrdinal++
		slug := qb.QueryKey
		slug = strings.ToLower(slug)
		slug = strings.ReplaceAll(slug, ".", "-")
		if c, ok := queryKeyCounters[slug]; ok {
			queryKeyCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			queryKeyCounters[slug] = 0
		}
		factID := fmt.Sprintf("frontend-query-key-%s", slug)

		statement := fmt.Sprintf("Frontend query key %s bound in %s", qb.QueryKey, qb.SourceFile)
		if qb.QueryFunction != "" {
			statement += fmt.Sprintf(" calling %s", qb.QueryFunction)
		}

		var evidence []Evidence
		if err := ValidateRepoRelativePath(qb.SourceFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: qb.SourceFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelDerived,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	matchCounters := make(map[string]int)
	for _, rm := range inv.RouteMatches {
		factOrdinal++
		slug := strings.ToLower(rm.FunctionName)
		if c, ok := matchCounters[slug]; ok {
			matchCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			matchCounters[slug] = 0
		}
		factID := fmt.Sprintf("frontend-backend-match-%s", slug)

		statement := fmt.Sprintf("Route match: frontend %s (%s %s) -> backend %s %s handled by %s in %s",
			rm.FunctionName, rm.Method, rm.FrontendPathTemplate,
			rm.Method, rm.BackendPathTemplate,
			rm.BackendHandler, rm.BackendSourceFile)

		var evidence []Evidence
		if err := ValidateRepoRelativePath(rm.BackendSourceFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: rm.BackendSourceFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	mismatchCounters := make(map[string]int)
	for _, rm := range inv.RouteMismatches {
		factOrdinal++
		slug := strings.ToLower(rm.FunctionName)
		if c, ok := mismatchCounters[slug]; ok {
			mismatchCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			mismatchCounters[slug] = 0
		}
		factID := fmt.Sprintf("frontend-backend-unmatched-%s", slug)

		statement := fmt.Sprintf("Unmatched frontend route: %s %s %s (%s)",
			rm.FunctionName, rm.Method, rm.FrontendPathTemplate, rm.Reason)

		var evidence []Evidence
		if err := ValidateRepoRelativePath(rm.SourceFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: rm.SourceFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelConflict,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	alignCounters := make(map[string]int)
	for _, al := range inv.TypeDTOAlignments {
		factOrdinal++

		hasDrift := len(al.FrontendOnlyFields) > 0 || len(al.BackendOnlyFields) > 0

		slug := strings.ToLower(al.FrontendType + "-" + al.BackendType)
		if c, ok := alignCounters[slug]; ok {
			alignCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			alignCounters[slug] = 0
		}

		var factID string
		var label FactLabel
		var statement string

		if hasDrift {
			factID = fmt.Sprintf("frontend-type-drift-%s", slug)
			label = FactLabelConflict
			statement = fmt.Sprintf("DTO drift: %s (TS, %s) vs %s (Go, %s) - matched=%d, frontend-only=%v, backend-only=%v",
				al.FrontendType, al.FrontendFile, al.BackendType, al.BackendFile,
				len(al.MatchedFields), al.FrontendOnlyFields, al.BackendOnlyFields)
		} else {
			factID = fmt.Sprintf("frontend-type-dto-%s", slug)
			label = FactLabelProven
			statement = fmt.Sprintf("DTO alignment: %s (TS, %s) matches %s (Go, %s) with %d matched fields",
				al.FrontendType, al.FrontendFile, al.BackendType, al.BackendFile, len(al.MatchedFields))
		}

		var evidence []Evidence
		if err := ValidateRepoRelativePath(al.FrontendFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: al.FrontendFile})
		}
		if err := ValidateRepoRelativePath(al.BackendFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: al.BackendFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     label,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	if !httpAPIExists {
		facts = append(facts, Fact{
			ID:        "frontend-backend-http-api-reference-missing",
			Label:     FactLabelUnresolved,
			Statement: "Generated HTTP API reference docs/generated/agent-references/http-api-surface.json is missing. Backend route matching may be incomplete.",
		})
	}

	labels := []FactLabel{
		FactLabelProven,
		FactLabelDerived,
		FactLabelConvention,
		FactLabelUnresolved,
		FactLabelConflict,
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "frontend-backend-contract",
		Repo: RepoIdentity{
			ProjectID: "relay",
			RepoID:    "Paintersrp/relay",
			Branch:    "main",
		},
		GeneratedBy: GeneratorIdentity{
			Name:    "relay-agentrefs",
			Version: "0.1.0",
		},
		Rendering: RenderingContract{
			JSONPrimary:       true,
			MarkdownFromJSON:  true,
			DeterministicSort: true,
			NoTimestamps:      true,
			RelativePathsOnly: true,
		},
		SourceInputs: sourceInputs,
		FactLabels:   labels,
		Facts:        facts,
		References:   []ReferenceEntry{},
	}

	return doc, nil
}
