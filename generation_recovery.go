package swarm

import (
	"reflect"

	"github.com/cloudwego/eino/schema"
)

// These copies remain in memory for one generation. Preserve concrete numeric
// and metadata types instead of round-tripping prompts through JSON. An opaque
// or cyclic value makes recovery unavailable without changing the model call.
func cloneReasoningRecoveryInput(messages []*schema.Message) ([]*schema.Message, string) {
	if len(messages) == 0 {
		return nil, "empty_input"
	}
	copy, unavailable := cloneRecoveryValue(reflect.ValueOf(messages), map[recoveryCopyVisit]bool{})
	if unavailable != "" {
		return nil, unavailable
	}
	return copy.Interface().([]*schema.Message), ""
}

type recoveryCopyVisit struct {
	typeID reflect.Type
	ptr    uintptr
}

func cloneRecoveryValue(value reflect.Value, active map[recoveryCopyVisit]bool) (reflect.Value, string) {
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value, ""
		}
		copy, unavailable := cloneRecoveryValue(value.Elem(), active)
		if unavailable != "" {
			return reflect.Value{}, unavailable
		}
		out := reflect.New(value.Type()).Elem()
		out.Set(copy)
		return out, ""
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return value, ""
		}
		visit := recoveryCopyVisit{value.Type(), value.Pointer()}
		if active[visit] {
			return reflect.Value{}, "cyclic_value"
		}
		active[visit] = true
		defer delete(active, visit)
		var out reflect.Value
		switch value.Kind() {
		case reflect.Pointer:
			copy, unavailable := cloneRecoveryValue(value.Elem(), active)
			if unavailable != "" {
				return reflect.Value{}, unavailable
			}
			out = reflect.New(value.Type().Elem())
			out.Elem().Set(copy)
		case reflect.Map:
			// Mutable pointer keys cannot retain both identity and independent
			// ownership. They are not model JSON object keys.
			keyKind := value.Type().Key().Kind()
			if keyKind != reflect.String && (keyKind < reflect.Int || keyKind > reflect.Uintptr) {
				return reflect.Value{}, "opaque_value"
			}
			out = reflect.MakeMapWithSize(value.Type(), value.Len())
			for _, key := range value.MapKeys() {
				copy, unavailable := cloneRecoveryValue(value.MapIndex(key), active)
				if unavailable != "" {
					return reflect.Value{}, unavailable
				}
				out.SetMapIndex(key, copy)
			}
		case reflect.Slice:
			out = reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			for i := range value.Len() {
				copy, unavailable := cloneRecoveryValue(value.Index(i), active)
				if unavailable != "" {
					return reflect.Value{}, unavailable
				}
				out.Index(i).Set(copy)
			}
		}
		return out, ""
	case reflect.Struct, reflect.Array:
		out := reflect.New(value.Type()).Elem()
		count := value.Len
		read := value.Index
		write := out.Index
		if value.Kind() == reflect.Struct {
			count, read, write = value.NumField, value.Field, out.Field
		}
		for i := range count() {
			if !write(i).CanSet() || !read(i).CanInterface() {
				return reflect.Value{}, "opaque_value"
			}
			copy, unavailable := cloneRecoveryValue(read(i), active)
			if unavailable != "" {
				return reflect.Value{}, unavailable
			}
			write(i).Set(copy)
		}
		return out, ""
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return value, ""
	default:
		return reflect.Value{}, "opaque_value"
	}
}
