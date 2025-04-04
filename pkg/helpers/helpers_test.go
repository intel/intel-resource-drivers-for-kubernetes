package helpers

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"context"
	"flag"
	"os"
	"testing"
)

func TestNewAppWithFlags(t *testing.T) {
	driverName := "test-driver"
	newDriver := func(ctx context.Context, config *Config) (Driver, error) {
		return nil, nil
	}

	app := NewApp(driverName, newDriver, []cli.Flag{}, (interface{})(nil))
	set := flag.NewFlagSet("test", 0)
	set.String("node-name", "test-node", "doc")
	set.String("cdi-root", "/test/cdi", "doc")
	set.Int("num-devices", 10, "doc")

	ctx := cli.NewContext(app, set, nil)

	err := app.Before(ctx)
	if err != nil {
		t.Fatalf("Before function failed: %v", err)
	}

	if ctx.String("node-name") != "test-node" {
		t.Errorf("Expected node-name to be 'test-node', got %v", ctx.String("node-name"))
	}

	if ctx.String("cdi-root") != "/test/cdi" {
		t.Errorf("Expected cdi-root to be '/test/cdi', got %v", ctx.String("cdi-root"))
	}

	if ctx.Int("num-devices") != 10 {
		t.Errorf("Expected num-devices to be 10, got %v", ctx.Int("num-devices"))
	}
}

func TestWriteFile(t *testing.T) {
	tests := []struct {
		name         string
		filePath     string
		fileContents string
		expectError  bool
	}{
		{
			name:         "Valid file path and contents",
			filePath:     "testfile.txt",
			fileContents: "Hello, World!",
			expectError:  false,
		},
		{
			name:         "Invalid file path",
			filePath:     "/invalidpath/testfile.txt",
			fileContents: "Hello, World!",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteFile(tt.filePath, tt.fileContents)
			if (err != nil) != tt.expectError {
				t.Errorf("WriteFile() error = %v, expectError %v", err, tt.expectError)
			}

			if !tt.expectError {
				content, err := os.ReadFile(tt.filePath)
				if err != nil {
					t.Fatalf("Failed to read file: %v", err)
				}
				if string(content) != tt.fileContents {
					t.Errorf("Expected file contents to be %v, got %v", tt.fileContents, string(content))
				}
				os.Remove(tt.filePath)
			}
		})
	}
}

func TestStartPlugin(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		newDriver   func(ctx context.Context, config *Config) (Driver, error)
		setup       func()
		expectError bool
	}{
		{
			name: "CDI root is not a directory",
			config: &Config{
				CommonFlags: &Flags{
					KubeletPluginDir: "/tmp/testplugin",
					CdiRoot:          "/tmp/testfile",
				},
			},
			setup: func() {
				if err := os.WriteFile("/tmp/testfile", []byte("not a directory"), 0644); err != nil {
					t.Fatalf("Failed to write file: %v", err)
				}
			},
			expectError: true,
		},
		{
			name: "KubeletPluginDir does not exist",
			config: &Config{
				CommonFlags: &Flags{
					KubeletPluginDir: "/does-not-exist",
				},
			},
			expectError: true,
		},
		{
			name: "CDIRoot does not exist",
			config: &Config{
				CommonFlags: &Flags{
					KubeletPluginDir: AddRandomString("/tmp/test"),
					CdiRoot:          "/does-not-exist",
				},
			},
			expectError: true,
		},
		{
			name: "NewDriver returns error",
			config: &Config{
				CommonFlags: &Flags{
					KubeletPluginDir: "/tmp/testplugin",
					CdiRoot:          "/tmp/testcdi",
				},
			},
			newDriver: func(ctx context.Context, config *Config) (Driver, error) {
				return nil, fmt.Errorf("fake error %v", "from newDriver")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			defer os.RemoveAll("/tmp/testplugin")
			defer os.RemoveAll("/tmp/testcdi")
			defer os.Remove("/tmp/testfile")

			ctx := context.Background()
			err := StartPlugin(ctx, tt.config, tt.newDriver)
			if (err != nil) != tt.expectError {
				t.Errorf("StartPlugin() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}
