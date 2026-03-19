package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
)

type stringEnum interface {
	~string
}

type objectUnionField struct {
	name  string
	value any
}

func parseStringEnum[T stringEnum](raw string, allowed map[string]T) (T, bool) {
	value, ok := allowed[raw]
	return value, ok
}

func isValidStringEnum[T stringEnum](value T, allowed map[string]T) bool {
	_, ok := allowed[string(value)]
	return ok
}

func marshalStringOrSingleFieldObjectUnion[T stringEnum](
	kind T,
	objectFields ...objectUnionField,
) ([]byte, error) {
	selected := 0
	if kind != "" {
		selected++
	}

	var objectField *objectUnionField
	for i := range objectFields {
		if isNilValue(objectFields[i].value) {
			continue
		}
		selected++
		field := objectFields[i]
		objectField = &field
	}

	if selected != 1 {
		return nil, fmt.Errorf("expected exactly one enum variant, got %d", selected)
	}

	if kind != "" {
		return json.Marshal(kind)
	}

	return json.Marshal(map[string]any{
		objectField.name: objectField.value,
	})
}

func unmarshalStringOrSingleFieldObjectUnion[T stringEnum](
	data []byte,
	allowed map[string]T,
	objectHandlers map[string]func(json.RawMessage) error,
) (T, error) {
	var zero T

	var rawString string
	if err := json.Unmarshal(data, &rawString); err == nil {
		value, ok := parseStringEnum(rawString, allowed)
		if !ok {
			return zero, fmt.Errorf("unknown enum value %q", rawString)
		}
		return value, nil
	}

	var rawObject map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawObject); err != nil {
		return zero, err
	}
	if len(rawObject) != 1 {
		return zero, fmt.Errorf("expected single-field enum object, got %d fields", len(rawObject))
	}

	for key, rawValue := range rawObject {
		handler, ok := objectHandlers[key]
		if !ok {
			return zero, fmt.Errorf("unknown enum object variant %q", key)
		}
		if err := handler(rawValue); err != nil {
			return zero, err
		}
		return zero, nil
	}

	return zero, fmt.Errorf("expected enum value")
}

func marshalStringOrInt64Union(stringValue *string, integerValue *int64) ([]byte, error) {
	selected := 0
	if stringValue != nil {
		selected++
	}
	if integerValue != nil {
		selected++
	}
	if selected != 1 {
		return nil, fmt.Errorf("expected exactly one scalar union variant, got %d", selected)
	}
	if stringValue != nil {
		return json.Marshal(*stringValue)
	}
	return json.Marshal(*integerValue)
}

func unmarshalStringOrInt64Union(data []byte) (*string, *int64, error) {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		return &stringValue, nil, nil
	}

	var integerValue int64
	if err := json.Unmarshal(data, &integerValue); err == nil {
		return nil, &integerValue, nil
	}

	return nil, nil, fmt.Errorf("expected string or int64 union")
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
