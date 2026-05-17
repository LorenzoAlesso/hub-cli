package logic

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateHelmValuesTag updates a tag inside a Helm values file, preserving
// structure and comments. If helmImagePath is non-empty, the leaf becomes
// "<helmImagePath>:<newTag>"; otherwise newTag alone.
func UpdateHelmValuesTag(filePath, helmSetKey, newTag, helmImagePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("lettura %s: %w", filePath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parsing YAML %s: %w", filePath, err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return fmt.Errorf("file YAML vuoto: %s", filePath)
	}

	docNode := root.Content[0]
	parts := strings.Split(helmSetKey, ".")

	var value string
	if helmImagePath != "" {
		value = helmImagePath + ":" + newTag
	} else {
		value = newTag
	}

	if err := setYAMLNodeValue(docNode, parts, value); err != nil {
		return fmt.Errorf("aggiornamento path %q in %s: %w", helmSetKey, filePath, err)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("serializzazione YAML: %w", err)
	}

	// yaml.Marshal wraps in a document directive; strip it
	out = []byte(strings.TrimPrefix(string(out), "---\n"))

	return os.WriteFile(filePath, out, 0o644)
}

func setYAMLNodeValue(node *yaml.Node, path []string, value string) error {
	if node.Kind == yaml.DocumentNode {
		return setYAMLNodeValue(node.Content[0], path, value)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("nodo non è una mappa (kind=%d)", node.Kind)
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		if keyNode.Value != path[0] {
			continue
		}

		if len(path) == 1 {
			valNode.Kind = yaml.ScalarNode
			valNode.Tag = "!!str"
			valNode.Value = value
			return nil
		}
		return setYAMLNodeValue(valNode, path[1:], value)
	}

	return fmt.Errorf("chiave %q non trovata", path[0])
}

// UpdateK8sManifestImage finds lines in a Kubernetes manifest containing
// imageRef and replaces the tag after the last ":". Works for both ECR and ACR.
func UpdateK8sManifestImage(filePath, imageRef, newTag string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("lettura %s: %w", filePath, err)
	}

	// Match lines like: "          image: registry/path/imageRef:oldtag"
	pattern := regexp.MustCompile(
		`(?m)(^\s*image:\s*\S+` + regexp.QuoteMeta(imageRef) + `):[^\s]+`,
	)

	if !pattern.Match(data) {
		return fmt.Errorf("imageRef %q non trovato in %s", imageRef, filePath)
	}

	updated := pattern.ReplaceAll(data, []byte("${1}:"+newTag))
	return os.WriteFile(filePath, updated, 0o644)
}
