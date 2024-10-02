/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package device

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	devicePath       = "bus/pci/devices"
	driverPath       = "bus/pci/drivers"
	moduleName       = "4xxx"
	vfioPCI          = "vfio-pci"
	vfioBind         = vfioPCI + "/bind"
	vfioUnbind       = vfioPCI + "/unbind"
	pciDevicePattern = "????:??:??.?"
	qatState         = "qat/state"
	qatServices      = "qat/cfg_services"
	driverOverride   = "driver_override"
	numVFs           = "sriov_numvfs"
	totalVFs         = "sriov_totalvfs"
	vfDevicePattern  = "virtfn*"
	vfDriver         = "driver"
	vfIOMMU          = "iommu_group"
	vfDeviceNode     = "/dev/vfio"
)

var sysfsRoot string = ""

func getSysfsRoot() string {
	if sysfsRoot != "" {
		return sysfsRoot
	}
	sysfsRoot = os.Getenv("SYSFS_ROOT")
	if sysfsRoot == "" {
		sysfsRoot = "/sys/"
	}
	return sysfsRoot
}

func sysfsDevicePath() string {
	return getSysfsRoot() + "/" + devicePath
}

func sysfsDriverPath() string {
	return getSysfsRoot() + "/" + driverPath
}

type State int

const (
	Down State = iota
	Up
)

func (s *State) String() string {
	for name, state := range stringToState {
		if state == *s {
			return name
		}
	}
	return ""
}

var stringToState = map[string]State{
	"down": Down,
	"up":   Up,
}

type Services uint64

const (
	None  Services = 1 << 0
	Sym            = 1 << 1
	Asym           = 1 << 2
	Dc             = 1 << 3
	Dcc            = 1 << 4
	Unset          = 0
)

var servicetostring = map[Services]string{None: "", Sym: "sym", Asym: "asym", Dc: "dc", Dcc: "dcc"}

func (s *Services) String() string {
	str := ""
	for _, i := range []Services{None, Sym, Asym, Dc, Dcc} {
		if *s&i != 0 {
			if str != "" {
				str = str + ";" + servicetostring[i]
			} else {
				str = servicetostring[i]
			}
		}
	}
	return str
}

func (s *Services) Supports(service Services) bool {
	return *s&service == service
}

func StringToServices(servicestr string) (Services, error) {
	var service Services = Unset

	for _, str := range strings.Split(servicestr, ";") {
		exists := false

		for i, strtoservice := range servicetostring {
			if str == strtoservice {
				service |= i
				exists = true
				break
			}
		}
		if !exists {
			return Unset, fmt.Errorf("unknown service '%s'", servicestr)
		}
	}

	if service != None {
		service &= ^None
	}

	return service, nil
}

type QATDevices []*PFDevice

// Available devices mapped by UID (PCI address minus colons and dots).
type VFDevices map[string]*VFDevice

// Allocated devices mapped by supplied string, then by device UID as above.
type AllocatedDevices map[string]VFDevices

type PFDevice struct {
	AllowReconfiguration bool // enable dynamic service reconfiguration
	Device               string
	State                State
	Services             Services
	NumVFs               int
	TotalVFs             int
	AvailableDevices     VFDevices        // mapped by device uid
	AllocatedDevices     AllocatedDevices // mapped by claim id
}

type VFDriver int

const (
	Unbound VFDriver = iota
	VfioPci
	Unknown
)

var stringToDriver = map[string]VFDriver{
	"":         Unbound,
	"vfio-pci": VfioPci,
}

func (s *VFDriver) String() string {
	if *s == Unbound {
		return ""
	}
	if *s == VfioPci {
		return "vfio-pci"
	}
	return "unknown"
}

type VFDevice struct {
	pfdevice *PFDevice
	VFDevice string
	VFDriver VFDriver
	VFIommu  string
}

func New() (QATDevices, error) {
	pcidevices := make(QATDevices, 0)

	pattern := filepath.Join(sysfsDriverPath(), moduleName, pciDevicePattern)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("no PCI PF devices found")
	}

	for _, p := range paths {
		symlinktarget, err := filepath.EvalSymlinks(p)
		if err != nil {
			fmt.Printf("Warning symlink for %s: %v\n", p, err)
			continue
		}

		newdevice := &PFDevice{
			AllowReconfiguration: false,
			Device:               filepath.Base(symlinktarget),
			AvailableDevices:     make(map[string]*VFDevice, 0),
			AllocatedDevices:     make(map[string]VFDevices, 0),
		}

		if err = newdevice.syncConfig(); err != nil {
			fmt.Printf("Warning: sync config for %s: %v\n", symlinktarget, err)
			continue
		}
		if err := newdevice.getVFs(); err != nil {
			fmt.Printf("Could not find VFs for %s: %v\n", symlinktarget, err)
			continue
		}
		pcidevices = append(pcidevices, newdevice)

	}

	return pcidevices, nil
}

