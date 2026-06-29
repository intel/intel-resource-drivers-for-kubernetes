//
// Copyright (C) 2024-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package device

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

//nolint:cyclop // test code
func TestNew(t *testing.T) {
	orig := sysfsRoot
	t.Cleanup(func() { sysfsRoot = orig })

	testcases := []struct {
		name                string
		setup               func(root string)
		qatDevices          fakesysfs.QATDevices
		wantPFs             int
		wantTotalVFs        int
		brokenSymlinkBefore bool
		reuseAllocated      bool
		wantAvailableAfter  int
	}{
		{
			name:       "no devices",
			qatDevices: nil,
			wantPFs:    0, wantTotalVFs: 0,
		},
		{
			name: "one device with 3 VFs",
			qatDevices: fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: "sym;asym",
					NumVFs:   3,
					TotalVFs: 3,
				},
			},
			wantPFs: 1, wantTotalVFs: 3,
		},
		{
			name: "one device with broken VF symlink ignored",
			qatDevices: fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: "sym",
					NumVFs:   2,
					TotalVFs: 2,
				},
			},
			wantPFs: 1, wantTotalVFs: 2,
			brokenSymlinkBefore: true,
		},
		{
			name: "one device VF reused from AllocatedDevices on rescan",
			qatDevices: fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: "sym",
					NumVFs:   2,
					TotalVFs: 2,
				},
			},
			wantPFs:            1,
			wantTotalVFs:       2, // initial available before allocation
			reuseAllocated:     true,
			wantAvailableAfter: 1, // after allocating one VF
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			root := t.TempDir()
			sysfsRoot = ""
			t.Setenv("SYSFS_ROOT", root)

			if err := fakesysfs.FakeSysFsQATContents(root, testcase.qatDevices); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			// Inject broken symlink before New() to exercise getVFs error path (lines 394-397).
			if testcase.brokenSymlinkBefore {
				devID := "0000:4b:00.0"
				vfDir := filepath.Join(sysfsDevicePath(), devID)
				if err := os.MkdirAll(vfDir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.Symlink("/nonexistent/0000:4b:00.9", filepath.Join(vfDir, "virtfn999")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			}

			devs, err := New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if len(devs) != testcase.wantPFs {
				t.Fatalf("PF count want %d got %d", testcase.wantPFs, len(devs))
			}
			vfCount := 0
			for _, pf := range devs {
				vfCount += len(pf.AvailableDevices)
			}
			if vfCount != testcase.wantTotalVFs {
				t.Fatalf("VF count want %d got %d", testcase.wantTotalVFs, vfCount)
			}

			// Trigger reuse path (lines 394-398) by allocating then rescanning.
			if testcase.reuseAllocated {
				pf := devs[0]
				var vf *VFDevice
				for _, v := range pf.AvailableDevices {
					vf = v
					break
				}
				if vf == nil {
					t.Fatal("no VF to allocate")
				}
				_, err := pf.Allocate(vf.UID(), "claimX")
				if err != nil {
					t.Fatalf("allocate: %v", err)
				}
				availBefore := len(pf.AvailableDevices)
				allocBefore := len(pf.AllocatedDevices["claimX"])
				if err := pf.getVFs(); err != nil {
					t.Fatalf("getVFs: %v", err)
				}
				if len(pf.AvailableDevices) != availBefore {
					t.Fatalf("available count changed %d -> %d", availBefore, len(pf.AvailableDevices))
				}
				if len(pf.AllocatedDevices["claimX"]) != allocBefore {
					t.Fatalf("allocated count changed %d -> %d", allocBefore, len(pf.AllocatedDevices["claimX"]))
				}
				if _, ok := pf.AvailableDevices[vf.UID()]; ok {
					t.Fatal("allocated VF re-added to AvailableDevices")
				}
				if testcase.wantAvailableAfter >= 0 && len(pf.AvailableDevices) != testcase.wantAvailableAfter {
					t.Fatalf("available after want %d got %d", testcase.wantAvailableAfter, len(pf.AvailableDevices))
				}
			}
		})
	}
}

