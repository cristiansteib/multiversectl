package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// addressPool is one of Docker's default-address-pools: networks get subnets of
// Size bits carved out of Base, and the pools together bound how many networks
// can exist on the machine.
type addressPool struct {
	Base string
	Size int
}

// builtinPools are the pools Docker uses when its configuration names none.
var builtinPools = func() []addressPool {
	var pools []addressPool
	for i := 17; i <= 31; i++ {
		pools = append(pools, addressPool{Base: fmt.Sprintf("172.%d.0.0/16", i), Size: 16})
	}
	return append(pools, addressPool{Base: "192.168.0.0/16", Size: 20})
}()

// poolUse is how many networks the pools can hold and how many of them have a
// subnet taken from one of the pools. A network with a subnet of its own choosing
// (outside the pools) uses none of the capacity.
func poolUse(pools []addressPool, subnets []string) (capacity, used int) {
	var bases []netip.Prefix
	for _, p := range pools {
		base, err := netip.ParsePrefix(p.Base)
		if err != nil || p.Size < base.Bits() || p.Size > 32 {
			continue
		}
		bases = append(bases, base.Masked())
		capacity += 1 << (p.Size - base.Bits())
	}
	for _, sn := range subnets {
		prefix, err := netip.ParsePrefix(sn)
		if err != nil {
			continue
		}
		for _, base := range bases {
			if base.Contains(prefix.Addr()) {
				used++
				break
			}
		}
	}
	return capacity, used
}

// checkNetworkPool reports how many more networks Docker can create. When the pools
// are used up, compose fails to create a project's network with "all predefined
// address pools have been fully subnetted", which says nothing about what to change.
func (s *Service) checkNetworkPool(ctx context.Context, add addCheck) {
	if s.Commands == nil {
		return
	}
	out, err := s.Commands.RunHost(ctx, HostCommand{Args: []string{"docker", "info", "--format", "{{json .DefaultAddressPools}}"}})
	if err != nil {
		return
	}
	var pools []addressPool
	assumed := false
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &pools); err != nil || len(pools) == 0 {
		pools, assumed = builtinPools, true
	}
	ids, err := s.Commands.RunHost(ctx, HostCommand{Args: []string{"docker", "network", "ls", "--quiet"}})
	if err != nil || strings.TrimSpace(ids) == "" {
		return
	}
	args := append([]string{"docker", "network", "inspect", "--format", `{{range .IPAM.Config}}{{.Subnet}} {{end}}`}, strings.Fields(ids)...)
	raw, err := s.Commands.RunHost(ctx, HostCommand{Args: args})
	if err != nil {
		return
	}
	capacity, used := poolUse(pools, strings.Fields(raw))
	free := capacity - used
	where := "Docker's address pools"
	if assumed {
		where = "Docker's built-in address pools (assumed: its configuration names none)"
	}
	switch {
	case free <= 0:
		add("networks", StatusWarn, "%s hold %d networks and all %d are in use: docker cannot create another one, so a new universe or app fails to start (apps that already have their network restart fine). "+
			"Make room: remove networks nothing uses (docker network prune removes every unused one on this machine), or give Docker more addresses in /etc/docker/daemon.json, "+
			`for example "default-address-pools": [{"base": "10.200.0.0/16", "size": 24}] (a /16 split into /24s holds 256 networks; pick a range your VPN and LAN do not use), then restart Docker, which stops every container`,
			where, capacity, used)
	case free < 6:
		add("networks", StatusWarn, "%s hold %d networks and %d are in use: only %d more can be created, and each app of a universe takes one. "+
			"Give Docker more addresses in /etc/docker/daemon.json before starting more universes, for example "+
			`"default-address-pools": [{"base": "10.200.0.0/16", "size": 24}] (256 networks; pick a range your VPN and LAN do not use), then restart Docker, which stops every container`,
			where, capacity, used, free)
	default:
		add("networks", StatusOK, "%s hold %d networks, %d in use, %d free", where, capacity, used, free)
	}
}

// sharedNetworkMembers are the containers on the old shared proxy network that are not
// the proxy itself. Before each universe had its own networks, every routed service
// joined it, and there a service name is an alias of the service of every universe.
func sharedNetworkMembers(inspected string) []string {
	var out []string
	for _, name := range strings.Fields(inspected) {
		if !strings.HasPrefix(name, "multiverse-proxy") {
			out = append(out, name)
		}
	}
	return out
}

// checkSharedNetwork warns about containers still on the old shared proxy network, which
// is where universes can see each other's services by name. They move to their own
// network the next time their universe is brought up.
func (s *Service) checkSharedNetwork(ctx context.Context, add addCheck) {
	if s.Commands == nil || s.Manifest == nil {
		return
	}
	network := s.Manifest.Proxy.Network
	if network == "" {
		network = "multiversenet"
	}
	out, err := s.Commands.RunHost(ctx, HostCommand{Args: []string{"docker", "network", "inspect", network, "--format", `{{range .Containers}}{{.Name}} {{end}}`}})
	if err != nil {
		return // no such network: nothing was ever on it
	}
	members := sharedNetworkMembers(out)
	if len(members) == 0 {
		return
	}
	shown := members
	if len(shown) > 6 {
		shown = shown[:6]
	}
	add("shared network", StatusWarn,
		"%d container(s) are still on the shared network %s, where a service name such as `auth` answers for the auth service of every universe on it (%s). "+
			"Bring each universe up again to move its containers to its own network: multiversectl universe up <universe>",
		len(members), network, strings.Join(shown, ", "))
}
