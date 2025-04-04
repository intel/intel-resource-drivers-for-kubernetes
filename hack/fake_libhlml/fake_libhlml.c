/* SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (C) 2025, Intel Corporation.
 * All Rights Reserved.
 *
 */

#include "../../vendor/github.com/HabanaAI/gohlml/hlml.h"
#include "../../pkg/fakehlml/fake_hlml.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>


#define DEVICES_MAX              8
#define FAKE_EVENTS_MAX          8
#define NAME_MAX                 64
#define SERIAL_MAX               64

struct device_info_t {
       char pci_addr[PCI_ADDR_LEN];
       unsigned int device_id;
       unsigned int vendor_id;
       char serial[SERIAL_MAX];
       unsigned int index;
};

struct main_struct_t {
    bool initialized;
    int devices_num;
    struct device_info_t devices_info[DEVICES_MAX];
} main_struct;

struct flow_control_t {
    // this is used by tests to dictate which response should be faked.
    // There are as many items as there are supported calls.
    // Each supported call is assigned a return value it should respond with.
    hlml_return_t func_ret[FAKE_CALL_IDENTITY_MAX];

    // this is used by tests to dictate which events should be faked.
    // events[event][serial char]
    char events [FAKE_EVENTS_MAX][SERIAL_MAX];
    int events_num;
} flow_control;

struct hlml_device_events {
	struct device_info_t *device_info;
};

struct hlml_event_set {
	struct hlml_device_events dev_events[DEVICES_MAX];
};

#define RETURN_IF_FAKE_ERROR(call_id) \
  if (flow_control.func_ret[call_id] != HLML_SUCCESS) { \
    return flow_control.func_ret[call_id]; \
  }

static void log_call(const char *name) { printf("%s called\n", name); }

// custom_init is called from a test function to populate the main_struct
// with fake information that otherwise would have been deduced from the
// sysfs by a real HLML library.
void add_device(const char *pci_addr, const char *pci_device_id, const char *pci_vendor_id,
                const char *serial, unsigned int index) {
    if (pci_addr) {
        snprintf(main_struct.devices_info[main_struct.devices_num].pci_addr, PCI_ADDR_LEN, "%s", pci_addr);
    } else {
        main_struct.devices_info[main_struct.devices_num].pci_addr[0] = '\0';
    }

    if (serial) {
        snprintf(main_struct.devices_info[main_struct.devices_num].serial, SERIAL_MAX, "%s", serial);
    } else {
        main_struct.devices_info[main_struct.devices_num].serial[0] = '\0';
    }

    main_struct.devices_info[main_struct.devices_num].index = index;
    sscanf(pci_device_id, "%x", &main_struct.devices_info[main_struct.devices_num].device_id);
    sscanf(pci_vendor_id, "%x", &main_struct.devices_info[main_struct.devices_num].vendor_id);
    main_struct.devices_num ++;
};

void reset() {
    main_struct.initialized = false;
    main_struct.devices_num = 0;
    // reset active events in flow control
    flow_control.events_num = 0;
    // reset flow control map
    for (int call_id = 0; call_id < FAKE_CALL_IDENTITY_MAX; call_id++) {
        flow_control.func_ret[call_id] = false;
    }
};

void add_critical_event(const char *serial) {
    if (flow_control.events_num == FAKE_EVENTS_MAX) {
        printf("ERROR: maximum number of fake evets reached");
        return;
    }

    if (serial) {
        snprintf(flow_control.events[flow_control.events_num], sizeof(flow_control.events[0]), "%s", serial);
    } else {
        flow_control.events[flow_control.events_num][0] = '\0';
    }

    flow_control.events_num ++;
}

void reset_events() {
    flow_control.events_num = 0;
}

void set_error(call_identity_t call_id, hlml_return_t errCode) {
    assert(call_id < FAKE_CALL_IDENTITY_MAX);

    flow_control.func_ret[call_id] = errCode;
}

/* supported APIs */
hlml_return_t hlml_init(void) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_INIT);

    return hlml_init_with_flags(0);
};

hlml_return_t hlml_init_with_flags(unsigned int flags) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_INIT_WITH_FLAGS);

    main_struct.initialized = true;

    return HLML_SUCCESS;
};

