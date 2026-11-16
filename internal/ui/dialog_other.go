//go:build !linux

package ui

// RunDialogProcess exists only so cmd/trecs-notes builds everywhere; the
// `dialog` child process is used on Linux only.
func RunDialogProcess() int { return 2 }

// RunSecretsProcess exists only so cmd/trecs-notes builds everywhere; the
// `secrets-dialog` child process is used on Linux only.
func RunSecretsProcess() int { return 2 }
