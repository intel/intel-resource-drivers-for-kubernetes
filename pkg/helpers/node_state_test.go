package helpers

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func TestGetOrCreatePreparedClaims(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		expectError    bool
		expectedClaims ClaimPreparations
	}{
		{
			name:           "FileExists",
			initialContent: `{"claim1": {"devices":[{"devicename": "device1"}]}}`,
			expectError:    false,
			expectedClaims: ClaimPreparations{
				"claim1": {
					Devices: []kubeletplugin.Device{{DeviceName: "device1"}},
				},
			},
		},
		{
			name:           "FileNotExist",
			initialContent: "",
			expectError:    false,
			expectedClaims: ClaimPreparations{},
		},
		{
			name:           "InvalidFileContent",
			initialContent: `{"claim1": [`,
			expectError:    true,
			expectedClaims: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := "test_prepared_claims.json"
			defer os.Remove(filePath)

			if tt.initialContent != "" {
				err := os.WriteFile(filePath, []byte(tt.initialContent), 0600)
				if err != nil {
					t.Fatalf("failed to write initial content to file: %v", err)
				}
			}

			preparedClaims, err := GetOrCreatePreparedClaims(filePath)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected an error but got none: %v", err)
				}
				if preparedClaims != nil {
					t.Fatalf("expected preparedClaims to be nil but got: %v", preparedClaims)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if preparedClaims == nil {
					t.Fatalf("expected preparedClaims but got nil: %v", preparedClaims)
				}
				if !reflect.DeepEqual(tt.expectedClaims, preparedClaims) {
					t.Fatalf("expected %v but got %v", tt.expectedClaims, preparedClaims)
				}

				// Verify file creation
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					t.Fatalf("expected file to exist but it does not: %v", err)
				}
			}
		})
	}
}

func TestWritePreparedClaimsToFile(t *testing.T) {
	tests := []struct {
		name           string
		claims         ClaimPreparations
		expectedError  bool
		expectedOutput string
	}{
		{
			name: "ValidClaims",
			claims: ClaimPreparations{
				"claim1": {
					Devices: []kubeletplugin.Device{{DeviceName: "device1"}},
				},
			},
			expectedError:  false,
			expectedOutput: `{"claim1":{"Devices":[{"DeviceName":"device1","PoolName":"","Requests":null,"CDIDeviceIDs":null}], "Err":null}}`,
		},
		{
			name:           "EmptyClaims",
			claims:         ClaimPreparations{},
			expectedError:  false,
			expectedOutput: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := "test_write_prepared_claims.json"
			defer os.Remove(filePath)

			err := WritePreparedClaimsToFile(filePath, tt.claims)

			if tt.expectedError {
				if err == nil {
					t.Fatalf("expected an error but got none: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("failed to read file: %v", err)
				}

				var actualOutput map[string]interface{}
				var expectedOutput map[string]interface{}

				if err := json.Unmarshal(content, &actualOutput); err != nil {
					t.Fatalf("failed to unmarshal actual output: %v", err)
				}

				if err := json.Unmarshal([]byte(tt.expectedOutput), &expectedOutput); err != nil {
					t.Fatalf("failed to unmarshal expected output: %v", err)
				}

				if !reflect.DeepEqual(actualOutput, expectedOutput) {
					t.Fatalf("expected %v but got %v", expectedOutput, actualOutput)
				}
			}
		})
	}
}

func TestUnprepare(t *testing.T) {
	tests := []struct {
		name             string
		initialPrepared  ClaimPreparations
		claimUID         string
		expectedPrepared ClaimPreparations
		expectError      bool
	}{
		{
			name: "Unprepare existing claim",
			initialPrepared: ClaimPreparations{
				"claim1": {Devices: []kubeletplugin.Device{{DeviceName: "device1"}}},
			},
			claimUID:         "claim1",
			expectedPrepared: ClaimPreparations{},
			expectError:      false,
		},
		{
			name: "Unprepare nonexisting claim",
			initialPrepared: ClaimPreparations{
				"claim1": {Devices: []kubeletplugin.Device{{DeviceName: "device1"}}},
			},
			claimUID: "claim2",
			expectedPrepared: ClaimPreparations{
				"claim1": {Devices: []kubeletplugin.Device{{DeviceName: "device1"}}},
			},
			expectError: false,
		},
		{
			name:             "Unprepare when no claims are prepared",
			initialPrepared:  ClaimPreparations{},
			claimUID:         "claim2",
			expectedPrepared: ClaimPreparations{},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := "test_unprepare_claims.json"
			defer os.Remove(filePath)

			err := WritePreparedClaimsToFile(filePath, tt.initialPrepared)
			if err != nil {
				t.Fatalf("failed to write initial prepared claims to file: %v", err)
			}

			nodeState := &NodeState{
				Prepared:               tt.initialPrepared,
				PreparedClaimsFilePath: filePath,
			}

			err = nodeState.Unprepare(context.Background(), tt.claimUID)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected an error but got none: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if !reflect.DeepEqual(tt.expectedPrepared, nodeState.Prepared) {
					t.Fatalf("expected %v but got %v", tt.expectedPrepared, nodeState.Prepared)
				}

				// Verify file content
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("failed to read file: %v", err)
				}

				var actualOutput ClaimPreparations
				if err := json.Unmarshal(content, &actualOutput); err != nil {
					t.Fatalf("failed to unmarshal actual output: %v", err)
				}

				if !reflect.DeepEqual(tt.expectedPrepared, actualOutput) {
					t.Fatalf("expected %v but got %v", tt.expectedPrepared, actualOutput)
				}
			}
		})
	}
}
