// SPDX-License-Identifier: LGPL-3.0-only
// Derived from pinned Sub2API; see UPSTREAM.md.
package googlewire

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func cleanToolSchema(schema any) any {
	return cleanToolSchemaNode(schema, true)
}

func cleanToolSchemaNode(schema any, schemaNode bool) any {
	if schema == nil {
		return nil
	}

	switch v := schema.(type) {
	case map[string]any:
		if !schemaNode {
			cleaned := make(map[string]any, len(v))
			for key, value := range v {
				cleaned[key] = cleanToolSchemaNode(value, false)
			}
			return cleaned
		}

		cleaned := make(map[string]any)
		for key, value := range v {
			// 跳过不支持的字段
			if key == "$schema" || key == "$id" || key == "$ref" ||
				key == "$defs" || key == "definitions" ||
				key == "additionalProperties" || key == "patternProperties" || key == "minLength" ||
				key == "maxLength" || key == "minItems" || key == "maxItems" || key == "exclusiveMinimum" ||
				key == "deprecated" || key == "examples" {
				continue
			}

			switch key {
			case "properties", "dependentSchemas":
				if propertySchemas, ok := value.(map[string]any); ok {
					cleanedProperties := make(map[string]any, len(propertySchemas))
					for propertyName, propertySchema := range propertySchemas {
						cleanedProperties[propertyName] = cleanToolSchemaNode(propertySchema, true)
					}
					cleaned[key] = cleanedProperties
				} else {
					cleaned[key] = cleanToolSchemaNode(value, false)
				}
			case "items", "contains", "propertyNames", "if", "then", "else", "not", "contentSchema",
				"additionalItems", "unevaluatedItems", "unevaluatedProperties":
				cleaned[key] = cleanToolSchemaNode(value, true)
			case "dependencies":
				if dependencies, ok := value.(map[string]any); ok {
					cleanedDependencies := make(map[string]any, len(dependencies))
					for propertyName, dependency := range dependencies {
						if dependencySchema, ok := dependency.(map[string]any); ok {
							cleanedDependencies[propertyName] = cleanToolSchemaNode(dependencySchema, true)
						} else {
							cleanedDependencies[propertyName] = cleanToolSchemaNode(dependency, false)
						}
					}
					cleaned[key] = cleanedDependencies
				} else {
					cleaned[key] = cleanToolSchemaNode(value, false)
				}
			case "anyOf", "allOf", "oneOf", "prefixItems":
				if schemas, ok := value.([]any); ok {
					cleanedSchemas := make([]any, len(schemas))
					for i, childSchema := range schemas {
						cleanedSchemas[i] = cleanToolSchemaNode(childSchema, true)
					}
					cleaned[key] = cleanedSchemas
				} else {
					cleaned[key] = cleanToolSchemaNode(value, false)
				}
			default:
				cleaned[key] = cleanToolSchemaNode(value, false)
			}
		}
		if constValue, exists := v["const"]; exists {
			if stringValue, ok := constValue.(string); ok {
				if originalEnum, hasEnum := v["enum"].([]any); hasEnum {
					intersection := make([]any, 0, 1)
					for _, value := range originalEnum {
						if enumString, ok := value.(string); ok && enumString == stringValue {
							intersection = append(intersection, stringValue)
							break
						}
					}
					cleaned["enum"] = intersection
				} else {
					cleaned["enum"] = []any{stringValue}
				}
				if _, hasType := cleaned["type"]; !hasType {
					cleaned["type"] = "STRING"
				}
			}
			// Non-string constants are unsupported: omit the constant, retain the
			// original type, and do not stringify the constant or infer a type.
			delete(cleaned, "const")
		}
		if enum, ok := cleaned["enum"].([]any); ok {
			if normalized, ok := normalizeGeminiEnum(enum); ok {
				cleaned["enum"] = normalized
			} else {
				delete(cleaned, "enum")
			}
		}
		// 规范化 type 字段为大写
		if typeVal, ok := cleaned["type"].(string); ok {
			cleaned["type"] = strings.ToUpper(typeVal)
		} else if typeValues, ok := cleaned["type"].([]any); ok {
			for _, typeValue := range typeValues {
				typeName, ok := typeValue.(string)
				if ok && !strings.EqualFold(typeName, "null") {
					cleaned["type"] = strings.ToUpper(typeName)
					break
				}
			}
			if _, ok := cleaned["type"].([]any); ok {
				delete(cleaned, "type")
			}
		}
		if cleaned["type"] == "INTEGER" {
			if minimum, ok := incrementIntegralSchemaBound(v["exclusiveMinimum"]); ok {
				if existing, exists := cleaned["minimum"]; !exists || schemaNumberLess(existing, minimum) {
					cleaned["minimum"] = minimum
				}
			}
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(v))
		for i, item := range v {
			cleaned[i] = cleanToolSchemaNode(item, schemaNode)
		}
		return cleaned
	default:
		return v
	}
}

func normalizeGeminiEnum(values []any) ([]any, bool) {
	normalized := make([]any, len(values))
	for i, value := range values {
		if stringValue, ok := value.(string); ok {
			normalized[i] = stringValue
			continue
		}

		switch value.(type) {
		case nil, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, false
			}
			normalized[i] = string(encoded)
		default:
			return nil, false
		}
	}
	return normalized, true
}

func incrementIntegralSchemaBound(value any) (any, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v+1 <= v {
			return nil, false
		}
		return v + 1, true
	case int:
		if v == math.MaxInt {
			return nil, false
		}
		return v + 1, true
	case int64:
		if v == math.MaxInt64 {
			return nil, false
		}
		return v + 1, true
	case json.Number:
		i, err := v.Int64()
		if err != nil || i == math.MaxInt64 {
			return nil, false
		}
		return json.Number(fmt.Sprintf("%d", i+1)), true
	default:
		return nil, false
	}
}

func schemaNumberLess(left, right any) bool {
	leftNumber, leftOK := schemaNumberFloat64(left)
	rightNumber, rightOK := schemaNumberFloat64(right)
	return leftOK && rightOK && leftNumber < rightNumber
}

func schemaNumberFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}
