# Intel CDI Spec Generator

## Overview
The Intel CDI Specs Generator is a command line tool to generate Container Device Interface (CDI) specifications for supported accelerators.

## Prerequisites
- Administrative privileges on the system to write CDI specs.

## Usage
Execute the built executable with the type of device you wish to generate CDI specs for:
```bash
intel-cdi-specs-generator <gpu | gaudi>
```

Supported device types:
- gpu: Use this option to generate CDI specs for Intel GPUs.
- gaudi: Use this option to generate CDI specs for Intel Gaudi accelerators.

## Display Version
To display the version of the binary, use the following command:
```bash
intel-cdi-specs-generator --version
```

## Example Usage
To generate CDI specifications for GPUs, run the tool with gpu as an argument:
```bash
intel-cdi-specs-generator gpu
```
This command will detect supported GPUs on the system, and ensure that there is a CDI device record for each of them.


## Building
- [How to build CDI Spec Generator](BUILD.md)

## Releases
The binary is available for download in the releases section:
- [Intel Resource Drivers for Kubernetes releases](https://github.com/intel/intel-resource-drivers-for-kubernetes/releases)
- [CDI Spec Generator v0.1.0](https://github.com/intel/intel-resource-drivers-for-kubernetes/releases/tag/specs-generator-v0.1.0)
