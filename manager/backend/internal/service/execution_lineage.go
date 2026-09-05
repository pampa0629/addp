package service

import (
	"fmt"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
)

const (
	managerLineageInputPort  = "source"
	managerLineageOutputPort = "result"
)

func managerItemLineageRef(locator, fingerprint string, itemID uint) commonExecution.LineageResourceRef {
	if itemID == 0 {
		return managerItemLineageRefWithID(locator, fingerprint, nil)
	}
	return managerItemLineageRefWithID(locator, fingerprint, &itemID)
}

func managerChildResourceLocator(parentLocator, name string) string {
	parent, err := resourcetree.ParseURI(strings.TrimSpace(parentLocator))
	if err != nil || parent == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	parent.Path = append(parent.Path, strings.Trim(strings.TrimSpace(name), "/"))
	switch parent.Type {
	case resourcetree.TypeDirectory, resourcetree.TypeDir, resourcetree.TypeRoot:
		parent.Type = resourcetree.TypeFile
	default:
		parent.Type = resourcetree.TypeObject
	}
	parent.NodeID = nil
	parent.ItemID = nil
	return parent.ToURI()
}

func managerItemLineageRefWithID(locator, fingerprint string, itemID *uint) commonExecution.LineageResourceRef {
	ref := commonExecution.LineageResourceRef{
		Port:            managerLineageInputPort,
		Locator:         strings.TrimSpace(locator),
		ItemFingerprint: strings.TrimSpace(fingerprint),
		ItemID:          itemID,
	}
	return ref
}

func managerResourceLineageRef(port, locator string) commonExecution.LineageResourceRef {
	return commonExecution.LineageResourceRef{
		Port:    strings.TrimSpace(port),
		Locator: strings.TrimSpace(locator),
	}
}

func managerInfraObjectLineageRef(storageRef, defaultBucket string) (commonExecution.LineageResourceRef, error) {
	return managerInfraLineageRef(storageRef, defaultBucket, "object")
}

func managerInfraLineageRef(storageRef, defaultBucket, resourceType string) (commonExecution.LineageResourceRef, error) {
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, defaultBucket)
	if err != nil {
		return commonExecution.LineageResourceRef{}, err
	}
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		resourceType = "object"
	}
	return managerResourceLineageRef(
		managerLineageOutputPort,
		fmt.Sprintf("addp-infra://minio/%s/%s?type=%s", strings.Trim(bucket, "/"), strings.Trim(objectName, "/"), resourceType),
	), nil
}

func managerExecutionLineage(
	metadata commonModels.JSONMap,
	operator string,
	inputs []commonExecution.LineageResourceRef,
	outputs []commonExecution.LineageResourceRef,
	metaScanRefs ...string,
) commonModels.JSONMap {
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	inputPorts := lineagePorts(inputs)
	outputPorts := lineagePorts(outputs)
	if inputs == nil {
		inputs = []commonExecution.LineageResourceRef{}
	}
	if outputs == nil {
		outputs = []commonExecution.LineageResourceRef{}
	}
	facts := commonExecution.LineageFacts{
		SchemaVersion: commonExecution.LineageFactsSchemaVersion,
		Inputs:        inputs,
		Outputs:       outputs,
		Operations: []commonExecution.LineageOperation{{
			Kind:        managerLineageOperationKind(operator),
			Operator:    strings.TrimSpace(operator),
			InputPorts:  inputPorts,
			OutputPorts: outputPorts,
		}},
		MetaScanRefs: compactStrings(metaScanRefs),
	}
	metadata["lineage_facts"] = facts
	return metadata
}

func managerLineageOperationKind(operator string) string {
	switch strings.TrimSpace(operator) {
	case commonExecution.TaskTypeDataProfiling:
		return "profile"
	case commonExecution.TaskTypeEmbedding:
		return "embed"
	default:
		return "derive"
	}
}

func managerEmbeddingExecutionLineage(metadata, executionConfig commonModels.JSONMap) commonModels.JSONMap {
	target, ok := asJSONMap(executionConfig["target"])
	if !ok {
		return metadata
	}
	input := managerItemLineageRef(
		stringFromConfig(target["locator"]),
		stringFromConfig(target["item_fingerprint"]),
		uintFromConfig(target["item_id"]),
	)
	if input.Locator == "" && input.ItemID == nil && input.ItemFingerprint == "" {
		return metadata
	}
	return managerExecutionLineage(metadata, commonExecution.TaskTypeEmbedding,
		[]commonExecution.LineageResourceRef{input}, nil,
	)
}

func lineagePorts(refs []commonExecution.LineageResourceRef) []string {
	ports := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		port := strings.TrimSpace(ref.Port)
		if port == "" {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	return ports
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
