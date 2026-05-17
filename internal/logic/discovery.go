package logic

import (
	"os"
	"path/filepath"
	"strings"
)

// FindDockerfiles walks root recursively and returns every Dockerfile (or *.Dockerfile) found.
func FindDockerfiles(root string) ([]string, error) {
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if name == "Dockerfile" || strings.HasSuffix(name, ".Dockerfile") {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}

// FindHelmValues walks root recursively and returns every values*.yaml file found.
func FindHelmValues(root string) ([]string, error) {
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, "values") && strings.HasSuffix(name, ".yaml") {
			found = append(found, path)
		}
		return nil
	})
	return found, err
}
