package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type UserNS struct {
	file *os.File
}

func Current() (*UserNS, error) {
	return OpenUserNS(os.Getpid())
}

func OpenUserNS(pid int) (*UserNS, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/ns/user", pid))
	if err != nil {
		return nil, fmt.Errorf("open userns of pid %d: %v", pid, err)
	}
	return &UserNS{file}, nil
}

func (ns *UserNS) Set() (err error) {
	if err = unix.Setns(int(ns.file.Fd()), unix.CLONE_NEWUSER); err != nil {
		err = fmt.Errorf("Error switching to userns %v: %v", ns.file.Name(), err)
	}
	return
}

func (ns *UserNS) Close() (err error) {
	if err = ns.file.Close(); err != nil {
		err = fmt.Errorf("close userns: %v", err)
	}
	return
}
