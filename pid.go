package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func writeSlirpPIDFile(pidFile string, slirpPID int) (err error) {
	if err = ioutil.WriteFile(pidFile, []byte(strconv.Itoa(slirpPID)), 0600); err != nil {
		return fmt.Errorf("error writing slirp PID file: %v", err)
	}
	return
}

func getSlirpPID(pidFile string) (slirpPID int, err error) {
	b, err := ioutil.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return
	}
	slirpPID, err = strconv.Atoi(string(b))
	if err != nil || !pidExists(slirpPID) {
		return 0, nil
	}
	return
}

func deleteSlirpPIDFile(pidFile string) (err error) {
	if e := os.Remove(pidFile); e != nil {
		if !os.IsNotExist(e) {
			err = fmt.Errorf("remove slirp PID file: %v", e)
		}
	}
	return
}

func slirpPIDFile(containerID, ifName string) string {
	return filepath.Join("/dev/shm", "slirp4netns-"+containerID+"-"+ifName)
}

func pidExists(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
