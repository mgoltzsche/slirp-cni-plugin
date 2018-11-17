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
	timeout -t1 echo '=> netns is ready' > cpipe &&
	timeout -t3 cat hpipe &&
	ip addr show eth0 &&
	wget --spider http://example.org &&
	timeout -t1 echo '=> connectivity verified' > cpipe &&
	timeout -t3 cat hpipe &&
	(! ip addr show eth0) &&
	timeout -t1 echo '=> iface deletion verified' > cpipe &&
	timeout -t3 cat hpipe &&
	wget --spider http://example.org
" &
PID=$!
NETNS=/proc/$PID/ns/net

# Add interface to netns
timeout -t3 cat cpipe &&
cnitool add slirp $NETNS &&
echo '=> iface initialized' > hpipe &&
timeout -t3 cat cpipe &&

# Remove interface from netns
cnitool del slirp $NETNS &&
echo '=> iface deleted' > hpipe &&
timeout -t3 cat cpipe &&

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