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
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/klog/v2"
	drapb "k8s.io/kubelet/pkg/apis/dra/v1"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

type healthcheck struct {
	grpc_health_v1.UnimplementedHealthServer
	server    *grpc.Server
	regClient registerapi.RegistrationClient
	draClient drapb.DRAPluginClient
}

func startHealthcheck(ctx context.Context, port int, registrarDir, pluginDir, driverName string) (*healthcheck, error) {
	if port < 0 {
		klog.Info("Health check server disabled")
		return nil, nil
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("health check listen on port %d: %w", port, err)
	}

	regSocketPath := filepath.Join(registrarDir, driverName+"-reg.sock")
	regConn, err := grpc.NewClient("unix://"+regSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		lis.Close()
		return nil, fmt.Errorf("health check connect to registration socket %s: %w", regSocketPath, err)
	}

	draSocketPath := filepath.Join(pluginDir, "dra.sock")
	draConn, err := grpc.NewClient("unix://"+draSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		lis.Close()
		regConn.Close()
		return nil, fmt.Errorf("health check connect to DRA socket %s: %w", draSocketPath, err)
	}

	hc := &healthcheck{
		server:    grpc.NewServer(),
		regClient: registerapi.NewRegistrationClient(regConn),
		draClient: drapb.NewDRAPluginClient(draConn),
	}

	grpc_health_v1.RegisterHealthServer(hc.server, hc)

	go func() {
		klog.Infof("Starting health check server on port %d", port)
		if err := hc.server.Serve(lis); err != nil {
			klog.Errorf("Health check server failed to serve: %v", err)
		}
	}()

	return hc, nil
}

func (hc *healthcheck) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	service := req.GetService()
	if service != "" && service != "liveness" {
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_SERVICE_UNKNOWN,
		}, nil
	}

	_, err := hc.regClient.GetInfo(ctx, &registerapi.InfoRequest{})
	if err != nil {
		klog.V(3).Infof("Health check: registration server check failed: %v", err)
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		}, nil
	}

	_, err = hc.draClient.NodePrepareResources(ctx, &drapb.NodePrepareResourcesRequest{})
	if err != nil {
		klog.V(3).Infof("Health check: DRA server check failed: %v", err)
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		}, nil
	}

	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (hc *healthcheck) stop() {
	if hc != nil && hc.server != nil {
		hc.server.GracefulStop()
	}
}
