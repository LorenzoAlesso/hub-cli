package logic

import (
	"fmt"
	"io"
	"os/exec"
)

// ECRLogin pipes `aws ecr get-login-password` into `docker login` for the given registry.
func ECRLogin(region, accountID string, out io.Writer) error {
	registry := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, region)

	awsCmd := exec.Command("aws", "ecr", "get-login-password", "--region", region)
	dockerCmd := exec.Command("docker", "login", "--username", "AWS", "--password-stdin", registry)

	pipe, err := awsCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("errore creazione pipe: %w", err)
	}
	awsCmd.Stderr = out

	dockerCmd.Stdin = pipe
	dockerCmd.Stdout = out
	dockerCmd.Stderr = out

	if err := awsCmd.Start(); err != nil {
		return fmt.Errorf("errore avvio aws cli: %w", err)
	}
	if err := dockerCmd.Start(); err != nil {
		return fmt.Errorf("errore avvio docker login: %w", err)
	}
	if err := awsCmd.Wait(); err != nil {
		return fmt.Errorf("aws ecr get-login-password fallito: %w", err)
	}
	if err := dockerCmd.Wait(); err != nil {
		return fmt.Errorf("docker login fallito: %w", err)
	}
	return nil
}

// ECRDeleteImage removes a tag from ECR. Used to roll back a successful push.
func ECRDeleteImage(region, ecrRepository, tag string) error {
	idx := 0
	for i, c := range ecrRepository {
		if c == '/' {
			idx = i
			break
		}
	}
	repoName := ecrRepository[idx+1:]

	cmd := exec.Command(
		"aws", "ecr", "batch-delete-image",
		"--region", region,
		"--repository-name", repoName,
		"--image-ids", fmt.Sprintf("imageTag=%s", tag),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rollback ECR fallito: %w", err)
	}
	return nil
}
