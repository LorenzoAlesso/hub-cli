package logic

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DockerArg is an ARG declared in a Dockerfile.
type DockerArg struct {
	Name    string
	Default string // empty if no default is given
}

// ParseDockerfileArgs reads all ARG directives (case-insensitive) from a Dockerfile.
// Results are deduplicated by name: multi-stage builds may repeat the same ARG.
func ParseDockerfileArgs(dockerfilePath string) ([]DockerArg, error) {
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return nil, err
	}

	var args []DockerArg
	seen := make(map[string]bool)

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "ARG ") {
			continue
		}
		rest := strings.TrimSpace(line[4:])
		if rest == "" {
			continue
		}

		var arg DockerArg
		if idx := strings.IndexByte(rest, '='); idx != -1 {
			arg.Name = rest[:idx]
			arg.Default = rest[idx+1:]
		} else {
			arg.Name = rest
		}

		if !seen[arg.Name] {
			seen[arg.Name] = true
			args = append(args, arg)
		}
	}
	return args, nil
}

// DockerBuild runs `docker build --no-cache -t <repo>:<tag> -f <dockerfile> [--build-arg ...] <contextDir>`.
func DockerBuild(ecrRepository, tag, dockerfilePath string, buildArgs map[string]string, out io.Writer) error {
	fullImage := fmt.Sprintf("%s:%s", ecrRepository, tag)
	contextDir := filepath.Dir(dockerfilePath)

	cmdArgs := []string{"build", "--no-cache", "-t", fullImage, "-f", dockerfilePath}
	for k, v := range buildArgs {
		cmdArgs = append(cmdArgs, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	cmdArgs = append(cmdArgs, contextDir)

	cmd := exec.Command("docker", cmdArgs...)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build fallito: %w", err)
	}
	return nil
}

// SplitImageRef splits a full image reference "registry/path:tag" into
// repository ("registry/path") and tag. Tag is empty when absent; a port in
// the registry host ("host:5000/path") is not mistaken for a tag.
func SplitImageRef(image string) (repository, tag string) {
	idx := strings.LastIndex(image, ":")
	if idx == -1 || strings.Contains(image[idx:], "/") {
		return image, ""
	}
	return image[:idx], image[idx+1:]
}

// DockerPush runs `docker push <repo>:<tag>`.
func DockerPush(ecrRepository, tag string, out io.Writer) error {
	fullImage := fmt.Sprintf("%s:%s", ecrRepository, tag)

	cmd := exec.Command("docker", "push", fullImage)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push fallito: %w", err)
	}
	return nil
}
