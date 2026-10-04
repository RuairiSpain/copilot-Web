package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$`)

// IsGUID reports whether value looks like a GUID (an Entra object ID).
func IsGUID(value string) bool { return guid.MatchString(value) }

// UnmarshalJSON accepts a plain string (group display name or object ID) or an object.
func (p *Principal) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*p = Principal{Type: "group"}
		if IsGUID(s) {
			p.ID = s
		} else {
			p.Name = s
		}
		return nil
	}
	type plain Principal
	return json.Unmarshal(b, (*plain)(p))
}

// Key identifies a principal for de-duplication.
func (p Principal) Key() string {
	if p.ID != "" {
		return strings.ToLower(p.ID)
	}
	return strings.ToLower(p.Name)
}

// Decode converts a schema-valid x-foundry mapping into typed configuration: it fills
// defaults from `default` struct tags and records which keys the author set.
func Decode(raw map[string]any) (*XFoundry, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var cfg XFoundry
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode x-foundry: %w", err)
	}
	if err := fill(reflect.ValueOf(&cfg).Elem(), raw); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var trackedType = reflect.TypeOf(Tracked{})

func jsonKey(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("json"), ",")[0]
}

func fill(v reflect.Value, raw any) error {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return fill(v.Elem(), raw)
	case reflect.Slice:
		items, _ := raw.([]any)
		for i := 0; i < v.Len() && i < len(items); i++ {
			if err := fill(v.Index(i), items[i]); err != nil {
				return err
			}
		}
	case reflect.Struct:
		return fillStruct(v, raw)
	}
	return nil
}

func fillStruct(v reflect.Value, raw any) error {
	obj, _ := raw.(map[string]any)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fv := v.Field(i)
		if f.Anonymous && f.Type == trackedType {
			set := make(map[string]bool, len(obj))
			for k := range obj {
				set[k] = true
			}
			fv.FieldByName("Set").Set(reflect.ValueOf(set))
			continue
		}
		key := jsonKey(f)
		if key == "" || key == "-" {
			continue
		}
		sub, present := obj[key]
		switch {
		case present:
			if err := fill(fv, sub); err != nil {
				return err
			}
		case f.Tag.Get("default") != "":
			if err := setDefault(fv, f.Tag.Get("default")); err != nil {
				return fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
			}
		case fv.Kind() == reflect.Struct:
			if err := fill(fv, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func setDefault(v reflect.Value, def string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(def)
	case reflect.Bool:
		b, err := strconv.ParseBool(def)
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int:
		n, err := strconv.Atoi(def)
		if err != nil {
			return err
		}
		v.SetInt(int64(n))
	case reflect.Float64:
		n, err := strconv.ParseFloat(def, 64)
		if err != nil {
			return err
		}
		v.SetFloat(n)
	case reflect.Slice:
		parts := strings.Split(def, ",")
		out := reflect.MakeSlice(v.Type(), len(parts), len(parts))
		for i, p := range parts {
			if err := setDefault(out.Index(i), p); err != nil {
				return err
			}
		}
		v.Set(out)
	default:
		return fmt.Errorf("unsupported default kind %s", v.Kind())
	}
	return nil
}

// Clone returns a deep copy of v, including the explicit-key sets.
func Clone[T any](v T) T {
	return deepCopy(reflect.ValueOf(&v).Elem()).Interface().(T) //nolint:forcetypeassert
}

func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(deepCopy(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if out.Field(i).CanSet() {
				out.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopy(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			out.SetMapIndex(k, deepCopy(v.MapIndex(k)))
		}
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopy(v.Elem()))
		return out
	}
	return v
}

// New returns a T with every `default` tag applied, as if decoded from an empty object.
func New[T any]() *T {
	v := new(T)
	_ = fill(reflect.ValueOf(v).Elem(), nil)
	return v
}
