package main

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
)

type netConf struct {
	types.NetConf
	MTU int
}

func cmdAdd(args *skel.CmdArgs) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("add slirp %s: %v", args.IfName, err)
		}
	}()

	conf := netConf{}
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	if conf.IPAM.Type != "" {
		return fmt.Errorf("ipam plugin not supported within slirp")
	}

	if conf.MTU <= 0 {
		conf.MTU = 1500 // default
	}
	if conf.MTU > 65521 || conf.MTU < 1500 {
		return fmt.Errorf("invalid MTU value %d configured. 1500 <= MTU <= 65521", conf.MTU)
	}

	containerPID, err := pidFromNetns(args.Netns)
	if err != nil {
		return
	}

	pidFile := slirpPIDFile(args.ContainerID, args.IfName)
	slirpPID, err := getSlirpPID(pidFile)
	if err != nil {
		return
	}
	if slirpPID > 0 {
		return fmt.Errorf("a slirp4netns process (%d) is already running for netns (containerID: %s, PID: %d)", slirpPID, args.ContainerID, containerPID)
	}

	slirpPID, err = startSlirp(containerPID, args.IfName, conf.MTU)
	if err != nil {
		return
	}
	if err = writeSlirpPIDFile(pidFile, slirpPID); err != nil {
		stopSlirp(slirpPID)
		return
	}

	// See https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md#description
	r := current.Result{
		CNIVersion: current.ImplementedSpecVersion,
		Interfaces: []*current.Interface{{
			Name:    args.IfName,
			Sandbox: args.Netns,
		}},
		IPs: []*current.IPConfig{{
			Version:   "4",
			Interface: current.Int(0),
			Address: net.IPNet{
				IP:   net.ParseIP("10.0.2.100"),
				Mask: net.IPv4Mask(255, 255, 255, 0),
			},
			Gateway: net.ParseIP("10.0.2.2"),
		}},
		Routes: []*types.Route{{
			Dst: net.IPNet{
				IP:   net.ParseIP("0.0.0.0"),
				Mask: net.IPv4Mask(0, 0, 0, 0),
			},
			GW: net.ParseIP("10.0.2.2"),
		}},
		DNS: conf.DNS,
	}
	r.DNS.Nameservers = append(r.DNS.Nameservers, "10.0.2.3")

	if err = types.PrintResult(&r, conf.CNIVersion); err != nil {
		stopSlirp(slirpPID)
		return fmt.Errorf("error printing slirp result")
	}
	return
}

func cmdDel(args *skel.CmdArgs) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("del slirp %s: %v", args.IfName, err)
		}
	}()

	conf := netConf{}
	if err = json.Unmarshal(args.StdinData, &conf); err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}
	pidFile := slirpPIDFile(args.ContainerID, args.IfName)
	slirpPID, err := getSlirpPID(pidFile)
	if err != nil || slirpPID == 0 {
		return
	}
	if err = stopSlirp(slirpPID); err != nil {
		return
	}
	return deleteSlirpPIDFile(pidFile)
}

func pidFromNetns(netnsPath string) (pid int, err error) {
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
