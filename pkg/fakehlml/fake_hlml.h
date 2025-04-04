/* SPDX-License-Identifier: MIT
 *
 * Copyright (c) 2024, Intel Corporation. All Rights Reserved.
 *
 */

#ifndef __FAKE_HLML_H__
#define __FAKE_HLML_H__

#ifdef __cplusplus
extern "C" {
#endif

#include "../../vendor/github.com/HabanaAI/gohlml/hlml.h"

/* Enum for returned values of the different APIs */
typedef enum call_identity {
    FAKE_INIT = 0,
    FAKE_INIT_WITH_FLAGS,
    FAKE_SHUTDOWN,
    FAKE_DEVICE_GET_COUNT,
    FAKE_DEVICE_GET_HANDLE_BY_PCI_BUS_ID,
    FAKE_DEVICE_GET_HANDLE_BY_INDEX,
    FAKE_DEVICE_GET_HANDLE_BY_UUID,
    FAKE_DEVICE_GET_NAME,
    FAKE_DEVICE_GET_PCI_INFO,
    FAKE_DEVICE_GET_SERIAL,
    FAKE_DEVICE_REGISTER_EVENTS,
    FAKE_EVENT_SET_CREATE,
    FAKE_EVENT_SET_FREE,
    FAKE_EVENT_SET_WAIT,
    FAKE_CALL_IDENTITY_MAX
} call_identity_t;

void add_device(const char *pci_addr, const char *pci_device_id, const char *pci_vendor_id, const char *serial, unsigned int index);
void reset(void);

void set_error(call_identity_t call_id, hlml_return_t errCode);
void set_success(call_identity_t call_id);

void add_critical_event(const char *serial);
void reset_events(void);

#ifdef __cplusplus
}   //extern "C"
#endif

#endif /* __FAKE_HLML_H__ */