func GetControlNode() (*VFDevice, error) {
	return &VFDevice{
		VFDevice: "vfio",
		VFDriver: VfioPci,
		VFIommu:  "vfio",
	}, nil
}

func GetCDIDevices(pfdevices QATDevices) VFDevices {
	vfdevices := GetResourceDevices(pfdevices)

	ctrl, _ := GetControlNode()
	vfdevices[ctrl.UID()] = ctrl

	return vfdevices
}

func GetResourceDevices(pfdevices QATDevices) VFDevices {
	vfdevices := make(VFDevices, 0)

	for _, pf := range pfdevices {
		for _, vf := range pf.AvailableDevices {
			v := *vf
			vfdevices[v.UID()] = &v
		}
		for _, vflist := range pf.AllocatedDevices {
			for _, vf := range vflist {
				v := *vf
				vfdevices[v.UID()] = &v
			}
		}
	}

	return vfdevices
}

func (p *PFDevice) read(file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("missing file name")
	}

	val, err := os.ReadFile(filepath.Join(sysfsDevicePath(), p.Device, file))
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %v", file, err)
	}

	return strings.TrimSpace(string(val)), nil
}

func (p *PFDevice) write(file string, value string) error {
	err := os.WriteFile(filepath.Join(sysfsDevicePath(), p.Device, file), []byte(value), 0600)

	return err
}

func (p *PFDevice) syncConfig() error {
	qatstate, err := p.read(qatState)
	if err != nil {
		return err
	}
	state, exists := stringToState[qatstate]
	if !exists {
		return fmt.Errorf("unknown QAT state %s", qatstate)
	}

	qatservices, err := p.getServices()
	if err != nil {
		return fmt.Errorf("cannot read QAT services: %v", err)
	}

	numvfs, err := p.read(numVFs)
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", numVFs, err)
	}
	vfs, err := strconv.Atoi(numvfs)
	if err != nil {
		return fmt.Errorf("cannot read value from %s: %v", numVFs, err)
	}

	totalvfs, err := p.read(totalVFs)
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", totalVFs, err)
	}
	total, err := strconv.Atoi(totalvfs)
	if err != nil {
		return fmt.Errorf("cannot read value from %s: %v", totalVFs, err)
	}

	p.State = state
	p.Services = qatservices
	p.NumVFs = vfs
	p.TotalVFs = total

	return nil
}

func (p *PFDevice) getServices() (Services, error) {
	var services Services

	servicestr, err := p.read(qatServices)
	if err != nil {
		return 0, err
	}

	services, err = StringToServices(servicestr)
	if err != nil {
		return Unset, fmt.Errorf("'%s' is not a supported service", servicestr)
	}

	return services, nil
}

func (p *PFDevice) SetServices(srv []Services) error {
	config := None

	if len(p.AllocatedDevices) > 0 {
		return fmt.Errorf("cannot change QAT configuration while VF devices are allocated")
	}

	for _, s := range srv {
		config |= s
	}

	deviceState := p.State

	if err := p.down(); err != nil {
		return err
	}

	if err := p.write(qatServices, config.String()); err != nil {
		if deviceState == Up {
			// attempt to return to previous up state with VFs
			_ = p.EnableVFs()
		}
		return fmt.Errorf("configuration '%s' not supported: %v", config.String(), err)
	}

	if err := p.EnableVFs(); err != nil {
		return err
	}

	return nil
}

