// SPDX-FileCopyrightText: Copyright The Lima Authors
// SPDX-License-Identifier: Apache-2.0

package qemu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/coreos/go-semver/semver"
	"gotest.tools/v3/assert"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
)

func TestArgValue(t *testing.T) {
	type testCase struct {
		key           string
		expectedValue string
		expectedOK    bool
	}
	args := []string{"-cpu", "foo", "-no-reboot", "-m", "2G", "-s"}
	testCases := []testCase{
		{
			key:           "-cpu",
			expectedValue: "foo",
			expectedOK:    true,
		},
		{
			key:           "-no-reboot",
			expectedValue: "",
			expectedOK:    true,
		},
		{
			key:           "-m",
			expectedValue: "2G",
			expectedOK:    true,
		},
		{
			key:           "-machine",
			expectedValue: "",
			expectedOK:    false,
		},
		{
			key:           "-s",
			expectedValue: "",
			expectedOK:    true,
		},
	}

	for _, tc := range testCases {
		v, ok := argValue(args, tc.key)
		assert.Equal(t, tc.expectedValue, v)
		assert.Equal(t, tc.expectedOK, ok)
	}
}

func TestParseQemuVersion(t *testing.T) {
	type testCase struct {
		versionOutput string
		expectedValue string
		expectedError string
	}
	testCases := []testCase{
		{
			// old one line version
			versionOutput: "QEMU emulator version 1.5.3 (qemu-kvm-1.5.3-175.el7_9.6), " +
				"Copyright (c) 2003-2008 Fabrice Bellard\n",
			expectedValue: "1.5.3",
			expectedError: "",
		},
		{
			// new two line version
			versionOutput: "QEMU emulator version 8.0.0 (v8.0.0)\n" +
				"Copyright (c) 2003-2022 Fabrice Bellard and the QEMU Project developers\n",
			expectedValue: "8.0.0",
			expectedError: "",
		},
		{
			versionOutput: "foobar",
			expectedValue: "0.0.0",
			expectedError: "failed to parse",
		},
	}

	for _, tc := range testCases {
		v, err := parseQemuVersion(tc.versionOutput)
		if tc.expectedError == "" {
			assert.NilError(t, err)
		} else {
			assert.ErrorContains(t, err, tc.expectedError)
		}
		assert.Equal(t, tc.expectedValue, v.String())
	}
}

func TestParseVirtiofsdVersion(t *testing.T) {
	testCases := []struct {
		name          string
		versionOutput string
		expectedValue string
		expectedError string
	}{
		{
			name:          "release",
			versionOutput: "virtiofsd 1.13.0\n",
			expectedValue: "1.13.0",
		},
		{
			name:          "unrecognized output",
			versionOutput: "virtiofsd development build\n",
			expectedError: "failed to parse virtiofsd version",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			version, err := parseVirtiofsdVersion(tc.versionOutput)
			if tc.expectedError == "" {
				assert.NilError(t, err)
				assert.Equal(t, version.String(), tc.expectedValue)
			} else {
				assert.ErrorContains(t, err, tc.expectedError)
			}
		})
	}
}

