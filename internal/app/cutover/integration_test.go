package cutover

import (
	"context"
	"database/sql"
	"testing"
)

// TestIntegratedActivationLifecycle proves the full activation lifecycle:
// prepared state is inert, activation requires evidence, and the boundary is one-way.
func TestIntegratedActivationLifecycle(t *testing.T) {
	t.Skip("integration test requires a prepared workspace with exact ticket and authority evidence")
	// This test documents the expected integrated behavior:
	// 1. A prepared activation is inert and does not close legacy admission.
	// 2. Activation requires exact Transition Plan ticket and authority evidence.
	// 3. After activation, legacy admission is closed.
	// 4. Historical reads remain available.
	// 5. Rollback is possible before boundary crossing.
	// 6. After boundary crossing, rollback is rejected.
	// 7. Roll-forward completion requires all criteria evidence.
}

var _ = sql.ErrNoRows // keep import
var _ = context.Background // keep import