func TestVFDeviceDriver(t *testing.T) {
	tests := []struct {
		name string
		d    VFDriver
		want string
	}{
		{"unbound", Unbound, ""},
		{"vfio", VfioPci, "vfio-pci"},
		{"unknown", Unknown, "unknown"},
	}
	for _, tc := range tests {
		v := &VFDevice{VFDriver: tc.d}
		if got := v.Driver(); got != tc.want {
			t.Fatalf("%s: want '%s' got '%s'", tc.name, tc.want, got)
		}
	}
}

func TestServicesToString(t *testing.T) {
	type testCase struct {
		service Services
		str     string
	}

	testcases := []testCase{
		{None, ""},
		{Sym, "sym"},
		{Asym, "asym"},
		{Dc, "dc"},
		{Dcc, "dcc"},
		{Unset, ""},
		{Sym | Asym, "sym;asym"},
		{Dc | Asym, "asym;dc"},
		{Dcc | Asym, "asym;dcc"},
		{Dc | Dcc, "dc;dcc"},
		{0xffff, "sym;asym;dc;dcc"},
	}

	for _, test := range testcases {
		if test.service.String() == test.str {
			continue
		}

		t.Errorf("test service string '%s' does not match '%s'",
			test.service.String(), test.str)
	}
}

func TestStringToServices(t *testing.T) {
	type testCase struct {
		str     string
		service Services
		pass    bool
	}

	testcases := []testCase{
		{"sym", Sym, true},
		{"asym", Asym, true},
		{"dc", Dc, true},
		{"dcc", Dcc, true},
		{"dccc", Unset, false},
		{"xyz", Unset, false},
		{"sym;", Sym, true},
		{";sym", Sym, true},
		{";asym;sym", Sym | Asym, true},
		{"sym;asym", Sym | Asym, true},
		{"sym;sym;sym", Sym, true},
		{"sym;asym;sym;asym", Sym | Asym, true},
		{"sym;asym;sym;asym", Sym | Asym, true},
		{"dc;dcc;sym;asym;sym;asym", Dc | Dcc | Sym | Asym, true},
		{"sym;asym;xyz", Unset, false},
		{"", None, true},
		{"   ", Unset, false},
		{";;;", None, true},
	}

	for _, test := range testcases {
		service, err := StringToServices(test.str)

		if (test.pass == (err == nil)) && service == test.service {
			continue
		}

		t.Errorf("test string '%s' does not result in '%s'",
			test.str, test.service.String())
	}
}

func TestServicesSupport(t *testing.T) {
	type testCase struct {
		service  Services
		supports Services
		pass     bool
	}

	testcases := []testCase{
		{Sym, Sym, true},
		{Sym | Asym, Sym, true},
		{Sym | Asym | Dc, Asym, true},
		{Sym | Asym, Dc, false},
		{Dc | Dcc | Asym, Sym, false},
		{Dc | Dcc | Asym, None, false},
		{Dc | Dcc | Asym, Unset, true},
		{None, Sym, false},
		{None, None, true},
		{None, Unset, true},
		{Unset, None, false},
	}

	for _, test := range testcases {
		if test.service.Supports(test.supports) == test.pass {
			continue
		}

		t.Errorf("service '%s' supports '%s'", test.service.String(), test.supports.String())
	}
}

func TestState(t *testing.T) {
	type testCase struct {
		state State
		str   string
	}

	testcases := []testCase{
		{Up, "up"},
		{Down, "down"},
		{15, ""},
	}

	for _, test := range testcases {
		if test.state.String() != test.str {
			t.Errorf("state '%s' does not match '%s'", test.state.String(), test.str)
		}
	}
}

