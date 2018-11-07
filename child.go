package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/j-keck/arping"
	"github.com/vishvananda/netlink"

	//"github.com/opencontainers/runc/libcontainer"
	_ "github.com/opencontainers/runc/libcontainer/nsenter"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()
		conf, err := parseArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "nsenter: slirp: %v\n", err)
			os.Exit(255)
		}
		if err = runInUsernamespace(&conf); err != nil {
			fmt.Fprintf(os.Stderr, "nsenter: slirp: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}

type childConf struct {
	PID    int
	IfName string
	MTU    int
	Result *current.Result
}

func (a *childConf) Args() []string {
	result, err := json.MarshalIndent(a.Result, "", "    ")
	if err != nil {
		panic(err)
	}
	return []string{strconv.Itoa(a.PID), a.IfName, strconv.Itoa(a.MTU), string(result)}
}

func parseArgs(args []string) (a childConf, err error) {
	if len(args) != 4 {
		return a, fmt.Errorf("invalid arg count")
	}
	pid, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return a, fmt.Errorf("invalid pid arg provided: %q", args[0])
	}
	a.PID = int(pid)
	a.IfName = args[1]
	mtu, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil {
		return a, fmt.Errorf("invalid mtu arg provided: %q", args[2])
	}
	a.MTU = int(mtu)
	result, err := current.NewResult([]byte(args[3]))
	if err != nil {
		return a, fmt.Errorf("invalid result arg provided: %q", args[3])
	}
	a.Result, err = current.NewResultFromResult(result)
	return
}

func spawnChild(conf *childConf) (err error) {
	parent, child, err := newPipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %v", err)
	}
	defer func() {
		if e := parent.Close(); e != nil && err == nil {
			err = e
		}
		if e := child.Close(); e != nil && err == nil {
			err = e
		}
	}()
	namespaces := []string{
		// join pid ns of the current process
		fmt.Sprintf("user:/proc/%d/ns/user", conf.PID),
		fmt.Sprintf("net:/proc/%d/ns/net", conf.PID),
	}

	//cmd := exec.Command("/proc/self/exe", args...)
	// Run nsenter (enters netns by forking another process that switches ns in a c constructor to be sure to remain single threaded)
	cmd := &exec.Cmd{
		Path:       os.Args[0],
		Args:       append([]string{"nsenter-exec", "init"}, conf.Args()...),
		ExtraFiles: []*os.File{child},
		Env:        []string{"_LIBCONTAINER_INITPIPE=3"},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("failed to start child: %v", err)
	}

	// Instruct process to join namespaces
	r := nl.NewNetlinkRequest(62000 /* libcontainer.InitMsg */, 0)
	r.AddData(&Bytemsg{
		Type:  27282, /* libcontainer.NsPathsAttr */
		Value: []byte(strings.Join(namespaces, ",")),
	})
	if _, err = io.Copy(parent, bytes.NewReader(r.Serialize())); err != nil {
		return fmt.Errorf("failed to send message to nsenter pipe: %v", err)
	}

	// Wait for process
	decoder := json.NewDecoder(parent)
	var childPid *childPid

	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("nsenter: %v", err)
	}
	if err = decoder.Decode(&childPid); err != nil {
		return fmt.Errorf("decode child response: %v", err)
	}

	p, err := os.FindProcess(childPid.Pid)
	if err != nil {
		return
	}
	ps, err := p.Wait()
	if err != nil {
		return fmt.Errorf("nsenter: %v", err)
	}
	if ps == nil || !ps.Success() {
		return fmt.Errorf("exited with error")
	}
	return
}

type childPid struct {
	Pid int `json:"Pid"`
}

// Taken from libcontainer/message_linux.go
// Bytemsg has the following representation
// | nlattr len | nlattr type |
// | value              | pad |
type Bytemsg struct {
	Type  uint16
	Value []byte
}

func (msg *Bytemsg) Serialize() []byte {
	l := msg.Len()
	buf := make([]byte, (l+unix.NLA_ALIGNTO-1) & ^(unix.NLA_ALIGNTO-1))
	native := nl.NativeEndian()
	native.PutUint16(buf[0:2], uint16(l))
	native.PutUint16(buf[2:4], msg.Type)
	copy(buf[4:], msg.Value)
	return buf
}

