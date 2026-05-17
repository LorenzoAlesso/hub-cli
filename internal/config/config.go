package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// SeedStatus is the outcome of the first-run seeding step.
type SeedStatus int

const (
	SeedNone     SeedStatus = iota // config already exists; no seeding performed
	SeedFromFile                   // config created and populated from ~/.hub-cli.seed.yaml
	SeedEmpty                      // config created empty (seed file absent)
)

var freshlySeededStatus SeedStatus = SeedNone

// WasSeeded reports whether the config was just created (populated or empty).
func WasSeeded() bool { return freshlySeededStatus != SeedNone }

// SeededFromFile reports whether first-run loaded services from the seed YAML.
func SeededFromFile() bool { return freshlySeededStatus == SeedFromFile }

// SeedFilePath returns the expected path of the user's seed file.
func SeedFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hub-cli.seed.yaml")
}

type GlobalConfig struct {
	DockerRootPath   string `mapstructure:"docker_root_path"`
	HelmRootPath     string `mapstructure:"helm_root_path"`
	ECRRegion        string `mapstructure:"ecr_region"`
	ECRAccountID     string `mapstructure:"ecr_account_id"`
	ChartVersion     string `mapstructure:"chart_version"`
	LocalKubeContext string `mapstructure:"local_kube_context"`
	Theme            string `mapstructure:"theme"`
}

type ServiceConfig struct {
	LastTag           string `mapstructure:"last_tag"           yaml:"last_tag,omitempty"`
	DockerfileSubpath string `mapstructure:"dockerfile_subpath" yaml:"dockerfile_subpath,omitempty"`
	HelmValuesPath    string `mapstructure:"helm_values_path"   yaml:"helm_values_path,omitempty"`
	Namespace         string `mapstructure:"namespace"          yaml:"namespace,omitempty"`
	ChartName         string `mapstructure:"chart_name"         yaml:"chart_name,omitempty"`
	ChartVersion      string `mapstructure:"chart_version"      yaml:"chart_version,omitempty"`
	ReleaseName       string `mapstructure:"release_name"       yaml:"release_name,omitempty"`
	ECRRepository     string `mapstructure:"ecr_repository"     yaml:"ecr_repository"`
	HelmSetKey        string `mapstructure:"helm_set_key"       yaml:"helm_set_key"`
	HelmImagePath     string `mapstructure:"helm_image_path"    yaml:"helm_image_path,omitempty"`
	K8sManifestPath   string `mapstructure:"k8s_manifest_path"  yaml:"k8s_manifest_path,omitempty"`
	K8sImageRef       string `mapstructure:"k8s_image_ref"      yaml:"k8s_image_ref,omitempty"`
}

type Config struct {
	Config   GlobalConfig             `mapstructure:"config"`
	Services map[string]ServiceConfig `mapstructure:"services"`
}

func Init(customPath string) error {
	if customPath != "" {
		viper.SetConfigFile(customPath)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("impossibile trovare la home directory: %w", err)
		}
		viper.SetConfigFile(filepath.Join(home, ".hub-cli.yaml"))
	}

	viper.SetDefault("config.ecr_region", "eu-west-1")
	viper.SetDefault("config.ecr_account_id", "000000000000")
	viper.SetDefault("config.chart_version", "3.0.0")
	viper.SetDefault("config.local_kube_context", "kubernetes-admin@kubernetes")

	if err := viper.ReadInConfig(); err != nil {
		loaded, seedErr := seedDefaultServices()
		if seedErr != nil {
			return seedErr
		}
		if loaded {
			freshlySeededStatus = SeedFromFile
		} else {
			freshlySeededStatus = SeedEmpty
			// Ensure the services section exists (even empty) so later writes
			// (add-service) don't fail.
			viper.Set("services", map[string]any{})
		}
		if err := viper.WriteConfigAs(viper.ConfigFileUsed()); err != nil {
			return err
		}
		return viper.ReadInConfig()
	}

	// Maintenance on an existing config: strip service keys with control
	// characters (e.g. stray ANSI codes), then merge in any seed services
	// not yet present.
	if sanitizeServiceKeysInFile() {
		if err := viper.ReadInConfig(); err != nil {
			return err
		}
	}
	added, err := ensureMissingSeedServices()
	if err != nil {
		return err
	}
	if added {
		if err := viper.WriteConfig(); err != nil {
			return err
		}
		return viper.ReadInConfig()
	}
	return nil
}

// loadSeedFromFile reads ~/.hub-cli.seed.yaml and returns the services map.
// Missing file is a valid state and returns (nil, nil); a malformed file returns an error.
func loadSeedFromFile() (map[string]map[string]any, error) {
	path := SeedFilePath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("errore lettura seed file %s: %w", path, err)
	}
	var parsed struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("errore parsing seed file %s: %w", path, err)
	}
	return parsed.Services, nil
}

// seedDefaultServices populates viper with services from the seed file.
// Returns (true, nil) if at least one service was loaded.
func seedDefaultServices() (bool, error) {
	services, err := loadSeedFromFile()
	if err != nil {
		return false, err
	}
	if len(services) == 0 {
		return false, nil
	}
	for name, fields := range services {
		for key, val := range fields {
			viper.Set(fmt.Sprintf("services.%s.%s", name, key), val)
		}
	}
	return true, nil
}

