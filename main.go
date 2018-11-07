package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
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

	result.DNS = conf.DNS
	return spawnChild(&childConf{
		PID:    pid,
		IfName: args.IfName,
		MTU:    conf.MTU,
		Result: result,
	})
}

func cmdDel(args *skel.CmdArgs) error {
	// TODO: impl
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

func main() {
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}
