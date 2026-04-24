package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	envfiles "github.com/joostme/conflux/internal/env"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

// ValidationError reports a repo state that was rejected before reconcile.
type ValidationError struct {
	Summary      string
	FailedStacks []string
	Err          error
}

func (e *ValidationError) Error() string {
	if len(e.FailedStacks) == 0 {
		return fmt.Sprintf("%s: %v", e.Summary, e.Err)
	}
	return fmt.Sprintf("%s for stacks %s: %v", e.Summary, strings.Join(e.FailedStacks, ", "), e.Err)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Validate checks whether the current repo state can be reconciled safely.
func (r *Reconciler) Validate(ctx context.Context) error {
	slog.Info("starting validation")

	cfg, err := r.loadConfig()
	if err != nil {
		return r.reportValidationFailure(ctx, "loading config", nil, err)
	}

	envResolver, err := envfiles.NewResolver(r.repoDir, cfg)
	if err != nil {
		return r.reportValidationFailure(ctx, "resolving global env files", nil, err)
	}
	defer r.cleanupEnvResolver(envResolver)

	var validationErrs []error
	var failedStacks []string

	if err := networks.Validate(cfg.Networks); err != nil {
		slog.Error("network validation failed", "error", err)
		validationErrs = append(validationErrs, fmt.Errorf("validating networks: %w", err))
	}

	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		slog.Error("stack discovery failed", "error", err)
		validationErrs = append(validationErrs, fmt.Errorf("discovering stacks: %w", err))
	} else if len(discovered) > 0 && r.validate == nil {
		slog.Error("stack validation unavailable", "error", "docker client not configured")
		validationErrs = append(validationErrs, fmt.Errorf("validating stack definitions: %w", fmt.Errorf("docker client not configured")))
	}

	if r.validate != nil {
		for _, stack := range discovered {
			if err := ctx.Err(); err != nil {
				return err
			}

			resolvedEnv, err := envResolver.ResolveForStack(stack.Dir, cfg.Stacks)
			if err != nil {
				slog.Error("failed to resolve env file during validation", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s env resolution: %w", stack.Name, err))
				continue
			}

			envFile, err := envResolver.FileFromContent(resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to prepare env file during validation", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s env file: %w", stack.Name, err))
				continue
			}

			if err := r.validate(ctx, stack, envFile); err != nil {
				slog.Error("stack validation failed", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s: %w", stack.Name, err))
			}
		}
	}

	if len(validationErrs) > 0 {
		sort.Strings(failedStacks)
		return r.reportValidationFailure(ctx, "validation failed", failedStacks, errors.Join(validationErrs...))
	}

	slog.Info("validation complete", "stacks", len(discovered), "networks", len(cfg.Networks))
	return nil
}
