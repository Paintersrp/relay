package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourcevault"
)

func TestLoadApprovedAuthoritySuccessAndMetadata(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	nestedPath := fixture.sourcePath
	rawBytes := []byte(fmt.Sprintf(" {\n  \"schema_version\": \"1.0\",\n  \"feature_slug\": \"checkout\",\n  \"ticket_id\": \"P2-T2\",\n  \"revision\": 1,\n  \"replaces_revision\": null,\n  \"repo_target\": \"relay\",\n  \"branch\": \"main\",\n  \"base_commit\": %q,\n  \"goal\": \"Package the selected ticket.\",\n  \"context\": \"Package basis context.\",\n  \"scope\": {\"in_scope\":[],\"out_of_scope\":[]},\n  \"depends_on\": [],\n  \"implementation_obligations\": [],\n  \"validation_intent\": [],\n  \"transition_applicability\": \"not_required\",\n  \"completion_criteria\": []\n} ", fixture.baseCommit))

	ctx := context.Background()
	expectedOID := strings.Repeat("e", 40)
	fixture.service.SetSourceVaultsForTest(&customSourceVaultReader{
		path:      nestedPath,
		objectOID: expectedOID,
		bytes:     rawBytes,
	})

	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(ctx, ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}

	authority, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID)
	if err != nil {
		t.Fatalf("LoadApprovedAuthorityForRun failed: %v", err)
	}

	doc := authority.DeliveryTicket
	if doc.DisplayName != "checkout.ticket-P2-T2.r1.delivery-ticket.json" {
		t.Errorf("DisplayName = %q, want %q", doc.DisplayName, "checkout.ticket-P2-T2.r1.delivery-ticket.json")
	}
	if doc.RelativePath != nestedPath {
		t.Errorf("RelativePath = %q, want %q", doc.RelativePath, nestedPath)
	}
	if doc.MediaType != "application/json" {
		t.Errorf("MediaType = %q, want %q", doc.MediaType, "application/json")
	}
	expectedSHA := sha256Hex(rawBytes)
	if doc.SHA256 != expectedSHA {
		t.Errorf("SHA256 = %q, want %q", doc.SHA256, expectedSHA)
	}
	if doc.SizeBytes != int64(len(rawBytes)) {
		t.Errorf("SizeBytes = %d, want %d", doc.SizeBytes, len(rawBytes))
	}
	if doc.ObjectOID != expectedOID {
		t.Errorf("ObjectOID = %q, want %q", doc.ObjectOID, expectedOID)
	}
	if !bytes.Equal(doc.Bytes, rawBytes) {
		t.Errorf("Bytes = %q, want %q", doc.Bytes, rawBytes)
	}
}

func TestLoadApprovedAuthorityRepeatedAndDefensiveCopy(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	first, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(first.DeliveryTicket, second.DeliveryTicket) {
		t.Fatalf("repeated load returned different DeliveryTicket documents:\nfirst: %#v\nsecond: %#v", first.DeliveryTicket, second.DeliveryTicket)
	}

	first.DeliveryTicket.Bytes[0] = 'X'

	third, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.DeliveryTicket.Bytes, third.DeliveryTicket.Bytes) {
		t.Fatal("mutation of returned Bytes affected subsequent load")
	}
}

func TestLoadApprovedAuthorityReadOnlyGuarantee(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	tables := []string{
		"artifacts", "source_vault_retentions", "execution_packages",
		"delivery_ticket_selections", "execution_package_approvals", "runs",
	}
	countsBefore := make(map[string]int64)
	for _, tbl := range tables {
		var cnt int64
		if err := fixture.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		countsBefore[tbl] = cnt
	}

	if _, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID); err != nil {
		t.Fatal(err)
	}

	for _, tbl := range tables {
		var cnt int64
		if err := fixture.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		if cnt != countsBefore[tbl] {
			t.Errorf("table %s count changed from %d to %d", tbl, countsBefore[tbl], cnt)
		}
	}
}

func TestValidateDeliveryTicketSourcePath(t *testing.T) {
	invalidPaths := []string{
		"",
		" tickets/file.json",
		"tickets/file.json ",
		"/tickets/file.json",
		"\\tickets/file.json",
		"C:/tickets/file.json",
		"tickets\\file.json",
		"tickets//file.json",
		"./tickets/file.json",
		"tickets/../file.json",
		"tickets/file\x01.json",
	}

	for _, path := range invalidPaths {
		if err := validateDeliveryTicketSourcePath(path); err == nil {
			t.Errorf("validateDeliveryTicketSourcePath(%q) accepted invalid path", path)
		}
	}

	validPaths := []string{
		"tickets/file.json",
		"tickets/checkout/checkout.ticket-P2-T2.r1.delivery-ticket.json",
	}
	for _, path := range validPaths {
		if err := validateDeliveryTicketSourcePath(path); err != nil {
			t.Errorf("validateDeliveryTicketSourcePath(%q) rejected valid path: %v", path, err)
		}
	}
}