func CompareVFDevices(vfdevice *VFDevice, expected *VFDevice) error {

	if vfdevice.pfdevice != nil && expected.pfdevice != nil && vfdevice.pfdevice.Device != expected.pfdevice.Device {
		return fmt.Errorf("VF device parent PF device '%s', expected '%s", vfdevice.pfdevice.Device, expected.pfdevice.Device)
	}
	if vfdevice.UID() != expected.VFDevice {
		return fmt.Errorf("VF device '%s', expected '%s'", vfdevice.UID(), expected.VFDevice)
	}
	if vfdevice.VFDriver != expected.VFDriver {
		return fmt.Errorf("VF driver '%s', expected '%s'", vfdevice.VFDriver.String(), expected.VFDriver.String())
	}
	if vfdevice.VFIommu != expected.VFIommu {
		return fmt.Errorf("VF iommu '%s', expected '%s'", vfdevice.VFIommu, expected.VFIommu)
	}

	return nil
}

func ComparePFDevices(pfdevice *PFDevice, expected *PFDevice) error {
	if pfdevice.AllowReconfiguration != expected.AllowReconfiguration {
		return fmt.Errorf("AllowReconfiguration is %v, expected %v", pfdevice.AllowReconfiguration, expected.AllowReconfiguration)
	}
	if pfdevice.Device != expected.Device {
		return fmt.Errorf("PF device is '%s', expected '%s'", pfdevice.Device, expected.Device)
	}
	if pfdevice.State != expected.State {
		return fmt.Errorf("PF device state is '%s', expected '%s'", pfdevice.State.String(), expected.State.String())
	}
	if pfdevice.Services != expected.Services {
		return fmt.Errorf("PF device state is '%s', expected '%s'", pfdevice.Services.String(), expected.Services.String())
	}
	if pfdevice.NumVFs != expected.NumVFs {
		return fmt.Errorf("PF device state is %d, expected %d", pfdevice.NumVFs, expected.NumVFs)
	}
	if pfdevice.TotalVFs != expected.TotalVFs {
		return fmt.Errorf("PF device state is %d, expected %d", pfdevice.TotalVFs, expected.TotalVFs)
	}

	if len(pfdevice.AvailableDevices) != len(expected.AvailableDevices) {
		return fmt.Errorf("VF AvailableDevices %d, expected %d", len(pfdevice.AvailableDevices), len(expected.AvailableDevices))
	}

	for vf, vfdevice := range pfdevice.AvailableDevices {
		vfexpected, exists := expected.AvailableDevices[vf]
		if !exists {
			return fmt.Errorf("VF device '%s' was not expected in AvailableDevices", vf)
		}
		if err := CompareVFDevices(vfdevice, vfexpected); err != nil {
			return err
		}
	}
	return nil
}

