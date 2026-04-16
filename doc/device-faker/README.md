# device-faker

A tool to generate fake sysfs and devfs simulating presence of supported accelerator devices.

Device-faker can be used with Intel DRA drivers to run experiments with Kubernetes and DRA without
having access to the real hardware, and running dummy workloads that do not require actual accelerator
hardware.


## Supported accelerators

- GPU
- Gaudi

## [device-faker overlay](../../deployments/gpu/overlays/device-faker/)

All supported accelerators have a kustomization overlay in `deployments` directory,
with `device-faker` sidecar container to provide fake sysfs and devfs.

To deploy DRA driver with faked devices:
```shell
kubectl apply -k deployments/gpu/overlay/device-faker
```

> [!IMPORTANT]
> `device-faker` container image for the sidecar container is not yet published to ghcr.io,
> therefore one has to build it locally before deploying the `device-faker` overlay.

## Parameters

```shell
$ device-faker -h
device-faker creates fake sysfs and devfs in /tmp for Intel GPU or Intel Gaudi based on template

Usage:
  device-faker <gpu | gaudi> [flags]

Flags:
  -c, --cleanup             Wait for SIGTERM, cleanup before exiting
  -h, --help                help for device-faker
  -n, --new-template        Create new template file for given accelerator
  -p, --print               Print resulting file-system tree
  -r, --real-devices        Create real device files (requires root)
  -d, --target-dir string   Target directory, default is random /tmp/test-*
  -t, --template string     Template file to populate devices from
  -v, --version             Show the version of the binary
```

When used without `--real-devices` parameter, the implied device files are plain text files and
therefore container runtime will not be able to mount them as actual device nodes, and the Pod
requesting them will never get to a `Running` state.  But it allows testing e.g. discovery,
discovery announcement and allocation without need for extra privileges.

"Real" device files, needed to get the requesting Pod to a `Running` state, are created with the `-r`
(`--real-devices`) parameter, when tool has `CAP_MKNOD` capability. Device files are `null`-devices,
which is enough for container runtime to provide them as devices[^1] to the workload container.

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
    "meiname": "mei0",
    "cardidx": 0,
    "renderdidx": 128,
    "memorymib": 1024,
    "millicores": 1000,
    "devicetype": "gpu",
    "maxvfs": 8,
    "parentuid": "",
    "vfprofile": "",
    "vfindex": 0,
    "provisioned": false,
    "driver": "i915",
    "currentdriver": "",
    "pciroot": "pci0000:01",
    "health": "",
    "healthstatus": null
  },
  "card1": {
    "uid": "0000-04-00-1-0xe20b",
    "pciaddress": "0000:04:00.1",
    "model": "0xe20b",
    "modelname": "",
    "familyname": "",
    "meiname": "mei1",
    "cardidx": 1,
    "renderdidx": 129,
    "memorymib": 2048,
    "millicores": 1000,
    "devicetype": "gpu",
    "maxvfs": 0,
    "parentuid": "0000-04-00-0-0xe20b",
    "vfprofile": "",
    "vfindex": 0,
    "provisioned": false,
    "driver": "xe",
    "currentdriver": "",
    "pciroot": "pci0000:02",
    "health": "",
    "healthstatus": null
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
fake file system: /tmp/test-2503111759/
fake sysfs: /tmp/test-2503111759/sysfs
fake devfs: /tmp/test-2503111759/dev
fake CDI: /tmp/test-2503111759/cdi

$ sudo tree /tmp/test-2503111759/
/tmp/test-2503111759/
├── cdi
├── dev
│   ├── dri
│   │   ├── by-path
│   │   │   ├── pci-0000:03:00.0-card -> ../card0
│   │   │   ├── pci-0000:03:00.0-render -> ../renderD128
│   │   │   ├── pci-0000:04:00.1-card -> ../card1
│   │   │   └── pci-0000:04:00.1-render -> ../renderD129
│   │   ├── card0
│   │   ├── card1
│   │   ├── renderD128
│   │   └── renderD129
│   ├── mei0
│   └── mei1
├── kubelet-plugin
│   ├── plugins
│   │   └── gpu.intel.com
│   └── plugins_registry
└── sysfs
    ├── bus
    │   └── pci
    │       ├── devices
    │       │   ├── 0000:03:00.0 -> ../../../devices/pci0000:01/0000:03:00.0
    │       │   └── 0000:04:00.1 -> ../../../devices/pci0000:02/0000:04:00.1
    │       └── drivers
    │           ├── i915
    │           │   ├── 0000:03:00.0 -> ../../../../devices/pci0000:01/0000:03:00.0
    │           │   └── bind
    │           └── xe
    │               ├── 0000:04:00.1 -> ../../../../devices/pci0000:02/0000:04:00.1
    │               └── bind
    ├── class
    │   ├── drm
    │   │   ├── card0 -> /tmp/test-2503111759/sysfs/bus/pci/drivers/i915/0000:03:00.0/drm/card0
    │   │   └── card1 -> /tmp/test-2503111759/sysfs/bus/pci/drivers/xe/0000:04:00.1/drm/card1
    │   └── mei
    │       ├── mei0 -> ../../devices/pci0000:01/0000:03:00.0/i915.mei-gscfi.2304/mei/mei0
    │       └── mei1 -> ../../devices/pci0000:02/0000:04:00.1/xe.mei-gscfi.768/mei/mei1
    └── devices
        ├── pci0000:01
        │   └── 0000:03:00.0
        │       ├── device
        │       ├── drm
        │       │   ├── card0
        │       │   │   ├── lmem_total_bytes
        │       │   │   └── prelim_iov
        │       │   │       ├── pf
        │       │   │       │   └── auto_provisioning
        │       │   │       ├── vf1
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf2
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf3
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf4
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf5
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf6
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       ├── vf7
        │       │   │       │   └── gt
        │       │   │       │       ├── contexts_quota
        │       │   │       │       ├── doorbells_quota
        │       │   │       │       ├── exec_quantum_ms
        │       │   │       │       ├── ggtt_quota
        │       │   │       │       ├── lmem_quota
        │       │   │       │       └── preempt_timeout_us
        │       │   │       └── vf8
        │       │   │           └── gt
        │       │   │               ├── contexts_quota
        │       │   │               ├── doorbells_quota
        │       │   │               ├── exec_quantum_ms
        │       │   │               ├── ggtt_quota
        │       │   │               ├── lmem_quota
        │       │   │               └── preempt_timeout_us
        │       │   └── renderD128
        │       ├── i915.mei-gscfi.2304
        │       │   └── mei
        │       │       └── mei0
        │       ├── sriov_drivers_autoprobe
        │       ├── sriov_numvfs
        │       └── sriov_totalvfs
        └── pci0000:02
            └── 0000:04:00.1
                ├── device
                ├── drm
                │   ├── card1
                │   │   └── lmem_total_bytes
                │   └── renderD129
                └── xe.mei-gscfi.768
                    └── mei
                        └── mei1

62 directories, 68 files
```

</details>