func (p *PFDevice) getVFs() error {
	paths, err := filepath.Glob(filepath.Join(sysfsDevicePath(), p.Device, vfDevicePattern))
	if err != nil {
		return nil
	}

	for _, path := range paths {
		var vf *VFDevice = nil

		vfpath, err := filepath.EvalSymlinks(path)
		if err != nil {
			fmt.Printf("Warning symlink for %s: %v\n", path, err)
			continue
		}

		vfdevice := filepath.Base(vfpath)

		// already in AvailableDevices
		if _, ok := p.AvailableDevices[deviceuid(vfdevice)]; ok {
			break
		}

		if vf == nil {
			for _, allocateddevices := range p.AllocatedDevices {
				for _, d := range allocateddevices {
					if vfdevice == d.VFDevice {
						vf = d
						break
					}
				}
				// already in AllocatedDevices
				if vf != nil {
					break
				}
			}
		}

		// new device found
		if vf == nil {
			vf = &VFDevice{
				pfdevice: p,
				VFDevice: vfdevice,
			}
			p.AvailableDevices[vf.UID()] = vf
		}

		vf.update()

	}

	return nil
}

func (p *PFDevice) up() error {
	state := Up

	if p.State != Up {
		if err := p.write(qatState, state.String()); err != nil {
			return err
		}
		p.State = Up
	}

	return nil
}

func (p *PFDevice) down() error {
	state := Down

	if len(p.AllocatedDevices) > 0 {
		return fmt.Errorf("cannot set QAT device down while VF devices are allocated")
	}

	if p.State != Down {
		if err := p.write(qatState, state.String()); err != nil {
			return err
		}
		p.State = Down
	}

	return nil
}

func (p *PFDevice) EnableVFs() error {
	var (
		totalvfs string
		err      error
	)

	// qat/state does not need to be down

	if totalvfs, err = p.read(totalVFs); err != nil {
		return err
	}

	if err = p.write(numVFs, totalvfs); err != nil {
		return err
	}

	_ = p.getVFs()
	for _, vf := range p.AvailableDevices {
		_ = vf.enableVFIO()
	}

	if err := p.up(); err != nil {
		return err
	}

	return nil
}

// Whether to allow dynamic reconfiguration of PF device services on Free()
// and Allocate() forcing the caller to update further device resources in K8s.
func (p *PFDevice) EnableReconfiguration(allow bool) {
	p.AllowReconfiguration = allow
}

func (p *PFDevice) Allocate(deviceUID string, allocatedBy string) (*VFDevice, error) {
	var vf *VFDevice = nil
	exists := false

	if allocatedBy == "" {
		return nil, fmt.Errorf("no allocator ID given")
	}

	if deviceUID != "" {
		if vf, exists = p.AvailableDevices[deviceUID]; !exists {
			return nil, fmt.Errorf("no such device '%s' available", deviceUID)
		}
	} else {
		// no device uid, pick any device
		for _, vf = range p.AvailableDevices {
			break
		}
		if vf == nil {
			return nil, fmt.Errorf("no more devices available in PF dev '%s'", p.Device)
		}
	}

	if _, exists = p.AllocatedDevices[allocatedBy]; !exists {
		p.AllocatedDevices[allocatedBy] = make(VFDevices, 0)
	}

	p.AllocatedDevices[allocatedBy][vf.UID()] = vf
	delete(p.AvailableDevices, vf.UID())

	return vf, nil
}

func (q QATDevices) Allocate(requestedDeviceUID string, requestedService Services, requestedBy string) (*VFDevice, bool, error) {
	for _, pf := range q {
		// check for already allocated service mapped by request ID
		if !pf.Services.Supports(requestedService) {
			fmt.Printf("pfdev '%s' service '%s' does not support service '%s'\n", pf.Device, pf.Services.String(), requestedService.String())
			continue
		}
		if allocatedDevices, exists := pf.AllocatedDevices[requestedBy]; exists {
			for _, vf := range allocatedDevices {
				if requestedDeviceUID == vf.UID() {
					// duplicated request, already allocated
					return vf, false, nil
				}
			}
		}
	}

	for _, pf := range q {
		// allocate from devices already configured for this service
		if !pf.Services.Supports(requestedService) {
			continue
		}
		// attempt allocation of requested device
		if vf, err := pf.Allocate(requestedDeviceUID, requestedBy); err == nil {
			return vf, false, nil
		}
	}

	for _, pf := range q {
		// allocate from an unconfigured device
		if pf.Services != None || !pf.AllowReconfiguration {
			continue
		}
		// attempt allocation of requested device
		if vf, err := pf.Allocate(requestedDeviceUID, requestedBy); err == nil {
			// attempt configuration of requested service
			if err := pf.SetServices([]Services{requestedService}); err != nil {
				_, _ = pf.Free(requestedDeviceUID, requestedBy)
				continue
			}
			return vf, true, nil
		}
	}

	return nil, false, fmt.Errorf("could not allocate device '%s', service '%s' from any device", requestedDeviceUID, requestedService.String())
}

