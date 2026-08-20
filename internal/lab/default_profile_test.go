package lab

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func defaultProfileDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "profiles", "default")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDefaultLabDNSBootstrapEnablesOperatorConsole(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(defaultProfileDir(t), "labdns", "bootstrap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			UI struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"ui"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.APIVersion != "labdns.dev/v1alpha1" || doc.Kind != "LabDNS" {
		t.Fatalf("apiVersion=%q kind=%q", doc.APIVersion, doc.Kind)
	}
	if !doc.Spec.UI.Enabled {
		t.Fatal("profiles/default/labdns/bootstrap.yaml: spec.ui.enabled must be true (LabDNS 1.1.0 operator console)")
	}
}
