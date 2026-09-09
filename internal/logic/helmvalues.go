package logic

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// HelmImageKey is one place in a values file that names an image tag. ImagePath
// is non-empty only for the inline "repository:tag" form — the two shapes
// UpdateHelmValuesTag supports.
type HelmImageKey struct {
	Key       string // top-level values key, e.g. "jbossBe"
	SetKey    string // dotted path for `helm --set`, e.g. "jbossBe.image.tag"
	ImagePath string

	// Deployment is the `name` declared next to the image: the Kubernetes
	// Deployment the templates build from this block. It often matches the image
	// name, but that is this chart's habit rather than a rule, so a restart has
	// to read it instead of guessing. Empty when the block declares no name —
	// an image referenced outside a Deployment, like an init container.
	Deployment string
}

// HelmService is one deployable image declared in a chart values file: what can
// be deployed, where it is pushed and which tag is live. Services are keyed by
// image rather than by values key, because the same image can be referenced more
// than once and a new tag has to land on every reference.
type HelmService struct {
	Name       string // image name, e.g. "jboss-be" — resolves the Dockerfile
	Repository string // full repository, e.g. "acr.azurecr.io/apps/jboss-be"
	Tag        string
	Keys       []HelmImageKey

	mixedTags bool
}

// TagsAgree reports whether every reference to this image declares the same tag.
// They normally do; when they do not the values are already inconsistent and the
// caller should say so rather than silently pick one.
func (s HelmService) TagsAgree() bool { return !s.mixedTags }

// HelmValues is what a chart values file declares about a deploy.
type HelmValues struct {
	Namespace string
	Services  []HelmService
}

// ReadHelmValues collects the namespace and the services from a values file.
// Services are the top-level keys carrying an image, in file order.
func ReadHelmValues(valuesPath string) (HelmValues, error) {
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		return HelmValues{}, fmt.Errorf("lettura %s: %w", valuesPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return HelmValues{}, fmt.Errorf("parsing YAML %s: %w", valuesPath, err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return HelmValues{}, fmt.Errorf("%s non contiene una mappa YAML", valuesPath)
	}

	out := HelmValues{}
	doc := root.Content[0]
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, value := doc.Content[i].Value, doc.Content[i+1]

		if key == "namespace" && value.Kind == yaml.ScalarNode {
			out.Namespace = value.Value
			continue
		}
		if ref, ok := helmImageFrom(key, value); ok {
			out.add(ref)
		}
	}
	return out, nil
}

// add merges a reference into the service that owns the image, keeping the
// services in order of first appearance.
func (v *HelmValues) add(ref helmImageRef) {
	for i := range v.Services {
		if v.Services[i].Repository == ref.repository {
			if v.Services[i].Tag != ref.tag {
				v.Services[i].mixedTags = true
			}
			v.Services[i].Keys = append(v.Services[i].Keys, ref.key)
			return
		}
	}
	v.Services = append(v.Services, HelmService{
		Name:       imageName(ref.repository),
		Repository: ref.repository,
		Tag:        ref.tag,
		Keys:       []HelmImageKey{ref.key},
	})
}

// Deployments lists the Kubernetes Deployments running this image, in values
// order and without repeats, plus the values keys that name none: an image
// referenced outside a Deployment cannot be restarted, and the caller has to say
// so rather than skip it quietly.
func (s HelmService) Deployments() (names []string, unnamed []string) {
	for _, k := range s.Keys {
		if k.Deployment == "" {
			unnamed = append(unnamed, k.Key)
			continue
		}
		if !slices.Contains(names, k.Deployment) {
			names = append(names, k.Deployment)
		}
	}
	return names, unnamed
}

// DeploymentFor returns the Deployment declared by the values block setKey points
// at ("jbossBe.image.tag" → the name in the "jbossBe" block), or "" when the
// block names none.
func (v HelmValues) DeploymentFor(setKey string) string {
	key, _, _ := strings.Cut(setKey, ".")
	for _, svc := range v.Services {
		for _, k := range svc.Keys {
			if k.Key == key {
				return k.Deployment
			}
		}
	}
	return ""
}

// helmImageRef is one image reference found while walking the values.
type helmImageRef struct {
	repository string
	tag        string
	key        HelmImageKey
}

// helmImageFrom recognises the two shapes a values file uses for an image:
// a map with repository and tag, or a single "repository:tag" string.
func helmImageFrom(key string, node *yaml.Node) (helmImageRef, bool) {
	if node.Kind != yaml.MappingNode {
		return helmImageRef{}, false
	}

	image := mappingValue(node, "image")
	if image == nil {
		return helmImageRef{}, false
	}

	deployment := ""
	if name := mappingValue(node, "name"); name != nil && name.Kind == yaml.ScalarNode {
		deployment = name.Value
	}

	switch image.Kind {
	case yaml.MappingNode:
		repo := mappingValue(image, "repository")
		tag := mappingValue(image, "tag")
		if repo == nil || tag == nil {
			return helmImageRef{}, false
		}
		return helmImageRef{
			repository: repo.Value,
			tag:        tag.Value,
			key:        HelmImageKey{Key: key, SetKey: key + ".image.tag", Deployment: deployment},
		}, true

	case yaml.ScalarNode:
		repo, tag := SplitImageRef(image.Value)
		if repo == "" || tag == "" {
			return helmImageRef{}, false
		}
		return helmImageRef{
			repository: repo,
			tag:        tag,
			key:        HelmImageKey{Key: key, SetKey: key + ".image", ImagePath: repo, Deployment: deployment},
		}, true
	}
	return helmImageRef{}, false
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// imageName is the last segment of a repository path: the service name the
// Dockerfile is looked up by. Taking it from the repository rather than from the
// values key avoids a camelCase-to-kebab conversion that would be guesswork.
func imageName(repository string) string {
	if idx := strings.LastIndex(repository, "/"); idx != -1 {
		return repository[idx+1:]
	}
	return repository
}

// ReadChartVersion reads the version declared in a chart directory, so the
// deployed version always matches the chart sitting next to the values instead
// of a number copied into the configuration.
func ReadChartVersion(chartDir string) (string, error) {
	path := filepath.Join(chartDir, "Chart.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("lettura %s: %w", path, err)
	}

	var chart struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &chart); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	if chart.Version == "" {
		return "", fmt.Errorf("%s non dichiara una version", path)
	}
	return chart.Version, nil
}