func (q *QATDevices) Free(requestedDeviceUID string, requestedBy string) (bool, error) {
	var err error
	updated := false

	for _, pfdevice := range *q {
		if updated, err = pfdevice.Free(requestedDeviceUID, requestedBy); err == nil {
			return updated, nil
		}
	}
	return false, err
}

func (p *PFDevice) free(requestedDeviceUID string, vfdevices VFDevices) (bool, error) {
	if vf, exists := vfdevices[requestedDeviceUID]; exists {
		p.AvailableDevices[vf.UID()] = vf
		delete(vfdevices, vf.UID())

		for _, vfdevices := range p.AllocatedDevices {
			if len(vfdevices) > 0 {
				return false, nil
			}
		}

		// set PF device configuration back to an unconfigured state
		if p.AllowReconfiguration {
			if err := p.SetServices([]Services{None}); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil
	}

	return false, fmt.Errorf("device '%s' could not be found", requestedDeviceUID)
}

func (p *PFDevice) Free(requestedDeviceUID string, requestedBy string) (bool, error) {
	if requestedDeviceUID == "" {
		return false, fmt.Errorf("no device UID for request '%s'", requestedBy)
	}

	if requestedBy != "" {
		if vfdevices, exists := p.AllocatedDevices[requestedBy]; exists {
			return p.free(requestedDeviceUID, vfdevices)
		}
	} else {
		for _, vfdevices := range p.AllocatedDevices {
			if update, err := p.free(requestedDeviceUID, vfdevices); err == nil {
				return update, err
			}
		}
	}

	return false, fmt.Errorf("device '%s' requested by '%s' does not exist", requestedDeviceUID, requestedBy)
}

func (v *VFDevice) Free(requestedBy string) (bool, error) {
	return v.pfdevice.Free(v.UID(), requestedBy)
}

func (v *VFDevice) update() {
	driverpath := filepath.Join(sysfsDevicePath(), v.VFDevice, vfDriver)
	driver, err := filepath.EvalSymlinks(driverpath)
	if err == nil {
		driver = filepath.Base(driver)
		v.VFDriver = stringToDriver[driver]
	}

	iommupath := filepath.Join(sysfsDevicePath(), v.VFDevice, vfIOMMU)
	iommu, err := filepath.EvalSymlinks(iommupath)
	if err == nil {
		v.VFIommu = filepath.Base(iommu)
	}
}

func (v *VFDevice) writeFile(file string, val string) error {
	err := os.WriteFile(file, []byte(val), 0600)
	return err
}

func (v *VFDevice) bindVFIODriver() error {
	return v.writeFile(filepath.Join(sysfsDriverPath(), vfioBind), v.VFDevice)
}

func (v *VFDevice) unbindVFIODriver() error {
	err := v.writeFile(filepath.Join(sysfsDriverPath(), vfioUnbind), v.VFDevice)
	if err != nil {
		// fs.PathError is returned if the device was not bound
		err, _ = err.(*os.PathError)
	}
	return err
}

func (v *VFDevice) overrideVFIODriver() error {
	return v.writeFile(filepath.Join(sysfsDevicePath(), v.VFDevice, driverOverride), vfioPCI)
}

func (v *VFDevice) enableVFIO() error {
	if err := v.overrideVFIODriver(); err != nil {
		return err
	}

	if err := v.unbindVFIODriver(); err != nil {
		return err
	}

	if err := v.bindVFIODriver(); err != nil {
		return err
	}

	v.update()

	return nil
}

func (v *VFDevice) DeviceNode() string {
	return vfDeviceNode + "/" + v.VFIommu
}

func (v *VFDevice) PCIDevice() string {
	return v.VFDevice
}

func (v *VFDevice) Driver() string {
	return v.VFDriver.String()
}

func (v *VFDevice) Iommu() string {
	return v.VFIommu
}

func deviceuid(device string) string {
	return "qatvf-" + strings.ReplaceAll(strings.ReplaceAll(device, ":", "-"), ".", "-")
}

func (v *VFDevice) UID() string {
	return deviceuid(v.VFDevice)
}

func (v *VFDevice) Services() string {
	return v.pfdevice.Services.String()
}