func TestLoadApprovedAuthorityFailures(t *testing.T) {
	validBytes := func(fixture *packageServiceFixture, modify func(map[string]any)) []byte {
		m := map[string]any{
			"schema_version":             "1.0",
			"feature_slug":               "checkout",
			"ticket_id":                  "P2-T2",
			"revision":                   1,
			"replaces_revision":          nil,
			"repo_target":                "relay",
			"branch":                     "main",
			"base_commit":                fixture.baseCommit,
			"goal":                       "Package the selected ticket.",
			"context":                    "Package basis context.",
			"scope":                      map[string]any{"in_scope": []string{}, "out_of_scope": []string{}},
			"depends_on":                 []any{},
			"implementation_obligations": []any{},
			"validation_intent":          []any{},
			"transition_applicability":   "not_required",
			"completion_criteria":        []any{},
		}
		if modify != nil {
			modify(m)
		}
		var buf bytes.Buffer
		buf.WriteString("{")
		first := true
		for k, v := range m {
			if !first {
				buf.WriteString(",")
			}
			first = false
			fmt.Fprintf(&buf, "%q:", k)
			switch val := v.(type) {
			case string:
				fmt.Fprintf(&buf, "%q", val)
			case int:
				fmt.Fprintf(&buf, "%d", val)
			case nil:
				buf.WriteString("null")
			default:
				fmt.Fprintf(&buf, "%v", val)
			}
		}
		buf.WriteString("}")
		return buf.Bytes()
	}

	tests := []struct {
		name   string
		reader SourceVaultReader
	}{
		{
			name: "missing source path",
			reader: &customSourceVaultReader{
				err: &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable},
			},
		},
		{
			name: "source path resolving to a directory",
			reader: &customSourceVaultReader{
				err: &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable},
			},
		},
		{
			name: "malformed JSON",
			reader: &customSourceVaultReader{
				bytes: []byte(`{"schema_version":`),
			},
		},
		{
			name: "non-object JSON string",
			reader: &customSourceVaultReader{
				bytes: []byte(`"just a string"`),
			},
		},
		{
			name: "non-object JSON array",
			reader: &customSourceVaultReader{
				bytes: []byte(`[1, 2, 3]`),
			},
		},
		{
			name: "source Ticket ID mismatch",
			reader: &customSourceVaultReader{
				bytes: validBytes(newPackageServiceFixture(t), func(m map[string]any) { m["ticket_id"] = "WRONG-ID" }),
			},
		},
		{
			name: "revision-number mismatch",
			reader: &customSourceVaultReader{
				bytes: validBytes(newPackageServiceFixture(t), func(m map[string]any) { m["revision"] = 99 }),
			},
		},
		{
			name: "repository identity mismatch repo_target",
			reader: &customSourceVaultReader{
				bytes: validBytes(newPackageServiceFixture(t), func(m map[string]any) { m["repo_target"] = "other-repo" }),
			},
		},
		{
			name: "repository identity mismatch branch",
			reader: &customSourceVaultReader{
				bytes: validBytes(newPackageServiceFixture(t), func(m map[string]any) { m["branch"] = "other-branch" }),
			},
		},
		{
			name: "repository identity mismatch base_commit",
			reader: &customSourceVaultReader{
				bytes: validBytes(newPackageServiceFixture(t), func(m map[string]any) { m["base_commit"] = strings.Repeat("f", 40) }),
			},
		},
		{
			name: "changed or unavailable retained source",
			reader: &customSourceVaultReader{
				err: &sourcevault.Error{Code: sourcevault.CodeVaultUnavailable},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newPackageServiceFixture(t)
			if tt.reader != nil {
				if cr, ok := tt.reader.(*customSourceVaultReader); ok {
					if cr.path == "" {
						cr.path = fixture.sourcePath
					}
				}
				fixture.service.SetSourceVaultsForTest(tt.reader)
			}

			prepared := preparePackage(t, fixture, false)
			approved, err := fixture.service.Approve(context.Background(), ApproveInput{
				PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
			if err == nil {
				t.Fatal("LoadApprovedAuthorityForRun succeeded, want error")
			}
			if !errors.Is(err, ErrApprovedAuthorityInvalid) {
				t.Fatalf("error = %v, want wrapping %v", err, ErrApprovedAuthorityInvalid)
			}
		})
	}
}

type customSourceVaultReader struct {
	path      string
	objectOID string
	bytes     []byte
	err       error
}

func (c *customSourceVaultReader) ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if c.err != nil {
		return sourcevault.ReadPathResult{}, c.err
	}
	if c.path != "" && request.Path != c.path {
		return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	oid := c.objectOID
	if oid == "" {
		oid = strings.Repeat("d", 40)
	}
	return sourcevault.ReadPathResult{
		ObjectOID: oid,
		Bytes:     append([]byte(nil), c.bytes...),
	}, nil
}