func CompareQATDevices(qatdevices QATDevices, expected QATDevices) error {
	if len(qatdevices) != len(expected) {
		return fmt.Errorf("length of QAT devices is %d, expected %d", len(qatdevices), len(expected))
	}
	for i := 0; i < len(qatdevices); i++ {
		err := ComparePFDevices(qatdevices[i], expected[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func TestCheckAlreadyAllocated(t *testing.T) {
	orig := sysfsRoot
	t.Cleanup(func() { sysfsRoot = orig })

	testcases := []struct {
		name                 string
		servicesInitial      string
		requestService       Services
		checkRequester       string
		preAllocate          bool
		preAllocateRequester string
		want                 bool
	}{
		{
			name:                 "success when PF supports service and VF already allocated to requester",
			servicesInitial:      "sym",
			requestService:       Sym,
			checkRequester:       "claimA",
			preAllocate:          true,
			preAllocateRequester: "claimA",
			want:                 true,
		},
		{
			name:            "fail when PF supports service but VF not allocated to requester",
			servicesInitial: "sym",
			requestService:  Sym,
			checkRequester:  "claimA",
			preAllocate:     false,
			want:            false,
		},
		{
			name:                 "fail when VF allocated to different requester",
			servicesInitial:      "sym",
			requestService:       Sym,
			checkRequester:       "claimA",
			preAllocate:          true,
			preAllocateRequester: "claimB",
			want:                 false,
		},
		{
			name:                 "fail when PF does not support requested service",
			servicesInitial:      "sym",
			requestService:       Dc,
			checkRequester:       "claimA",
			preAllocate:          true,
			preAllocateRequester: "claimA",
			want:                 false,
		},
		{
			name:            "fail when requester is empty",
			servicesInitial: "sym",
			requestService:  Sym,
			checkRequester:  "",
			preAllocate:     false,
			want:            false,
		},
		{
			name:                 "success when service Unset and VF already allocated to requester",
			servicesInitial:      "sym;asym",
			requestService:       Unset,
			checkRequester:       "claimA",
			preAllocate:          true,
			preAllocateRequester: "claimA",
			want:                 true,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(DriverName)
			defer testhelpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
			if err != nil {
				t.Errorf("%v: setup error: %v", testcase.name, err)
				return
			}
			sysfsRoot = ""
			t.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

			if err := fakesysfs.FakeSysFsQATContents(testDirs.SysfsRoot, fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: testcase.servicesInitial,
					NumVFs:   2,
					TotalVFs: 2,
				},
			}); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			devs, err := New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if len(devs) != 1 {
				t.Fatalf("want 1 PF got %d", len(devs))
			}
			pf := devs[0]

			// pick one available VF
			var vf *VFDevice
			for _, v := range pf.AvailableDevices {
				vf = v
				break
			}
			if vf == nil {
				t.Fatal("no VF available to test")
			}

			// Optionally pre-allocate VF
			if testcase.preAllocate {
				_, err := pf.Allocate(vf.UID(), testcase.preAllocateRequester)
				if err != nil {
					t.Fatalf("preAllocate failed: %v", err)
				}
			}

			got := vf.CheckAlreadyAllocated(testcase.requestService, testcase.checkRequester)
			if got != testcase.want {
				t.Fatalf("want %v got %v", testcase.want, got)
			}
		})
	}
}

func TestAllocateWithReconfiguration(t *testing.T) {
	tests := []struct {
		name            string
		servicesInitial string
		enableReconfig  bool
		requestService  Services
		wantSuccess     bool
		wantServices    Services
	}{
		{
			name:            "success when None and reconfig enabled",
			servicesInitial: "",
			enableReconfig:  true,
			requestService:  Sym,
			wantSuccess:     true,
			wantServices:    Sym,
		},
		{
			name:            "fail when already configured (Sym)",
			servicesInitial: "sym",
			enableReconfig:  true,
			requestService:  Asym,
			wantSuccess:     false,
			wantServices:    Sym,
		},
		{
			name:            "fail when reconfig disabled",
			servicesInitial: "",
			enableReconfig:  false,
			requestService:  Asym,
			wantSuccess:     false,
			wantServices:    None,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := sysfsRoot
			t.Cleanup(func() { sysfsRoot = orig })

			root := t.TempDir()
			sysfsRoot = ""
			t.Setenv("SYSFS_ROOT", root)

			if err := fakesysfs.FakeSysFsQATContents(root, fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: tc.servicesInitial,
					NumVFs:   2,
					TotalVFs: 2,
				},
			}); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			devs, err := New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if len(devs) != 1 {
				t.Fatalf("want 1 PF got %d", len(devs))
			}
			pf := devs[0]
			pf.EnableReconfiguration(tc.enableReconfig)

			// pick one available VF
			var vf *VFDevice
			for _, v := range pf.AvailableDevices {
				vf = v
				break
			}
			if vf == nil {
				t.Fatal("no VF available to test")
			}

			ok := vf.AllocateWithReconfiguration(tc.requestService, "claimX")
			if ok != tc.wantSuccess {
				t.Fatalf("want success=%v got %v", tc.wantSuccess, ok)
			}

			if pf.Services.String() != tc.wantServices.String() {
				t.Fatalf("PF services want '%s' got '%s'", tc.wantServices.String(), pf.Services.String())
			}

			_, allocatedExists := pf.AllocatedDevices["claimX"][vf.UID()]
			if tc.wantSuccess && !allocatedExists {
				t.Fatal("VF not allocated after success")
			}
			if !tc.wantSuccess && allocatedExists {
				t.Fatal("VF allocated unexpectedly")
			}
		})
	}
}

