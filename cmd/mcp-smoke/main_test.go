package main

import "testing"

func TestContainsPathLeakVariants(t *testing.T) {
	tests := []struct {
		name      string
		payload   any
		forbidden string
		want      bool
	}{
		{
			name:      "windows raw path string",
			payload:   `C:\Users\operator\fixture`,
			forbidden: `C:\Users\operator\fixture`,
			want:      true,
		},
		{
			name:      "json escaped windows path",
			payload:   `C:\\Users\\operator\\fixture`,
			forbidden: `C:\Users\operator\fixture`,
			want:      true,
		},
		{
			name:      "forward slash windows path",
			payload:   `C:/Users/operator/fixture`,
			forbidden: `C:\Users\operator\fixture`,
			want:      true,
		},
		{
			name:      "lowercase windows path",
			payload:   `c:\users\operator\fixture`,
			forbidden: `C:\USERS\OPERATOR\FIXTURE`,
			want:      true,
		},
		{
			name:      "posix path",
			payload:   `/tmp/relay-smoke`,
			forbidden: `/tmp/relay-smoke`,
			want:      true,
		},
		{
			name:      "json escaped posix path",
			payload:   `\/tmp\/relay-smoke`,
			forbidden: `/tmp/relay-smoke`,
			want:      true,
		},
		{
			name:      "nested map path",
			payload:   map[string]interface{}{"result": map[string]interface{}{"path": `C:\Users\operator\fixture`}},
			forbidden: `C:\Users\operator\fixture`,
			want:      true,
		},
		{
			name:      "nested array path",
			payload:   []interface{}{map[string]interface{}{"paths": []interface{}{`/tmp/relay-smoke`}}},
			forbidden: `/tmp/relay-smoke`,
			want:      true,
		},
		{
			name:      "safe repo relative path",
			payload:   `cmd/mcp-smoke/main.go`,
			forbidden: `C:\Users\operator\fixture`,
			want:      false,
		},
		{
			name:      "unrelated basename",
			payload:   `fixtures/fixture`,
			forbidden: `C:\Users\operator\fixture`,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var values []string
			collectStringValues(tt.payload, &values)
			if got := containsPathLeak(values, tt.forbidden); got != tt.want {
				t.Fatalf("containsPathLeak() = %v, want %v; values=%q", got, tt.want, values)
			}
		})
	}
}

func TestCheckNoLocalPathLeakDetectsLocalPathField(t *testing.T) {
	h := &harness{}
	h.checkNoLocalPathLeak("payload", map[string]interface{}{
		"result": map[string]interface{}{
			"local_path": "artifact.txt",
		},
	})
	if h.fail != 1 {
		t.Fatalf("fail count = %d, want 1", h.fail)
	}
}

func TestRepositoriesExplicitlyClean(t *testing.T) {
	tests := []struct {
		name string
		repo any
		want bool
	}{
		{
			name: "one repo clean",
			repo: []interface{}{map[string]interface{}{"git_status": map[string]interface{}{"dirty": false}}},
			want: true,
		},
		{
			name: "multiple repos clean",
			repo: []interface{}{
				map[string]interface{}{"git_status": map[string]interface{}{"dirty": false}},
				map[string]interface{}{"git_status": map[string]interface{}{"dirty": false}},
			},
			want: true,
		},
		{
			name: "go exported dirty field clean",
			repo: []interface{}{map[string]interface{}{"git_status": map[string]interface{}{"Dirty": false}}},
			want: true,
		},
		{
			name: "one repo dirty",
			repo: []interface{}{map[string]interface{}{"git_status": map[string]interface{}{"dirty": true}}},
			want: false,
		},
		{
			name: "missing git status",
			repo: []interface{}{map[string]interface{}{"repo_id": "relay"}},
			want: false,
		},
		{
			name: "missing dirty",
			repo: []interface{}{map[string]interface{}{"git_status": map[string]interface{}{"branch": "main"}}},
			want: false,
		},
		{
			name: "non boolean dirty",
			repo: []interface{}{map[string]interface{}{"git_status": map[string]interface{}{"dirty": "false"}}},
			want: false,
		},
		{
			name: "empty repository list",
			repo: []interface{}{},
			want: false,
		},
		{
			name: "malformed repository entry",
			repo: []interface{}{"relay"},
			want: false,
		},
		{
			name: "absent repository list",
			repo: map[string]interface{}{"repositories": nil},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repositoriesExplicitlyClean(tt.repo); got != tt.want {
				t.Fatalf("repositoriesExplicitlyClean() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceSnapshotRepositoriesExplicitlyCleanPrefersFreshnessCounts(t *testing.T) {
	result := map[string]interface{}{
		"freshness_report": map[string]interface{}{
			"repository_count": float64(1),
			"dirty_repo_count": float64(0),
		},
		"repositories": []interface{}{
			map[string]interface{}{"repo_id": "relay"},
		},
	}
	if !sourceSnapshotRepositoryCountExplicit(result) {
		t.Fatal("sourceSnapshotRepositoryCountExplicit() = false, want true")
	}
	if !sourceSnapshotRepositoriesExplicitlyClean(result) {
		t.Fatal("sourceSnapshotRepositoriesExplicitlyClean() = false, want true")
	}

	missingCountsWithCleanRepos := map[string]interface{}{
		"freshness_report": map[string]interface{}{},
		"repositories": []interface{}{
			map[string]interface{}{"git_status": map[string]interface{}{"dirty": false}},
		},
	}
	if !sourceSnapshotRepositoriesExplicitlyClean(missingCountsWithCleanRepos) {
		t.Fatal("sourceSnapshotRepositoriesExplicitlyClean() with missing freshness counts and clean repos = false, want true")
	}

	missingCountsAndRepoCleanliness := map[string]interface{}{
		"freshness_report": map[string]interface{}{},
		"repositories": []interface{}{
			map[string]interface{}{"repo_id": "relay"},
		},
	}
	if sourceSnapshotRepositoriesExplicitlyClean(missingCountsAndRepoCleanliness) {
		t.Fatal("sourceSnapshotRepositoriesExplicitlyClean() with missing freshness counts and repo cleanliness = true, want false")
	}
}