func TestVirtiofsdCmdline(t *testing.T) {
	guestGID := uint32(1000)
	writable := true
	readOnly := false
	instanceDir := t.TempDir()
	cfg := Config{
		InstanceDir: instanceDir,
		LimaYAML: &limatype.LimaYAML{
			User: limatype.User{UID: &guestGID},
			Mounts: []limatype.Mount{
				{Location: "/first", Writable: &writable},
				{Location: "/second", Writable: &writable},
				{Location: "/read-only", Writable: &readOnly},
			},
		},
	}
	translationVersion := semver.New("1.13.0")
	oldVersion := semver.New("1.12.0")

	testCases := []struct {
		name             string
		mountIndex       int
		hostGID          uint32
		virtiofsdVersion *semver.Version
		expectedArgs     []string
		expectedError    string
	}{
		{
			name:             "mismatched GID first mount",
			mountIndex:       0,
			hostGID:          100,
			virtiofsdVersion: translationVersion,
			expectedArgs: []string{
				"--socket-path", filepath.Join(instanceDir, "virtiofsd-0.sock"),
				"--shared-dir", "/first",
				"--translate-gid", "map:1000:100:1",
			},
		},
		{
			name:             "mismatched GID second mount",
			mountIndex:       1,
			hostGID:          100,
			virtiofsdVersion: translationVersion,
			expectedArgs: []string{
				"--socket-path", filepath.Join(instanceDir, "virtiofsd-1.sock"),
				"--shared-dir", "/second",
				"--translate-gid", "map:1000:100:1",
			},
		},
		{
			name:             "matching GID",
			mountIndex:       0,
			hostGID:          1000,
			virtiofsdVersion: oldVersion,
			expectedArgs: []string{
				"--socket-path", filepath.Join(instanceDir, "virtiofsd-0.sock"),
				"--shared-dir", "/first",
			},
		},
		{
			name:             "old virtiofsd",
			mountIndex:       0,
			hostGID:          100,
			virtiofsdVersion: oldVersion,
			expectedError:    "upgrade virtiofsd to 1.13.0 or later",
		},
		{
			name:          "unknown virtiofsd version",
			mountIndex:    0,
			hostGID:       100,
			expectedError: "could not verify support for GID translation",
		},
		{
			name:             "read-only mount",
			mountIndex:       2,
			hostGID:          100,
			virtiofsdVersion: oldVersion,
			expectedArgs: []string{
				"--socket-path", filepath.Join(instanceDir, "virtiofsd-2.sock"),
				"--shared-dir", "/read-only",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := VirtiofsdCmdline(cfg, tc.mountIndex, tc.hostGID, tc.virtiofsdVersion)
			if tc.expectedError == "" {
				assert.NilError(t, err)
				assert.DeepEqual(t, args, tc.expectedArgs)
			} else {
				assert.ErrorContains(t, err, tc.expectedError)
			}
		})
	}
}

func TestSwtpmCmdline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("swtpm unix socket mode is not supported on Windows host")
	}

	tmpDir := t.TempDir()

	// Create a mock swtpm binary.
	binDir := filepath.Join(tmpDir, "bin")
	err := os.MkdirAll(binDir, 0o755)
	assert.NilError(t, err)
	swtpmPath := filepath.Join(binDir, "swtpm")
	err = os.WriteFile(swtpmPath, []byte{}, 0o755)
	assert.NilError(t, err)

	// Overwrite PATH so that the function find the mock binary.
	t.Setenv("PATH", binDir)

	// Setup configs and expected value
	cfg := Config{
		Name:        "tpm-test",
		InstanceDir: tmpDir,
		LimaYAML:    &limatype.LimaYAML{},
	}

	stateDir := filepath.Join(tmpDir, filenames.SwtpmDir)
	swtpmSock := filepath.Join(tmpDir, filenames.SwtpmSock)

	expectedArgs := []string{
		"socket",
		"--tpmstate", "dir=" + stateDir,
		"--ctrl", "type=unixio,path=" + swtpmSock,
		"--tpm2",
		"--terminate",
		"--log", "level=1",
	}

	exe, args, err := SwtpmCmdline(cfg)
	assert.NilError(t, err)
	assert.Equal(t, exe, swtpmPath)
	assert.DeepEqual(t, args, expectedArgs)

	// Verify that state directory was created.
	_, err = os.Stat(stateDir)
	assert.NilError(t, err)

	// Verify that stale socket is removed.
	err = os.WriteFile(swtpmSock, []byte("stale socket"), 0o644)
	assert.NilError(t, err)
	// Call again to clean up the stale socket.
	_, _, err = SwtpmCmdline(cfg)
	assert.NilError(t, err)
	_, err = os.Stat(swtpmSock)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
