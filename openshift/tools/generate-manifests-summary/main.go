package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	sigsyaml "sigs.k8s.io/yaml"
)

// ManifestMetadata represents extracted metadata from a Kubernetes manifest.
type ManifestMetadata struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
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

// parseManifests parses all documents using a YAML decoder and returns a slice of ManifestMetadata.
func parseManifests(r io.Reader) ([]ManifestMetadata, error) {
	var manifests []ManifestMetadata
	decoder := yaml.NewYAMLOrJSONDecoder(r, 4096)

	for i := 0; ; i++ {
		var obj metav1.PartialObjectMetadata
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode manifest[%d]: %w", i, err)
		}
		if obj.Kind == "" {
			return nil, fmt.Errorf("manifest[%d] has empty kind", i)
		}
		if obj.Name == "" {
			return nil, fmt.Errorf("manifest[%d] has empty metadata.name", i)
		}
		manifests = append(manifests, ManifestMetadata{
			Kind:       obj.Kind,
			APIVersion: obj.APIVersion,
			Name:       obj.Name,
			Namespace:  obj.Namespace,
		})
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
		return fmt.Errorf("failed to parse input file %q: %w", inputPath, err)
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
	// Sort manifests by kind, apiVersion, namespace, name
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Kind != manifests[j].Kind {
			return manifests[i].Kind < manifests[j].Kind
		}
		if manifests[i].APIVersion != manifests[j].APIVersion {
			return manifests[i].APIVersion < manifests[j].APIVersion
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