hlml_return_t hlml_shutdown(void) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_SHUTDOWN);

    main_struct.initialized = false;

    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_count(unsigned int *device_count) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_DEVICE_GET_COUNT);

    if (!device_count)
        return HLML_ERROR_INVALID_ARGUMENT;

    *device_count = main_struct.devices_num;

    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_handle_by_pci_bus_id(const char *pci_addr, hlml_device_t *device) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_DEVICE_GET_HANDLE_BY_PCI_BUS_ID);

    struct device_info_t *device_info;

    if (!main_struct.initialized)
        return HLML_ERROR_UNINITIALIZED;


    if (!device || !pci_addr)
        return HLML_ERROR_INVALID_ARGUMENT;

    for (int n = 0; n < main_struct.devices_num; n++) {
        device_info = &main_struct.devices_info[n];
        if (strncmp(pci_addr, device_info->pci_addr, PCI_ADDR_LEN)) {
            *device = device_info;

            return HLML_SUCCESS;
        }
    }
    return HLML_ERROR_NOT_FOUND;
};

hlml_return_t hlml_device_get_handle_by_index(unsigned int index, hlml_device_t *device) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_DEVICE_GET_HANDLE_BY_INDEX);

    struct device_info_t *device_info;

    if (!main_struct.initialized)
        return HLML_ERROR_UNINITIALIZED;

    if (!device || ((int)index >= main_struct.devices_num))
        return HLML_ERROR_INVALID_ARGUMENT;

    for (int n = 0; n < main_struct.devices_num; n++) {
        device_info = &main_struct.devices_info[n];
        if (index == device_info->index) {
            *device = device_info;
            return HLML_SUCCESS;
        }
    }
    return HLML_ERROR_NOT_FOUND;
};

hlml_return_t hlml_device_get_handle_by_UUID (const char* uuid, hlml_device_t *device) {
    log_call(__func__);
    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_name(hlml_device_t device, char *name,
                   unsigned int  length) {
    log_call(__func__);
    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_pci_info(hlml_device_t device, hlml_pci_info_t *pci) {
    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_DEVICE_GET_PCI_INFO);

    if (!main_struct.initialized)
        return HLML_ERROR_UNINITIALIZED;

    if (!device || !pci)
        return HLML_ERROR_INVALID_ARGUMENT;

    struct device_info_t *device_info = (struct device_info_t *)device;

    strncpy(pci->bus_id, device_info->pci_addr, PCI_ADDR_LEN);
    pci->bus_id[PCI_ADDR_LEN - 1] = '\0';

    pci->pci_device_id = device_info->device_id | (device_info->vendor_id << 16);

    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_clock_info(hlml_device_t device,
                     hlml_clock_type_t type,
                     unsigned int *clock) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_max_clock_info(hlml_device_t device,
                         hlml_clock_type_t type,
                         unsigned int *clock) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_clock_limit_info(hlml_device_t device,
                                            hlml_clock_type_t type,
                                            unsigned int *clock) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_utilization_rates(hlml_device_t device,
                    hlml_utilization_t *utilization) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_memory_info(hlml_device_t device, hlml_memory_t *memory) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_temperature(hlml_device_t device,
                      hlml_temperature_sensors_t sensor_type,
                      unsigned int *temp) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_temperature_threshold(hlml_device_t device,
                hlml_temperature_thresholds_t threshold_type,
                unsigned int *temp) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_persistence_mode(hlml_device_t device,
                        hlml_enable_state_t *mode) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_performance_state(hlml_device_t device,
                        hlml_p_states_t *p_state) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_supported_performance_states(hlml_device_t device,
                                                       hlml_p_states_t *pstates,
                                                       unsigned int size) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_power_usage(hlml_device_t device,
                      unsigned int *power) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_power_management_mode(hlml_device_t device,
                                                   hlml_enable_state_t *state) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_power_management_limit(hlml_device_t device,
                                                    unsigned int *limit) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
}

hlml_return_t hlml_device_set_power_management_limit(hlml_device_t device, unsigned int limit) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
}

