package develop

import (
	"encoding/json"
	"fmt"
	"strings"

	"src.solsynth.dev/solsynth/distribution/internal/service"
)

// PayloadEnvelopeKeys are the keys every board payload field carries.
const (
	payloadValueKey  = "value"
	payloadLabelKey  = "label"
	payloadFormatKey = "format"
)

// BoardPayloadValidation is the outcome of validating a board widget payload.
type BoardPayloadValidation struct {
	Valid             bool
	Message           string
	NormalizedPayload map[string]any
	Widget            BoardWidgetManifest
}

// ValidateBoardPayload enforces the universal board payload contract: every
// field is an object carrying a value, a non-empty label, and an optional
// format. Field types are then checked against the widget manifest.
//
// It mirrors BoardPayloadContract + ValidateBoardWidgetPayload in the identity
// service, which Passport relies on through gRPC.
func (s *Service) ValidateBoardPayload(app *CustomApp, widgetKey string, payload map[string]any) BoardPayloadValidation {
	widget, err := s.LoadWidget(app.ID, widgetKey)
	if err != nil {
		return BoardPayloadValidation{Message: "Board widget is not configured for this app."}
	}
	manifest := widget.Manifest()
	if !app.OauthConfig.HasScope("accounts.profile.board") {
		return BoardPayloadValidation{
			Message: fmt.Sprintf("Custom app must declare the '%s' scope to provide board widgets.", "accounts.profile.board"),
			Widget:  manifest,
		}
	}
	if !widget.IsEnabled {
		return BoardPayloadValidation{Message: "Board widget is disabled for this app.", Widget: manifest}
	}
	if !strings.EqualFold(widget.PayloadType, "object") {
		return BoardPayloadValidation{Message: "Board widget payload_type must be 'object'.", Widget: manifest}
	}

	normalized := payload
	if normalized == nil {
		normalized = map[string]any{}
	}
	// A placement may leave the payload empty; the app fills it later. Only
	// supplied content is validated.
	if len(normalized) == 0 {
		return BoardPayloadValidation{Valid: true, NormalizedPayload: normalized, Widget: manifest}
	}

	if widget.MaxPayloadBytes != nil {
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return BoardPayloadValidation{Message: "Board widget payload is not serializable.", NormalizedPayload: normalized, Widget: manifest}
		}
		if len(encoded) > *widget.MaxPayloadBytes {
			return BoardPayloadValidation{
				Message:           fmt.Sprintf("Board widget payload exceeds %d bytes.", *widget.MaxPayloadBytes),
				NormalizedPayload: normalized,
				Widget:            manifest,
			}
		}
	}

	for _, required := range widget.RequiredFields {
		if _, ok := normalized[required]; !ok {
			return BoardPayloadValidation{
				Message:           fmt.Sprintf("Board widget payload is missing required field '%s'.", required),
				NormalizedPayload: normalized,
				Widget:            manifest,
			}
		}
	}

	envelope, err := NormalizeBoardPayload(normalized)
	if err != nil {
		return BoardPayloadValidation{Message: err.Error(), NormalizedPayload: normalized, Widget: manifest}
	}

	for _, fieldType := range widget.FieldTypes {
		field, ok := envelope[fieldType.Name]
		if !ok {
			continue
		}
		value, ok := ExtractBoardFieldValue(field)
		if !ok {
			return BoardPayloadValidation{
				Message:           fmt.Sprintf("Board widget payload field '%s' must be an object with 'value', 'label', and optional 'format'.", fieldType.Name),
				NormalizedPayload: envelope,
				Widget:            manifest,
			}
		}
		if !valueMatchesType(value, fieldType.Type) {
			return BoardPayloadValidation{
				Message:           fmt.Sprintf("Board widget payload field '%s' must be of type '%s'.", fieldType.Name, fieldType.Type),
				NormalizedPayload: envelope,
				Widget:            manifest,
			}
		}
	}

	return BoardPayloadValidation{Valid: true, NormalizedPayload: envelope, Widget: manifest}
}

// NormalizeBoardPayload validates and canonicalizes every field envelope.
func NormalizeBoardPayload(payload map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(payload))
	for key, raw := range payload {
		value, label, format, ok := readEnvelope(raw)
		if !ok {
			return nil, fmt.Errorf("payload field '%s' must be an object with 'value', 'label', and optional 'format'", key)
		}
		if strings.TrimSpace(label) == "" {
			return nil, fmt.Errorf("payload field '%s' must include a non-empty 'label'", key)
		}
		envelope := map[string]any{payloadValueKey: value, payloadLabelKey: label}
		if strings.TrimSpace(format) != "" {
			envelope[payloadFormatKey] = format
		}
		result[key] = envelope
	}
	return result, nil
}

// ExtractBoardFieldValue returns the value of a field already in envelope form.
func ExtractBoardFieldValue(field any) (any, bool) {
	value, _, _, ok := readEnvelope(field)
	return value, ok
}

// readEnvelope pulls the value, label and format out of one payload field.
func readEnvelope(field any) (value any, label, format string, ok bool) {
	switch typed := field.(type) {
	case map[string]any:
		return envelopeFromMap(typed)
	case json.RawMessage:
		var decoded map[string]any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return nil, "", "", false
		}
		return envelopeFromMap(decoded)
	default:
		return nil, "", "", false
	}
}

func envelopeFromMap(field map[string]any) (any, string, string, bool) {
	value, present := field[payloadValueKey]
	if !present {
		return nil, "", "", false
	}
	label, isString := field[payloadLabelKey].(string)
	if !isString {
		return nil, "", "", false
	}
	format, _ := field[payloadFormatKey].(string)
	return normalizePayloadValue(value), label, format, true
}

// normalizePayloadValue flattens JSON numbers and nested containers so a value
// keeps its shape across the JSON and protobuf boundaries.
func normalizePayloadValue(value any) any {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if float, err := typed.Float64(); err == nil {
			return float
		}
		return typed.String()
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = normalizePayloadValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, nested := range typed {
			result[index] = normalizePayloadValue(nested)
		}
		return result
	default:
		return value
	}
}

// valueMatchesType checks one payload value against a declared field type. An
// unknown type accepts anything, matching the identity service.
func valueMatchesType(value any, expected string) bool {
	if value == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(expected)) {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "null":
		return false
	default:
		return true
	}
}

// BoardPayloadError renders a validation failure as a service error.
func BoardPayloadError(message string) error {
	return fmt.Errorf("%w: %s", service.ErrValidation, message)
}
