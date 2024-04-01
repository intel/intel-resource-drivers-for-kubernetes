Here are alert notification JSON messages for webhook test code.

Messages in `taint-<name>.json` files should taint devices with
`gpu-<name>' UID, and `taint-<name>-fail.json` files should fail to
taint them.  Taint reason for them is "GpuNeedsReset".

Single alert notifications with invalid content:
* taint-1-fail-dev: missing "pci_dev" label
* taint-1-fail-json: invalid JSON
* taint-2-fail-node: unknown "node" value
* taint-3-fail-status: status != "firing"

Single alert notifications failing silently due to "groupLabel" filter rules:
* taint-1-filtered: namespace != "monitoring"
* taint-2-filtered: service != "xpu-manager"

Multi-alert notifications failing with invalid start/end timings:
* multi-fail.json: endsAt < startsAt

Notifications resolving alert reasons:
* taint-1-resolve: resolve "taintReason" for 1st GPU

Multiple alerts for multiple GPUs with different timestamps:
* multi-taint:
  1st taint for 1st GPU replaced with 3rd one, 2nd stale/skipped,
  4th taint resolved => 1 remaining 1st GPU taint + 2 taints for 2nd GPU
* multi-resolve:
  resolve 1st taint reason for 2nd GPU from "taint-multiple"

Verify JSON file syntax:
------------------------
for i in *.json; do
  echo "- $i";
  python3 -c "import json; json.load(open('$i'))";
done
------------------------
