package projects

import "testing"

func TestValidateProjectRepositoryInputAcceptsSupportedRoles(t *testing.T) {
	t.Parallel()

	roles := []string{
		RepositoryRolePrimary,
		RepositoryRoleReference,
		RepositoryRoleContracts,
		RepositoryRoleDocs,
	}

	for _, role := range roles {
		role := role
		t.Run(role, func(t *testing.T) {
			t.Parallel()

			issues := ValidateProjectRepositoryInput(ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "repo-" + role,
				Role:             role,
				LocalPath:        `D:\Code\relay`,
				AllowedRoots:     []string{"internal", "docs/specs"},
				IgnoredGlobs:     []string{"node_modules/**"},
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			})
			if len(issues) != 0 {
				t.Fatalf("expected no issues, got %+v", issues)
			}
		})
	}
}

func TestValidateProjectInputRequiresProjectID(t *testing.T) {
	t.Parallel()

	issues := ValidateProjectInput(ProjectInput{
		Name:   "Relay",
		Status: ProjectStatusActive,
	})
	if len(issues) == 0 {
		t.Fatal("expected validation issues")
	}
	if issues[0].Field != "project_id" {
		t.Fatalf("expected project_id issue, got %+v", issues)
	}
}

func TestValidateProjectRepositoryInputRejectsInvalidCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     ProjectRepositoryInput
		wantField string
	}{
		{
			name: "invalid role",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "relay",
				Role:             "secondary",
				LocalPath:        `D:\Code\relay`,
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			},
			wantField: "role",
		},
		{
			name: "empty repo id",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				Role:             RepositoryRolePrimary,
				LocalPath:        `D:\Code\relay`,
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			},
			wantField: "repo_id",
		},
		{
			name: "unsafe allowed root",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "relay",
				Role:             RepositoryRolePrimary,
				LocalPath:        `D:\Code\relay`,
				AllowedRoots:     []string{"../secrets"},
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			},
			wantField: "allowed_roots",
		},
		{
			name: "unsafe ignored glob absolute path",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "relay",
				Role:             RepositoryRolePrimary,
				LocalPath:        `D:\Code\relay`,
				IgnoredGlobs:     []string{"/private/**"},
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			},
			wantField: "ignored_globs",
		},
		{
			name: "local path with newline",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "relay",
				Role:             RepositoryRolePrimary,
				LocalPath:        "D:\\Code\\relay\nbad",
				MaxFileSizeBytes: DefaultMaxFileSizeBytes,
				Enabled:          true,
			},
			wantField: "local_path",
		},
		{
			name: "max file size below minimum",
			input: ProjectRepositoryInput{
				ProjectID:        "relay",
				RepoID:           "relay",
				Role:             RepositoryRolePrimary,
				LocalPath:        `D:\Code\relay`,
				MaxFileSizeBytes: 512,
				Enabled:          true,
			},
			wantField: "max_file_size_bytes",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			issues := ValidateProjectRepositoryInput(tc.input)
			if len(issues) == 0 {
				t.Fatal("expected validation issues")
			}

			found := false
			for _, issue := range issues {
				if issue.Field == tc.wantField {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected issue for %s, got %+v", tc.wantField, issues)
			}
		})
	}
}
