package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func startSlirp(containerPID int, ifName string, mtu int) (slirpPID int, err error) {
	slirp4netns, err := findSlirp4netnsBinary()
	if err != nil {
		return
	}

	readyPipeR, readyPipeW, err := os.Pipe()
	if err != nil {
		return 0, fmt.Errorf("failed to create pipe for ready message: %v", err)
	}
	defer func() {
		readyPipeR.Close()
		readyPipeW.Close()
	}()

	cmd := &exec.Cmd{
		Path:       slirp4netns,
		Args:       append([]string{"slirp4netns", "-c", "-m", strconv.Itoa(mtu), "-r", "3", strconv.Itoa(containerPID), ifName}),
		ExtraFiles: []*os.File{readyPipeW},
	}
	if err = redirectOutput(cmd); err != nil {
		return
	}
	if err = cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start slirp4netns: %v", err)
	}
	slirpPID = cmd.Process.Pid
	if err = awaitSlirp4netnsReady(readyPipeR); err != nil {
		stopSlirp(slirpPID)
		return 0, err
	}
	if err = cmd.Process.Release(); err != nil {
		return 0, fmt.Errorf("failed to release slirp4netns: %v", err)
	}
	return
}

func stopSlirp(slirpPID int) (err error) {
	if err = signalExist(slirpPID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to terminate slirp4netns: %v", err)
	}
	if err = awaitSlirp4netnsTermination(slirpPID, time.Duration(5*time.Second)); err != nil {
		if err = signalExist(slirpPID, syscall.SIGKILL); err != nil {
			return fmt.Errorf("failed to kill slirp4netns: %v", err)
		}
	}
	return awaitSlirp4netnsTermination(slirpPID, time.Duration(1*time.Second))
}

func signalExist(pid int, s syscall.Signal) (err error) {
	if err = syscall.Kill(pid, s); err == syscall.ESRCH {
		err = nil // process does not exist (anymore)
	}
	return
}

func awaitSlirp4netnsReady(readyPipeR *os.File) (err error) {
	if err = readyPipeR.SetReadDeadline(time.Now().Add(time.Duration(5 * time.Second))); err != nil {
		return fmt.Errorf("setting slirp4netns ready pipe read timeout: %v", err)
	}
	read := make([]byte, 1)
	if _, err = readyPipeR.Read(read); err != nil {
		return fmt.Errorf("await ready message from slirp4netns: %v", err)
	}
	if string(read) != "1" {
		return fmt.Errorf("unexpected message from slirp4netns: %q, expected 1", string(read))
	}
	return
}

func redirectOutput(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create slirp4netns stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create slirp4netns stderr pipe: %v", err)
	}
	go func() {
		io.Copy(os.Stderr, stderr)
	}()
	go func() {
		io.Copy(os.Stderr, stdout)
	}()
	return nil
}

func findSlirp4netnsBinary() (binary string, err error) {
	binary, ok := os.LookupEnv("SLIRP4NETNS")
	if ok {
		return
	}
	if binary, err = exec.LookPath("slirp4netns"); err != nil {
		err = fmt.Errorf("cannot find slirp4netns in path (hint: specify SLIRP4NETNS env var or add slirp4netns to PATH)")
	}
	return
}

func awaitSlirp4netnsTermination(pid int, timeout time.Duration) error {
	if !pidExists(pid) {
		return nil
	}
	checkInterval := time.Duration(30 * time.Millisecond)
	for i := 0; i < int(timeout/checkInterval); i++ {
		time.Sleep(checkInterval)
		if !pidExists(pid) {
			return nil
		}
	}
	return fmt.Errorf("slirp4netns did not terminate (timeout)")
}
