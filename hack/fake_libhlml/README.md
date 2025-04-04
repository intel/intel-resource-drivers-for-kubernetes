This implements a stub / mock for the interface defined in
https://github.com/HabanaAI/gohlml/blob/main/hlml.h.

The result is a shared library fake_libhlml.so - it can be used to simulate presense of Gaudi
devices and kernel driver.

To run tests for Gaudi health monitoring locally, follow these steps:

- build hack/fake_libhlml
```
cd hack/fake_libhlml
make
```
- deploy it where Go module expects to find it
```
sudo mkdir /usr/lib/habanalabs
sudo cp hack/fake_libhlml/fake_libhlml.so /usr/lib/habanalabs/libhlml.so
```
- add ld config to use that library and trigger ldconfig, it will be needed for running tests
  with and without VSCode:
```
cat << EOF | sudo tee /etc/ld.so.conf.d/habanalabs.conf
/usr/lib/habanalabs/
EOF

sudo ldconfig
```

