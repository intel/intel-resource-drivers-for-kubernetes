package utils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
)

// LocateRepoFile locates a file inside this repository.
func LocateRepoFile(repopath string) (string, error) {
	root := os.Getenv("PLUGINS_REPO_DIR")
	if root != "" {
		path := filepath.Join(root, repopath)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return path, nil
		}
	}

	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	path := filepath.Join(currentDir, repopath)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return path, nil
	}

	path = filepath.Join(currentDir, "../../"+repopath)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return path, err
	}

	return "", errors.New("no file found, try to define PLUGINS_REPO_DIR pointing to the root of the repository")
}

// GetPodLogs returns the log of the container. If not possible to get logs, it returns the error message.
func GetPodLogs(ctx context.Context, f *framework.Framework, podName, containerName string) string {
	log, err := e2epod.GetPodLogs(ctx, f.ClientSet, f.Namespace.Name, podName, containerName)
	if err != nil {
		return fmt.Sprintf("unable to get log from pod: %v", err)
	}

	return fmt.Sprintf("log output of the container %s in the pod %s:%s", containerName, podName, log)
}
