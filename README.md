# slirp-cni-plugin
A [CNI](https://github.com/containernetworking/cni) plugin that provides
container networking for unprivileged users using a tap device ("slirp").  


THIS PROJECT IS IN EARLY DEVELOPMENT!

## Build
Build plugin, cni-tool and other CNI plugins:
```
make slirp-static cni-tool cni-plugins-static
```

## Usage
Create tap device and assign IP to it.  

Terminal 1: Create user/network/mount namespaces:
```
$ unshare --user --map-root-user --net --mount
unshared$ echo $$ > /tmp/pid
```

Terminal 2: Add and configure network interface:
```
$ export PATH="$PATH:$(pwd)/dist/bin" CNI_PATH=$(pwd)/dist/cni-plugins NETCONFPATH=$(pwd)/example/conf
$ cni-tool add slirp "/proc/$(cat /tmp/pid)/ns/net"
```