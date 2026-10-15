// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package netmon

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"

	"tailscale.com/tstest"
)

// test the specific /proc/net/route path as found on Google Cloud Run instances
func TestGoogleCloudRunDefaultRouteInterface(t *testing.T) {
	dir := t.TempDir()
	tstest.Replace(t, &procNetRoutePath, filepath.Join(dir, "CloudRun"))
	buf := []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t8008FEA9\t00000000\t0001\t0\t0\t0\t01FFFFFF\t0\t0\t0\n" +
		"eth1\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n")
	err := os.WriteFile(procNetRoutePath, buf, 0644)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DefaultRouteInterface()
	if err != nil {
		t.Fatal(err)
	}

	if got != "eth1" {
		t.Fatalf("got %s, want eth1", got)
	}
}

// we read chunks of /proc/net/route at a time, test that files longer than the chunk
// size can be handled.
func TestExtremelyLongProcNetRoute(t *testing.T) {
	dir := t.TempDir()
	tstest.Replace(t, &procNetRoutePath, filepath.Join(dir, "VeryLong"))
	f, err := os.Create(procNetRoutePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n"))
	if err != nil {
		t.Fatal(err)
	}

	for n := 0; n <= 900; n++ {
		line := fmt.Sprintf("eth%d\t8008FEA9\t00000000\t0001\t0\t0\t0\t01FFFFFF\t0\t0\t0\n", n)
		_, err := f.Write([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.Write([]byte("tokenring1\t00000000\t00000000\t0001\t0\t0\t0\t00000000\t0\t0\t0\n"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := DefaultRouteInterface()
	if err != nil {
		t.Fatal(err)
	}

	if got != "tokenring1" {
		t.Fatalf("got %q, want tokenring1", got)
	}
}

// test the specific /proc/net/route path as found on AWS App Runner instances
func TestAwsAppRunnerDefaultRouteInterface(t *testing.T) {
	dir := t.TempDir()
	tstest.Replace(t, &procNetRoutePath, filepath.Join(dir, "CloudRun"))
	buf := []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\tF9AFFEA9\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"*\tFEA9FEA9\t00000000\t0005\t0\t0\t0\tFFFFFFFF\t0\t0\t0\n" +
		"ecs-eth0\t02AAFEA9\t01ACFEA9\t0007\t0\t0\t0\tFFFFFFFF\t0\t0\t0\n" +
		"ecs-eth0\t00ACFEA9\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n" +
		"eth0\t00AFFEA9\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n")
	err := os.WriteFile(procNetRoutePath, buf, 0644)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DefaultRouteInterface()
	if err != nil {
		t.Fatal(err)
	}

	if got != "eth0" {
		t.Fatalf("got %s, want eth0", got)
	}
}

func BenchmarkDefaultRouteInterface(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		if _, err := DefaultRouteInterface(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRouteLinuxNetlink(t *testing.T) {
	d, err := defaultRouteFromNetlink()
	if errors.Is(err, fs.ErrPermission) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Got: %+v", d)
}

// test that link-local IPv4 counts as usable only on the default route
func TestGetStateLinkLocalDefaultRoute(t *testing.T) {
	ipnet := func(s string) net.Addr {
		ip, ipnet, err := net.ParseCIDR(s)
		ipnet.IP = ip
		if err != nil {
			t.Fatal(err)
		}
		return ipnet
	}

	// Mock out the responses from netInterfaces()
	tstest.Replace(t, &altNetInterfaces, func() ([]Interface, error) {
		return []Interface{
			{
				Interface: &net.Interface{
					Index: 1,
					MTU:   1500,
					Name:  "fake0",
					Flags: net.FlagUp |
						net.FlagBroadcast |
						net.FlagMulticast |
						net.FlagRunning,
				},
				AltAddrs: []net.Addr{
					ipnet("169.254.100.2/30"),
				},
			},
		}, nil
	})

	tests := []struct {
		name string
		buf  []byte
		want bool
	}{
		{
			name: "default_route",
			buf: []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
				"fake0\t00000000\t0164FEA9\t0003\t0\t0\t0\t00000000\t0\t0\t0\n"),
			want: true,
		},
		{
			name: "no_default_route",
			buf:  []byte("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tstest.Replace(t, &procNetRoutePath, filepath.Join(t.TempDir(), "route"))
			err := os.WriteFile(procNetRoutePath, tt.buf, 0644)
			if err != nil {
				t.Fatal(err)
			}

			st, err := getState("")
			if err != nil {
				t.Fatal(err)
			}
			if st.HaveV4 != tt.want {
				t.Errorf("HaveV4 = %v; want %v", st.HaveV4, tt.want)
			}
		})
	}
}
