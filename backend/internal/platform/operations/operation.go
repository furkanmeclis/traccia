package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

type Validator[I any] func(I) []FieldError
type Handler[I, O any] func(context.Context, Scope, I) (O, error)
type SchemaRefiner func(input, output *jsonschema.Schema) error

// Operation is immutable after construction and can be shared by transports.
type Operation struct {
	contract Contract
	run      func(context.Context, Scope, json.RawMessage) (json.RawMessage, error)
}

// New infers concrete input/output schemas from struct DTOs. refine may add
// constraints such as minLength; domain validators still own semantic rules.
// A nil handler declares an unavailable operation. No endpoint is registered.
func New[I, O any](meta Metadata, validate Validator[I], handler Handler[I, O], refine SchemaRefiner) (*Operation, error) {
	if reflect.TypeFor[I]().Kind() != reflect.Struct || reflect.TypeFor[O]().Kind() != reflect.Struct {
		return nil, errors.New("operations: input and output DTOs must be structs")
	}
	if validate == nil {
		return nil, errors.New("operations: typed input validator required")
	}
	in, err := jsonschema.For[I](nil)
	if err != nil {
		return nil, fmt.Errorf("operations: infer input: %w", err)
	}
	out, err := jsonschema.For[O](nil)
	if err != nil {
		return nil, fmt.Errorf("operations: infer output: %w", err)
	}
	if refine != nil {
		if err := refine(in, out); err != nil {
			return nil, fmt.Errorf("operations: refine schema: %w", err)
		}
	}
	if !closedObject(in) || !closedObject(out) {
		return nil, errors.New("operations: concrete closed object schemas required")
	}
	inSchema, inJSON, err := resolveSchema(in)
	if err != nil {
		return nil, fmt.Errorf("operations: input schema: %w", err)
	}
	outSchema, outJSON, err := resolveSchema(out)
	if err != nil {
		return nil, fmt.Errorf("operations: output schema: %w", err)
	}
	errSchema, err := jsonschema.For[Error](nil)
	if err != nil {
		return nil, fmt.Errorf("operations: infer error: %w", err)
	}
	errSchema.Properties["code"].Enum = []any{string(Unauthenticated), string(Forbidden), string(Validation), string(NotFound), string(Conflict), string(Unavailable), string(Canceled), string(Internal)}
	_, errJSON, err := resolveSchema(errSchema)
	if err != nil {
		return nil, fmt.Errorf("operations: error schema: %w", err)
	}
	op := &Operation{contract: Contract{Metadata: meta, Input: inJSON, Output: outJSON, Error: errJSON, Available: handler != nil}}
	if handler == nil {
		return op, nil
	}
	op.run = func(ctx context.Context, scope Scope, raw json.RawMessage) (json.RawMessage, error) {
		value, err := decodeJSON(raw)
		if err != nil {
			return nil, failure(Validation, err)
		}
		if err := inSchema.Validate(value); err != nil {
			return nil, failure(Validation, err)
		}
		var input I
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		dec.UseNumber()
		if err := dec.Decode(&input); err != nil {
			return nil, failure(Validation, err)
		}
		if details := validate(input); len(details) > 0 {
			return nil, &Error{Code: Validation, Details: details}
		}
		output, err := handler(ctx, scope, input)
		if err != nil {
			return nil, publicError(err)
		}
		encoded, err := json.Marshal(output)
		if err != nil {
			return nil, failure(Internal, err)
		}
		value, err = decodeJSON(encoded)
		if err != nil {
			return nil, failure(Internal, err)
		}
		if err := outSchema.Validate(value); err != nil {
			return nil, failure(Internal, err)
		}
		return encoded, nil
	}
	return op, nil
}

func closedObject(schema *jsonschema.Schema) bool {
	// Require the inference library's closed-object declaration. This is a
	// catalog policy, not an implementation of JSON Schema validation.
	return schema.Type == "object" && reflect.DeepEqual(schema.AdditionalProperties, &jsonschema.Schema{Not: &jsonschema.Schema{}})
}

func resolveSchema(schema *jsonschema.Schema) (*validator.Schema, json.RawMessage, error) {
	// Refiners may retain pointers. Resolve an independent clone so later mutation
	// cannot change a running operation or its published contract.
	schema = schema.CloneSchemas()
	schema.Schema = "https://json-schema.org/draft/2020-12/schema"
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, nil, err
	}
	doc, err := decodeJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	compiler := validator.NewCompiler()
	compiler.UseLoader(offlineLoader{})
	if err := compiler.AddResource("https://traccia.invalid/operation.schema.json", doc); err != nil {
		return nil, nil, err
	}
	resolved, err := compiler.Compile("https://traccia.invalid/operation.schema.json")
	return resolved, raw, err
}

type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, errors.New("operations: external schema resources are disabled")
}

func decodeJSON(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("operations: JSON must contain valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // Preserve integers larger than 2^53 during schema validation.
	value, err := readJSONValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("operations: expected one JSON value")
	}
	return value, nil
}

// readJSONValue builds the schema-validation value using the standard JSON
// tokenizer. Duplicate names must fail before typed decoding: encoding/json
// merges repeated struct objects while map decoding discards the earlier object,
// which would otherwise let the handler see a value the schema never validated.
func readJSONValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isContainer := token.(json.Delim)
	if !isContainer {
		return token, nil
	}
	switch delim {
	case '{':
		value := make(map[string]any)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("operations: expected object key")
			}
			if _, exists := value[key]; exists {
				return nil, errors.New("operations: duplicate object key")
			}
			child, err := readJSONValue(dec)
			if err != nil {
				return nil, err
			}
			value[key] = child
		}
		if end, err := dec.Token(); err != nil || end != json.Delim('}') {
			return nil, errors.New("operations: invalid JSON object")
		}
		return value, nil
	case '[':
		value := make([]any, 0)
		for dec.More() {
			child, err := readJSONValue(dec)
			if err != nil {
				return nil, err
			}
			value = append(value, child)
		}
		if end, err := dec.Token(); err != nil || end != json.Delim(']') {
			return nil, errors.New("operations: invalid JSON array")
		}
		return value, nil
	default:
		return nil, errors.New("operations: unexpected closing delimiter")
	}
}