hlml_return_t hlml_device_get_power_management_default_limit(hlml_device_t device,
                        unsigned int *default_limit) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_ecc_mode(hlml_device_t device,
                       hlml_enable_state_t *current,
                       hlml_enable_state_t *pending) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_total_ecc_errors(hlml_device_t device,
                    hlml_memory_error_type_t error_type,
                    hlml_ecc_counter_type_t counter_type,
                    unsigned long long *ecc_counts) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_memory_error_counter(hlml_device_t device,
                    hlml_memory_error_type_t error_type,
                    hlml_ecc_counter_type_t counter_type,
                    hlml_memory_location_type_t location,
                    unsigned long long *ecc_counts) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_uuid(hlml_device_t device,
                   char *uuid,
                   unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_minor_number(hlml_device_t device,
                       unsigned int *minor_number) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_register_events(hlml_device_t device,
                      unsigned long long event_types,
                      hlml_event_set_t set) {
    // cast HLML void* types
    struct device_info_t *device_info = (struct device_info_t *)device;
    struct hlml_event_set *event_set = (struct hlml_event_set *)set;
    struct hlml_device_events *dev_events;

    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_DEVICE_REGISTER_EVENTS);

    int i;
	for (i = 0; i < DEVICES_MAX; i++) {
		dev_events = &event_set->dev_events[i];
		if (!dev_events->device_info) {
			memset(dev_events, 0, sizeof(*dev_events));
			dev_events->device_info = device_info;
			break;
		}
		else if (dev_events->device_info == device_info)
			break;
	}
	if (i == DEVICES_MAX)
		return HLML_ERROR_INVALID_ARGUMENT;

    return HLML_SUCCESS;
};

hlml_return_t hlml_event_set_create(hlml_event_set_t *set) {
    struct hlml_event_set *event_set;

    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_EVENT_SET_CREATE);

    if (!main_struct.initialized) {
        return HLML_ERROR_UNINITIALIZED;
    }

	if (!set) {
		return HLML_ERROR_INVALID_ARGUMENT;
    }

	event_set = (struct hlml_event_set*)calloc(1, sizeof(struct hlml_event_set));
	if (!event_set)
		return HLML_ERROR_MEMORY;

	*set = event_set;

	return HLML_SUCCESS;
};

hlml_return_t hlml_event_set_free(hlml_event_set_t set) {
    struct hlml_event_set *event_set = (struct hlml_event_set*)set;

    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_EVENT_SET_FREE);

    if (!main_struct.initialized) {
        return HLML_ERROR_UNINITIALIZED;
    }

	if (!event_set) {
		return HLML_ERROR_INVALID_ARGUMENT;
    }

	free(event_set);
	event_set = NULL;

	return HLML_SUCCESS;
};

hlml_return_t hlml_event_set_wait(hlml_event_set_t set,
                  hlml_event_data_t *data,
                  unsigned int timeoutms) {
    struct hlml_event_set *event_set = (struct hlml_event_set *)set;
    struct hlml_device_events *dev_events;
    struct hlml_event_data event_data;

    log_call(__func__);

    RETURN_IF_FAKE_ERROR(FAKE_EVENT_SET_WAIT);

    if (!main_struct.initialized) {
        return HLML_ERROR_UNINITIALIZED;
    }

	if (!event_set || !data) {
		return HLML_ERROR_INVALID_ARGUMENT;
    }

    event_data.event_type = 0;

    for (int i = 0; i < DEVICES_MAX; i++) {
        // check each registered eventset, if any of them has event pending
        dev_events = &event_set->dev_events[i];
        if (!dev_events->device_info) /* no more devices registered */
            break;

        // if there are events, and the last event is for the current device
        if (flow_control.events_num > 0 &&
            strcmp(flow_control.events[flow_control.events_num-1], dev_events->device_info->serial) == 0) {
            printf("fake HLML: event for device %s found", dev_events->device_info->serial);
            // set found
            event_data.device = dev_events->device_info;
            event_data.event_type |= HLML_EVENT_CRITICAL_ERR;
            flow_control.events_num --;
            break;
        }
    }

    if (event_data.event_type != 0) {
        event_data.device = dev_events->device_info;
        *data = event_data;
        return HLML_SUCCESS;
    }

    return HLML_ERROR_TIMEOUT;
};

