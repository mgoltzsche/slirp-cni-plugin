package main

import (
	"io/ioutil"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlirpPIDFile(t *testing.T) {
	assert.Equal(t, "/dev/shm/slirp4netns-cid-tap0", slirpPIDFile("cid", "tap0"))
}

func TestPidExists(t *testing.T) {
	for _, pid := range []int{32768, -1} {
		assert.False(t, pidExists(pid), "pidExists(%d) returned true for non-existing PID", pid)
	}
	assert.True(t, pidExists(os.Getpid()), "pidExists(ownPID) returned false for existing PID")
}

func TestWriteSlirpPIDFile(t *testing.T) {
	pidFile := "/dev/shm/slirptest-cid-tap0"
	defer os.Remove(pidFile)
	for _, pid := range []int{123, 12345} {
		err := writeSlirpPIDFile(pidFile, pid)
		require.NoError(t, err)
		b, err := ioutil.ReadFile(pidFile)
		require.NoError(t, err)
		assert.Equal(t, strconv.Itoa(pid), string(b))
	}
}

func TestDeleteSlirpPIDFile(t *testing.T) {
	pidFile := "/dev/shm/slirptest-cid-tap0"
	defer os.Remove(pidFile)
	err := ioutil.WriteFile(pidFile, []byte("123"), 0644)
	require.NoError(t, err, "create")

	for range [2]int{} {
		err = deleteSlirpPIDFile(pidFile)
		require.NoError(t, err, "delete")
		_, err = os.Stat(pidFile)
		if !assert.True(t, os.IsNotExist(err), "pid file %s should not exist but stat returned error: %v", pidFile, err) {
			t.FailNow()
		}
	}
}

func TestGetSlirpPID(t *testing.T) {
	// Write mocked PID files
	currPID := strconv.Itoa(os.Getpid())
	for _, content := range []string{currPID, "32768", "-1", "Invalid", ""} {
		pidFile := "/dev/shm/slirptest-cid" + content + "-tap0"
		err := ioutil.WriteFile(pidFile, []byte(content), 0644)
		require.NoError(t, err)
		defer os.Remove(pidFile)
	}

	// Assert PID file lookup returns valid PID
	currPIDFile := "/dev/shm/slirptest-cid" + currPID + "-tap0"
	pid, err := getSlirpPID(currPIDFile)
	require.NoError(t, err, "get current PID from file")
	assert.Equal(t, os.Getpid(), pid, "current PID")

	// Assert non-existing/invalid pid file returns 0
	// to be able to continue operations after processes have been killed
	// (this pid file does not serve as a lock)
	for _, cid := range []string{"nonExisting", "cidInvalid", ""} {
		pidFile := "/dev/shm/slirptest-" + cid + "-tap0"
		pid, err := getSlirpPID(pidFile)
		require.NoError(t, err, "lookup non-existing/invalid PID file of %s", cid)
		assert.Equal(t, 0, pid, "lookup of non-existing/invalid PID file should return 0")
	}
}
