# How to build Intel CDI Spec Generator
A pre-compiled binary is already available for download, eliminating the need for manual building. See documentation [README.md](README.md#Releases)

## Prerequisites
- Go 1.22

## Building
1. Clone the repository
```bash
git clone https://github.com/intel/intel-resource-drivers-for-kubernetes.git
cd intel-resource-drivers-for-kubernetes/cmd/cdi-specs-generator
```

2. Build the executable
```bash
go build -o intel-cdi-specs-generator main.go
```
This command will generate an executable named intel-cdi-specs-generator in the current directory.

## Verification
To verify that the build was successful, you can check the version of the tool by running:
```bash
intel-cdi-specs-generator --version
```