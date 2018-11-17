#!/bin/sh

# Must be run in the repository directory.

export PATH="$(pwd)/build/bin:$PATH" \
	CNI_PATH="$(pwd):$CNI_PATH" \
	NETCONFPATH=$(pwd)/example-conf

TMPDIR=$(mktemp -d) || exit 1

cd $TMPDIR &&
mkfifo cpipe &&
mkfifo hpipe || (rm -rf "$TMPDIR"; false) || exit 1

# Create user/net/mount namespaces
unshare --user --map-root-user --net --mount sh -c "
	timeout 1s echo '=> netns is ready' > cpipe &&
	timeout 3s cat hpipe &&
	ip addr show eth0 &&
	wget --spider http://example.org &&
	timeout 1s echo '=> connectivity verified' > cpipe &&
	timeout 3s cat hpipe &&
	(! ip addr show eth0) &&
	timeout 1s echo '=> iface deletion verified' > cpipe &&
	timeout 3s cat hpipe &&
	wget --spider http://example.org
" &
PID=$!
NETNS=/proc/$PID/ns/net

# Add interface to netns
timeout 3s cat cpipe &&
cnitool add slirp $NETNS &&
echo '=> iface initialized' > hpipe &&
timeout 3s cat cpipe &&

# Remove interface from netns
cnitool del slirp $NETNS &&
echo '=> iface deleted' > hpipe &&
timeout 3s cat cpipe &&

# Add interface again
cnitool add slirp $NETNS &&
echo '=> iface added again' > hpipe &&
wait $PID
STATUS=$?

# Clean up
cnitool del slirp $NETNS || STATUS=1
rm -rf "$TMPDIR"

[ $STATUS -eq 0 ] && echo '=> INTEGRATION TEST SUCCEEDED' || '=> INTEGRATION TEST FAILED'

exit $STATUS