func (msg *Bytemsg) Len() int {
	return unix.NLA_HDRLEN + len(msg.Value) + 1 // null-terminated
}

func runInUsernamespace(args *childConf) (err error) {
	fmt.Fprintf(os.Stderr, "CHILD ARGS: %+v\n", args)

	err = configureInterface(args.PID, args.IfName, args.MTU, args.Result)

	ifaces, _ := net.Interfaces()
	for _, n := range ifaces {
		addr, _ := n.Addrs()
		addrs := ""
		if len(addr) > 0 {
			addrs = addr[0].String()
		}
		fmt.Fprintf(os.Stderr, "CONTAINER INTERFACE %s: %s\n", n.Name, addrs)
	}

	if err != nil {
		return fmt.Errorf("configure netns: %v", err)
	}

	// TODO: remove experimental instruction
	d, _ := time.ParseDuration("10s")
	time.Sleep(d)

	return args.Result.Print()
}

func configureInterface(pid int, ifName string, mtu int, pr *current.Result) (err error) {
	tapDev, err := NewTapDevice(ifName, false)
	if err != nil {
		return
	}
	// TODO: fix this: device handle must be kept open in order to work
	// Thus when the plugin terminates the device is gone.
	// Can the device be bound to the container somehow?
	/*defer func() {
		if e := tapDev.Close(); e != nil && err == nil {
			err = e
		}
	}()*/
	// TODO: configure lo interface here as well?
	tapIface := &current.Interface{
		Name:    tapDev.Name(),
		Mac:     tapDev.HardwareAddr().String(),
		Sandbox: fmt.Sprintf("/proc/%d/ns/net", pid),
	}

	for _, ipc := range pr.IPs {
		// Assign all IPs to tap device
		ipc.Interface = current.Int(0)
	}

	pr.Interfaces = []*current.Interface{tapIface}

	if err = ipam.ConfigureIface(tapIface.Name, pr); err != nil {
		return fmt.Errorf("ipam: %v", err)
	}

	contIface, err := net.InterfaceByName(tapIface.Name)
	if err != nil {
		return fmt.Errorf("failed to look up interface %q: %v", tapIface.Name, err)
	}

	for _, ipc := range pr.IPs {
		// Delete the route that was automatically added
		route := netlink.Route{
			LinkIndex: contIface.Index,
			Dst: &net.IPNet{
				IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
				Mask: ipc.Address.Mask,
			},
			Scope: netlink.SCOPE_NOWHERE,
		}

		if err := netlink.RouteDel(&route); err != nil {
			return fmt.Errorf("failed to delete route %v: %v", route, err)
		}

		addrBits := 32
		if ipc.Version == "6" {
			addrBits = 128
		}

		for _, r := range []netlink.Route{
			netlink.Route{
				LinkIndex: contIface.Index,
				Dst: &net.IPNet{
					IP:   ipc.Gateway,
					Mask: net.CIDRMask(addrBits, addrBits),
				},
				Scope: netlink.SCOPE_LINK,
				Src:   ipc.Address.IP,
			},
			netlink.Route{
				LinkIndex: contIface.Index,
				Dst: &net.IPNet{
					IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
					Mask: ipc.Address.Mask,
				},
				Scope: netlink.SCOPE_UNIVERSE,
				Gw:    ipc.Gateway,
				Src:   ipc.Address.IP,
			},
		} {
			if err := netlink.RouteAdd(&r); err != nil {
				return fmt.Errorf("failed to add route %v: %v", r, err)
			}
		}
	}

	// Send a gratuitous arp for all v4 addresses
	for _, ipc := range pr.IPs {
		if ipc.Version == "4" {
			_ = arping.GratuitousArpOverIface(ipc.Address.IP, *contIface)
		}
	}

	return nil
}

func newPipe() (parent *os.File, child *os.File, err error) {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[1]), "parent"), os.NewFile(uintptr(fds[0]), "child"), nil
}
