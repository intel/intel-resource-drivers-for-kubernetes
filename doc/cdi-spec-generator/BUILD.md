# How to build Intel CDI Spec Generator

## Prerequisites
- Go 1.25

## Building
1. Clone the repository
```bash
git clone https://github.com/intel/intel-resource-drivers-for-kubernetes.git
cd intel-resource-drivers-for-kubernetes
```

2. Build the executable
```bash
make bin/intel-cdi-specs-generator
```
This command will generate an executable named intel-cdi-specs-generator in the `bin` directory.

## Verification
To verify that the build was successful, you can check the version of the tool by running:
```bash
bin/intel-cdi-specs-generator --version
```