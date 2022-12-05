# Runtime

Runtime needs to have CDI injection support

- CRI-O already has it since 1.23.
- Containerd: CDI device injection is planned for [Containerd 1.7](https://github.com/containerd/containerd/milestone/42). If you need containerd, and cannot get by with CRI-O runtime, you'll have to build it from sources.

## Containerd

### Building

<span style='color:red'>NOTE: containerd on Ubuntu 22 might have some issue, for me it was terminating containers for no
reason</span>

```bash
git clone git@github.com:containerd/containerd.git
cd containerd
make all -j<your number cpus plus 2>
```

### Installing freshly built containerd

- install whatever version of containerd is in your distribution
- stop containerd
```bash
sudo systemctl stop containerd
```
- ensure no containerd binaries are running
```
ps auxf | grep containerd
```
- if any shim process is still running, stop your docker containers
- check where the containerd binaries are by listing contents of the containerd package (/usr/bin/ typically)
```bash
# Ubuntu / Debian example
dpkg -l containerd | grep containerd$
```
- replace the distribution package containerd binaries with freshly built
```bash
sudo cp bin/* /usr/bin/
```
- start containerd and ensure new version has been started
```bash
sudo systemctl start containerd
sudo journalctl -u containerd # scroll till the end / hit 'end' button
```

# Generated source code

## CRDs

Custsom resource definitions are in pkg/crd/intel/<apiversion>/*.go (except generated zz_deepcopy) and in
pkg/crd/intel/v1/api/

When changing those CRDs remember to re-generate the listers, informers, client by running
```bash
make generate
./hack/update_codegen.sh
```

To generate CRD YAMLs (developments/static/crd/...) controller-gen is needed:
```bash
git clone git@github.com:kubernetes-sigs/controller-tools.git
cd controller-tools
go build ./cmd/controller-gen
```
Make it available in PATH, $HOME/bin for instance, if you use it:
```bash
cp controller-gen $HOME/bin
# ensure it's in the path
[[ ":$PATH:" == *":$HOME/bin:"* ]] || export PATH=$HOME/bin:$PATH
```

# Known issues

## "Parametrses"

- CRD generator might make "Parameterses" as plural for "Parameters", so `hack/update_codegen.sh`
fixes these artifacts
