package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	sigsyaml "sigs.k8s.io/yaml"
)

// ManifestMetadata represents extracted metadata from a Kubernetes manifest.
type ManifestMetadata struct {
	Kind      string `json:"kind,omitempty"`
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ManifestsSummary maps profile names to their manifest metadata lists.
type ManifestsSummary map[string][]ManifestMetadata

func main() {
	klog.InitFlags(nil)
	manifestsFile := flag.String("manifests-file", "capi-operator-manifests/default/manifests.yaml", "Path to input manifests YAML file")
	manifestsSummaryFile := flag.String("manifests-summary-file", "manifests-summary.yaml", "Path to output summary YAML file")
	profileName := flag.String("profile", "default", "Profile name for grouping manifests")

	flag.Parse()

	if err := run(*manifestsFile, *manifestsSummaryFile, *profileName); err != nil {
		klog.Fatal(err, "Failed to generate manifests summary")
	}
}

// parseDocument parses a single YAML document and extracts metadata.
func parseDocument(obj *metav1.PartialObjectMetadata, docIndex int) (*ManifestMetadata, error) {
	// Validate required fields
	if obj.Kind == "" {
		return nil, fmt.Errorf("manifest at document %d has empty kind", docIndex)
	}

	if obj.Name == "" {
		return nil, fmt.Errorf("manifest at document %d has empty metadata.name", docIndex)
	}

	// Parse APIVersion
	group, version := parseAPIVersion(obj.APIVersion)

	return &ManifestMetadata{
		Kind:      obj.Kind,
		Group:     group,
		Version:   version,
		Name:      obj.Name,
		Namespace: obj.Namespace,
	}, nil
}

// parseManifests parses all documents using a YAML decoder and returns a slice of ManifestMetadata.
func parseManifests(r io.Reader) ([]ManifestMetadata, error) {
	var manifests []ManifestMetadata
	decoder := yaml.NewYAMLOrJSONDecoder(r, 4096)
	docIndex := 0

	for {
		var obj metav1.PartialObjectMetadata
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode document %d: %w", docIndex+1, err)
		}

		docIndex++

		metadata, err := parseDocument(&obj, docIndex)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, *metadata)
	}

	return manifests, nil
}

func run(inputPath, outputPath, profileName string) error {
	klog.V(1).InfoS("Opening input file", "path", inputPath)
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("failed to open input file %q: %w", inputPath, err)
	}
	defer file.Close()

	klog.V(1).InfoS("Parsing manifests from input file")
	manifests, err := parseManifests(file)
	if err != nil {
		return err
	}
	klog.V(1).InfoS("Parsed manifests", "count", len(manifests))

	// Read existing summary
	existingSummary, err := readExistingSummary(outputPath)
	if err != nil {
		return fmt.Errorf("failed reading existing summary: %w", err)
	}

	// Update the specified profile
	summary := updateSummary(existingSummary, manifests, profileName)

	klog.V(1).InfoS("Writing output file", "path", outputPath)
	if err := writeSummary(summary, outputPath); err != nil {
		return err
	}

	klog.InfoS("Successfully generated manifests summary", "output", outputPath, "manifestCount", len(manifests), "profile", profileName)

	return nil
}

// readExistingSummary reads the existing manifests-summary.yaml file.
func readExistingSummary(outputPath string) (ManifestsSummary, error) {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}

	var summary ManifestsSummary
	if err := sigsyaml.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("failed to unmarshal existing summary: %w", err)
	}

	return summary, nil
}

// updateSummary updates the summary with new manifests for the given profile.
func updateSummary(existingSummary ManifestsSummary, manifests []ManifestMetadata, profileName string) ManifestsSummary {
	// Sort manifests by kind, group, version, namespace, name
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Kind != manifests[j].Kind {
			return manifests[i].Kind < manifests[j].Kind
		}
		if manifests[i].Group != manifests[j].Group {
			return manifests[i].Group < manifests[j].Group
		}
		if manifests[i].Version != manifests[j].Version {
			return manifests[i].Version < manifests[j].Version
		}
		if manifests[i].Namespace != manifests[j].Namespace {
			return manifests[i].Namespace < manifests[j].Namespace
		}
		return manifests[i].Name < manifests[j].Name
	})

	// Update the profile
	existingSummary[profileName] = manifests
	return existingSummary
}

// writeSummary writes the summary to the output file.
func writeSummary(summary ManifestsSummary, outputPath string) error {
	data, err := sigsyaml.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write output file %q: %w", outputPath, err)
	}

	return nil
}

// parseAPIVersion splits an apiVersion string into group and version components.
func parseAPIVersion(apiVersion string) (group, version string) {
	if apiVersion == "" {
		return "", ""
	}

	// Check if apiVersion contains a group (has '/')
	if strings.Contains(apiVersion, "/") {
		parts := strings.SplitN(apiVersion, "/", 2)
		return parts[0], parts[1]
	}

	// Core API resources have no group (e.g., "v1")
	return "", apiVersion
}