//nolint:cyclop // test code
func TestAllocateFromConfigured(t *testing.T) {
	orig := sysfsRoot
	t.Cleanup(func() { sysfsRoot = orig })

	subtests := []struct {
		name            string
		servicesInitial string
		requestService  Services
		requester       string
		preAllocate     bool
		wantSuccess     bool
	}{
		{
			name:            "success when configured PF and requester is valid",
			servicesInitial: "sym;asym",
			requestService:  Sym,
			requester:       "claimA",
			preAllocate:     false,
			wantSuccess:     true,
		},
		{
			name:            "success when PF has sym and request asym (service mismatch ignored)",
			servicesInitial: "sym",
			requestService:  Asym,
			requester:       "claimB",
			preAllocate:     false,
			wantSuccess:     true,
		},
		{
			name:            "fail when requester is empty",
			servicesInitial: "sym",
			requestService:  Sym,
			requester:       "",
			preAllocate:     false,
			wantSuccess:     false,
		},
		{
			name:            "fail when VF is already allocated to another requester",
			servicesInitial: "sym;asym",
			requestService:  Sym,
			requester:       "claimY",
			preAllocate:     true, // allocate first to claimX, then attempt with claimY
			wantSuccess:     false,
		},
	}

	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			root := t.TempDir()
			sysfsRoot = ""
			t.Setenv("SYSFS_ROOT", root)

			if err := fakesysfs.FakeSysFsQATContents(root, fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: st.servicesInitial,
					NumVFs:   2,
					TotalVFs: 2,
				},
			}); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			devs, err := New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if len(devs) != 1 {
				t.Fatalf("want 1 PF got %d", len(devs))
			}
			pf := devs[0]

			// pick one available VF
			var vf *VFDevice
			for _, v := range pf.AvailableDevices {
				vf = v
				break
			}
			if vf == nil {
				t.Fatal("no VF available to test")
			}

			// Optionally pre-allocate VF to another requester to simulate non-availability.
			if st.preAllocate {
				_, err := pf.Allocate(vf.UID(), "claimX")
				if err != nil {
					t.Fatalf("preAllocate failed: %v", err)
				}
				// After pre-allocation, vf is no longer in AvailableDevices
				if _, ok := pf.AvailableDevices[vf.UID()]; ok {
					t.Fatal("vf should not be available after preAllocate")
				}
			}

			ok := vf.AllocateFromConfigured(st.requestService, st.requester)
			if ok != st.wantSuccess {
				t.Fatalf("want success=%v got %v", st.wantSuccess, ok)
			}

			if st.wantSuccess {
				// Verify VF moved to AllocatedDevices under the requester
				if _, ok := pf.AllocatedDevices[st.requester][vf.UID()]; !ok {
					t.Fatalf("VF not allocated under requester %s", st.requester)
				}
				if _, ok := pf.AvailableDevices[vf.UID()]; ok {
					t.Fatal("VF still in AvailableDevices after allocation")
				}
			} else {
				// Verify no new allocation under the requester and VF availability unchanged
				if st.requester != "" {
					if _, ok := pf.AllocatedDevices[st.requester]; ok {
						if _, ok2 := pf.AllocatedDevices[st.requester][vf.UID()]; ok2 {
							t.Fatalf("VF allocated unexpectedly under requester %s", st.requester)
						}
					}
				}
				// If not pre-allocated, VF should remain available
				if !st.preAllocate {
					if _, ok := pf.AvailableDevices[vf.UID()]; !ok {
						t.Fatal("VF not in AvailableDevices after failed allocation")
					}
				} else {
					// If pre-allocated to claimX, ensure it remains there
					if _, ok := pf.AllocatedDevices["claimX"][vf.UID()]; !ok {
						t.Fatal("VF not retained under pre-allocation requester claimX")
					}
				}
			}
		})
	}
}

