/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// fakeHealthStream implements drahealthv1alpha1.DRAResourceHealth_NodeWatchResourcesServer
// (grpc.ServerStreamingServer[NodeWatchResourcesResponse]) for unit tests.
type fakeHealthStream struct {
	sent    []*drahealthv1alpha1.NodeWatchResourcesResponse
	context context.Context
}

func (f *fakeHealthStream) Send(r *drahealthv1alpha1.NodeWatchResourcesResponse) error {
	f.sent = append(f.sent, r)
	return nil
}

func (f *fakeHealthStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeHealthStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeHealthStream) SetTrailer(metadata.MD)       {}
func (f *fakeHealthStream) Context() context.Context {
	if f.context != nil {
		return f.context
	}
	return context.Background()
}
func (f *fakeHealthStream) SendMsg(any) error { return nil }
func (f *fakeHealthStream) RecvMsg(any) error { return nil }

func newDriverForHealthTests(allocatable map[string]*device.DeviceInfo) *driver {
	return &driver{
		state: &nodeState{
			Allocatable: allocatable,
			NodeName:    "node1",
		},
		healthStreams: make(map[int]chan *drahealthv1alpha1.NodeWatchResourcesResponse),
	}
}

func TestRegisterAndUnregisterHealthStream(t *testing.T) {
	drv := newDriverForHealthTests(map[string]*device.DeviceInfo{})

	ch := make(chan *drahealthv1alpha1.NodeWatchResourcesResponse, 1)
	id := drv.registerHealthStream(ch)
	if drv.healthStreams[id] != ch {
		t.Fatalf("expected channel to be registered under id %d", id)
	}

	drv.unregisterHealthStream(id)
	if _, exists := drv.healthStreams[id]; exists {
		t.Errorf("expected stream %d to be removed after unregister", id)
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel to be closed after unregister")
	}
}

func TestDeviceInfoToDeviceHealth(t *testing.T) {
	drv := newDriverForHealthTests(map[string]*device.DeviceInfo{})

	cases := []struct {
		input    string
		expected drahealthv1alpha1.HealthStatus
	}{
		{device.HealthHealthy, drahealthv1alpha1.HealthStatus_HEALTHY},
		{device.HealthUnhealthy, drahealthv1alpha1.HealthStatus_UNHEALTHY},
		{device.HealthUnknown, drahealthv1alpha1.HealthStatus_UNKNOWN},
	}

	for _, tc := range cases {
		dev := &device.DeviceInfo{UID: "uid1", Health: tc.input}
		got := drv.deviceInfoToDeviceHealth(dev)
		if got.Health != tc.expected {
			t.Errorf("health %q: expected %v, got %v", tc.input, tc.expected, got.Health)
		}
		if got.Device.DeviceName != "uid1" || got.Device.PoolName != "node1" {
			t.Errorf("unexpected device identifier: %+v", got.Device)
		}
	}
}

func TestBuildHealthResponse(t *testing.T) {
	allocatable := map[string]*device.DeviceInfo{
		"uid1": {UID: "uid1", Health: device.HealthHealthy},
		"uid2": {UID: "uid2", Health: device.HealthUnhealthy},
	}
	drv := newDriverForHealthTests(allocatable)

	resp := drv.buildHealthResponse()
	if len(resp.Devices) != len(allocatable) {
		t.Errorf("expected %d devices, got %d", len(allocatable), len(resp.Devices))
	}
}

func TestBroadcastHealthUpdateWithResponse(t *testing.T) {
	drv := newDriverForHealthTests(map[string]*device.DeviceInfo{})

	ch := make(chan *drahealthv1alpha1.NodeWatchResourcesResponse, 1)
	drv.registerHealthStream(ch)

	resp := &drahealthv1alpha1.NodeWatchResourcesResponse{}
	drv.broadcastHealthUpdateWithResponse(resp)

	select {
	case got := <-ch:
		if got != resp {
			t.Error("stream received unexpected response")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not receive update")
	}
}

func TestSendCurrentHealthStatus(t *testing.T) {
	allocatable := map[string]*device.DeviceInfo{
		"uid1": {UID: "uid1", Health: device.HealthHealthy},
	}
	drv := newDriverForHealthTests(allocatable)

	stream := &fakeHealthStream{}
	if err := drv.sendCurrentHealthStatus(context.Background(), stream); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(stream.sent) != 1 || len(stream.sent[0].Devices) != 1 {
		t.Errorf("expected one response with 1 device, got %+v", stream.sent)
	}
}

// nodeWatchResourceCallerRoutine calls drv.NodeWatchResources and waits while it exits when the
// stream context is cancelled.
func nodeWatchResourceCallerRoutine(t *testing.T, drv *driver, stream *fakeHealthStream, expectedErr string, wg *sync.WaitGroup) {
	defer wg.Done()
	err := drv.NodeWatchResources(&drahealthv1alpha1.NodeWatchResourcesRequest{}, stream)
	if (err == nil && expectedErr != "") || (err != nil && err.Error() != expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestNodeWatchResources(t *testing.T) {
	var wg sync.WaitGroup
	drv := newDriverForHealthTests(map[string]*device.DeviceInfo{
		"uid1": {UID: "uid1", Health: device.HealthHealthy},
	})

	shortLivedContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream := &fakeHealthStream{context: shortLivedContext}
	wg.Add(1)
	go nodeWatchResourceCallerRoutine(t, drv, stream, "", &wg)

	time.Sleep(200 * time.Millisecond) // wait for initial health status to be sent
	streamCh, found := drv.healthStreams[drv.healthStreamID]
	if !found {
		t.Fatalf("expected stream to be registered with id %v", drv.healthStreamID)
	}
	close(streamCh)

	if len(stream.sent) != 1 || len(stream.sent[0].Devices) != 1 {
		t.Errorf("expected one response with 1 device, got %+v", stream.sent)
	}

	wg.Wait() // wait for routine to exit without panicing if Testing logged an error
}
