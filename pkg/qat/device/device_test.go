/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package device

import (
	"fmt"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
)

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

func TestDevices(t *testing.T) {
	setupdevices := fakesysfs.QATDevices{
		{Device: "0000:aa:00.0",
			State:    "down",
			Services: "sym;asym",
			TotalVFs: 3,
			NumVFs:   0,
		},
		{Device: "0000:bb:00.0",
			State:    "up",
			Services: "dc",
			TotalVFs: 3,
			NumVFs:   0,
		},
	}

	defer fakesysfs.FakeSysFsRemove()
	if err := fakesysfs.FakeSysFsQATContents(setupdevices); err != nil {
		t.Errorf("err: %v", err)
	}

	qatdevices, err := New()
	if err != nil {
		t.Errorf("could not create qatdevices with New(): %v", err)
	}

	expected := QATDevices{
		&PFDevice{
			AllowReconfiguration: false,
			Device:               "0000:aa:00.0",
			State:                Down,
			Services:             Sym | Asym,
			NumVFs:               0,
			TotalVFs:             3,
			AvailableDevices: map[string]*VFDevice{
				"qatvf-0000-aa-00-1": {nil, "qatvf-0000-aa-00-1", VfioPci, "351"},
				"qatvf-0000-aa-00-2": {nil, "qatvf-0000-aa-00-2", VfioPci, "352"},
				"qatvf-0000-aa-00-3": {nil, "qatvf-0000-aa-00-3", VfioPci, "353"},
			},
			AllocatedDevices: map[string]VFDevices{},
		},
		&PFDevice{
			AllowReconfiguration: false,
			Device:               "0000:bb:00.0",
			State:                Up,
			Services:             Dc,
			NumVFs:               0,
			TotalVFs:             3,
			AvailableDevices: map[string]*VFDevice{
				"qatvf-0000-bb-00-1": {nil, "qatvf-0000-bb-00-1", VfioPci, "354"},
				"qatvf-0000-bb-00-2": {nil, "qatvf-0000-bb-00-2", VfioPci, "355"},
				"qatvf-0000-bb-00-3": {nil, "qatvf-0000-bb-00-3", VfioPci, "356"},
			},
			AllocatedDevices: map[string]VFDevices{},
		},
	}
	err = CompareQATDevices(qatdevices, expected)
	if err != nil {
		t.Errorf("error comparing device setup: %v", err)
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