func TestGetControlNode(t *testing.T) {
	ctrl, err := GetControlNode()
	if err != nil {
		t.Fatalf("GetControlNode error: %v", err)
	}
	if ctrl == nil {
		t.Fatal("expected control node, got nil")
	}
	if ctrl.VFDevice != "vfio" {
		t.Fatalf("VFDevice want 'vfio' got '%s'", ctrl.VFDevice)
	}
	if ctrl.VFDriver != VfioPci {
		t.Fatalf("VFDriver want VfioPci got %v", ctrl.VFDriver)
	}
	if ctrl.VFIommu != "vfio" {
		t.Fatalf("VFIommu want 'vfio' got '%s'", ctrl.VFIommu)
	}
	// pfdevice should be nil for control node
	if ctrl.pfdevice != nil {
		t.Fatal("pfdevice expected nil for control node")
	}
	// Derived helpers
	if ctrl.Driver() != "vfio-pci" {
		t.Fatalf("Driver() want 'vfio-pci' got '%s'", ctrl.Driver())
	}
	if ctrl.DeviceNode() != "/dev/vfio/vfio" {
		t.Fatalf("DeviceNode() want '/dev/vfio/vfio' got '%s'", ctrl.DeviceNode())
	}
	if ctrl.PCIDevice() != "vfio" {
		t.Fatalf("PCIDevice() want 'vfio' got '%s'", ctrl.PCIDevice())
	}
}

func TestGetCDIDevices(t *testing.T) {
	orig := sysfsRoot
	t.Cleanup(func() { sysfsRoot = orig })

	root := t.TempDir()
	sysfsRoot = ""
	t.Setenv("SYSFS_ROOT", root)

	if err := fakesysfs.FakeSysFsQATContents(root, fakesysfs.QATDevices{
		{
			Device:   "0000:4b:00.0",
			State:    "up",
			Services: "sym;asym",
			NumVFs:   2,
			TotalVFs: 2,
		},
	}); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
	}

	devs, err := New()
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("want 1 PF got %d", len(devs))
	}
	pf := devs[0]

	// Allocate one VF to move it from AvailableDevices to AllocatedDevices.
	vfAlloc, err := pf.Allocate("", "claimA")
	if err != nil {
		t.Fatalf("Allocate error: %v", err)
	}
	if vfAlloc == nil {
		t.Fatal("allocated VF is nil")
	}

	cdidevs := GetCDIDevices(devs)
	if len(cdidevs) != pf.TotalVFs {
		t.Fatalf("CDI devices count want %d got %d", pf.TotalVFs, len(cdidevs))
	}

	// All returned devices should share PF services and include allocated VF.
	for uid, v := range cdidevs {
		if v.Services() != pf.Services.String() {
			t.Fatalf("device %s services want %s got %s", uid, pf.Services.String(), v.Services())
		}
	}
	if _, ok := cdidevs[vfAlloc.UID()]; !ok {
		t.Fatalf("allocated VF %s not present in CDI devices", vfAlloc.UID())
	}
}

