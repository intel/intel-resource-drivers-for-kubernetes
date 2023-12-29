Here are alert notification JSON messages for webhook test code.

Messages in `taint-<name>.json` files should taint devices with
`gpu-<name>' UID, and `taint-<name>-fail.json` files should fail to
taint them.

Invalid taint fail reasons:
* taint-1-fail: missing "pci_dev" label
* taint-2-fail: unknown "node" value
* taint-3-fail: status != "firing"

Taints failing silently due to groupLabel filter rules:
* taint-1-filtered: namespace != "monitoring"
* taint-2-filtered: service != "xpu-manager"
