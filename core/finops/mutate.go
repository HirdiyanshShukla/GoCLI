package finops

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ApplyMutation parses the manifest at manifestPath into a yaml.v3 node
// tree, locates the named container's resources block, and sets or adds the
// specified field. No shell execution - this never touches sed or any
// text-based substitution. Returns an error if the container or expected
// structure is not found, rather than silently doing nothing.
func ApplyMutation(manifestPath string, containerName, fieldPath, newValue string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("could not read manifest: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("could not parse manifest YAML: %w", err)
	}

	containerNode, err := findContainerNode(&root, containerName)
	if err != nil {
		return err
	}

	resourcesNode := findOrCreateMapKey(containerNode, "resources")
	parts := splitFieldPath(fieldPath) // e.g. "requests.cpu" -> ["requests", "cpu"]
	if len(parts) != 2 {
		return fmt.Errorf("unexpected field_path format: %s", fieldPath)
	}
	tierNode := findOrCreateMapKey(resourcesNode, parts[0])
	setMapValue(tierNode, parts[1], newValue)

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("could not re-marshal manifest: %w", err)
	}
	return os.WriteFile(manifestPath, out, 0644)
}

// splitFieldPath splits a dotted field path into its two components.
func splitFieldPath(fieldPath string) []string {
	return strings.SplitN(fieldPath, ".", 2)
}

// findContainerNode walks the yaml.Node tree and returns the MappingNode for
// the named container inside any Deployment spec found in the document.
func findContainerNode(root *yaml.Node, containerName string) (*yaml.Node, error) {
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("empty YAML document")
	}
	// Single-document: root is DocumentNode, Content[0] is the mapping.
	docNode := root.Content[0]
	if docNode.Kind == yaml.MappingNode {
		if node, err := findContainerInDoc(docNode, containerName); err == nil {
			return node, nil
		}
	}
	// Multi-document: iterate DocumentNodes.
	for _, child := range root.Content {
		if child.Kind == yaml.DocumentNode && len(child.Content) > 0 {
			if node, err := findContainerInDoc(child.Content[0], containerName); err == nil {
				return node, nil
			}
		}
	}
	return nil, fmt.Errorf("container %q not found in manifest", containerName)
}

// findContainerInDoc searches a single YAML document for the named container.
func findContainerInDoc(doc *yaml.Node, containerName string) (*yaml.Node, error) {
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("not a mapping node")
	}
	kind := mappingGet(doc, "kind")
	if kind == nil || kind.Value != "Deployment" {
		return nil, fmt.Errorf("not a Deployment")
	}
	spec := mappingGet(doc, "spec")
	if spec == nil {
		return nil, fmt.Errorf("no spec")
	}
	template := mappingGet(spec, "template")
	if template == nil {
		return nil, fmt.Errorf("no template")
	}
	podSpec := mappingGet(template, "spec")
	if podSpec == nil {
		return nil, fmt.Errorf("no pod spec")
	}
	for _, key := range []string{"containers", "initContainers"} {
		list := mappingGet(podSpec, key)
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range list.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			nameNode := mappingGet(item, "name")
			if nameNode != nil && nameNode.Value == containerName {
				return item, nil
			}
		}
	}
	return nil, fmt.Errorf("container %q not found", containerName)
}

// mappingGet returns the value node for key inside a MappingNode, or nil.
func mappingGet(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// findOrCreateMapKey returns the value node for key inside a MappingNode,
// creating a new empty MappingNode child if the key does not exist.
func findOrCreateMapKey(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content, keyNode, valNode)
	return valNode
}

// setMapValue sets or adds a scalar string value for key inside a MappingNode.
func setMapValue(node *yaml.Node, key, value string) {
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = value
			node.Content[i+1].Tag = "!!str"
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	node.Content = append(node.Content, keyNode, valNode)
}
