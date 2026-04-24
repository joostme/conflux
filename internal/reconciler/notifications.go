package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Result summarizes the outcome of a reconciliation cycle.
type Result struct {
	Deployed        int
	DeployedStacks  []string
	Skipped         int
	Failed          int
	FailedStacks    []string
	RemovedStacks   int
	RemovedStackIDs []string
	RemovedNetworks int
}

// Changed reports whether the reconcile run applied any changes.
func (r Result) Changed() bool {
	return r.Deployed > 0 || r.RemovedStacks > 0 || r.RemovedNetworks > 0 || r.Failed > 0
}

func (r Result) notificationMessage() string {
	parts := []string{"Conflux run changed"}
	if r.Deployed > 0 {
		parts = append(parts, fmt.Sprintf("Deployed: %s", strings.Join(r.DeployedStacks, ", ")))
	}
	if r.RemovedStacks > 0 {
		parts = append(parts, fmt.Sprintf("Removed: %s", strings.Join(r.RemovedStackIDs, ", ")))
	}
	if r.RemovedNetworks > 0 {
		parts = append(parts, fmt.Sprintf("Removed networks: %d", r.RemovedNetworks))
	}
	if r.Failed > 0 {
		parts = append(parts, fmt.Sprintf("Failed: %s", strings.Join(r.FailedStacks, ", ")))
	}

	return strings.Join(parts, "\n")
}

func (r *Reconciler) notifyChanged(ctx context.Context, result Result) {
	if r.notify == nil || !result.Changed() {
		return
	}

	params := map[string]string{"title": "Conflux run changed"}
	if err := r.notify(ctx, result.notificationMessage(), params); err != nil {
		slog.Warn("failed to send notification", "error", err)
	}
}

func (r *Reconciler) reportValidationFailure(ctx context.Context, summary string, failedStacks []string, err error) error {
	validationErr := &ValidationError{
		Summary:      summary,
		FailedStacks: append([]string(nil), failedStacks...),
		Err:          err,
	}
	r.notifyValidationFailed(ctx, validationErr)
	return validationErr
}

func (r *Reconciler) notifyValidationFailed(ctx context.Context, err *ValidationError) {
	if r.notify == nil {
		return
	}

	lines := []string{"Conflux validation failed"}
	if len(err.FailedStacks) > 0 {
		lines = append(lines, fmt.Sprintf("Stacks: %s", strings.Join(err.FailedStacks, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("Error: %s", err.Summary))
	}
	if err.Err != nil {
		lines = append(lines, fmt.Sprintf("Details: %v", err.Err))
	}

	params := map[string]string{"title": "Conflux validation failed"}
	if notifyErr := r.notify(ctx, strings.Join(lines, "\n"), params); notifyErr != nil {
		slog.Warn("failed to send validation notification", "error", notifyErr)
	}
}
