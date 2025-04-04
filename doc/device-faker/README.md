# device-faker

A tool to generate fake sysfs and devfs simulating presence of supported accelerator devices.

Device-faker can be used with Intel DRA drivers to run experiments with Kubernetes and DRA without
having access to the real hardware, and running dummy workloads that do not require actual accelerator
hardware.


## Supported accelerators

- GPU
- Gaudi

## Parameters

```shell
device-faker -h
device-faker creates fake sysfs and devfs in /tmp for Intel GPU or Intel Gaudi based on a template

Usage:
  device-faker <gpu | gaudi> [flags]

Flags:
  -h, --help                help for device-faker
  -n, --new-template        Create new template file for given accelerator
  -r, --real-devices        Create real device files (requires root)
  -d, --target-dir string   Target directory, default is random /tmp/test-*
  -t, --template string     Template file to populate devices from
  -v, --version             Show the version of the binary
```

When used without `--real-devices` parameter, the implied device files are plain text files and
therefore container runtime will not be able to mount them as actual device nodes, and the Pod
requesting them will never get to a `Running` state.  But it allows testing e.g. discovery,
discovery announcement and allocation without need for extra privileges.

"Real" device files, needed to get the requesting Pod to a `Running` state, are created with the `-r (`--real-devices`) parameter, when tool has `CAP_MKNOD` capability. Device files are `null`-devices, which is enough for container runtime to provide them as devices[^1] to the workload container.

[^1]: Cgroup `device` (whitelist) controller requires files specified in OCI spec to be real devices:
    * https://github.com/opencontainers/runtime-spec/blob/main/config-linux.md#devices
    * https://www.kernel.org/doc/Documentation/cgroup-v1/devices.txt
    * https://www.kernel.org/doc/Documentation/cgroup-v2.txt


## Example usage

### Generate a new template file if needed

```shell
device-faker -n gpu
```

Example output and template file contents

<details>

```shell
$ device-faker -n gpu
new template: /tmp/gpu-template-3524438793.json

$ cat /tmp/gpu-template-3524438793.json
{
  "card0": {
    "uid": "0000-03-00-0-0x56c0",
    "pciaddress": "0000:03:00.0",
    "model": "0x56c0",
    "modelname": "",
    "familyname": "",
    "cardidx": 0,
    "renderdidx": 128,
    "memorymib": 1024,
    "millicores": 1000,
    "devicetype": "gpu",
    "maxvfs": 8,
    "parentuid": "",
    "vfprofile": "",
    "vfindex": 0,
    "provisioned": false
  },
  "card1": {
    "uid": "0000-03-00-1-0x56c0",
    "pciaddress": "0000:03:00.1",
    "model": "0x56c0",
    "modelname": "",
    "familyname": "",
    "cardidx": 1,
    "renderdidx": 129,
    "memorymib": 512,
    "millicores": 1000,
    "devicetype": "vf",
    "maxvfs": 0,
    "parentuid": "0000-03-00-0-0x56c0",
    "vfprofile": "",
    "vfindex": 0,
    "provisioned": false
  }
}
```

</details>

### Generate fake file-system

```shell
device-faker -t /tmp/gpu-template-3524438793.json gpu
```

Sample output and fake file-system contents

<details>

```shell
$ device-faker -t /tmp/gpu-template-3524438793.json gpu
fake file system: /tmp/test-3985488568
fake sysfs: /tmp/test-3985488568/sysfs
fake devfs: /tmp/test-3985488568/dev
fake CDI: /tmp/test-3985488568/cdi

/tmp/test-3985488568
├── cdi
├── dev
│   └── dri
│       ├── by-path
│       │   ├── pci-0000:03:00.0-card -> ../card0
│       │   ├── pci-0000:03:00.0-render -> ../renderD128
│       │   ├── pci-0000:03:00.1-card -> ../card1
│       │   └── pci-0000:03:00.1-render -> ../renderD129
│       ├── card0
│       ├── card1
│       ├── renderD128
│       └── renderD129
├── kubelet-plugin
│   ├── plugins
│   │   └── gpu.intel.com
│   └── plugins_registry
└── sysfs
    ├── bus
    │   └── pci
    │       └── drivers
    │           └── i915
    │               ├── 0000:03:00.0
    │               │   ├── device
    │               │   ├── drm
    │               │   │   ├── card0
    │               │   │   │   ├── lmem_total_bytes
    │               │   │   │   └── prelim_iov
    │               │   │   │       ├── pf
    │               │   │   │       │   └── auto_provisioning
    │               │   │   │       ├── vf1
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf2
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf3
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf4
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf5
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf6
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       ├── vf7
    │               │   │   │       │   └── gt
    │               │   │   │       │       ├── contexts_quota
    │               │   │   │       │       ├── doorbells_quota
    │               │   │   │       │       ├── exec_quantum_ms
    │               │   │   │       │       ├── ggtt_quota
    │               │   │   │       │       ├── lmem_quota
    │               │   │   │       │       └── preempt_timeout_us
    │               │   │   │       └── vf8
    │               │   │   │           └── gt
    │               │   │   │               ├── contexts_quota
    │               │   │   │               ├── doorbells_quota
    │               │   │   │               ├── exec_quantum_ms
    │               │   │   │               ├── ggtt_quota
    │               │   │   │               ├── lmem_quota
    │               │   │   │               └── preempt_timeout_us
    │               │   │   └── renderD128
    │               │   ├── sriov_drivers_autoprobe
    │               │   ├── sriov_numvfs
    │               │   ├── sriov_totalvfs
    │               │   └── virtfn0 -> ../0000:03:00.1
    │               └── 0000:03:00.1
    │                   ├── device
    │                   ├── drm
    │                   │   ├── card1
    │                   │   │   └── lmem_total_bytes
    │                   │   └── renderD129
    │                   └── physfn -> ../0000:03:00.0
    └── class
        └── drm
            ├── card0 -> /tmp/test-3985488568/sysfs/bus/pci/drivers/i915/0000:03:00.0/drm/card0
            └── card1 -> /tmp/test-3985488568/sysfs/bus/pci/drivers/i915/0000:03:00.1/drm/card1

45 directories, 64 files
```

</details>
