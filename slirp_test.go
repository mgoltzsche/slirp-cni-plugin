package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopSlirp(t *testing.T) {
	for _, c := range [][]string{
		// Should terminate process gracefully
		{"/bin/sleep", "60"},
		// Should kill process after termination timeout
		{"/bin/sh", "-c", "sig() { echo signal received but refusing to terminate; sleep 60; }; trap sig 2 3 15; sleep 60 & wait"},
	} {
		fmt.Printf("Process: %+v\n", c)
		cmd := exec.Command(c[0], c[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdout, err := cmd.StdoutPipe()
		require.NoError(t, err)
		stderr, err := cmd.StderrPipe()
		require.NoError(t, err)
		go func() {
			io.Copy(os.Stdout, stdout)
		}()
		go func() {
			io.Copy(os.Stderr, stderr)
		}()
		err = cmd.Start()
		require.NoError(t, err)
		pid := cmd.Process.Pid
		defer syscall.Kill(pid, syscall.SIGKILL)
		go func() {
			cmd.Wait()
		}()
		time.Sleep(time.Duration(300 * time.Millisecond))
		startTime := time.Now()
		stopCh := make(chan error)
		go func() {
			stopCh <- stopSlirp(pid)
		}()
		select {
		case err = <-stopCh:
			require.NoError(t, err, "stopSlirp()")
			fmt.Println("stopSlirp() returned after", time.Since(startTime).String())
			err = syscall.Kill(pid, syscall.Signal(0))
			assert.True(t, err == syscall.ESRCH, "process has not been terminated")
		case <-time.After(time.Duration(7 * time.Second)):
			t.Errorf("timed out waiting for stopSlirp() to return")
			t.FailNow()
		}
	}
}
