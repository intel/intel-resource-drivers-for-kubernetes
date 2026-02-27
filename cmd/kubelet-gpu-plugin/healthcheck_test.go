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
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	drapb "k8s.io/kubelet/pkg/apis/dra/v1"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

func TestStartHealthcheck_Disabled(t *testing.T) {
	hc, err := startHealthcheck(context.Background(), -1, "/tmp", "/tmp", "test-driver")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hc != nil {
		t.Fatal("expected nil healthcheck when disabled")
	}
	// should not panic with nil receiver
	hc.stop()
}

func TestStartHealthcheck_StartAndStop(t *testing.T) {
	hc, err := startHealthcheck(context.Background(), 0, t.TempDir(), t.TempDir(), "test-driver")
	if err != nil {
		t.Fatalf("startHealthcheck failed: %v", err)
	}
	if hc == nil {
		t.Fatal("expected non-nil healthcheck")
	}
	hc.stop()
}

type fakeRegistrationServer struct {
	registerapi.UnimplementedRegistrationServer
}

func (f *fakeRegistrationServer) GetInfo(_ context.Context, _ *registerapi.InfoRequest) (*registerapi.PluginInfo, error) {
	return &registerapi.PluginInfo{
		Type:              registerapi.DRAPlugin,
		Name:              "test-driver",
		SupportedVersions: []string{"v1"},
	}, nil
}

type fakeDRAPluginServer struct {
	drapb.UnimplementedDRAPluginServer
}

func (f *fakeDRAPluginServer) NodePrepareResources(_ context.Context, _ *drapb.NodePrepareResourcesRequest) (*drapb.NodePrepareResourcesResponse, error) {
	return &drapb.NodePrepareResourcesResponse{}, nil
}

func TestHealthcheck_Check(t *testing.T) {
	// start fake registration server
	regLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	regSrv := grpc.NewServer()
	registerapi.RegisterRegistrationServer(regSrv, &fakeRegistrationServer{})
	go func() {
		if err := regSrv.Serve(regLis); err != nil {
			t.Logf("registration server stopped serving: %v", err)
		}
	}()
	defer regSrv.GracefulStop()

	//nolint:forcetypeassert // net.Listen always returns *net.TCPAddr.
	regConn, err := grpc.NewClient(
		fmt.Sprintf("dns:///127.0.0.1:%d", regLis.Addr().(*net.TCPAddr).Port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial reg: %v", err)
	}
	defer regConn.Close()

	// start fake DRA server
	draLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	draSrv := grpc.NewServer()
	drapb.RegisterDRAPluginServer(draSrv, &fakeDRAPluginServer{})
	go func() {
		if err := draSrv.Serve(draLis); err != nil {
			t.Logf("DRA server stopped serving: %v", err)
		}
	}()
	defer draSrv.GracefulStop()

	//nolint:forcetypeassert // net.Listen always returns *net.TCPAddr.
	draConn, err := grpc.NewClient(
		fmt.Sprintf("dns:///127.0.0.1:%d", draLis.Addr().(*net.TCPAddr).Port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial dra: %v", err)
	}
	defer draConn.Close()

	// connection to nowhere, for NOT_SERVING tests
	badConn, err := grpc.NewClient("dns:///127.0.0.1:1",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bad: %v", err)
	}
	defer badConn.Close()

	tests := []struct {
		name      string
		service   string
		regClient registerapi.RegistrationClient
		draClient drapb.DRAPluginClient
		want      grpc_health_v1.HealthCheckResponse_ServingStatus
	}{
		{
			name:      "empty service",
			service:   "",
			regClient: registerapi.NewRegistrationClient(regConn),
			draClient: drapb.NewDRAPluginClient(draConn),
			want:      grpc_health_v1.HealthCheckResponse_SERVING,
		},
		{
			name:      "liveness service",
			service:   "liveness",
			regClient: registerapi.NewRegistrationClient(regConn),
			draClient: drapb.NewDRAPluginClient(draConn),
			want:      grpc_health_v1.HealthCheckResponse_SERVING,
		},
		{
			name:      "unknown service",
			service:   "unknown",
			regClient: registerapi.NewRegistrationClient(regConn),
			draClient: drapb.NewDRAPluginClient(draConn),
			want:      grpc_health_v1.HealthCheckResponse_SERVICE_UNKNOWN,
		},
		{
			name:      "registration down",
			service:   "liveness",
			regClient: registerapi.NewRegistrationClient(badConn),
			draClient: drapb.NewDRAPluginClient(draConn),
			want:      grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		},
		{
			name:      "DRA plugin down",
			service:   "liveness",
			regClient: registerapi.NewRegistrationClient(regConn),
			draClient: drapb.NewDRAPluginClient(badConn),
			want:      grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := &healthcheck{
				regClient: tt.regClient,
				draClient: tt.draClient,
			}
			resp, err := hc.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
				Service: tt.service,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, resp.Status)
			}
		})
	}
}
