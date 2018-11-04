package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

type DevKind int

const (
	// Receive/send layer routable 3 packets (IP, IPv6...). Notably,
	// you don't receive link-local multicast with this interface
	// type.
	DevTun DevKind = iota
	// Receive/send Ethernet II frames. You receive all packets that
	// would be visible on an Ethernet link, including broadcast and
	// multicast traffic.
	DevTap
)

type TapDevice struct {
	file         *os.File
	name         string
	hardwareAddr net.HardwareAddr
}

func (d *TapDevice) Close() (err error) {
	if d.file == nil {
		return
	}
	err = d.file.Close()
	d.file = nil
	return
}

func (d *TapDevice) Name() string {
	return d.name
}

func (d *TapDevice) HardwareAddr() net.HardwareAddr {
	return d.hardwareAddr
}

func NewTapDevice(ifPattern string, meta bool) (d *TapDevice, err error) {
	file, err := openDevice()
	if err != nil {
		return
	}
	ifName, err := createInterface(file, ifPattern, DevTap, meta)
	if err != nil {
		file.Close()
		return
	}
	contIface, err := net.InterfaceByName(ifName)
	if err != nil {
		return nil, fmt.Errorf("new tap device: failed to look up interface %q: %v", ifName, err)
	}
	return &TapDevice{file, ifName, contIface.HardwareAddr}, nil
}

func openDevice() (*os.File, error) {
	return os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
}

func createInterface(file *os.File, ifPattern string, kind DevKind, meta bool) (string, error) {
	var req ifReq
	//req.Flags = iffOneQueue
	req.Flags = 0
	copy(req.Name[:15], ifPattern)
	switch kind {
	case DevTun:
		req.Flags |= iffTun
	case DevTap:
		req.Flags |= iffTap
	default:
		panic("Unknown interface type")
	}
	if !meta {
		req.Flags |= iffnopi
	}
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TUNSETIFF), uintptr(unsafe.Pointer(&req)))
	if err != 0 {
		return "", err
	}
	idxNull := bytes.IndexByte(req.Name[:], 0)
	if idxNull < 0 {
		idxNull = len(req.Name)
	}
	return string(req.Name[:idxNull]), nil
}