// sanitizeServiceKeysInFile strips service keys containing control characters
// (e.g. ANSI codes) from the YAML file. Returns true if any key was removed.
func sanitizeServiceKeysInFile() bool {
	filePath := viper.ConfigFileUsed()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false
	}
	doc := root.Content[0]

	var servicesNode *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "services" {
			servicesNode = doc.Content[i+1]
			break
		}
	}
	if servicesNode == nil {
		return false
	}

	removed := false
	var clean []*yaml.Node
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		key := servicesNode.Content[i].Value
		hasControl := false
		for _, r := range key {
			if r < 0x20 {
				hasControl = true
				break
			}
		}
		if hasControl {
			removed = true
			continue
		}
		clean = append(clean, servicesNode.Content[i], servicesNode.Content[i+1])
	}
	if !removed {
		return false
	}

	servicesNode.Content = clean
	out, err := yaml.Marshal(&root)
	if err != nil {
		return false
	}
	_ = os.WriteFile(filePath, out, 0644)
	return true
}

// ensureMissingSeedServices adds seed-file services that are not yet in the config.
// Returns (true, nil) if any service was added.
func ensureMissingSeedServices() (bool, error) {
	services, err := loadSeedFromFile()
	if err != nil {
		return false, err
	}
	if len(services) == 0 {
		return false, nil
	}
	current := viper.GetStringMap("services")
	added := false
	for name, fields := range services {
		if _, exists := current[strings.ToLower(name)]; exists {
			continue
		}
		for key, val := range fields {
			viper.Set(fmt.Sprintf("services.%s.%s", name, key), val)
		}
		added = true
	}
	return added, nil
}

func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("errore parsing config: %w", err)
	}
	return &cfg, nil
}

func Save() error {
	return viper.WriteConfig()
}

func UpdateServiceTag(serviceName, newTag string) error {
	viper.Set(fmt.Sprintf("services.%s.last_tag", serviceName), newTag)
	return Save()
}

func UpdateServiceDockerfilePath(serviceName, subpath string) error {
	viper.Set(fmt.Sprintf("services.%s.dockerfile_subpath", serviceName), subpath)
	return Save()
}

func GetFilePath() string {
	return viper.ConfigFileUsed()
}

func SetDockerRootPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	viper.Set("config.docker_root_path", abs)
	return Save()
}

func SetHelmRootPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	viper.Set("config.helm_root_path", abs)
	return Save()
}

func UpdateServiceChartVersion(serviceName, chartVersion string) error {
	viper.Set(fmt.Sprintf("services.%s.chart_version", serviceName), chartVersion)
	return Save()
}

func SetTheme(name string) error {
	viper.Set("config.theme", name)
	return Save()
}

// ParseServiceYAML parses a single-service YAML block ("name:\n  field: value")
// and returns the service name and its configuration. Errors if the format is
// invalid or required fields are missing.
func ParseServiceYAML(raw string) (string, ServiceConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ServiceConfig{}, fmt.Errorf("input vuoto")
	}

	var data map[string]ServiceConfig
	if err := yaml.Unmarshal([]byte(raw), &data); err != nil {
		return "", ServiceConfig{}, fmt.Errorf("YAML non valido: %w", err)
	}

	if len(data) == 0 {
		return "", ServiceConfig{}, fmt.Errorf("nessun servizio trovato nell'input")
	}
	if len(data) > 1 {
		return "", ServiceConfig{}, fmt.Errorf("trovati %d servizi: incolla un solo servizio per volta", len(data))
	}

	var name string
	var svc ServiceConfig
	for k, v := range data {
		name = k
		svc = v
	}

	if name == "" {
		return "", ServiceConfig{}, fmt.Errorf("nome servizio non valido")
	}
	if svc.ECRRepository == "" {
		return "", ServiceConfig{}, fmt.Errorf("campo obbligatorio mancante: ecr_repository")
	}
	if svc.HelmSetKey == "" {
		return "", ServiceConfig{}, fmt.Errorf("campo obbligatorio mancante: helm_set_key")
	}

	return name, svc, nil
}

// RemoveService removes a service from the config file, preserving structure and comments.
func RemoveService(name string) error {
	filePath := viper.ConfigFileUsed()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("errore lettura config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("errore parsing config: %w", err)
	}
	if len(doc.Content) == 0 {
		return fmt.Errorf("config vuota")
	}

	root := doc.Content[0]
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "services" {
			svcMap := root.Content[i+1]
			for j := 0; j < len(svcMap.Content)-1; j += 2 {
				if svcMap.Content[j].Value == name {
					svcMap.Content = append(svcMap.Content[:j], svcMap.Content[j+2:]...)
					out, err := yaml.Marshal(&doc)
					if err != nil {
						return fmt.Errorf("errore serializzazione: %w", err)
					}
					if err := os.WriteFile(filePath, out, 0644); err != nil {
						return fmt.Errorf("errore scrittura config: %w", err)
					}
					return viper.ReadInConfig()
				}
			}
			return fmt.Errorf("servizio %q non trovato", name)
		}
	}
	return fmt.Errorf("sezione services non trovata")
}

func GetDockerRootPath() string {
	return viper.GetString("config.docker_root_path")
}

func GetHelmRootPath() string {
	return viper.GetString("config.helm_root_path")
}
