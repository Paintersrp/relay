package executor

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	executionpackages "relay/internal/app/packages"
	"relay/internal/applier"
	"relay/internal/pipeline"
	workflowrepos "relay/internal/repos/workflow"
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
	{"NewPackagePreparation", NewPackagePreparation, true},
	{"NewExecution", NewExecution, true},
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
			var args []reflect.Value
			if fn.Type().NumIn() == 4 {
				args = []reflect.Value{
					reflect.ValueOf(store),
					reflect.Zero(reflect.TypeOf((*slog.Logger)(nil))),
					reflect.ValueOf("test-owner"),
					reflect.Zero(readerType),
				}
			} else {
				args = []reflect.Value{
					reflect.ValueOf(store),
					reflect.Zero(readerType),
				}
			}
			result := fn.Call(args)
			if err := result[1].Interface(); err == nil {
				t.Fatal("nil reader was accepted")
			}
			if !result[0].IsNil() {
				t.Fatal("constructor returned non-nil object on failure")
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
			var args []reflect.Value
			if fn.Type().NumIn() == 4 {
				args = []reflect.Value{
					reflect.ValueOf(store),
					reflect.Zero(reflect.TypeOf((*slog.Logger)(nil))),
					reflect.ValueOf("test-owner"),
					reflect.ValueOf(reader).Convert(readerType),
				}
			} else {
				args = []reflect.Value{
					reflect.ValueOf(store),
					reflect.ValueOf(reader).Convert(readerType),
				}
			}
			result := fn.Call(args)
			if err := result[1].Interface(); err != nil {
				t.Fatalf("constructor failed: %v", err)
			}
			if result[0].IsNil() {
				t.Fatal("service is nil")
			}
		})
	}
}

func TestExecutionPassesReaderToNestedPackageServices(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	reader := fixture.sourceVaultReader
	service, err := NewExecution(fixture.store, nil, "reader-test", reader)
	if err != nil {
		t.Fatal(err)
	}
	if service.packagePreparation == nil {
		t.Fatal("packagePreparation was not initialized")
	}
	if service.adaptiveAdmission == nil {
		t.Fatal("adaptiveAdmission was not initialized")
	}
	prepareExecutionAssignment(t, fixture)
	if _, err := service.packagePreparation.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "opencode_go", Model: "model"}); err != nil {
		t.Fatalf("packagePreparation cannot use the reader: %v", err)
	}
}

func TestPackagePreparationSucceedsWithValidRetainedTicket(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service, err := NewPackagePreparation(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err != nil {
		t.Fatalf("preparation failed with valid retained ticket: %v", err)
	}
}

func TestPackagePreparationFailsClosedWithMissingRetainedTicket(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	reader := newUnavailableSourceVaultReader()
	service, err := NewPackagePreparation(fixture.store, reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Prepare(context.Background(), PackagePreparationInput{RunID: fixture.run.RunID, Adapter: "codex", Model: "model"})
	if err == nil {
		t.Fatal("missing retained ticket was accepted")
	}
	if !errors.Is(err, executionpackages.ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want ErrApprovedAuthorityInvalid", err)
	}
}

func TestExecutionLegacyNonPackageStartAvoidsPackagePath(t *testing.T) {
	fixture := newLegacyWorkflowFixture(t)
	packagePathTaken := false
	withPackageWorkflowStartSeams(t,
		func(context.Context, *PackagePreparation, PackagePreparationInput) (PackagePreparationResult, error) {
			packagePathTaken = true
			return PackagePreparationResult{}, errors.New("package path should not be taken")
		},
		func(context.Context, *Execution, PackagePreparationResult) (PackageWorkflowDispatchResult, error) {
			packagePathTaken = true
			return PackageWorkflowDispatchResult{}, errors.New("package path should not be taken")
		},
	)
	fixture.service.preflight = func(context.Context, string, string, string) workflowrepos.ExecutionPreflightResult {
		t.Fatal("legacy Run entered repository preflight")
		return workflowrepos.ExecutionPreflightResult{}
	}
	fixture.service.applier = func(context.Context, applier.Input) (applier.Result, error) {
		t.Fatal("legacy Run invoked deterministic applier")
		return applier.Result{}, nil
	}
	fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
		t.Fatal("legacy Run built an executor adapter")
		return nil, nil
	}
	fixture.service.launch = func(func()) { t.Fatal("legacy Run launched an executor process") }
	fixture.service.runner = func(context.Context, string, string, []string, string, time.Duration, pipeline.AgentCommandStreamCallbacks, pipeline.ProcessController) pipeline.AgentCommandRunResult {
		t.Fatal("legacy Run launched a model process")
		return pipeline.AgentCommandRunResult{}
	}
	_, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if !errors.Is(err, ErrLegacyExecutionRetired) {
		t.Fatalf("error = %v, want ErrLegacyExecutionRetired", err)
	}
	if packagePathTaken {
		t.Fatal("non-package run took the package path")
	}
}

func TestExecutionValidReaderInitializesPackageServices(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")
	prepareExecutionAssignment(t, fixture)
	service, err := NewExecution(fixture.store, nil, "reader-test", fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	if service.packagePreparation == nil {
		t.Fatal("packagePreparation is nil with a valid reader")
	}
	if service.adaptiveAdmission == nil {
		t.Fatal("adaptiveAdmission is nil with a valid reader")
	}
}

func TestNewExecutionRejectsNilStoreAndReader(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")

	if exec, err := NewExecution(nil, nil, "owner", fixture.sourceVaultReader); err == nil || exec != nil {
		t.Fatal("NewExecution accepted nil store or returned non-nil object")
	}

	if exec, err := NewExecution(fixture.store, nil, "owner", nil); err == nil || exec != nil {
		t.Fatal("NewExecution accepted nil source-vault reader or returned non-nil object")
	}
}

func TestNewPackagePreparationRejectsNilReader(t *testing.T) {
	fixture := newExecutionAssignmentFixture(t, false, "")

	if prep, err := NewPackagePreparation(fixture.store, nil); err == nil || prep != nil {
		t.Fatal("NewPackagePreparation accepted nil reader or returned non-nil object")
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
			var args []reflect.Value
			if fn.Type().NumIn() == 4 {
				args = []reflect.Value{
					reflect.ValueOf((*workflowstore.Store)(nil)),
					reflect.Zero(reflect.TypeOf((*slog.Logger)(nil))),
					reflect.ValueOf("test-owner"),
					reflect.ValueOf(reader).Convert(readerType),
				}
			} else {
				args = []reflect.Value{
					reflect.ValueOf((*workflowstore.Store)(nil)),
					reflect.ValueOf(reader).Convert(readerType),
				}
			}
			result := fn.Call(args)
			if err := result[1].Interface(); err == nil {
				t.Fatal("nil store was accepted")
			}
			if !result[0].IsNil() {
				t.Fatal("constructor returned non-nil object on failure")
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
			var args []reflect.Value
			if fn.Type().NumIn() == 4 {
				args = []reflect.Value{
					reflect.ValueOf(fixture.store),
					reflect.Zero(reflect.TypeOf((*slog.Logger)(nil))),
					reflect.ValueOf("test-owner"),
					reflect.ValueOf(reader).Convert(readerType),
				}
			} else {
				args = []reflect.Value{
					reflect.ValueOf(fixture.store),
					reflect.ValueOf(reader).Convert(readerType),
				}
			}
			result := fn.Call(args)
			if err := result[1].Interface(); err != nil {
				t.Fatalf("constructor failed because of reader error: %v", err)
			}
		})
	}
}