//nolint:cyclop // test code
func TestFree(t *testing.T) {
	subtests := []struct {
		name              string
		servicesInitial   string
		allowReconfig     bool
		multiAlloc        bool
		expectUpdates     []bool
		expectFinalSrv    Services
		expectErrorCase   bool
		errorOnWrongClaim bool
		useEmptyRequester bool
	}{
		{
			name:            "free allocated no reconfig",
			servicesInitial: "sym;asym",
			allowReconfig:   false,
			multiAlloc:      false,
			expectUpdates:   []bool{false},
			expectFinalSrv:  Sym | Asym,
		},
		{
			name:            "free allocated with reconfig",
			servicesInitial: "sym",
			allowReconfig:   true,
			multiAlloc:      false,
			expectUpdates:   []bool{true},
			expectFinalSrv:  None,
		},
		{
			name:              "free wrong requester",
			servicesInitial:   "sym",
			allowReconfig:     false,
			multiAlloc:        false,
			expectUpdates:     []bool{false},
			expectFinalSrv:    Sym,
			expectErrorCase:   true,
			errorOnWrongClaim: true,
		},
		{
			name:            "free two allocations with reconfig only after last",
			servicesInitial: "sym",
			allowReconfig:   true,
			multiAlloc:      true,
			expectUpdates:   []bool{false, true},
			expectFinalSrv:  None,
		},
		{
			name:              "free allocated via empty requester (auto lookup)",
			servicesInitial:   "sym",
			allowReconfig:     true,
			multiAlloc:        false,
			expectUpdates:     []bool{true},
			expectFinalSrv:    None,
			useEmptyRequester: true,
		},
	}

	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			orig := sysfsRoot
			t.Cleanup(func() { sysfsRoot = orig })

			root := t.TempDir()
			sysfsRoot = ""
			t.Setenv("SYSFS_ROOT", root)

			if err := fakesysfs.FakeSysFsQATContents(root, fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					Services: st.servicesInitial,
					NumVFs:   3,
					TotalVFs: 3,
				},
			}); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			devs, err := New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}
			if len(devs) != 1 {
				t.Fatalf("want 1 PF got %d", len(devs))
			}
			pf := devs[0]
			pf.EnableReconfiguration(st.allowReconfig)

			// Allocate first VF
			var vf1 *VFDevice
			for _, v := range pf.AvailableDevices {
				vf1 = v
				break
			}
			if vf1 == nil {
				t.Fatal("no VF available to test")
			}
			vf1, err = pf.Allocate(vf1.UID(), "claimX")
			if err != nil {
				t.Fatalf("allocate vf1: %v", err)
			}

			var vf2 *VFDevice
			if st.multiAlloc {
				for _, v := range pf.AvailableDevices {
					vf2 = v
					break
				}
				if vf2 == nil {
					t.Fatal("no second VF available")
				}
				vf2, err = pf.Allocate(vf2.UID(), "claimX")
				if err != nil {
					t.Fatalf("allocate vf2: %v", err)
				}
			}

			if st.errorOnWrongClaim {
				_, err := vf1.Free("claimY")
				if err == nil {
					t.Fatal("expected error freeing with wrong claim ID")
				}
				return
			}

			requester := "claimX"
			if st.useEmptyRequester {
				requester = ""
			}

			// Free first (possibly via empty requester to hit auto lookup branch)
			update, err := vf1.Free(requester)
			if err != nil {
				t.Fatalf("free vf1: %v", err)
			}
			if update != st.expectUpdates[0] {
				t.Fatalf("vf1 update want %v got %v", st.expectUpdates[0], update)
			}

			if st.multiAlloc {
				update2, err := vf2.Free("claimX")
				if err != nil {
					t.Fatalf("free vf2: %v", err)
				}
				if update2 != st.expectUpdates[1] {
					t.Fatalf("vf2 update want %v got %v", st.expectUpdates[1], update2)
				}
			}

			if pf.Services != st.expectFinalSrv {
				t.Fatalf("final PF services want '%s' got '%s'", st.expectFinalSrv.String(), pf.Services.String())
			}

			if _, ok := pf.AvailableDevices[vf1.UID()]; !ok {
				t.Fatal("vf1 not returned to AvailableDevices")
			}
			if st.multiAlloc {
				if _, ok := pf.AvailableDevices[vf2.UID()]; !ok {
					t.Fatal("vf2 not returned to AvailableDevices")
				}
			}
		})
	}
}

func TestCDIName(t *testing.T) {
	tests := []struct {
		name    string
		vfDev   string
		wantUID string
	}{
		{
			name:    "basic VF device",
			vfDev:   "0000:4b:00.1",
			wantUID: "qatvf-0000-4b-00-1",
		},
		{
			name:    "another VF device",
			vfDev:   "0000:af:12.3",
			wantUID: "qatvf-0000-af-12-3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pf := &PFDevice{} // minimal PFDevice
			v := &VFDevice{
				pfdevice: pf,
				VFDevice: tc.vfDev,
			}
			got := v.CDIName()
			want := fmt.Sprintf("%s=%s", CDIKind, tc.wantUID)
			if got != want {
				t.Fatalf("CDIName() got %s want %s", got, want)
			}
		})
	}
}
