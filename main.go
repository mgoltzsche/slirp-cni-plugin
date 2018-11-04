package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"sync"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/j-keck/arping"
	"github.com/vishvananda/netlink"
)

type NetConf struct {
	types.NetConf
	MTU int
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}
	pid, err := getPID(args.Netns)
	if err != nil {
		return
	}

	// run the IPAM plugin and get back the config to apply
	r, err := ipam.ExecAdd(conf.IPAM.Type, args.StdinData)
	if err != nil {
		return fmt.Errorf("reserve IP: %v", err)
	}

	// Convert whatever the IPAM result was into the current Result type
	result, err := current.NewResultFromResult(r)
	if err != nil {
		return err
	}

	if len(result.IPs) == 0 {
		return errors.New("IPAM plugin returned no IP")
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	containerInterface, err := configureInterface(netns, args.IfName, conf.MTU, pid, result)
	if err != nil {
		return err
	}
	result.DNS = conf.DNS
	result.Interfaces = []*current.Interface{containerInterface}

	return types.PrintResult(result, conf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	conf := NetConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	if err := ipam.ExecDel(conf.IPAM.Type, args.StdinData); err != nil {
		return err
	}

	if args.Netns == "" {
		return nil
	}

	// There is a netns so try to clean up. Delete can be called multiple times
	// so don't return an error if the device is already removed.
	// If the device isn't there then don't try to clean up IP masq either.
	var ipn *net.IPNet
	err := ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		var err error
		ipn, err = ip.DelLinkByNameAddr(args.IfName, netlink.FAMILY_V4)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})

	return err
}

func getPID(netnsPath string) (pid int, err error) {
	regex := regexp.MustCompile("^/proc/([0-9]+)/ns/net$")
	matches := regex.FindStringSubmatch(netnsPath)
	if len(matches) != 2 {
		return 0, fmt.Errorf("get pid: unsupported netns path provided: %q", netnsPath)
	}
	return strconv.Atoi(matches[1])
}

func configureInterface(netns ns.NetNS, ifName string, mtu int, pid int, pr *current.Result) (i *current.Interface, err error) {
	// The IPAM result will be something like IP=192.168.3.5/24, GW=192.168.3.1.
	// What we want is really a point-to-point link but veth does not support IFF_POINTTOPOINT.
	// Next best thing would be to let it ARP but set interface to 192.168.3.5/32 and
	// add a route like "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// Unfortunately that won't work as the GW will be outside the interface's subnet.

	// Our solution is to configure the interface with 192.168.3.5/24, then delete the
	// "192.168.3.0/24 dev $ifName" route that was automatically added. Then we add
	// "192.168.3.1/32 dev $ifName" and "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// In other words we force all traffic to ARP via the gateway except for GW itself.

	newIface := &current.Interface{}

	/*cmd, err := exec.Command("/bin/echo", "hello")
	if err != nil {
		return
	}
	cmd.SysProcAttr.Unshareflags
	*/

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	currUserns, err := Current()
	if err != nil {
		return
	}
	defer func() {
		e := currUserns.Set()
		if e != nil && err == nil {
			err = fmt.Errorf("switch back to host userns: %v", e)
		}
		if e = currUserns.Close(); e != nil && err == nil {
			err = fmt.Errorf("close host userns: %v", e)
		}
	}()
	contUserns, err := OpenUserNS(pid)
	if err != nil {
		return
	}
	defer func() {
		if e := contUserns.Close(); e != nil && err == nil {
			err = fmt.Errorf("close container userns: %v", e)
		}
	}()
	if err = contUserns.Set(); err != nil {
		return nil, fmt.Errorf("enter container userns: %v", err)
	}

	err = configureNetns(netns, func(hostNS ns.NetNS) (err error) {
		tapDev, err := NewTapDevice(ifName, false)
		if err != nil {
			return
		}
		defer func() {
			if e := tapDev.Close(); e != nil && err == nil {
				err = e
			}
		}()
		newIface.Name = tapDev.Name()
		newIface.Mac = tapDev.HardwareAddr().String()
		newIface.Sandbox = netns.Path()

		for _, ipc := range pr.IPs {
			// All addresses apply to the container veth interface
			ipc.Interface = current.Int(1)
		}

		pr.Interfaces = []*current.Interface{newIface}

		if err = ipam.ConfigureIface(newIface.Name, pr); err != nil {
			return err
		}

		contIface, err := net.InterfaceByName(newIface.Name)
		if err != nil {
			return fmt.Errorf("failed to look up interface %q: %v", newIface.Name, err)
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
	})
	if err != nil {
		return nil, fmt.Errorf("configure container's network namespace: %v", err)
	}
	return newIface, nil
}

func configureNetns(contNetns ns.NetNS, toRun func(hostns ns.NetNS) error) error {
	containedCall := func(hostNS ns.NetNS) error {
		threadNS, err := ns.GetCurrentNS()
		if err != nil {
			return fmt.Errorf("failed to open current netns: %v", err)
		}
		defer threadNS.Close()

		// switch to target namespace
		if err = contNetns.Set(); err != nil {
			return fmt.Errorf("error switching to ns %s: %v", contNetns.Path(), err)
		}
		defer threadNS.Set() // switch back

		return toRun(hostNS)
	}

	// save a handle to current network namespace
	hostNS, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("Failed to open current namespace: %v", err)
	}
	defer hostNS.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var innerError error
	go func() {
		defer wg.Done()
		runtime.LockOSThread()
		innerError = containedCall(hostNS)
	}()
	wg.Wait()

	return innerError
}

func main() {
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}
