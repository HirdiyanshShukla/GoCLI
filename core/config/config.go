package config

type PipelineConfig struct {
	Version  string         `yaml:"version"`
	App      AppConfig      `yaml:"app"`
	Policies PoliciesConfig `yaml:"policies"`
}

type AppConfig struct {
	Name          string      `yaml:"name"`
	Port          int         `yaml:"port"`
	PythonVersion string      `yaml:"python_version"`
	NodeVersion   string      `yaml:"node_version"`
	JavaVersion   string      `yaml:"java_version"`
	HealthPath    string      `yaml:"health_path"`
	TestCommand   string      `yaml:"test_command"`
	FinOpsRegion  string      `yaml:"finops_region"`
	Env           []EnvVar    `yaml:"env"`
	Secrets       []SecretRef `yaml:"secrets"`
}

type EnvVar struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type SecretRef struct {
	Name       string `yaml:"name"`
	SecretName string `yaml:"secret_name"`
	SecretKey  string `yaml:"secret_key"`
}

type PoliciesConfig struct {
	Mode     string                            `yaml:"mode"`
	Enabled  []string                          `yaml:"enabled"`  // used when mode: opt-in
	Disabled []string                          `yaml:"disabled"` // used when mode: opt-out
	Config   map[string]map[string]interface{} `yaml:"config"`
}