hlml_return_t hlml_device_get_mac_info(hlml_device_t device,
                       hlml_mac_info_t *mac_info,
                       unsigned int mac_info_size,
                       unsigned int start_mac_id,
                       unsigned int *actual_mac_count) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_hl_revision(hlml_device_t device, int *hl_revision) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_pcb_info(hlml_device_t device, hlml_pcb_info_t *pcb) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_serial(hlml_device_t device, char *serial, unsigned int length) {
    log_call(__func__);

    assert(serial && length >0);

    if (flow_control.func_ret[FAKE_DEVICE_GET_SERIAL] != HLML_SUCCESS) {
        // just in case, set the serial to empty string
        serial[0] = '\0';
        return flow_control.func_ret[FAKE_DEVICE_GET_SERIAL];
    }

    if (!device) {
        serial[0] = '\0';
        return HLML_SUCCESS;
    }

    if (SERIAL_MAX > (int)length)
        return HLML_ERROR_INSUFFICIENT_SIZE;

    // cast void* type to concrete type
    struct device_info_t *device_info = (struct device_info_t *)device;
    if (device_info) {
        strncpy(serial, device_info->serial, length);
    }

    return HLML_SUCCESS;
};

hlml_return_t hlml_device_get_module_id(hlml_device_t device, unsigned int *module_id) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_board_id(hlml_device_t device, unsigned int* board_id) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_pcie_throughput(hlml_device_t device,
                          hlml_pcie_util_counter_t counter,
                          unsigned int *value) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_pcie_replay_counter(hlml_device_t device, unsigned int *value) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_curr_pcie_link_generation(hlml_device_t device,
                            unsigned int *curr_link_gen) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_curr_pcie_link_width(hlml_device_t device,
                           unsigned int *curr_link_width) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_current_clocks_throttle_reasons(hlml_device_t device,
        unsigned long long *clocks_throttle_reasons) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_total_energy_consumption(hlml_device_t device,
        unsigned long long *energy) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_mac_addr_info(hlml_device_t device, uint64_t *mask, uint64_t *ext_mask) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_nic_get_link(hlml_device_t device, uint32_t port, bool *up) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_nic_get_statistics(hlml_device_t device, hlml_nic_stats_info_t *stats_info) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_clear_cpu_affinity(hlml_device_t device) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_cpu_affinity(hlml_device_t device,
                       unsigned int cpu_set_size,
                       unsigned long *cpu_set) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_cpu_affinity_within_scope(hlml_device_t device,
                            unsigned int cpu_set_size,
                            unsigned long *cpu_set,
                            hlml_affinity_scope_t scope) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_memory_affinity(hlml_device_t device,
                          unsigned int node_set_size,
                          unsigned long *node_set,
                          hlml_affinity_scope_t scope) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_set_cpu_affinity(hlml_device_t device) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_violation_status(hlml_device_t device,
                           hlml_perf_policy_type_t perf_policy_type,
                           hlml_violation_time_t *viol_time) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_replaced_rows(hlml_device_t device,
                        hlml_row_replacement_cause_t cause,
                        unsigned int *row_count,
                        hlml_row_address_t *addresses) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_replaced_rows_pending_status(hlml_device_t device,
                               hlml_enable_state_t *is_pending) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_hlml_version(char *version, unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_driver_version(char *driver_version, unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_nic_driver_version(char *driver_version, unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
}

hlml_return_t hlml_get_model_number(hlml_device_t device, char *model_number,
                    unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_firmware_fit_version(hlml_device_t device, char *firmware_fit,
                        unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_firmware_spi_version(hlml_device_t device, char *firmware_spi,
                        unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_fw_boot_version(hlml_device_t device, char *fw_boot_version,
                       unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_fw_os_version(hlml_device_t device, char *fw_os_version,
                     unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_get_cpld_version(hlml_device_t device, char *cpld_version,
                    unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};

hlml_return_t hlml_device_get_oper_status(hlml_device_t device, char *status,
                                         unsigned int length) {
    log_call(__func__);
    return HLML_ERROR_NOT_SUPPORTED;
};
