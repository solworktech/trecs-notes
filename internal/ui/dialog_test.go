package ui

import (
	"os/exec"
	"testing"
	"time"
)

func TestCloseDialogEndsRunningProcess(t *testing.T) {
	done := make(chan error, 1)
	go func() { done <- runTracked(exec.Command("sleep", "30")) }()
	time.Sleep(200 * time.Millisecond) // let it start
	CloseDialog()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the killed process to report an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CloseDialog did not end the process")
	}
}
