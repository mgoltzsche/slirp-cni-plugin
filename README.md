# slirp-cni-plugin
A [CNI](https://github.com/containernetworking/cni) plugin that provides
container networking for unprivileged users ("slirp") using [slirp4netns](https://github.com/rootless-containers/slirp4netns).  


## Build
Build the plugin using make and docker:
```
git clone https://github.com/mgoltzsche/slirp-cni-plugin.git
cd slirp-cni-plugin
make slirp
```

In order to run the examples below you can also build the dependencies
[slirp4netns](https://github.com/rootless-containers/slirp4netns) and
[cnitool](https://github.com/containernetworking/cni/tree/master/cnitool) (written to `build/bin`):
```
make slirp4netns cnitool
```


## Plugin configuration

### JSON configuration file
An example configuration file can be found [here](example/conf/10-slirp.json).

| Field  | Default | Description |
| ------ | ------- | ----------- |
| `name` |  | The network/configuration file's name |
| `type` |  | Name used to lookup the plugin binary _(must be `slirp` to make the CNI runtime use this plugin)_ |
| `mtu`  | `1500` | Maximum Transmission Unit _(1499 < MTU < 65522)_ |

Nothing but the MTU can be configured since slirp4netns provides sufficient
[defaults](https://github.com/rootless-containers/slirp4netns/blob/master/slirp4netns.1.md#description).
Thus the `ipam` CNI plugin configuration is also not supported.

### Environment variables
To make the plugin use a specific [slirp4netns](https://github.com/rootless-containers/slirp4netns)
binary set the `SLIRP4NETNS` environment variable.
Otherwise the plugin will lookup slirp4netns in the `PATH`.


## Usage
This example shows how to create namespaces and add a [slirp network](example/conf/10-slirp.json)
using [cnitool](https://github.com/containernetworking/cni/tree/master/cnitool).
Please note that the [slirp4netns](https://github.com/rootless-containers/slirp4netns)
binary must be in the `PATH` or specified in the `SLIRP4NETNS` environment variable.  

Terminal 1: Create user, network and mount namespaces:
```
$ unshare --user --map-root-user --net --mount
unshared$ echo $$ > /tmp/pid
```

Terminal 2: Add network interface:
```
$ export PATH="$(pwd)/build/bin:$PATH" \
         CNI_PATH="$(pwd):$CNI_PATH" \
         NETCONFPATH=$(pwd)/example-conf
$ cnitool add slirp "/proc/$(cat /tmp/pid)/ns/net"
```

Terminal 1: test connectivity:
```
unshared$ ip a
1: lo: <LOOPBACK> mtu 65536 qdisc noop state DOWN group default qlen 1
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
2: eth0: <BROADCAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UNKNOWN group default qlen 1000
    link/ether 12:72:1b:c0:e0:0e brd ff:ff:ff:ff:ff:ff
    inet 10.0.2.100/24 brd 10.0.2.255 scope global tap0
       valid_lft forever preferred_lft forever
    inet6 fe80::1072:1bff:fec0:e00e/64 scope link 
       valid_lft forever preferred_lft forever
unshared$ curl http://example.org
<!doctype html>
...
```

Terminal 2: remove slirp network from the namespace after you're done:
```
$ cnitool del slirp "/proc/$(cat /tmp/pid)/ns/net"
```