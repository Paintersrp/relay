package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"

	executionpackages "relay/internal/app/packages"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

var executorConstructors = []struct {
	name       string
	value      any
	returnsErr bool
}{
	{"NewExecutionAssignmentService", NewExecutionAssignmentService, true},
	{"NewDeterministicOutcomeService", NewDeterministicOutcomeService, true},
	{"NewEffectiveExecutorBriefService", NewEffectiveExecutorBriefService, true},
	{"NewAdaptiveExecutionAttemptService", NewAdaptiveExecutionAttemptService, true},
	{"NewAdaptiveDispatchAdmissionService", NewAdaptiveDispatchAdmissionService, true},
	{"NewPackageDeterministicExecutionService", NewPackageDeterministicExecutionService, true},
	{"NewPackageWorkflowPreparationService", NewPackageWorkflowPreparationService, true},
	{"NewWorkflowExecutionService", NewWorkflowExecutionService, false},
}

func TestExecutorConstructorsAreNotVariadic(t *testing.T) {
	for _, c := range executorConstructors {
		if reflect.TypeOf(c.value).IsVariadic() {
			t.Errorf("%s is variadic", c.name)
		}
	}
}

func TestErrorReturningConstructorsRejectNilReader(t *testing.T) {
	readerType := reflect.TypeOf((*executionpackages.SourceVaultReader)(nil)).Elem()
	for _, c := range executorConstructors {
		if !c.returnsErr {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := workflowstore.Open(root+"/workflow.sqlite", root+"/artifacts")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			fn := reflect.ValueOf(c.value)
			result := fn.Call([]reflect.Value{
				reflect.ValueOf(store),
				reflect.Zero(readerType),
			})
			if err := result[1].Interface(); err == nil {
				t.Fatal("nil reader was accepted")
			}
		})
	}
}

func TestErrorReturningConstructorsSucceedWithNonNilReader(t *testing.T) {
	readerType := reflect.TypeOf((*executionpackages.SourceVaultReader)(nil)).Elem()
	for _, c := range executorConstructors {
		if !c.returnsErr {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := workflowstore.Open(root+"/workflow.sqlite", root+"/artifacts")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			fn := reflect.ValueOf(c.value)
			reader := newUnavailableSourceVaultReader()
			result := fn.Call([]reflect.Value{
				reflect.ValueOf(store),
				reflect.ValueOf(reader).Convert(readerType),
			})
			if err := result[1].Interface(); err != nil {
				t.Fatalf("constructor failed: %v", err)
			}
			if result[0].IsNil() {
				t.Fatal("service is nil")
			}
		})
	}
}

func TestWorkflowExecutionServicePassesReaderToNestedPackageServices(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	reader := fixture.sourceVaultReader
	service := NewWorkflowExecutionService(fixture.store, nil, "reader-test", reader)
	if service.packagePreparation == nil {
		t.Fatal("packagePreparation was not initialized")
	}
	if service.adaptiveAdmission == nil {
		t.Fatal("adaptiveAdmission was not initialized")
	}
	prepareExecutionAssignment(t, fixture)
	if _, err := service.packagePreparation.Prepare(context.Background(), PackageWorkflowPreparationInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"}); err != nil {
		t.Fatalf("packagePreparation cannot use the reader: %v", err)
	}
}

func TestPackageWorkflowPreparationSucceedsWithValidRetainedTicket(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service, err := NewPackageWorkflowPreparationService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Prepare(context.Background(), PackageWorkflowPreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatalf("preparation failed with valid retained ticket: %v", err)
	}
}

func TestPackageWorkflowPreparationFailsClosedWithMissingRetainedTicket(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	reader := newUnavailableSourceVaultReader()
	service, err := NewPackageWorkflowPreparationService(fixture.store, reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Prepare(context.Background(), PackageWorkflowPreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err == nil {
		t.Fatal("missing retained ticket was accepted")
	}
	if !errors.Is(err, executionpackages.ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want ErrApprovedAuthorityInvalid", err)
	}
}

func TestWorkflowExecutionServiceLegacyNonPackageStartAvoidsPackagePath(t *testing.T) {
	fixture := newWorkflowFixture(t)
	fixture.service.runner = successfulRunner
	packagePathTaken := false
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackageWorkflowPreparationService, PackageWorkflowPreparationInput) (PackageWorkflowPreparationResult, error) {
			packagePathTaken = true
			return PackageWorkflowPreparationResult{}, errors.New("package path should not be taken")
		},
		func(context.Context, *WorkflowExecutionService, PackageWorkflowPreparationResult) (PackageWorkflowDispatchResult, error) {
			packagePathTaken = true
			return PackageWorkflowDispatchResult{}, errors.New("package path should not be taken")
		},
	)
	_, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err == nil {
		t.Fatal("expected legacy path to encounter the deterministic applier stub")
	}
	if packagePathTaken {
		t.Fatal("non-package run took the package path")
	}
}

func TestWorkflowExecutionServiceValidReaderInitializesPackageServices(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service := NewWorkflowExecutionService(fixture.store, nil, "reader-test", fixture.sourceVaultReader)
	if service.packagePreparation == nil {
		t.Fatal("packagePreparation is nil with a valid reader")
	}
	if service.adaptiveAdmission == nil {
		t.Fatal("adaptiveAdmission is nil with a valid reader")
	}
}

func TestWorkflowExecutionServiceNilReaderLeavesPackageServicesNil(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service := NewWorkflowExecutionService(fixture.store, nil, "reader-test", nil)
	if service.packagePreparation != nil {
		t.Fatal("packagePreparation must be nil when reader is nil")
	}
	if service.adaptiveAdmission != nil {
		t.Fatal("adaptiveAdmission must be nil when reader is nil")
	}
}

func TestConstructorsRejectNilStore(t *testing.T) {
	reader := newUnavailableSourceVaultReader()
	readerType := reflect.TypeOf((*executionpackages.SourceVaultReader)(nil)).Elem()
	for _, c := range executorConstructors {
		if !c.returnsErr {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			fn := reflect.ValueOf(c.value)
			result := fn.Call([]reflect.Value{
				reflect.ValueOf((*workflowstore.Store)(nil)),
				reflect.ValueOf(reader).Convert(readerType),
			})
			if err := result[1].Interface(); err == nil {
				t.Fatal("nil store was accepted")
			}
		})
	}
}

func TestSourceVaultReaderErrorPropagatesToErrorReturningConstructors(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	reader := fixture.sourceVaultReader.WithErr(&sourcevault.Error{Code: sourcevault.CodeVaultUnavailable})
	readerType := reflect.TypeOf((*executionpackages.SourceVaultReader)(nil)).Elem()
	for _, c := range executorConstructors {
		if !c.returnsErr {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			fn := reflect.ValueOf(c.value)
			result := fn.Call([]reflect.Value{
				reflect.ValueOf(fixture.store),
				reflect.ValueOf(reader).Convert(readerType),
			})
			if err := result[1].Interface(); err != nil {
				t.Fatalf("constructor failed because of reader error: %v", err)
			}
		})
	}
}
